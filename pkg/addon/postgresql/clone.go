package postgresql

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

type decodeSharedCloneSourceIn struct {
	SourceEntity *entity.Entity `saga:"sourceentity"`
}

type decodeSharedCloneSourceOut struct {
	ServerID          entity.Id
	SourceHost        string
	DatabaseName      string
	Username          string
	SourcePassword    string `saga:"source_password"`
	ServiceHost       string
	SuperuserPassword string
}

func decodeSharedCloneSource(ctx context.Context, in decodeSharedCloneSourceIn) (decodeSharedCloneSourceOut, error) {
	var data addon_v1alpha.PostgresqlSharedData
	data.Decode(in.SourceEntity)
	if data.PostgresServer == "" || data.DatabaseName == "" {
		return decodeSharedCloneSourceOut{}, fmt.Errorf("clone source has no shared PostgreSQL database")
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, data.PostgresServer, &server); err != nil {
		return decodeSharedCloneSourceOut{}, fmt.Errorf("reading shared PostgreSQL server: %w", err)
	}
	host, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return decodeSharedCloneSourceOut{}, err
	}
	return decodeSharedCloneSourceOut{
		ServerID: data.PostgresServer, SourceHost: host, DatabaseName: data.DatabaseName,
		Username: defaultPostgresUser, SourcePassword: server.SuperuserPassword,
		ServiceHost: host, SuperuserPassword: server.SuperuserPassword,
	}, nil
}

func undoDecodeSharedCloneSource(context.Context, decodeSharedCloneSourceIn, decodeSharedCloneSourceOut) error {
	return nil
}

type generateCloneCredentialsIn struct {
	AppName             string
	TargetAssociationID string
}

type generateCloneCredentialsOut struct {
	SharedPassword          string
	SharedDatabaseName      string
	GeneratedSharedUsername string
}

func generateCloneCredentials(_ context.Context, in generateCloneCredentialsIn) (generateCloneCredentialsOut, error) {
	suffix := in.TargetAssociationID
	if i := strings.LastIndexByte(suffix, '/'); i >= 0 {
		suffix = suffix[i+1:]
	}
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	name := sanitizeIdentifier(in.AppName + "_" + suffix)
	return generateCloneCredentialsOut{
		SharedPassword:          idgen.Gen("pw"),
		SharedDatabaseName:      name,
		GeneratedSharedUsername: name,
	}, nil
}

func undoGenerateCloneCredentials(context.Context, generateCloneCredentialsIn, generateCloneCredentialsOut) error {
	return nil
}

type createCloneSharedUserIn struct {
	ServiceHost             string
	SuperuserPassword       string
	GeneratedSharedUsername string
	SharedPassword          string
}

type createCloneSharedUserOut struct {
	SharedUsername string
}

func createCloneSharedUser(ctx context.Context, in createCloneSharedUserIn) (createCloneSharedUserOut, error) {
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return createCloneSharedUserOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", in.GeneratedSharedUsername).Scan(&exists); err != nil {
		return createCloneSharedUserOut{}, fmt.Errorf("checking clone user %s: %w", in.GeneratedSharedUsername, err)
	}
	if exists {
		err = alterPostgresUserPassword(ctx, conn, in.GeneratedSharedUsername, in.SharedPassword)
	} else {
		err = createPostgresUser(ctx, conn, in.GeneratedSharedUsername, in.SharedPassword)
	}
	if err != nil {
		return createCloneSharedUserOut{}, err
	}
	return createCloneSharedUserOut{SharedUsername: in.GeneratedSharedUsername}, nil
}

func undoCreateCloneSharedUser(ctx context.Context, in createCloneSharedUserIn, out createCloneSharedUserOut) error {
	return UndoCreateSharedUser(ctx, CreateSharedUserIn{
		ServiceHost: in.ServiceHost, SuperuserPassword: in.SuperuserPassword,
		GeneratedSharedUsername: in.GeneratedSharedUsername,
	}, CreateSharedUserOut(out))
}

type createCloneSharedDatabaseIn struct {
	ServiceHost         string
	SuperuserPassword   string
	SharedDatabaseName  string
	SharedUsername      string
	TargetAssociationID string
}

