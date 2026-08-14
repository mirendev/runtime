package mysql

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

var _ addon.CredentialRotator = (*Provider)(nil)

// rotateCapture carries the RotationResult out of a rotation saga.
type rotateCapture struct {
	Result *addon.RotationResult
}

// RotateCredential implements addon.CredentialRotator for MySQL. Both variants
// expose two independently-rotatable credentials, and both connect through the
// stable root account to make the change (MySQL always keeps root as a separate
// admin, so unlike dedicated PostgreSQL we never need the app user's own password
// to authenticate the ALTER):
//
//   - "" / "user" — the association's per-app database user, the credential the
//     app connects with. Rotated with ALTER USER; the new password flows to the
//     consuming app, which is redeployed. (Class A: no restart.)
//   - "root"      — the server's admin password. Rotated with ALTER USER and
//     recorded on the server entity. Apps never receive it, so no consumer is
//     redeployed. (Class C.) For the shared server this is safe now that the data
//     disk name is decoupled from the root password.
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential, newSecret string) (*addon.RotationResult, error) {
	if IsSharedVariant(assoc.Variant) {
		switch credential {
		case "", "user":
			return p.rotateSharedUser(ctx, assoc, newSecret)
		case "root":
			return p.rotateSharedRoot(ctx, assoc, newSecret)
		default:
			return nil, fmt.Errorf("unknown mysql credential %q (valid: \"user\", \"root\")", credential)
		}
	}

	switch credential {
	case "", "user":
		return p.rotateDedicatedUser(ctx, assoc, newSecret)
	case "root":
		return p.rotateDedicatedRoot(ctx, assoc, newSecret)
	default:
		return nil, fmt.Errorf("unknown mysql credential %q (valid: \"user\", \"root\")", credential)
	}
}

// associationVar reads a variable's value from the addon association's own
// record of what it supplies.
//
// The association is the current record. The rotation controller rewrites it on
// every rotation, and coreutil.ResolveRuntimeConfig resolves the app's bindings
// from it. Reading the app's stored ConfigVersion instead finds nothing, because
// no version records addon variables any more.
//
// An association that last rotated before that change can still hold a stale
// password. ReportStaleAssociationVariables names those at boot; the repair is
// to rotate again.
func associationVar(assoc *addon_v1alpha.AddonAssociation, key string) string {
	for _, v := range assoc.Variables {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
}

// bestEffortOldUserPassword reads the app's current MYSQL_PASSWORD for use as a
// compensation target. It never fails the saga: restoring the prior user password
// on rollback is a safety net, and the forward rotation (which durably redeploys
// the app onto the new secret) is authoritative. An empty result just means the
// rollback ALTER is skipped.
func bestEffortOldUserPassword(assoc *addon_v1alpha.AddonAssociation) string {
	// Read before the rotation controller records the new values, so this is the
	// password the app is running with.
	return associationVar(assoc, "MYSQL_PASSWORD")
}

// --- Shared per-app user rotation (Class A) ---

type CaptureOldUserPasswordIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type CaptureOldUserPasswordOut struct {
	UserOldPassword string `saga:"useroldpassword"`
}

func CaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn) (CaptureOldUserPasswordOut, error) {
	var assoc addon_v1alpha.AddonAssociation
	if in.AssocEntity != nil {
		assoc.Decode(in.AssocEntity)
	}
	return CaptureOldUserPasswordOut{UserOldPassword: bestEffortOldUserPassword(&assoc)}, nil
}

func UndoCaptureOldUserPassword(ctx context.Context, in CaptureOldUserPasswordIn, out CaptureOldUserPasswordOut) error {
	return nil
}

type AlterSharedUserPasswordIn struct {
	SharedServiceHost  string `saga:"sharedservicehost"`
	SharedRootPassword string `saga:"sharedrootpassword"`
	SharedUserName     string `saga:"sharedusername"`
	UserNewPassword    string `saga:"usernewpassword"`
	UserOldPassword    string `saga:"useroldpassword"`
}

type AlterSharedUserPasswordOut struct {
	UserAltered bool
}

func AlterSharedUserPassword(ctx context.Context, in AlterSharedUserPasswordIn) (AlterSharedUserPasswordOut, error) {
	db, err := connectAsRoot(ctx, in.SharedServiceHost, in.SharedRootPassword)
	if err != nil {
		return AlterSharedUserPasswordOut{}, fmt.Errorf("connecting to rotate user password: %w", err)
	}
	defer db.Close()

	if err := alterMysqlUserPassword(ctx, db, in.SharedUserName, in.UserNewPassword); err != nil {
		return AlterSharedUserPasswordOut{}, err
	}
	return AlterSharedUserPasswordOut{UserAltered: true}, nil
}

