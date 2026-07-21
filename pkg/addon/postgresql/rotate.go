package postgresql

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

var _ addon.CredentialRotator = (*Provider)(nil)

// rotateCapture carries the RotationResult out of a rotation saga.
type rotateCapture struct {
	Result *addon.RotationResult
}

// RotateCredential implements addon.CredentialRotator for PostgreSQL. Shared
// servers expose two independently-rotatable credentials:
//
//   - "" / "user"  — the association's per-app database user. Rotated live with
//     ALTER USER; the new password flows to the consuming app, which is
//     redeployed. (Class A: no restart, no disk coupling.)
//   - "superuser"  — the shared server's admin password. Rotated live with
//     ALTER ROLE and recorded on the server entity. Apps never receive it, so
//     no consumer is redeployed. (Class C: safe now that the data disk name is
//     decoupled from this password.)
//
// Dedicated PostgreSQL rotation is not implemented yet.
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential string) (*addon.RotationResult, error) {
	if !IsSharedVariant(assoc.Variant) {
		return nil, fmt.Errorf("credential rotation is not yet supported for dedicated PostgreSQL")
	}

	switch credential {
	case "", "user":
		return p.rotateSharedUser(ctx, assoc)
	case "superuser":
		return p.rotateSharedSuperuser(ctx, assoc)
	default:
		return nil, fmt.Errorf("unknown postgresql credential %q (valid: \"user\", \"superuser\")", credential)
	}
}

// oldVarValue reads the current value of an addon variable recorded on the
// association, used to restore a per-app password on compensation.
func oldVarValue(assocEntity *entity.Entity, key string) string {
	if assocEntity == nil {
		return ""
	}
	var a addon_v1alpha.AddonAssociation
	a.Decode(assocEntity)
	for _, v := range a.Variables {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
}

// --- Per-app user rotation (Class A) ---

type CaptureOldUserPasswordIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type CaptureOldUserPasswordOut struct {
	UserOldPassword string `saga:"useroldpassword"`
}

func CaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn) (CaptureOldUserPasswordOut, error) {
	return CaptureOldUserPasswordOut{UserOldPassword: oldVarValue(in.AssocEntity, "PGPASSWORD")}, nil
}

func UndoCaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn, out CaptureOldUserPasswordOut) error {
	return nil
}

type GenerateNewUserPasswordIn struct{}

type GenerateNewUserPasswordOut struct {
	UserNewPassword string `saga:"usernewpassword"`
}

func GenerateNewUserPassword(ctx context.Context, in GenerateNewUserPasswordIn) (GenerateNewUserPasswordOut, error) {
	return GenerateNewUserPasswordOut{UserNewPassword: idgen.Gen("pw")}, nil
}

func UndoGenerateNewUserPassword(ctx context.Context, in GenerateNewUserPasswordIn, out GenerateNewUserPasswordOut) error {
	return nil
}

type AlterSharedUserPasswordIn struct {
	SharedServiceHost       string `saga:"sharedservicehost"`
	SharedSuperuserPassword string `saga:"sharedsuperuserpassword"`
	SharedUserName          string `saga:"sharedusername"`
	UserNewPassword         string `saga:"usernewpassword"`
	UserOldPassword         string `saga:"useroldpassword"`
}

type AlterSharedUserPasswordOut struct {
	UserAltered bool
}

func AlterSharedUserPassword(ctx context.Context, in AlterSharedUserPasswordIn) (AlterSharedUserPasswordOut, error) {
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return AlterSharedUserPasswordOut{}, fmt.Errorf("connecting to rotate user password: %w", err)
	}
	defer conn.Close(ctx)

	if err := alterPostgresUserPassword(ctx, conn, in.SharedUserName, in.UserNewPassword); err != nil {
		return AlterSharedUserPasswordOut{}, err
	}
	return AlterSharedUserPasswordOut{UserAltered: true}, nil
}

func UndoAlterSharedUserPassword(ctx context.Context, in AlterSharedUserPasswordIn, out AlterSharedUserPasswordOut) error {
	if !out.UserAltered || in.UserOldPassword == "" {
		return nil
	}
	// The superuser password is unchanged in a per-app rotation, so we can still
	// connect and restore the old per-app password.
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore user password: %w", err)
	}
	defer conn.Close(ctx)
	return alterPostgresUserPassword(ctx, conn, in.SharedUserName, in.UserOldPassword)
}

