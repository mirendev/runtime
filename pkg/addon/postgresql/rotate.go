package postgresql

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
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
// Dedicated servers have a single credential: the app's own role, which is the
// instance's bootstrap superuser (POSTGRES_USER) and the value consumers embed.
// It rotates live with ALTER ROLE — the pool never relaunches, since the env
// password only seeds initdb — and the new secret both flows to the app and is
// recorded on the server entity. (Class A + entity update.)
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential, newSecret string) (*addon.RotationResult, error) {
	if !IsSharedVariant(assoc.Variant) {
		switch credential {
		case "", "user":
			return p.rotateDedicated(ctx, assoc, newSecret)
		default:
			return nil, fmt.Errorf("unknown dedicated postgresql credential %q (valid: \"user\")", credential)
		}
	}

	switch credential {
	case "", "user":
		return p.rotateSharedUser(ctx, assoc, newSecret)
	case "superuser":
		return p.rotateSharedSuperuser(ctx, assoc, newSecret)
	default:
		return nil, fmt.Errorf("unknown postgresql credential %q (valid: \"user\", \"superuser\")", credential)
	}
}

// activeConfigVar reads a variable's value from an app's active ConfigVersion.
// This is the authoritative current value — unlike association.variables, which
// is deliberately not rewritten on rotation and can hold a stale password. The
// per-app compensation uses it so a rollback restores the password the app is
// actually running with, not an older one.
func activeConfigVar(ctx context.Context, fw *addon.ProviderFramework, appID entity.Id, key string) (string, error) {
	var app core_v1alpha.App
	if err := fw.EC.GetById(ctx, appID, &app); err != nil {
		return "", fmt.Errorf("getting app %s: %w", appID, err)
	}
	if app.ActiveVersion == "" {
		return "", nil
	}
	var version core_v1alpha.AppVersion
	if err := fw.EC.GetById(ctx, app.ActiveVersion, &version); err != nil {
		return "", fmt.Errorf("getting app version: %w", err)
	}
	spec, err := coreutil.ResolveConfig(ctx, fw.EAC, &version)
	if err != nil {
		return "", fmt.Errorf("resolving config: %w", err)
	}
	for _, v := range spec.Variables {
		if v.Key == key {
			return v.Value, nil
		}
	}
	return "", nil
}

// --- Per-app user rotation (Class A) ---

type CaptureOldUserPasswordIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type CaptureOldUserPasswordOut struct {
	UserOldPassword string `saga:"useroldpassword"`
}

func CaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn) (CaptureOldUserPasswordOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var assoc addon_v1alpha.AddonAssociation
	if in.AssocEntity != nil {
		assoc.Decode(in.AssocEntity)
	}
	if assoc.App == "" {
		return CaptureOldUserPasswordOut{}, fmt.Errorf("association has no app ref")
	}

	old, err := activeConfigVar(ctx, fw, assoc.App, "PGPASSWORD")
	if err != nil {
		return CaptureOldUserPasswordOut{}, fmt.Errorf("reading current user password from active config: %w", err)
	}
	return CaptureOldUserPasswordOut{UserOldPassword: old}, nil
}

func UndoCaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn, out CaptureOldUserPasswordOut) error {
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
		Action(AlterSharedUserPassword).Undo(UndoAlterSharedUserPassword).
		Action(BuildUserRotationResult).Undo(UndoBuildUserRotationResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedUser(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared PostgreSQL user credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedUserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering user rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-postgresql-user").
		Input("assocentity", assoc.Entity).
		Input("usernewpassword", newSecret).
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
	// Try the recorded (old) password and the target; a retry after a crash may
	// find the engine already on the new one.
	conn, err := connectAsSuperuserTrying(ctx, in.SharedServiceHost, in.SharedSuperuserPassword, in.SuperuserNewPassword)
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
	// The engine may hold either password depending on where a crash landed, so
	// try both to reconnect and restore the old one.
	conn, err := connectAsSuperuserTrying(ctx, in.SharedServiceHost, in.SuperuserNewPassword, in.SharedSuperuserPassword)
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
		Action(AlterSuperuserPassword).Undo(UndoAlterSuperuserPassword).
		Action(UpdateSuperuserEntity).Undo(UndoUpdateSuperuserEntity).
		Action(CaptureSuperuserResult).Undo(UndoCaptureSuperuserResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedSuperuser(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared PostgreSQL superuser credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedSuperuserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering superuser rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-postgresql-superuser").
		Input("assocentity", assoc.Entity).
		Input("superusernewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("superuser rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}

// --- Dedicated rotation (Class A + entity update) ---
//
// A dedicated server backs exactly one app. Its single role is the instance's
// bootstrap superuser and the credential the app connects with, so rotation is
// the shared-user live ALTER plus recording the new secret on the server entity
// (which durably holds this password, unlike the shared per-app user). No pool
// relaunch: POSTGRES_PASSWORD only seeds initdb, so the running engine keeps its
// password until ALTER changes it, and existing connections stay authenticated.

type LoadDedicatedRotationStateIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

type LoadDedicatedRotationStateOut struct {
	DedicatedServiceHost string `saga:"dedicatedservicehost"`
	DedicatedOldPassword string `saga:"dedicatedoldpassword"`
}

func LoadDedicatedRotationState(ctx context.Context, in LoadDedicatedRotationStateIn) (LoadDedicatedRotationStateOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LoadDedicatedRotationStateOut{}, fmt.Errorf("looking up dedicated server: %w", err)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LoadDedicatedRotationStateOut{}, fmt.Errorf("resolving dedicated service address: %w", err)
	}

	return LoadDedicatedRotationStateOut{
		DedicatedServiceHost: serviceHost,
		DedicatedOldPassword: server.SuperuserPassword,
	}, nil
}

func UndoLoadDedicatedRotationState(ctx context.Context, in LoadDedicatedRotationStateIn, out LoadDedicatedRotationStateOut) error {
	return nil
}

type CaptureDedicatedConnInfoIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type CaptureDedicatedConnInfoOut struct {
	DedicatedUser     string `saga:"dedicateduser"`
	DedicatedDatabase string `saga:"dedicateddatabase"`
}

// CaptureDedicatedConnInfo resolves the role name and database to connect to.
// Provisioning records both on the association attrs (see BuildDedicatedResult),
// so they read straight off the entity — no dependency on a deployed app.
// Associations created before those attrs existed fall back to the app's active
// ConfigVersion, the authoritative record of what the app connects as. The
// password is never read here: the durable current password lives on the server
// entity (see LoadDedicatedRotationState), which survives even a retry that
// already redeployed the app onto the new secret.
func CaptureDedicatedConnInfo(ctx context.Context, in CaptureDedicatedConnInfoIn) (CaptureDedicatedConnInfoOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var data addon_v1alpha.PostgresqlDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}
	user, database := data.Username, data.DatabaseName

	// Legacy associations predate these attrs; recover conn info from the app's
	// active config so they stay rotatable.
	if user == "" || database == "" {
		var assoc addon_v1alpha.AddonAssociation
		if in.AssocEntity != nil {
			assoc.Decode(in.AssocEntity)
		}
		if assoc.App == "" {
			return CaptureDedicatedConnInfoOut{}, fmt.Errorf("association has no stored connection info and no app ref to fall back to")
		}
		if user == "" {
			u, err := activeConfigVar(ctx, fw, assoc.App, "PGUSER")
			if err != nil {
				return CaptureDedicatedConnInfoOut{}, fmt.Errorf("reading PGUSER from active config: %w", err)
			}
			user = u
		}
		if database == "" {
			d, err := activeConfigVar(ctx, fw, assoc.App, "PGDATABASE")
			if err != nil {
				return CaptureDedicatedConnInfoOut{}, fmt.Errorf("reading PGDATABASE from active config: %w", err)
			}
			database = d
		}
	}

	if user == "" {
		return CaptureDedicatedConnInfoOut{}, fmt.Errorf("could not determine dedicated postgres user (no stored attr, no PGUSER in active config)")
	}
	if database == "" {
		return CaptureDedicatedConnInfoOut{}, fmt.Errorf("could not determine dedicated postgres database (no stored attr, no PGDATABASE in active config)")
	}

	return CaptureDedicatedConnInfoOut{DedicatedUser: user, DedicatedDatabase: database}, nil
}

func UndoCaptureDedicatedConnInfo(ctx context.Context, in CaptureDedicatedConnInfoIn, out CaptureDedicatedConnInfoOut) error {
	return nil
}

type AlterDedicatedUserPasswordIn struct {
	DedicatedServiceHost string `saga:"dedicatedservicehost"`
	DedicatedUser        string `saga:"dedicateduser"`
	DedicatedDatabase    string `saga:"dedicateddatabase"`
	DedicatedOldPassword string `saga:"dedicatedoldpassword"`
	DedicatedNewPassword string `saga:"dedicatednewpassword"`
}

type AlterDedicatedUserPasswordOut struct {
	// Edge producer: gate the entity update on the engine actually changing.
	DedicatedUserAltered bool `saga:"dedicated_user_altered"`
}

func AlterDedicatedUserPassword(ctx context.Context, in AlterDedicatedUserPasswordIn) (AlterDedicatedUserPasswordOut, error) {
	// Try the recorded (old) password and the target; a retry after a crash may
	// find the engine already on the new one. The connecting role is the very
	// credential being rotated, so both candidates are needed to converge.
	conn, err := connectTrying(ctx, in.DedicatedServiceHost, postgresPort, in.DedicatedUser, in.DedicatedDatabase,
		in.DedicatedOldPassword, in.DedicatedNewPassword)
	if err != nil {
		return AlterDedicatedUserPasswordOut{}, fmt.Errorf("connecting to rotate dedicated password: %w", err)
	}
	defer conn.Close(ctx)

	if err := alterPostgresUserPassword(ctx, conn, in.DedicatedUser, in.DedicatedNewPassword); err != nil {
		return AlterDedicatedUserPasswordOut{}, err
	}
	return AlterDedicatedUserPasswordOut{DedicatedUserAltered: true}, nil
}

func UndoAlterDedicatedUserPassword(ctx context.Context, in AlterDedicatedUserPasswordIn, out AlterDedicatedUserPasswordOut) error {
	if !out.DedicatedUserAltered {
		return nil
	}
	// The engine may hold either password depending on where a crash landed, so
	// try both to reconnect and restore the old one.
	conn, err := connectTrying(ctx, in.DedicatedServiceHost, postgresPort, in.DedicatedUser, in.DedicatedDatabase,
		in.DedicatedNewPassword, in.DedicatedOldPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore dedicated password: %w", err)
	}
	defer conn.Close(ctx)
	return alterPostgresUserPassword(ctx, conn, in.DedicatedUser, in.DedicatedOldPassword)
}

type UpdateDedicatedEntityIn struct {
	DedicatedServerID    entity.Id `saga:"dedicatedserverid"`
	DedicatedNewPassword string    `saga:"dedicatednewpassword"`
	DedicatedOldPassword string    `saga:"dedicatedoldpassword"`

	Altered saga.Edge `saga:"dedicated_user_altered"`
}

type UpdateDedicatedEntityOut struct {
	DedicatedRecorded bool
}

func UpdateDedicatedEntity(ctx context.Context, in UpdateDedicatedEntityIn) (UpdateDedicatedEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.PostgresServerSuperuserPasswordId, in.DedicatedNewPassword),
	); err != nil {
		return UpdateDedicatedEntityOut{}, fmt.Errorf("recording new dedicated password: %w", err)
	}
	return UpdateDedicatedEntityOut{DedicatedRecorded: true}, nil
}

func UndoUpdateDedicatedEntity(ctx context.Context, in UpdateDedicatedEntityIn, out UpdateDedicatedEntityOut) error {
	if !out.DedicatedRecorded {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.PostgresServerSuperuserPasswordId, in.DedicatedOldPassword),
	)
}