func UndoAlterSharedUserPassword(ctx context.Context, in AlterSharedUserPasswordIn, out AlterSharedUserPasswordOut) error {
	if !out.UserAltered || in.UserOldPassword == "" {
		return nil
	}
	// Root is unchanged in a user rotation, so we can always reconnect to restore
	// the old per-app password.
	db, err := connectAsRoot(ctx, in.SharedServiceHost, in.SharedRootPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore user password: %w", err)
	}
	defer db.Close()
	return alterMysqlUserPassword(ctx, db, in.SharedUserName, in.UserOldPassword)
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
		EnvVars: buildEnvVars(in.SharedServiceHost, mysqlPort, in.SharedUserName, in.UserNewPassword, in.SharedDbName),
	}
	return BuildUserRotationResultOut{Done: true}, nil
}

func UndoBuildUserRotationResult(ctx context.Context, in BuildUserRotationResultIn, out BuildUserRotationResultOut) error {
	return nil
}

func RegisterRotateSharedUserSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-shared-mysql-user").
		Using(fw).
		Using(rc).
		Action(DecodeSharedAttrs).Undo(UndoDecodeSharedAttrs).
		Action(LookupSharedServer).Undo(UndoLookupSharedServer).
		Action(CaptureOldUserPassword).Undo(UndoCaptureOldUserPassword).
		Action(AlterSharedUserPassword).Undo(UndoAlterSharedUserPassword).
		Action(BuildUserRotationResult).Undo(UndoBuildUserRotationResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedUser(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared MySQL user credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedUserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering user rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-mysql-user").
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

// --- Shared root rotation (Class C) ---

type BackfillRootDiskNameIn struct {
	SharedServerRef entity.Id `saga:"sharedserverref"`
}

type BackfillRootDiskNameOut struct {
	// Edge producer: the disk name must be recorded before the password changes,
	// or the legacy password-derived name would be lost.
	RootDiskEnsured bool `saga:"root_disk_ensured"`
}

func BackfillRootDiskName(ctx context.Context, in BackfillRootDiskNameIn) (BackfillRootDiskNameOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MysqlServer
	if err := fw.EC.GetById(ctx, in.SharedServerRef, &server); err != nil {
		return BackfillRootDiskNameOut{}, fmt.Errorf("reading shared server: %w", err)
	}
	if server.DiskName == "" {
		legacy := sharedDiskNameForPassword(server.RootPassword)
		if err := fw.EC.Patch(ctx, in.SharedServerRef, 0,
			entity.String(addon_v1alpha.MysqlServerDiskNameId, legacy),
		); err != nil {
			return BackfillRootDiskNameOut{}, fmt.Errorf("backfilling disk name before root rotation: %w", err)
		}
	}
	return BackfillRootDiskNameOut{RootDiskEnsured: true}, nil
}

func UndoBackfillRootDiskName(ctx context.Context, in BackfillRootDiskNameIn, out BackfillRootDiskNameOut) error {
	// Intentional no-op: recording disk_name is a one-way migration ratchet
	// (it decouples the disk identity from the rotating password), so it is
	// never unwritten even if a later action rolls the rotation back.
	return nil
}

type AlterSharedRootPasswordIn struct {
	SharedServiceHost  string `saga:"sharedservicehost"`
	SharedRootPassword string `saga:"sharedrootpassword"`
	RootNewPassword    string `saga:"rootnewpassword"`

	DiskEnsured saga.Edge `saga:"root_disk_ensured"`
}

type AlterSharedRootPasswordOut struct {
	// Edge producer: gate the entity update on the engine actually changing.
	RootAltered bool `saga:"root_altered"`
}

func AlterSharedRootPassword(ctx context.Context, in AlterSharedRootPasswordIn) (AlterSharedRootPasswordOut, error) {
	// Try the recorded (old) password and the target; a retry after a crash may
	// find the engine already on the new one.
	db, err := connectAsRootTrying(ctx, in.SharedServiceHost, in.SharedRootPassword, in.RootNewPassword)
	if err != nil {
		return AlterSharedRootPasswordOut{}, fmt.Errorf("connecting to rotate root password: %w", err)
	}
	defer db.Close()

	if err := alterMysqlUserPassword(ctx, db, defaultMysqlUser, in.RootNewPassword); err != nil {
		return AlterSharedRootPasswordOut{}, err
	}
	return AlterSharedRootPasswordOut{RootAltered: true}, nil
}

func UndoAlterSharedRootPassword(ctx context.Context, in AlterSharedRootPasswordIn, out AlterSharedRootPasswordOut) error {
	if !out.RootAltered {
		return nil
	}
	db, err := connectAsRootTrying(ctx, in.SharedServiceHost, in.RootNewPassword, in.SharedRootPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore root password: %w", err)
	}
	defer db.Close()
	return alterMysqlUserPassword(ctx, db, defaultMysqlUser, in.SharedRootPassword)
}

type UpdateSharedRootEntityIn struct {
	SharedServerRef    entity.Id `saga:"sharedserverref"`
	RootNewPassword    string    `saga:"rootnewpassword"`
	SharedRootPassword string    `saga:"sharedrootpassword"`

	Altered saga.Edge `saga:"root_altered"`
}

type UpdateSharedRootEntityOut struct {
	RootRecorded bool
}

func UpdateSharedRootEntity(ctx context.Context, in UpdateSharedRootEntityIn) (UpdateSharedRootEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.SharedServerRef, 0,
		entity.String(addon_v1alpha.MysqlServerRootPasswordId, in.RootNewPassword),
	); err != nil {
		return UpdateSharedRootEntityOut{}, fmt.Errorf("recording new root password: %w", err)
	}
	return UpdateSharedRootEntityOut{RootRecorded: true}, nil
}

func UndoUpdateSharedRootEntity(ctx context.Context, in UpdateSharedRootEntityIn, out UpdateSharedRootEntityOut) error {
	if !out.RootRecorded {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.SharedServerRef, 0,
		entity.String(addon_v1alpha.MysqlServerRootPasswordId, in.SharedRootPassword),
	)
}

type CaptureRootResultIn struct {
	Recorded saga.Edge `saga:"root_altered"`
}

type CaptureRootResultOut struct {
	Done bool
}

// CaptureRootResult records a non-nil, empty result: rotating root touches no
// consumer variables, so there is nothing to redeploy.
func CaptureRootResult(ctx context.Context, in CaptureRootResultIn) (CaptureRootResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{}
	return CaptureRootResultOut{Done: true}, nil
}

func UndoCaptureRootResult(ctx context.Context, in CaptureRootResultIn, out CaptureRootResultOut) error {
	return nil
}

func RegisterRotateSharedRootSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-shared-mysql-root").
		Using(fw).
		Using(rc).
		Action(DecodeSharedAttrs).Undo(UndoDecodeSharedAttrs).
		Action(LookupSharedServer).Undo(UndoLookupSharedServer).
		Action(BackfillRootDiskName).Undo(UndoBackfillRootDiskName).
		Action(AlterSharedRootPassword).Undo(UndoAlterSharedRootPassword).
		Action(UpdateSharedRootEntity).Undo(UndoUpdateSharedRootEntity).
		Action(CaptureRootResult).Undo(UndoCaptureRootResult).
		RegisterTo(registry)
}

