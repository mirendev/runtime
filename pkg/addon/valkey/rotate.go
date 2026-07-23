package valkey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// valkeyCommand is the single source of truth for the valkey launch command, so
// provisioning and rotation can never drift on how the password is passed.
func valkeyCommand(password string) string {
	return fmt.Sprintf("valkey-server --save 60 1 --requirepass %s", password)
}

var _ addon.CredentialRotator = (*Provider)(nil)

// rotateCapture carries the RotationResult out of the saga (analogous to
// resultCapture for provisioning).
type rotateCapture struct {
	Result *addon.RotationResult
}

// --- Dedicated Rotation Saga Actions ---
//
// Valkey stores its password as the requirepass argument baked into the pool's
// launch command, so rotating it means re-launching the pool. The data disk is
// single-attach, so old and new pods can't overlap: we scale the old pool to
// zero (releasing the disk), stand up a new pool with the new password on the
// same disk, then delete the old pool. Every mutating step compensates.

type DecodeValkeyServerRefIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type DecodeValkeyServerRefOut struct {
	RotateServerID entity.Id `saga:"rotateserverid"`
}

func DecodeValkeyServerRef(ctx context.Context, in DecodeValkeyServerRefIn) (DecodeValkeyServerRefOut, error) {
	var data addon_v1alpha.ValkeyDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}
	if data.ValkeyServer == "" {
		return DecodeValkeyServerRefOut{}, fmt.Errorf("no valkey server ref found on association")
	}
	return DecodeValkeyServerRefOut{RotateServerID: data.ValkeyServer}, nil
}

func UndoDecodeValkeyServerRef(ctx context.Context, in DecodeValkeyServerRefIn, out DecodeValkeyServerRefOut) error {
	return nil
}

type LoadValkeyRotationStateIn struct {
	RotateServerID entity.Id `saga:"rotateserverid"`
}

type LoadValkeyRotationStateOut struct {
	RotateOldPassword string    `saga:"rotateoldpassword"`
	RotateOldPoolID   entity.Id `saga:"rotateoldpoolid"`
	RotateServiceHost string    `saga:"rotateservicehost"`
}

func LoadValkeyRotationState(ctx context.Context, in LoadValkeyRotationStateIn) (LoadValkeyRotationStateOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.ValkeyServer
	if err := fw.EC.GetById(ctx, in.RotateServerID, &server); err != nil {
		return LoadValkeyRotationStateOut{}, fmt.Errorf("looking up valkey server: %w", err)
	}
	if server.SandboxPool == "" {
		return LoadValkeyRotationStateOut{}, fmt.Errorf("valkey server %s has no sandbox pool", in.RotateServerID)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LoadValkeyRotationStateOut{}, fmt.Errorf("resolving valkey service address: %w", err)
	}

	return LoadValkeyRotationStateOut{
		RotateOldPassword: server.Password,
		RotateOldPoolID:   server.SandboxPool,
		RotateServiceHost: serviceHost,
	}, nil
}

func UndoLoadValkeyRotationState(ctx context.Context, in LoadValkeyRotationStateIn, out LoadValkeyRotationStateOut) error {
	return nil
}

type SetValkeyServerPasswordIn struct {
	RotateServerID    entity.Id `saga:"rotateserverid"`
	RotateNewPassword string    `saga:"rotatenewpassword"`
	RotateOldPassword string    `saga:"rotateoldpassword"`
}

type SetValkeyServerPasswordOut struct {
	RotatePasswordSet bool
}

func SetValkeyServerPassword(ctx context.Context, in SetValkeyServerPasswordIn) (SetValkeyServerPasswordOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.RotateServerID, 0,
		entity.String(addon_v1alpha.ValkeyServerPasswordId, in.RotateNewPassword),
	); err != nil {
		return SetValkeyServerPasswordOut{}, fmt.Errorf("updating valkey server password: %w", err)
	}
	return SetValkeyServerPasswordOut{RotatePasswordSet: true}, nil
}