type BuildUserRotationResultIn struct {
	SharedServiceHost string `saga:"sharedservicehost"`
	SharedUserName    string `saga:"sharedusername"`
	SharedDbName      string `saga:"shareddbname"`
	UserNewPassword   string `saga:"usernewpassword"`
}

type BuildUserRotationResultOut struct {
	Done bool
}

func BuildUserRotationResult(ctx context.Context, in BuildUserRotationResultIn) (BuildUserRotationResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{
		EnvVars: buildEnvVars(in.SharedServiceHost, postgresPort, in.SharedUserName, in.UserNewPassword, in.SharedDbName),
	}
	return BuildUserRotationResultOut{Done: true}, nil
}

func UndoBuildUserRotationResult(ctx context.Context, in BuildUserRotationResultIn, out BuildUserRotationResultOut) error {
	return nil
}

// RegisterRotateSharedUserSaga registers the per-app user rotation saga.
func RegisterRotateSharedUserSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	b := saga.Define("rotate-shared-postgresql-user").
		Using(fw).
		Using(rc)
	return b.
		Action(DecodeSharedAttrs).Undo(UndoDecodeSharedAttrs).
		Action(LookupSharedServer).Undo(UndoLookupSharedServer).
		Action(CaptureOldUserPassword).Undo(UndoCaptureOldUserPassword).
		Action(GenerateNewUserPassword).Undo(UndoGenerateNewUserPassword).
		Action(AlterSharedUserPassword).Undo(UndoAlterSharedUserPassword).
		Action(BuildUserRotationResult).Undo(UndoBuildUserRotationResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedUser(ctx context.Context, assoc addon.AddonAssociation) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared PostgreSQL user credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedUserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering user rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-postgresql-user").
		Input("assocentity", assoc.Entity).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("user rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}

// --- Superuser rotation (Class C) ---

type BackfillSuperuserDiskNameIn struct {
	SharedServerRef entity.Id `saga:"sharedserverref"`
}

type BackfillSuperuserDiskNameOut struct {
	// Edge producer: the disk name must be recorded before the password changes,
	// or the legacy password-derived name would be lost.
	SuperuserDiskEnsured bool `saga:"superuser_disk_ensured"`
}

func BackfillSuperuserDiskName(ctx context.Context, in BackfillSuperuserDiskNameIn) (BackfillSuperuserDiskNameOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, in.SharedServerRef, &server); err != nil {
		return BackfillSuperuserDiskNameOut{}, fmt.Errorf("reading shared server: %w", err)
	}
	if server.DiskName == "" {
		legacy := sharedDiskNameForPassword(server.SuperuserPassword)
		if err := fw.EC.Patch(ctx, in.SharedServerRef, 0,
			entity.String(addon_v1alpha.PostgresServerDiskNameId, legacy),
		); err != nil {
			return BackfillSuperuserDiskNameOut{}, fmt.Errorf("backfilling disk name before superuser rotation: %w", err)
		}
	}
	return BackfillSuperuserDiskNameOut{SuperuserDiskEnsured: true}, nil
}

func UndoBackfillSuperuserDiskName(ctx context.Context, in BackfillSuperuserDiskNameIn, out BackfillSuperuserDiskNameOut) error {
	return nil
}

type GenerateNewSuperuserPasswordIn struct{}

type GenerateNewSuperuserPasswordOut struct {
	SuperuserNewPassword string `saga:"superusernewpassword"`
}

func GenerateNewSuperuserPassword(ctx context.Context, in GenerateNewSuperuserPasswordIn) (GenerateNewSuperuserPasswordOut, error) {
	return GenerateNewSuperuserPasswordOut{SuperuserNewPassword: idgen.Gen("su")}, nil
}

func UndoGenerateNewSuperuserPassword(ctx context.Context, in GenerateNewSuperuserPasswordIn, out GenerateNewSuperuserPasswordOut) error {
	return nil
}

type AlterSuperuserPasswordIn struct {
	SharedServiceHost       string `saga:"sharedservicehost"`
	SharedSuperuserPassword string `saga:"sharedsuperuserpassword"`
	SuperuserNewPassword    string `saga:"superusernewpassword"`

	DiskEnsured saga.Edge `saga:"superuser_disk_ensured"`
}

type AlterSuperuserPasswordOut struct {
	// Edge producer: gate the entity update on the engine actually changing.
	SuperuserAltered bool `saga:"superuser_altered"`
}

func AlterSuperuserPassword(ctx context.Context, in AlterSuperuserPasswordIn) (AlterSuperuserPasswordOut, error) {
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return AlterSuperuserPasswordOut{}, fmt.Errorf("connecting to rotate superuser password: %w", err)
	}
	defer conn.Close(ctx)

	if err := alterPostgresUserPassword(ctx, conn, defaultPostgresUser, in.SuperuserNewPassword); err != nil {
		return AlterSuperuserPasswordOut{}, err
	}
	return AlterSuperuserPasswordOut{SuperuserAltered: true}, nil
}