func (p *Provider) rotateSharedRoot(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating shared MySQL root credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateSharedRootSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering root rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-shared-mysql-root").
		Input("assocentity", assoc.Entity).
		Input("rootnewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("root rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}

// --- Dedicated rotation ---
//
// A dedicated server backs one app but, unlike dedicated PostgreSQL, still keeps
// root as a separate admin. So every rotation authenticates as root: the app
// user's own password is never needed to make the change. The user rotation is
// Class A (redeploy the app); the root rotation is Class C (no consumer, and no
// disk landmine since a dedicated server's disk name derives from the server
// name, not the password).

type LoadDedicatedRotationStateIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

type LoadDedicatedRotationStateOut struct {
	DedicatedServiceHost  string `saga:"dedicatedservicehost"`
	DedicatedRootPassword string `saga:"dedicatedrootpassword"`
}

func LoadDedicatedRotationState(ctx context.Context, in LoadDedicatedRotationStateIn) (LoadDedicatedRotationStateOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MysqlServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LoadDedicatedRotationStateOut{}, fmt.Errorf("looking up dedicated server: %w", err)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LoadDedicatedRotationStateOut{}, fmt.Errorf("resolving dedicated service address: %w", err)
	}

	return LoadDedicatedRotationStateOut{
		DedicatedServiceHost:  serviceHost,
		DedicatedRootPassword: server.RootPassword,
	}, nil
}

func UndoLoadDedicatedRotationState(ctx context.Context, in LoadDedicatedRotationStateIn, out LoadDedicatedRotationStateOut) error {
	return nil
}

type CaptureDedicatedConnInfoIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type CaptureDedicatedConnInfoOut struct {
	DedicatedUser        string `saga:"dedicateduser"`
	DedicatedDatabase    string `saga:"dedicateddatabase"`
	DedicatedUserOldPass string `saga:"dedicateduseroldpass"`
}