type createCloneSharedDatabaseOut struct {
	DatabaseCreated bool `saga:"database_created"`
}

func createCloneSharedDatabase(ctx context.Context, in createCloneSharedDatabaseIn) (createCloneSharedDatabaseOut, error) {
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return createCloneSharedDatabaseOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer conn.Close(ctx)
	var owner string
	err = conn.QueryRow(ctx, `SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`, in.SharedDatabaseName).Scan(&owner)
	if err == nil {
		if owner != in.SharedUsername {
			return createCloneSharedDatabaseOut{}, fmt.Errorf("clone database %s already exists with owner %s, expected %s", in.SharedDatabaseName, owner, in.SharedUsername)
		}
		return createCloneSharedDatabaseOut{DatabaseCreated: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return createCloneSharedDatabaseOut{}, fmt.Errorf("checking clone database %s: %w", in.SharedDatabaseName, err)
	}
	if err := createPostgresDatabase(ctx, conn, in.SharedDatabaseName, in.SharedUsername); err != nil {
		return createCloneSharedDatabaseOut{}, err
	}
	return createCloneSharedDatabaseOut{DatabaseCreated: true}, nil
}

func undoCreateCloneSharedDatabase(ctx context.Context, in createCloneSharedDatabaseIn, out createCloneSharedDatabaseOut) error {
	// A failed restore may not have checkpointed its sandbox ID. Release that
	// deterministic resource before trying to drop the database it connects to.
	id := "sandbox/pg-" + strings.TrimPrefix(in.TargetAssociationID, "addon_association/") + "-restore"
	_, err := saga.Get[*addon.ProviderFramework](ctx).EAC.Delete(ctx, id)
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return UndoCreateSharedDatabase(ctx, CreateSharedDatabaseIn{
		ServiceHost: in.ServiceHost, SuperuserPassword: in.SuperuserPassword,
		SharedDatabaseName: in.SharedDatabaseName, SharedUsername: in.SharedUsername,
	}, CreateSharedDatabaseOut(out))
}

func registerCloneSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	b := saga.Define("clone-shared-postgresql").Using(fw)
	saga.UsingAs[dbsaga.ServerCounter](b, pgServerCounter{})
	return b.
		Action(decodeSharedCloneSource).Undo(undoDecodeSharedCloneSource).
		Action(generateCloneCredentials).Undo(undoGenerateCloneCredentials).
		Action(createCloneSharedUser).Undo(undoCreateCloneSharedUser).
		Action(createCloneSharedDatabase).Undo(undoCreateCloneSharedDatabase).
		Action(restoreSharedClone).Undo(undoRestoreSharedClone).
		Action(releaseRestoreSandbox).Undo(undoReleaseRestoreSandbox).
		Action(dbsaga.IncrementAssociationCount).Undo(dbsaga.UndoIncrementAssociationCount).
		RegisterTo(registry)
}

func (p *Provider) cloneShared(ctx context.Context, source, target addon.AddonAssociation, app addon.App) (*addon.ProvisionResult, error) {
	registry := saga.NewRegistry()
	if err := registerCloneSharedSaga(registry, p.Fw); err != nil {
		return nil, err
	}
	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	execID := addon.CloneExecutionID(target.ID)
	if err := executor.Start("clone-shared-postgresql").WithID(execID).
		Input("sourceentity", source.Entity).
		Input("appname", app.Name).
		Input("targetassociationid", target.ID.String()).
		Execute(ctx); err != nil {
		return nil, err
	}
	out, err := executor.ExecutionOutputs(ctx, execID)
	if err != nil {
		return nil, err
	}
	var host, username, password, dbName string
	var serverID entity.Id
	for key, target := range map[string]any{
		"servicehost": &host, "sharedusername": &username,
		"sharedpassword": &password, "shareddatabasename": &dbName,
		"serverid": &serverID,
	} {
		if err := out.Get(key, target); err != nil {
			return nil, fmt.Errorf("reading %s from clone outputs: %w", key, err)
		}
	}
	return &addon.ProvisionResult{
		EnvVars: buildEnvVars(host, postgresPort, username, password, dbName),
		Attrs: (&addon_v1alpha.PostgresqlSharedData{
			PostgresServer: serverID, DatabaseName: dbName, Username: username,
		}).Encode(),
	}, nil
}

