package valkey

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

// valkeyCommand is the single source of truth for the valkey launch command, so
// provisioning and rotation can never drift on how the password is passed.
func valkeyCommand(password string) string {
	return fmt.Sprintf("valkey-server --save 60 1 --requirepass %s", password)
}

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

type GenerateNewValkeyPasswordIn struct{}

type GenerateNewValkeyPasswordOut struct {
	RotateNewPassword string `saga:"rotatenewpassword"`
}

func GenerateNewValkeyPassword(ctx context.Context, in GenerateNewValkeyPasswordIn) (GenerateNewValkeyPasswordOut, error) {
	return GenerateNewValkeyPasswordOut{RotateNewPassword: idgen.Gen("pw")}, nil
}

func UndoGenerateNewValkeyPassword(ctx context.Context, in GenerateNewValkeyPasswordIn, out GenerateNewValkeyPasswordOut) error {
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

func SwapValkeyPool(ctx context.Context, in SwapValkeyPoolIn) (SwapValkeyPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var oldPool compute_v1alpha.SandboxPool
	if err := fw.EC.GetById(ctx, in.RotateOldPoolID, &oldPool); err != nil {
		return SwapValkeyPoolOut{}, fmt.Errorf("reading old valkey pool: %w", err)
	}

	spec := rebuildPoolSpec(&oldPool, valkeyCommand(in.RotateNewPassword))
	newPoolID, err := fw.CreateSandboxPool(ctx, spec)
	if err != nil {
		return SwapValkeyPoolOut{}, fmt.Errorf("creating new valkey pool: %w", err)
	}

	// Point the server at the new pool. Consumers reach valkey through the
	// service (unchanged, still selecting by the same labels), so the address
	// stays stable across the swap.
	if err := fw.EC.Patch(ctx, in.RotateServerID, 0,
		entity.Ref(addon_v1alpha.ValkeyServerSandboxPoolId, newPoolID),
	); err != nil {
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
		Action(GenerateNewValkeyPassword).Undo(UndoGenerateNewValkeyPassword).
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
// so the credential selector is ignored.
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated Valkey credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-valkey").
		Input("assocentity", assoc.Entity).
		Execute(ctx); err != nil {
		return nil, err
	}

	if rc.Result == nil {
		return nil, fmt.Errorf("rotation saga completed but no result was captured")
	}

	p.Log.Info("dedicated Valkey credential rotated", "assoc", assoc.ID)
	return rc.Result, nil
}
