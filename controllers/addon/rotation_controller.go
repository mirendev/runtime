package addon

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
)

// RotationController reconciles RotationRequest entities, driving in-place
// rotation of an addon server credential through the provider's optional
// CredentialRotator capability.
//
// Rotation is modeled as its own reactive entity rather than an association
// status because it is server-scoped: one request rotates the credential on the
// backing server, then redeploys whichever consumers actually embed the rotated
// secret. Implements controller.ReconcileControllerI[*addon_v1alpha.RotationRequest].
type RotationController struct {
	log      *slog.Logger
	ec       *entityserver.Client
	eac      *entityserver_v1alpha.EntityAccessClient
	registry *addon.Registry
}

// NewRotationController creates a new rotation controller.
func NewRotationController(
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	registry *addon.Registry,
) *RotationController {
	return &RotationController{
		log:      log.With("module", "addon-rotation"),
		ec:       ec,
		eac:      eac,
		registry: registry,
	}
}

func (c *RotationController) Init(ctx context.Context) error {
	c.log.Info("initializing addon rotation controller")
	return nil
}

func (c *RotationController) Reconcile(ctx context.Context, req *addon_v1alpha.RotationRequest, meta *entity.Meta) error {
	switch req.Status {
	case "pending":
		return c.rotate(ctx, req, meta)
	default:
		return nil
	}
}

func (c *RotationController) rotate(ctx context.Context, req *addon_v1alpha.RotationRequest, meta *entity.Meta) error {
	c.log.Info("rotating addon credential",
		"request", req.ID, "association", req.Association, "addon", req.Addon, "credential", req.Credential)

	// Guard against stale events: re-read and confirm the request is still
	// pending before acting.
	var current addon_v1alpha.RotationRequest
	if err := c.ec.GetById(ctx, req.ID, &current); err != nil {
		return fmt.Errorf("re-reading rotation request: %w", err)
	}
	if current.Status != "pending" {
		c.log.Info("rotation request no longer pending, skipping",
			"request", req.ID, "status", current.Status)
		return nil
	}

	if err := meta.Update((&addon_v1alpha.RotationRequest{Status: "rotating"}).Encode()); err != nil {
		return fmt.Errorf("setting status to rotating: %w", err)
	}

	// Load the target association (raw entity so the provider can decode its
	// addon-specific data attrs, e.g. the backing server ref).
	resp, err := c.eac.Get(ctx, req.Association.String())
	if err != nil {
		return c.setError(meta, fmt.Errorf("loading association %s: %w", req.Association, err))
	}
	assocEntity := resp.Entity().Entity()
	var assocData addon_v1alpha.AddonAssociation
	assocData.Decode(assocEntity)

	// Resolve the provider and check it supports rotation.
	addonName := addon.NameFromRef(req.Addon)
	provider, _, ok := c.registry.Get(addonName)
	if !ok {
		return c.setError(meta, fmt.Errorf("unknown addon %q", addonName))
	}
	rotator, ok := provider.(addon.CredentialRotator)
	if !ok {
		return c.setError(meta, fmt.Errorf("rotation not supported for addon %q", addonName))
	}

	assoc := addon.AddonAssociation{
		ID:      assocData.ID,
		App:     assocData.App,
		Addon:   assocData.Addon,
		Variant: assocData.Variant,
		Entity:  assocEntity,
	}

	// Apply the new secret to the live engine and update the stored value.
	result, err := rotator.RotateCredential(ctx, assoc, req.Credential)
	if err != nil {
		return c.setError(meta, fmt.Errorf("rotating credential: %w", err))
	}

	// Propagate any updated variables to the consuming app by minting a new
	// version, so the injected connection string picks up the new secret. The
	// authoritative running value lives in the ConfigVersion this creates; the
	// values recorded on the association are cosmetic (removal keys off the
	// variable name), so we deliberately don't rewrite them here — a full entity
	// replace would risk clobbering the association's addon data attrs.
	if len(result.EnvVars) > 0 {
		if err := createVersionWithVars(ctx, c.log, c.ec, c.eac, assoc.App, result.EnvVars); err != nil {
			return c.setError(meta, fmt.Errorf("redeploying consumer: %w", err))
		}
	}

	if err := meta.Update((&addon_v1alpha.RotationRequest{Status: "done"}).Encode()); err != nil {
		return fmt.Errorf("setting status to done: %w", err)
	}

	c.log.Info("addon credential rotated", "request", req.ID, "association", req.Association)
	return nil
}

func (c *RotationController) setError(meta *entity.Meta, err error) error {
	c.log.Error("rotation error", "error", err)
	updateErr := meta.Update((&addon_v1alpha.RotationRequest{
		Status:       "error",
		ErrorMessage: err.Error(),
	}).Encode())
	if updateErr != nil {
		return fmt.Errorf("setting error status: %w (original: %w)", updateErr, err)
	}
	return nil
}