type decodeDedicatedCloneSourceIn struct {
	SourceEntity *entity.Entity `saga:"sourceentity"`
}

type decodeDedicatedCloneSourceOut struct {
	SourcePoolID     entity.Id
	SourceHost       string
	SourceDiskSizeGb int64
	DatabaseName     string
	Username         string
	SourcePassword   string `saga:"source_password"`
}

func decodeDedicatedCloneSource(ctx context.Context, in decodeDedicatedCloneSourceIn) (decodeDedicatedCloneSourceOut, error) {
	var data addon_v1alpha.PostgresqlDedicatedData
	data.Decode(in.SourceEntity)
	if data.PostgresServer == "" {
		return decodeDedicatedCloneSourceOut{}, fmt.Errorf("clone source has no dedicated PostgreSQL server")
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, data.PostgresServer, &server); err != nil {
		return decodeDedicatedCloneSourceOut{}, err
	}
	var metadata core_v1alpha.Metadata
	if err := fw.EC.GetById(ctx, data.PostgresServer, &metadata); err != nil {
		return decodeDedicatedCloneSourceOut{}, err
	}
	host, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return decodeDedicatedCloneSourceOut{}, err
	}
	diskName := "pg-" + metadata.Name + "-data"
	disks, err := fw.EAC.List(ctx, entity.String(storage_v1alpha.DiskNameId, diskName))
	if err != nil {
		return decodeDedicatedCloneSourceOut{}, fmt.Errorf("looking up dedicated PostgreSQL disk: %w", err)
	}
	if len(disks.Values()) != 1 {
		return decodeDedicatedCloneSourceOut{}, fmt.Errorf("expected one dedicated PostgreSQL disk %q, found %d", diskName, len(disks.Values()))
	}
	var disk storage_v1alpha.Disk
	disk.Decode(disks.Values()[0].Entity())
	return decodeDedicatedCloneSourceOut{
		SourcePoolID: server.SandboxPool, SourceHost: host, SourceDiskSizeGb: disk.SizeGb,
		DatabaseName: data.DatabaseName, Username: data.Username, SourcePassword: server.SuperuserPassword,
	}, nil
}

func undoDecodeDedicatedCloneSource(context.Context, decodeDedicatedCloneSourceIn, decodeDedicatedCloneSourceOut) error {
	return nil
}

type restoreSharedCloneIn struct {
	SourceHost          string
	DatabaseName        string
	Username            string
	SourcePassword      string `saga:"source_password"`
	ServiceHost         string
	SharedUsername      string
	SharedPassword      string
	SharedDatabaseName  string
	VariantConfig       map[string]string `saga:"variantconfig,optional"`
	TargetAssociationID string
	AppName             string
	DatabaseCreated     bool `saga:"database_created"`
}

type restoreSharedCloneOut struct {
	RestoreSandboxID entity.Id
}

func restoreSharedClone(ctx context.Context, in restoreSharedCloneIn) (restoreSharedCloneOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	// Dump to a file before restoring, so a failed pg_dump cannot look like a
	// successful partial restore. Omitting owners and ACLs makes the preview
	// role own its objects without importing production roles into the cluster.
	command := `set -eu
PGPASSWORD="$SOURCE_PASSWORD" pg_dump -h "$SOURCE_HOST" -U "$SOURCE_USERNAME" -d "$SOURCE_DATABASE" --format=custom --no-owner --no-acl -f /tmp/clone.dump
PGPASSWORD="$TARGET_PASSWORD" pg_restore -h "$TARGET_HOST" -U "$TARGET_USERNAME" -d "$TARGET_DATABASE" --no-owner --no-acl --exit-on-error --single-transaction /tmp/clone.dump`
	image := restoreCloneImage(in.VariantConfig)
	id, err := fw.RunOneShotSandbox(ctx, addon.OneShotSandboxSpec{
		Name:  "pg-" + strings.TrimPrefix(in.TargetAssociationID, "addon_association/") + "-restore",
		Image: image, Command: command,
		Env: []string{
			"SOURCE_HOST=" + in.SourceHost, "SOURCE_PASSWORD=" + in.SourcePassword, "SOURCE_DATABASE=" + in.DatabaseName,
			"SOURCE_USERNAME=" + in.Username,
			"TARGET_HOST=" + in.ServiceHost, "TARGET_USERNAME=" + in.SharedUsername,
			"TARGET_PASSWORD=" + in.SharedPassword, "TARGET_DATABASE=" + in.SharedDatabaseName,
		},
		Labels: types.LabelSet("addon", AddonName, "app", in.AppName, "operation", "clone-restore"),
	}, addon.CloneCopyTimeout)
	if err != nil {
		if id != "" {
			_, cleanupErr := fw.EAC.Delete(context.WithoutCancel(ctx), id.String())
			if cleanupErr != nil && !errors.Is(cleanupErr, cond.ErrNotFound{}) {
				return restoreSharedCloneOut{}, errors.Join(err, cleanupErr)
			}
		}
		return restoreSharedCloneOut{}, fmt.Errorf("dumping and restoring PostgreSQL clone: %w", err)
	}
	return restoreSharedCloneOut{RestoreSandboxID: id}, nil
}