func UndoAlterSuperuserPassword(ctx context.Context, in AlterSuperuserPasswordIn, out AlterSuperuserPasswordOut) error {
	if !out.SuperuserAltered {
		return nil
	}
	// The engine now holds the new password, so reconnect with it to restore the
	// old one.
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SuperuserNewPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore superuser password: %w", err)
	}
	defer conn.Close(ctx)
	return alterPostgresUserPassword(ctx, conn, defaultPostgresUser, in.SharedSuperuserPassword)
}

type UpdateSuperuserEntityIn struct {
	SharedServerRef         entity.Id `saga:"sharedserverref"`
	SuperuserNewPassword    string    `saga:"superusernewpassword"`
	SharedSuperuserPassword string    `saga:"sharedsuperuserpassword"`

	Altered saga.Edge `saga:"superuser_altered"`
}

type UpdateSuperuserEntityOut struct {
	SuperuserRecorded bool
}

func UpdateSuperuserEntity(ctx context.Context, in UpdateSuperuserEntityIn) (UpdateSuperuserEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.SharedServerRef, 0,
		entity.String(addon_v1alpha.PostgresServerSuperuserPasswordId, in.SuperuserNewPassword),
	); err != nil {
		return UpdateSuperuserEntityOut{}, fmt.Errorf("recording new superuser password: %w", err)
	}
	return UpdateSuperuserEntityOut{SuperuserRecorded: true}, nil
}

func UndoUpdateSuperuserEntity(ctx context.Context, in UpdateSuperuserEntityIn, out UpdateSuperuserEntityOut) error {
	if !out.SuperuserRecorded {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.SharedServerRef, 0,
		entity.String(addon_v1alpha.PostgresServerSuperuserPasswordId, in.SharedSuperuserPassword),
	)
}

type CaptureSuperuserResultIn struct {
	Recorded saga.Edge `saga:"superuser_altered"`
}

type CaptureSuperuserResultOut struct {
	Done bool
}

// CaptureSuperuserResult records a non-nil, empty result: rotating the superuser
// touches no consumer variables, so there is nothing to redeploy.
func CaptureSuperuserResult(ctx context.Context, in CaptureSuperuserResultIn) (CaptureSuperuserResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{}
	return CaptureSuperuserResultOut{Done: true}, nil
}

func UndoCaptureSuperuserResult(ctx context.Context, in CaptureSuperuserResultIn, out CaptureSuperuserResultOut) error {
	return nil
}

// RegisterRotateSharedSuperuserSaga registers the superuser rotation saga.
func RegisterRotateSharedSuperuserSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	b := saga.Define("rotate-shared-postgresql-superuser").
		Using(fw).
		Using(rc)
	return b.
		Action(DecodeSharedAttrs).Undo(UndoDecodeSharedAttrs).
		Action(LookupSharedServer).Undo(UndoLookupSharedServer).
		Action(BackfillSuperuserDiskName).Undo(UndoBackfillSuperuserDiskName).
		Action(GenerateNewSuperuserPassword).Undo(UndoGenerateNewSuperuserPassword).
		Action(AlterSuperuserPassword).Undo(UndoAlterSuperuserPassword).
		Action(UpdateSuperuserEntity).Undo(UndoUpdateSuperuserEntity).
		Action(CaptureSuperuserResult).Undo(UndoCaptureSuperuserResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedSuperuser(ctx context.Context, assoc addon.AddonAssociation) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared PostgreSQL superuser credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedSuperuserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering superuser rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-postgresql-superuser").
		Input("assocentity", assoc.Entity).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("superuser rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}