type BuildDedicatedRotationResultIn struct {
	DedicatedServiceHost string `saga:"dedicatedservicehost"`
	DedicatedUser        string `saga:"dedicateduser"`
	DedicatedDatabase    string `saga:"dedicateddatabase"`
	DedicatedNewPassword string `saga:"dedicatednewpassword"`
}

type BuildDedicatedRotationResultOut struct {
	Done bool
}

func BuildDedicatedRotationResult(ctx context.Context, in BuildDedicatedRotationResultIn) (BuildDedicatedRotationResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{
		EnvVars: buildEnvVars(in.DedicatedServiceHost, postgresPort, in.DedicatedUser, in.DedicatedNewPassword, in.DedicatedDatabase),
	}
	return BuildDedicatedRotationResultOut{Done: true}, nil
}

func UndoBuildDedicatedRotationResult(ctx context.Context, in BuildDedicatedRotationResultIn, out BuildDedicatedRotationResultOut) error {
	return nil
}

// RegisterRotateDedicatedSaga registers the dedicated PostgreSQL rotation saga.
func RegisterRotateDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	b := saga.Define("rotate-dedicated-postgresql").
		Using(fw).
		Using(rc)
	return b.
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LoadDedicatedRotationState).Undo(UndoLoadDedicatedRotationState).
		Action(CaptureDedicatedConnInfo).Undo(UndoCaptureDedicatedConnInfo).
		Action(AlterDedicatedUserPassword).Undo(UndoAlterDedicatedUserPassword).
		Action(UpdateDedicatedEntity).Undo(UndoUpdateDedicatedEntity).
		Action(BuildDedicatedRotationResult).Undo(UndoBuildDedicatedRotationResult).
		RegisterTo(registry)
}

func (p *Provider) rotateDedicated(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated PostgreSQL credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-postgresql").
		Input("assocentity", assoc.Entity).
		Input("dedicatednewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("dedicated rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}