func restoreCloneImage(variantConfig map[string]string) string {
	image := variantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}
	return image
}

func undoRestoreSharedClone(ctx context.Context, _ restoreSharedCloneIn, out restoreSharedCloneOut) error {
	_, err := saga.Get[*addon.ProviderFramework](ctx).EAC.Delete(ctx, out.RestoreSandboxID.String())
	if errors.Is(err, cond.ErrNotFound{}) {
		return nil
	}
	return err
}

type releaseRestoreSandboxIn struct {
	RestoreSandboxID entity.Id
}
type releaseRestoreSandboxOut struct{}

func releaseRestoreSandbox(ctx context.Context, in releaseRestoreSandboxIn) (releaseRestoreSandboxOut, error) {
	return releaseRestoreSandboxOut{}, undoRestoreSharedClone(ctx, restoreSharedCloneIn{}, restoreSharedCloneOut(in))
}

func undoReleaseRestoreSandbox(context.Context, releaseRestoreSandboxIn, releaseRestoreSandboxOut) error {
	return nil
}

func registerCloneDedicatedToSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	if err := RegisterEnsureSharedServerSaga(registry, fw); err != nil {
		return err
	}
	b := saga.Define("clone-dedicated-to-shared-postgresql").Using(fw)
	saga.UsingAs[dbsaga.ServerCounter](b, pgServerCounter{})
	return b.
		Action(decodeDedicatedCloneSource).Undo(undoDecodeDedicatedCloneSource).
		Action(FindOrCreateSharedServer).Undo(UndoFindOrCreateSharedServer).
		Action(generateCloneCredentials).Undo(undoGenerateCloneCredentials).
		Action(createCloneSharedUser).Undo(undoCreateCloneSharedUser).
		Action(createCloneSharedDatabase).Undo(undoCreateCloneSharedDatabase).
		Action(restoreSharedClone).Undo(undoRestoreSharedClone).
		Action(releaseRestoreSandbox).Undo(undoReleaseRestoreSandbox).
		Action(dbsaga.IncrementAssociationCount).Undo(dbsaga.UndoIncrementAssociationCount).
		RegisterTo(registry)
}

func (p *Provider) cloneDedicatedToShared(ctx context.Context, source, target addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	registry := saga.NewRegistry()
	if err := registerCloneDedicatedToSharedSaga(registry, p.Fw); err != nil {
		return nil, err
	}
	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	execID := addon.CloneExecutionID(target.ID)
	if err := executor.Start("clone-dedicated-to-shared-postgresql").WithID(execID).
		Input("sourceentity", source.Entity).Input("appname", app.Name).
		Input("targetassociationid", target.ID.String()).Input("variantconfig", variant.Config).
		Execute(ctx); err != nil {
		return nil, err
	}
	out, err := executor.ExecutionOutputs(ctx, execID)
	if err != nil {
		return nil, err
	}
	var host, username, password, dbName string
	var serverID entity.Id
	for key, target := range map[string]any{
		"servicehost": &host, "sharedusername": &username,
		"sharedpassword": &password, "shareddatabasename": &dbName, "serverid": &serverID,
	} {
		if err := out.Get(key, target); err != nil {
			return nil, fmt.Errorf("reading %s from clone outputs: %w", key, err)
		}
	}
	return &addon.ProvisionResult{
		EnvVars: buildEnvVars(host, postgresPort, username, password, dbName),
		Attrs: (&addon_v1alpha.PostgresqlSharedData{
			PostgresServer: serverID, DatabaseName: dbName, Username: username,
		}).Encode(),
	}, nil
}

