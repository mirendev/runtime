package addon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
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
	case "pending", "rotating":
		// "rotating" is re-entered on retry/restart: the operation is idempotent,
		// so it resumes from wherever a previous attempt left off.
		return c.rotate(ctx, req)
	default:
		return nil
	}
}

func (c *RotationController) rotate(ctx context.Context, req *addon_v1alpha.RotationRequest) error {
	c.log.Info("rotating addon credential",
		"request", req.ID, "association", req.Association, "addon", req.Addon, "credential", req.Credential)

	// Re-read the durable state (status + any secret already claimed by a prior
	// attempt) rather than trusting a possibly-stale watch event.
	var current addon_v1alpha.RotationRequest
	if err := c.ec.GetById(ctx, req.ID, &current); err != nil {
		return fmt.Errorf("re-reading rotation request: %w", err)
	}
	if current.Status == "done" || current.Status == "error" {
		return nil
	}

	// Load the target association (raw entity so the provider can decode its
	// addon-specific data attrs, e.g. the backing server ref).
	resp, err := c.eac.Get(ctx, req.Association.String())
	if err != nil {
		// Only a genuinely missing association is terminal; transient store
		// errors should retry, like the propagation failure below.
		if errors.Is(err, cond.ErrNotFound{}) {
			return c.setError(ctx, req, fmt.Errorf("loading association %s: %w", req.Association, err))
		}
		return fmt.Errorf("loading association %s (will retry): %w", req.Association, err)
	}
	assocEntity := resp.Entity().Entity()
	var assocData addon_v1alpha.AddonAssociation
	assocData.Decode(assocEntity)

	// Resolve the provider and check it supports rotation (permanent failures).
	addonName := addon.NameFromRef(req.Addon)
	provider, _, ok := c.registry.Get(addonName)
	if !ok {
		return c.setError(ctx, req, fmt.Errorf("unknown addon %q", addonName))
	}
	rotator, ok := provider.(addon.CredentialRotator)
	if !ok {
		return c.setError(ctx, req, fmt.Errorf("rotation not supported for addon %q", addonName))
	}

	assoc := addon.AssociationFrom(&assocData, assocEntity)

	// Claim the target secret durably *before* any engine change, so a crash is
	// recoverable: a retry reuses this exact secret instead of minting a fresh one
	// it could no longer authenticate with. We use a direct Patch (not meta) so
	// the claim persists immediately, before RotateCredential runs. On retry the
	// existing claim is reused.
	newSecret := current.NewSecret
	if newSecret == "" {
		newSecret = idgen.Gen("rot")
		if err := c.ec.Patch(ctx, req.ID, 0,
			entity.String(addon_v1alpha.RotationRequestNewSecretId, newSecret),
			entity.String(addon_v1alpha.RotationRequestStatusId, "rotating"),
		); err != nil {
			return fmt.Errorf("claiming rotation secret: %w", err)
		}
	}

	// Apply to the live engine and stored value. Providers are idempotent for a
	// fixed newSecret, so this is safe to re-run after a crash. A returned error
	// means the operation compensated (no partial state committed), so it is
	// terminal — the operator can re-request.
	result, err := rotator.RotateCredential(ctx, assoc, current.Credential, newSecret)
	if err != nil {
		return c.setError(ctx, req, fmt.Errorf("rotating credential: %w", err))
	}

	// Propagate to the consuming app by minting a new version so the injected
	// connection string picks up the new secret. This runs *after* the credential
	// already changed, so a failure here must stay retryable rather than terminal:
	// returning an error re-reconciles, and re-running the idempotent rotation
	// above converges. (The authoritative running value lives in the ConfigVersion
	// this creates; association.variables are intentionally left as-is.)
	if len(result.EnvVars) > 0 {
		if err := createVersionWithVars(ctx, c.log, c.ec, c.eac, assoc.App, result.EnvVars); err != nil {
			return fmt.Errorf("redeploying consumer (will retry): %w", err)
		}
	}

	// Done — clear the recorded secret now that it lives in the app config.
	if err := c.ec.Patch(ctx, req.ID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "done"),
		entity.String(addon_v1alpha.RotationRequestNewSecretId, ""),
	); err != nil {
		return fmt.Errorf("finalizing rotation: %w", err)
	}

	c.log.Info("addon credential rotated", "request", req.ID, "association", req.Association)
	return nil
}

// setError marks the request terminally failed and clears any claimed secret.
func (c *RotationController) setError(ctx context.Context, req *addon_v1alpha.RotationRequest, err error) error {
	c.log.Error("rotation error", "request", req.ID, "error", err)
	if perr := c.ec.Patch(ctx, req.ID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "error"),
		entity.String(addon_v1alpha.RotationRequestErrorMessageId, err.Error()),
		entity.String(addon_v1alpha.RotationRequestNewSecretId, ""),
	); perr != nil {
		return fmt.Errorf("setting error status: %w (original: %w)", perr, err)
	}
	return nil
}