// CaptureDedicatedConnInfo resolves the role name and database to alter and
// rebuild env vars for. Provisioning records both on the association attrs, so
// they read straight off the entity; associations created before those attrs
// existed fall back to the app's active config. The old user password is a
// best-effort read used only for rollback.
func CaptureDedicatedConnInfo(ctx context.Context, in CaptureDedicatedConnInfoIn) (CaptureDedicatedConnInfoOut, error) {
	var data addon_v1alpha.MysqlDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}
	user, database := data.Username, data.DatabaseName

	var assoc addon_v1alpha.AddonAssociation
	if in.AssocEntity != nil {
		assoc.Decode(in.AssocEntity)
	}

	// Legacy associations predate these attrs. Recover conn info from the
	// variables the association records, which provisioning has written since
	// before the attrs existed, so they stay rotatable.
	if user == "" {
		user = associationVar(&assoc, "MYSQL_USER")
	}
	if database == "" {
		database = associationVar(&assoc, "MYSQL_DATABASE")
	}

	if user == "" {
		return CaptureDedicatedConnInfoOut{}, fmt.Errorf("could not determine dedicated mysql user (no stored attr, no MYSQL_USER on the association)")
	}
	if database == "" {
		return CaptureDedicatedConnInfoOut{}, fmt.Errorf("could not determine dedicated mysql database (no stored attr, no MYSQL_DATABASE on the association)")
	}

	return CaptureDedicatedConnInfoOut{
		DedicatedUser:        user,
		DedicatedDatabase:    database,
		DedicatedUserOldPass: bestEffortOldUserPassword(&assoc),
	}, nil
}

func UndoCaptureDedicatedConnInfo(ctx context.Context, in CaptureDedicatedConnInfoIn, out CaptureDedicatedConnInfoOut) error {
	return nil
}

type AlterDedicatedUserPasswordIn struct {
	DedicatedServiceHost  string `saga:"dedicatedservicehost"`
	DedicatedRootPassword string `saga:"dedicatedrootpassword"`
	DedicatedUser         string `saga:"dedicateduser"`
	DedicatedUserOldPass  string `saga:"dedicateduseroldpass"`
	DedicatedUserNewPass  string `saga:"dedicatedusernewpass"`
}

type AlterDedicatedUserPasswordOut struct {
	DedicatedUserAltered bool
}

func AlterDedicatedUserPassword(ctx context.Context, in AlterDedicatedUserPasswordIn) (AlterDedicatedUserPasswordOut, error) {
	db, err := connectAsRoot(ctx, in.DedicatedServiceHost, in.DedicatedRootPassword)
	if err != nil {
		return AlterDedicatedUserPasswordOut{}, fmt.Errorf("connecting to rotate dedicated user password: %w", err)
	}
	defer db.Close()

	if err := alterMysqlUserPassword(ctx, db, in.DedicatedUser, in.DedicatedUserNewPass); err != nil {
		return AlterDedicatedUserPasswordOut{}, err
	}
	return AlterDedicatedUserPasswordOut{DedicatedUserAltered: true}, nil
}

func UndoAlterDedicatedUserPassword(ctx context.Context, in AlterDedicatedUserPasswordIn, out AlterDedicatedUserPasswordOut) error {
	if !out.DedicatedUserAltered || in.DedicatedUserOldPass == "" {
		return nil
	}
	// Root is unchanged, so reconnect and restore the old user password.
	db, err := connectAsRoot(ctx, in.DedicatedServiceHost, in.DedicatedRootPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore dedicated user password: %w", err)
	}
	defer db.Close()
	return alterMysqlUserPassword(ctx, db, in.DedicatedUser, in.DedicatedUserOldPass)
}

type BuildDedicatedUserResultIn struct {
	DedicatedServiceHost string `saga:"dedicatedservicehost"`
	DedicatedUser        string `saga:"dedicateduser"`
	DedicatedDatabase    string `saga:"dedicateddatabase"`
	DedicatedUserNewPass string `saga:"dedicatedusernewpass"`
}

type BuildDedicatedUserResultOut struct {
	Done bool
}

func BuildDedicatedUserResult(ctx context.Context, in BuildDedicatedUserResultIn) (BuildDedicatedUserResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{
		EnvVars: buildEnvVars(in.DedicatedServiceHost, mysqlPort, in.DedicatedUser, in.DedicatedUserNewPass, in.DedicatedDatabase),
	}
	return BuildDedicatedUserResultOut{Done: true}, nil
}