type generateDedicatedCloneNameIn struct {
	AppName             string
	TargetAssociationID string
}
type generateDedicatedCloneNameOut struct {
	ServerName  string
	ServiceName string
	Password    string
}

func generateDedicatedCloneName(_ context.Context, in generateDedicatedCloneNameIn) (generateDedicatedCloneNameOut, error) {
	suffix := in.TargetAssociationID
	if i := strings.LastIndexByte(suffix, '/'); i >= 0 {
		suffix = suffix[i+1:]
	}
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	return generateDedicatedCloneNameOut{
		ServerName:  fmt.Sprintf("pg-%s-%s", in.AppName, suffix),
		ServiceName: fmt.Sprintf("%s-postgresql-%s", in.AppName, suffix),
		Password:    idgen.Gen("pw"),
	}, nil
}
func undoGenerateDedicatedCloneName(ctx context.Context, _ generateDedicatedCloneNameIn, out generateDedicatedCloneNameOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	_, err := fw.EAC.Delete(ctx, "sandbox/"+out.ServerName+"-basebackup")
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return fw.DeleteDiskByName(ctx, "pg-"+out.ServerName+"-data")
}

const (
	baseBackupTimeout  = addon.CloneCopyTimeout
	replicationHBALine = "host replication all 0.0.0.0/0 scram-sha-256 # miren-addon-basebackup"
)

const reloadPostgresConfigCommand = `kill -HUP "$(head -n 1 "$PGDATA/postmaster.pid")"`

type enableBaseBackupIn struct {
	SourcePoolID entity.Id
}
type enableBaseBackupOut struct {
	ReplicationReady saga.Edge `saga:"replication_ready"`
}

func enableBaseBackup(ctx context.Context, in enableBaseBackupIn) (enableBaseBackupOut, error) {
	command := fmt.Sprintf(`set -eu
line=%q
grep -Fqx "$line" "$PGDATA/pg_hba.conf" || printf '%%s\n' "$line" >> "$PGDATA/pg_hba.conf"
%s`, replicationHBALine, reloadPostgresConfigCommand)
	if err := saga.Get[*addon.ProviderFramework](ctx).ExecInPool(ctx, in.SourcePoolID, "sh", "-ceu", command); err != nil {
		return enableBaseBackupOut{}, fmt.Errorf("enabling PostgreSQL base backups: %w", err)
	}
	return enableBaseBackupOut{}, nil
}

func disableBaseBackup(ctx context.Context, poolID entity.Id) error {
	command := fmt.Sprintf(`set -eu
tmp=$(mktemp)
grep -Fv %q "$PGDATA/pg_hba.conf" > "$tmp" || true
cat "$tmp" > "$PGDATA/pg_hba.conf"
rm -f "$tmp"
%s`, "# miren-addon-basebackup", reloadPostgresConfigCommand)
	return saga.Get[*addon.ProviderFramework](ctx).ExecInPool(ctx, poolID, "sh", "-ceu", command)
}

func undoEnableBaseBackup(ctx context.Context, in enableBaseBackupIn, _ enableBaseBackupOut) error {
	return disableBaseBackup(ctx, in.SourcePoolID)
}

type runBaseBackupIn struct {
	AppName          string
	ServerName       string
	SourceHost       string
	SourceDiskSizeGb int64
	Username         string
	SourcePassword   string `saga:"source_password"`
	VariantConfig    map[string]string
	ReplicationReady saga.Edge `saga:"replication_ready"`
}
type runBaseBackupOut struct {
	BackupSandboxID entity.Id
	BaseBackupDone  saga.Edge `saga:"base_backup_done"`
}