func UndoSetValkeyServerPassword(ctx context.Context, in SetValkeyServerPasswordIn, out SetValkeyServerPasswordOut) error {
	if !out.RotatePasswordSet {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.RotateServerID, 0,
		entity.String(addon_v1alpha.ValkeyServerPasswordId, in.RotateOldPassword),
	)
}

type ScaleDownOldValkeyPoolIn struct {
	RotateOldPoolID entity.Id `saga:"rotateoldpoolid"`
}

type ScaleDownOldValkeyPoolOut struct {
	// Edge producer: gates the pool swap, so the single-attach disk is released
	// before the new pool tries to attach it.
	RotatePoolScaledDown bool `saga:"valkey_pool_scaled_down"`
}

func ScaleDownOldValkeyPool(ctx context.Context, in ScaleDownOldValkeyPoolIn) (ScaleDownOldValkeyPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.ScalePool(ctx, in.RotateOldPoolID, 0); err != nil {
		return ScaleDownOldValkeyPoolOut{}, fmt.Errorf("scaling down old valkey pool: %w", err)
	}
	return ScaleDownOldValkeyPoolOut{RotatePoolScaledDown: true}, nil
}

func UndoScaleDownOldValkeyPool(ctx context.Context, in ScaleDownOldValkeyPoolIn, out ScaleDownOldValkeyPoolOut) error {
	if !out.RotatePoolScaledDown {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.ScalePool(ctx, in.RotateOldPoolID, 1)
}

type SwapValkeyPoolIn struct {
	RotateServerID    entity.Id `saga:"rotateserverid"`
	RotateOldPoolID   entity.Id `saga:"rotateoldpoolid"`
	RotateNewPassword string    `saga:"rotatenewpassword"`

	PoolScaledDown saga.Edge `saga:"valkey_pool_scaled_down"`
}

type SwapValkeyPoolOut struct {
	RotateNewPoolID entity.Id `saga:"rotatenewpoolid"`
}

// rebuildPoolSpec reconstructs a pool spec from the existing pool, changing only
// the launch command. DesiredInstances is forced to 1 because the old pool was
// just scaled to zero (dedicated valkey always runs a single instance).
func rebuildPoolSpec(pool *compute_v1alpha.SandboxPool, command string) addon.CreateSandboxPoolSpec {
	spec := addon.CreateSandboxPoolSpec{
		DesiredInstances: 1,
		Labels:           pool.SandboxLabels,
		SandboxPrefix:    pool.SandboxPrefix,
		PortWaitTimeout:  pool.SandboxSpec.PortWaitTimeout,
		Command:          command,
		Volumes:          pool.SandboxSpec.Volume,
	}
	if len(pool.SandboxSpec.Container) > 0 {
		c := pool.SandboxSpec.Container[0]
		spec.Image = c.Image
		spec.Env = c.Env
		spec.Ports = c.Port
		spec.Mounts = c.Mount
	}
	return spec
}

// rotationPoolName derives the new pool's name deterministically from the server
// and the pool being replaced. Because it is stable across retries, a re-run of
// the swap rediscovers the pool a prior (possibly crashed) attempt created
// instead of leaking it and standing up a duplicate. It is intentionally keyed
// on non-secret ids, not the new password: the password lives in the pool's
// launch command, and a rotation toward a different secret is told apart by
// comparing that command when deciding whether to adopt.
func rotationPoolName(serverID, oldPoolID entity.Id) string {
	sum := sha256.Sum256([]byte(string(serverID) + "\x00" + string(oldPoolID)))
	return "valkey-rot-" + hex.EncodeToString(sum[:8])
}

// poolLaunchCommand returns a pool's container launch command, or "" if it has
// no container.
func poolLaunchCommand(pool *compute_v1alpha.SandboxPool) string {
	if len(pool.SandboxSpec.Container) == 0 {
		return ""
	}
	return pool.SandboxSpec.Container[0].Command
}

// createRotationPool stands up the new pool at the deterministic id, cloning the
// old pool's spec but launching with the rotated password's command. The create
// and the preceding lookup are not atomic, so a concurrent attempt could win the
// create; because the id is deterministic per (server, old pool) and a rotation
// reruns with the same secret, the winner targets the same command, so a
// conflict is treated as a successful adoption (verified defensively).
func createRotationPool(ctx context.Context, fw *addon.ProviderFramework, oldPool *compute_v1alpha.SandboxPool, poolID entity.Id, name, command string) error {
	spec := rebuildPoolSpec(oldPool, command)
	spec.Name = name
	switch _, err := fw.CreateSandboxPool(ctx, spec); {
	case err == nil:
		return nil
	case errors.Is(err, cond.ErrConflict{}):
		var raced compute_v1alpha.SandboxPool
		if gerr := fw.EC.GetById(ctx, poolID, &raced); gerr != nil {
			return fmt.Errorf("re-reading rotation pool after create conflict: %w", gerr)
		}
		if poolLaunchCommand(&raced) != command {
			return fmt.Errorf("rotation pool %s already exists with a different launch command", poolID)
		}
		fw.Log.Info("adopting concurrently-created valkey pool", "pool", poolID)
		return nil
	default:
		return fmt.Errorf("creating new valkey pool: %w", err)
	}
}

func SwapValkeyPool(ctx context.Context, in SwapValkeyPoolIn) (SwapValkeyPoolOut, error) {
	return swapValkeyPool(ctx, saga.Get[*addon.ProviderFramework](ctx), in)
}

// swapValkeyPool holds the swap logic with the framework passed explicitly, so it
// can be driven directly in tests without a full saga executor.
func swapValkeyPool(ctx context.Context, fw *addon.ProviderFramework, in SwapValkeyPoolIn) (SwapValkeyPoolOut, error) {
	var oldPool compute_v1alpha.SandboxPool
	if err := fw.EC.GetById(ctx, in.RotateOldPoolID, &oldPool); err != nil {
		return SwapValkeyPoolOut{}, fmt.Errorf("reading old valkey pool: %w", err)
	}

	// The new pool has a deterministic id, so a swap that reruns (e.g. a crash
	// landed between the create and the repoint) can rediscover it instead of
	// leaking it and creating a duplicate.
	poolName := rotationPoolName(in.RotateServerID, in.RotateOldPoolID)
	newPoolID := entity.Id("pool/" + poolName)
	target := valkeyCommand(in.RotateNewPassword)

	var existing compute_v1alpha.SandboxPool
	switch err := fw.EC.GetById(ctx, newPoolID, &existing); {
	case err == nil && poolLaunchCommand(&existing) == target:
		// A prior attempt for this same rotation already stood the pool up; adopt
		// it rather than leaking it and creating a duplicate.
		fw.Log.Info("adopting new valkey pool from a prior rotation attempt", "pool", newPoolID)
	case err == nil:
		// A pool sits at this slot but launches with a different password: an
		// orphan from an earlier rotation that crashed and was abandoned. Tear it
		// down and rebuild the slot for the current secret.
		fw.Log.Info("replacing stale valkey rotation pool", "pool", newPoolID)
		if derr := fw.DeleteSandboxPool(ctx, newPoolID); derr != nil {
			return SwapValkeyPoolOut{}, fmt.Errorf("removing stale rotation pool: %w", derr)
		}
		if err := createRotationPool(ctx, fw, &oldPool, newPoolID, poolName, target); err != nil {
			return SwapValkeyPoolOut{}, err
		}
	case errors.Is(err, cond.ErrNotFound{}):
		if err := createRotationPool(ctx, fw, &oldPool, newPoolID, poolName, target); err != nil {
			return SwapValkeyPoolOut{}, err
		}
	default:
		return SwapValkeyPoolOut{}, fmt.Errorf("checking for existing rotation pool: %w", err)
	}

	// Point the server at the new pool. Consumers reach valkey through the
	// service (unchanged, still selecting by the same labels), so the address
	// stays stable across the swap.
	if err := fw.EC.Patch(ctx, in.RotateServerID, 0,
		entity.Ref(addon_v1alpha.ValkeyServerSandboxPoolId, newPoolID),
	); err != nil {
		// This action's own Undo won't run (the framework only compensates
		// succeeded actions), and the failed action returns no pool id — so the
		// new pool would leak and fight the rescaled old one over the
		// single-attach disk. Tear it down here before failing.
		if delErr := fw.DeleteSandboxPool(ctx, newPoolID); delErr != nil {
			fw.Log.Warn("failed to clean up new valkey pool after repoint failure; leaked",
				"pool", newPoolID, "error", delErr)
		}
		return SwapValkeyPoolOut{}, fmt.Errorf("repointing valkey server to new pool: %w", err)
	}

	return SwapValkeyPoolOut{RotateNewPoolID: newPoolID}, nil
}

func UndoSwapValkeyPool(ctx context.Context, in SwapValkeyPoolIn, out SwapValkeyPoolOut) error {
	if out.RotateNewPoolID == "" {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	// Repoint the server back at the old pool (still present, scaled to zero and
	// restored by the scale-down undo) and tear down the new one.
	if err := fw.EC.Patch(ctx, in.RotateServerID, 0,
		entity.Ref(addon_v1alpha.ValkeyServerSandboxPoolId, in.RotateOldPoolID),
	); err != nil {
		return fmt.Errorf("restoring valkey server pool ref: %w", err)
	}
	return fw.DeleteSandboxPool(ctx, out.RotateNewPoolID)
}

type WaitValkeyPoolReadyIn struct {
	RotateNewPoolID entity.Id `saga:"rotatenewpoolid"`
}

type WaitValkeyPoolReadyOut struct {
	// Edge producer: gates the old-pool delete on the new pool being healthy.
	RotatePoolReady bool `saga:"valkey_pool_ready"`
}

func WaitValkeyPoolReady(ctx context.Context, in WaitValkeyPoolReadyIn) (WaitValkeyPoolReadyOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.WaitForPool(ctx, in.RotateNewPoolID, poolReadyTimeout); err != nil {
		return WaitValkeyPoolReadyOut{}, fmt.Errorf("waiting for new valkey pool: %w", err)
	}
	return WaitValkeyPoolReadyOut{RotatePoolReady: true}, nil
}

func UndoWaitValkeyPoolReady(ctx context.Context, in WaitValkeyPoolReadyIn, out WaitValkeyPoolReadyOut) error {
	return nil
}

type DeleteOldValkeyPoolIn struct {
	RotateOldPoolID entity.Id `saga:"rotateoldpoolid"`

	PoolReady saga.Edge `saga:"valkey_pool_ready"`
}

type DeleteOldValkeyPoolOut struct {
	RotateOldPoolDeleted bool
}

func DeleteOldValkeyPool(ctx context.Context, in DeleteOldValkeyPoolIn) (DeleteOldValkeyPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	// Best-effort: the rotation has already succeeded by the time we get here, so
	// a failure to remove the drained old pool must not roll it back. Log and
	// move on; a leftover zero-scaled pool is harmless and reapable later.
	if err := fw.DeleteSandboxPool(ctx, in.RotateOldPoolID); err != nil {
		fw.Log.Warn("failed to delete old valkey pool after rotation; leaving it drained",
			"pool", in.RotateOldPoolID, "error", err)
		return DeleteOldValkeyPoolOut{RotateOldPoolDeleted: false}, nil
	}
	return DeleteOldValkeyPoolOut{RotateOldPoolDeleted: true}, nil
}

func UndoDeleteOldValkeyPool(ctx context.Context, in DeleteOldValkeyPoolIn, out DeleteOldValkeyPoolOut) error {
	return nil
}

type CaptureValkeyRotationResultIn struct {
	RotateServiceHost string `saga:"rotateservicehost"`
	RotateNewPassword string `saga:"rotatenewpassword"`
}

type CaptureValkeyRotationResultOut struct {
	Done bool
}

func CaptureValkeyRotationResult(ctx context.Context, in CaptureValkeyRotationResultIn) (CaptureValkeyRotationResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{
		EnvVars: buildEnvVars(in.RotateServiceHost, valkeyPort, in.RotateNewPassword),
	}
	return CaptureValkeyRotationResultOut{Done: true}, nil
}

func UndoCaptureValkeyRotationResult(ctx context.Context, in CaptureValkeyRotationResultIn, out CaptureValkeyRotationResultOut) error {
	return nil
}

// RegisterRotateDedicatedSaga registers the dedicated valkey rotation saga.
func RegisterRotateDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-dedicated-valkey").
		Using(fw).
		Using(rc).
		Action(DecodeValkeyServerRef).Undo(UndoDecodeValkeyServerRef).
		Action(LoadValkeyRotationState).Undo(UndoLoadValkeyRotationState).
		Action(SetValkeyServerPassword).Undo(UndoSetValkeyServerPassword).
		Action(ScaleDownOldValkeyPool).Undo(UndoScaleDownOldValkeyPool).
		Action(SwapValkeyPool).Undo(UndoSwapValkeyPool).
		Action(WaitValkeyPoolReady).Undo(UndoWaitValkeyPoolReady).
		Action(DeleteOldValkeyPool).Undo(UndoDeleteOldValkeyPool).
		Action(CaptureValkeyRotationResult).Undo(UndoCaptureValkeyRotationResult).
		RegisterTo(registry)
}

// RotateCredential implements addon.CredentialRotator for dedicated Valkey.
// Valkey has a single password (its requirepass, also the value consumers use),
// so the credential selector is ignored. It is safe to re-invoke with the same
// newSecret: if the running pool already uses it, the pool re-launch is skipped.
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated Valkey credential", "assoc", assoc.ID)

	// Idempotency: if a prior attempt already re-launched the pool on newSecret,
	// don't churn it again — just return the vars a redeploy needs. This keeps a
	// retry (e.g. after a failed consumer rollout) from spinning up fresh pools.
	if result, done, err := p.valkeyAlreadyRotated(ctx, assoc, newSecret); err != nil {
		return nil, err
	} else if done {
		p.Log.Info("dedicated Valkey already on target secret; skipping pool re-launch", "assoc", assoc.ID)
		return result, nil
	}

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-valkey").
		Input("assocentity", assoc.Entity).
		Input("rotatenewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}

	if rc.Result == nil {
		return nil, fmt.Errorf("rotation saga completed but no result was captured")
	}

	p.Log.Info("dedicated Valkey credential rotated", "assoc", assoc.ID)
	return rc.Result, nil
}

// valkeyAlreadyRotated reports whether the server's running pool already launches
// with newSecret, and if so returns the env vars a consumer redeploy needs.
func (p *Provider) valkeyAlreadyRotated(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, bool, error) {
	var data addon_v1alpha.ValkeyDedicatedData
	if assoc.Entity != nil {
		data.Decode(assoc.Entity)
	}
	if data.ValkeyServer == "" {
		return nil, false, nil
	}

	var server addon_v1alpha.ValkeyServer
	if err := p.Fw.EC.GetById(ctx, data.ValkeyServer, &server); err != nil {
		return nil, false, fmt.Errorf("looking up valkey server: %w", err)
	}
	if server.SandboxPool == "" {
		return nil, false, nil
	}

	var pool compute_v1alpha.SandboxPool
	if err := p.Fw.EC.GetById(ctx, server.SandboxPool, &pool); err != nil {
		return nil, false, fmt.Errorf("looking up valkey pool: %w", err)
	}
	target := valkeyCommand(newSecret)
	rotated := false
	for _, c := range pool.SandboxSpec.Container {
		if c.Command == target {
			rotated = true
			break
		}
	}
	if !rotated {
		return nil, false, nil
	}

	serviceHost, err := p.Fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return nil, false, fmt.Errorf("resolving valkey service address: %w", err)
	}
	return &addon.RotationResult{EnvVars: buildEnvVars(serviceHost, valkeyPort, newSecret)}, true, nil
}