func UndoBuildDedicatedUserResult(ctx context.Context, in BuildDedicatedUserResultIn, out BuildDedicatedUserResultOut) error {
	return nil
}

func RegisterRotateDedicatedUserSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-dedicated-mysql-user").
		Using(fw).
		Using(rc).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LoadDedicatedRotationState).Undo(UndoLoadDedicatedRotationState).
		Action(CaptureDedicatedConnInfo).Undo(UndoCaptureDedicatedConnInfo).
		Action(AlterDedicatedUserPassword).Undo(UndoAlterDedicatedUserPassword).
		Action(BuildDedicatedUserResult).Undo(UndoBuildDedicatedUserResult).
		RegisterTo(registry)
}

func (p *Provider) rotateDedicatedUser(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated MySQL user credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedUserSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated user rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-mysql-user").
		Input("assocentity", assoc.Entity).
		Input("dedicatedusernewpass", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("dedicated user rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}

// --- Dedicated root rotation (Class C) ---

type AlterDedicatedRootPasswordIn struct {
	DedicatedServiceHost  string `saga:"dedicatedservicehost"`
	DedicatedRootPassword string `saga:"dedicatedrootpassword"`
	RootNewPassword       string `saga:"rootnewpassword"`
}

type AlterDedicatedRootPasswordOut struct {
	// Edge producer: gate the entity update on the engine actually changing.
	RootAltered bool `saga:"root_altered"`
}

func AlterDedicatedRootPassword(ctx context.Context, in AlterDedicatedRootPasswordIn) (AlterDedicatedRootPasswordOut, error) {
	db, err := connectAsRootTrying(ctx, in.DedicatedServiceHost, in.DedicatedRootPassword, in.RootNewPassword)
	if err != nil {
		return AlterDedicatedRootPasswordOut{}, fmt.Errorf("connecting to rotate dedicated root password: %w", err)
	}
	defer db.Close()

	if err := alterMysqlUserPassword(ctx, db, defaultMysqlUser, in.RootNewPassword); err != nil {
		return AlterDedicatedRootPasswordOut{}, err
	}
	return AlterDedicatedRootPasswordOut{RootAltered: true}, nil
}

func UndoAlterDedicatedRootPassword(ctx context.Context, in AlterDedicatedRootPasswordIn, out AlterDedicatedRootPasswordOut) error {
	if !out.RootAltered {
		return nil
	}
	db, err := connectAsRootTrying(ctx, in.DedicatedServiceHost, in.RootNewPassword, in.DedicatedRootPassword)
	if err != nil {
		return fmt.Errorf("connecting to restore dedicated root password: %w", err)
	}
	defer db.Close()
	return alterMysqlUserPassword(ctx, db, defaultMysqlUser, in.DedicatedRootPassword)
}

type UpdateDedicatedRootEntityIn struct {
	DedicatedServerID     entity.Id `saga:"dedicatedserverid"`
	RootNewPassword       string    `saga:"rootnewpassword"`
	DedicatedRootPassword string    `saga:"dedicatedrootpassword"`

	Altered saga.Edge `saga:"root_altered"`
}

type UpdateDedicatedRootEntityOut struct {
	RootRecorded bool
}

func UpdateDedicatedRootEntity(ctx context.Context, in UpdateDedicatedRootEntityIn) (UpdateDedicatedRootEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.MysqlServerRootPasswordId, in.RootNewPassword),
	); err != nil {
		return UpdateDedicatedRootEntityOut{}, fmt.Errorf("recording new dedicated root password: %w", err)
	}
	return UpdateDedicatedRootEntityOut{RootRecorded: true}, nil
}

func UndoUpdateDedicatedRootEntity(ctx context.Context, in UpdateDedicatedRootEntityIn, out UpdateDedicatedRootEntityOut) error {
	if !out.RootRecorded {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.MysqlServerRootPasswordId, in.DedicatedRootPassword),
	)
}

func RegisterRotateDedicatedRootSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-dedicated-mysql-root").
		Using(fw).
		Using(rc).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LoadDedicatedRotationState).Undo(UndoLoadDedicatedRotationState).
		Action(AlterDedicatedRootPassword).Undo(UndoAlterDedicatedRootPassword).
		Action(UpdateDedicatedRootEntity).Undo(UndoUpdateDedicatedRootEntity).
		Action(CaptureRootResult).Undo(UndoCaptureRootResult).
		RegisterTo(registry)
}

func (p *Provider) rotateDedicatedRoot(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated MySQL root credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedRootSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated root rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-mysql-root").
		Input("assocentity", assoc.Entity).
		Input("rootnewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("dedicated root rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}