const baseBackupCommand = `set -eu
rm -rf "$PGDATA"
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA"
chmod 0700 "$PGDATA"
gosu postgres env PGPASSWORD="$SOURCE_PASSWORD" pg_basebackup \
  --host="$SOURCE_HOST" --port=5432 --username="$SOURCE_USER" \
  --pgdata="$PGDATA" --format=plain --wal-method=stream \
  --checkpoint=fast --no-password
tmp=$(mktemp)
grep -Fv '# miren-addon-basebackup' "$PGDATA/pg_hba.conf" > "$tmp" || true
cat "$tmp" > "$PGDATA/pg_hba.conf"
rm -f "$tmp"`

func runBaseBackup(ctx context.Context, in runBaseBackupIn) (runBaseBackupOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	targetDisk := fmt.Sprintf("pg-%s-data", in.ServerName)
	image := in.VariantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}
	labels := types.LabelSet("addon", AddonName, "app", in.AppName, "server", in.ServerName, "operation", "basebackup")
	sandboxID, err := fw.RunOneShotSandbox(ctx, addon.OneShotSandboxSpec{
		Name:    in.ServerName + "-basebackup",
		Image:   image,
		Command: baseBackupCommand,
		Env: []string{
			"PGDATA=/var/lib/postgresql/data/pgdata",
			"SOURCE_HOST=" + in.SourceHost,
			"SOURCE_USER=" + in.Username,
			"SOURCE_PASSWORD=" + in.SourcePassword,
		},
		Labels: labels,
		Mounts: []compute_v1alpha.SandboxSpecContainerMount{
			{Source: "pgdata", Destination: "/var/lib/postgresql/data"},
		},
		Volumes: []compute_v1alpha.SandboxSpecVolume{{
			Name: "pgdata", Provider: "miren", DiskName: targetDisk,
			MountPath: "/var/lib/postgresql/data", SizeGb: in.SourceDiskSizeGb,
			Filesystem: "ext4", LeaseTimeout: "5m",
		}},
	}, baseBackupTimeout)
	if err != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		if sandboxID != "" {
			_, _ = fw.EAC.Delete(cleanupCtx, sandboxID.String())
		}
		_ = fw.DeleteDiskByName(cleanupCtx, targetDisk)
		return runBaseBackupOut{}, fmt.Errorf("running pg_basebackup: %w", err)
	}
	return runBaseBackupOut{BackupSandboxID: sandboxID}, nil
}

func undoRunBaseBackup(ctx context.Context, in runBaseBackupIn, out runBaseBackupOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if out.BackupSandboxID != "" {
		if _, err := fw.EAC.Delete(ctx, out.BackupSandboxID.String()); err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
	}
	return fw.DeleteDiskByName(ctx, fmt.Sprintf("pg-%s-data", in.ServerName))
}

type disableBaseBackupIn struct {
	SourcePoolID   entity.Id
	BaseBackupDone saga.Edge `saga:"base_backup_done"`
}
type disableBaseBackupOut struct{ Disabled bool }

func finishBaseBackupAccess(ctx context.Context, in disableBaseBackupIn) (disableBaseBackupOut, error) {
	if err := disableBaseBackup(ctx, in.SourcePoolID); err != nil {
		return disableBaseBackupOut{}, fmt.Errorf("disabling PostgreSQL base backups: %w", err)
	}
	return disableBaseBackupOut{Disabled: true}, nil
}
func undoFinishBaseBackupAccess(context.Context, disableBaseBackupIn, disableBaseBackupOut) error {
	return nil
}

type releaseBaseBackupSandboxIn struct {
	BackupSandboxID entity.Id
	BaseBackupDone  saga.Edge `saga:"base_backup_done"`
}
type releaseBaseBackupSandboxOut struct {
	StorageReady saga.Edge `saga:"storage_ready"`
}

func releaseBaseBackupSandbox(ctx context.Context, in releaseBaseBackupSandboxIn) (releaseBaseBackupSandboxOut, error) {
	_, err := saga.Get[*addon.ProviderFramework](ctx).EAC.Delete(ctx, in.BackupSandboxID.String())
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return releaseBaseBackupSandboxOut{}, fmt.Errorf("deleting base-backup sandbox: %w", err)
	}
	return releaseBaseBackupSandboxOut{}, nil
}
func undoReleaseBaseBackupSandbox(context.Context, releaseBaseBackupSandboxIn, releaseBaseBackupSandboxOut) error {
	return nil
}

type rotateCloneCredentialsIn struct {
	ServiceHost    string
	Username       string
	DatabaseName   string
	Password       string
	SourcePassword string `saga:"source_password"`
	Ready          bool
}

type rotateCloneCredentialsOut struct {
	CloneCredentialsReady saga.Edge `saga:"clone_credentials_ready"`
}

func rotateCloneCredentials(ctx context.Context, in rotateCloneCredentialsIn) (rotateCloneCredentialsOut, error) {
	// ALTER may have committed before its action checkpoint. Try the preview
	// password first so replay never depends on the old credential still working.
	conn, err := connectTrying(ctx, in.ServiceHost, postgresPort, in.Username, in.DatabaseName, in.Password, in.SourcePassword)
	if err != nil {
		return rotateCloneCredentialsOut{}, fmt.Errorf("connecting to PostgreSQL clone: %w", err)
	}
	defer conn.Close(ctx)
	if err := alterPostgresUserPassword(ctx, conn, in.Username, in.Password); err != nil {
		return rotateCloneCredentialsOut{}, err
	}
	return rotateCloneCredentialsOut{}, nil
}

func undoRotateCloneCredentials(context.Context, rotateCloneCredentialsIn, rotateCloneCredentialsOut) error {
	return nil
}

func registerCloneDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, Port: postgresPort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("clone-dedicated-postgresql").Using(fw).Using(cfg).
		Action(decodeDedicatedCloneSource).Undo(undoDecodeDedicatedCloneSource).
		Action(generateDedicatedCloneName).Undo(undoGenerateDedicatedCloneName).
		Action(enableBaseBackup).Undo(undoEnableBaseBackup).
		Action(runBaseBackup).Undo(undoRunBaseBackup).
		Action(finishBaseBackupAccess).Undo(undoFinishBaseBackupAccess).
		Action(releaseBaseBackupSandbox).Undo(undoReleaseBaseBackupSandbox).
		Action(CreatePostgresServer).Undo(UndoCreatePostgresServer).
		Action(CreateDedicatedPool).Undo(UndoCreateDedicatedPool).
		Action(dbsaga.WaitForDedicatedPool).Undo(dbsaga.UndoWaitForDedicatedPool).
		Action(dbsaga.CreateDedicatedService).Undo(dbsaga.UndoCreateDedicatedService).
		Action(dbsaga.WaitForDedicatedService).Undo(dbsaga.UndoWaitForDedicatedService).
		Action(rotateCloneCredentials).Undo(undoRotateCloneCredentials).
		Action(UpdateDedicatedServer).Undo(UndoUpdateDedicatedServer).
		RegisterTo(registry)
}

func (p *Provider) cloneDedicated(ctx context.Context, source, target addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	registry := saga.NewRegistry()
	if err := registerCloneDedicatedSaga(registry, p.Fw); err != nil {
		return nil, err
	}
	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	execID := addon.CloneExecutionID(target.ID)
	if err := executor.Start("clone-dedicated-postgresql").WithID(execID).
		Input("sourceentity", source.Entity).Input("appname", app.Name).
		Input("targetassociationid", target.ID.String()).Input("variantname", variant.Name).
		Input("variantconfig", variant.Config).Execute(ctx); err != nil {
		return nil, err
	}
	out, err := executor.ExecutionOutputs(ctx, execID)
	if err != nil {
		return nil, err
	}
	var host, username, password, dbName string
	var serverID entity.Id
	for key, target := range map[string]any{
		"servicehost": &host, "username": &username, "password": &password,
		"databasename": &dbName, "serverid": &serverID,
	} {
		if err := out.Get(key, target); err != nil {
			return nil, fmt.Errorf("reading %s from clone outputs: %w", key, err)
		}
	}
	return &addon.ProvisionResult{
		EnvVars: buildEnvVars(host, postgresPort, username, password, dbName),
		Attrs:   (&addon_v1alpha.PostgresqlDedicatedData{PostgresServer: serverID, DatabaseName: dbName, Username: username}).Encode(),
	}, nil
}
