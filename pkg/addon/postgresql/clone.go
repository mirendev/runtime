package postgresql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	SourceDbName      string
	SourceUsername    string
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
		ServerID: data.PostgresServer, SourceDbName: data.DatabaseName, SourceUsername: data.Username,
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

type cloneSharedDatabaseIn struct {
	ServiceHost        string
	SuperuserPassword  string
	SourceDbName       string
	SourceUsername     string
	SharedDatabaseName string
	SharedUsername     string
}

type cloneSharedDatabaseOut struct {
	DatabaseCreated bool `saga:"database_created"`
}

func cloneSharedDatabase(ctx context.Context, in cloneSharedDatabaseIn) (cloneSharedDatabaseOut, error) {
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return cloneSharedDatabaseOut{}, err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS false", quoteIdentifier(in.SourceDbName))); err != nil {
		return cloneSharedDatabaseOut{}, fmt.Errorf("pausing connections to %s: %w", in.SourceDbName, err)
	}
	paused := true
	defer func() {
		if paused {
			cleanupCtx := context.WithoutCancel(ctx)
			_, _ = conn.Exec(cleanupCtx, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS true", quoteIdentifier(in.SourceDbName)))
		}
	}()
	if err := terminatePostgresConnections(ctx, conn, in.SourceDbName); err != nil {
		return cloneSharedDatabaseOut{}, err
	}
	query := fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE %s OWNER %s",
		quoteIdentifier(in.SharedDatabaseName), quoteIdentifier(in.SourceDbName), quoteIdentifier(in.SharedUsername))
	if _, err := conn.Exec(ctx, query); err != nil {
		return cloneSharedDatabaseOut{}, fmt.Errorf("cloning database %s: %w", in.SourceDbName, err)
	}
	targetConn, err := connectPostgres(ctx, in.ServiceHost, postgresPort, defaultPostgresUser, in.SuperuserPassword, in.SharedDatabaseName)
	if err != nil {
		_ = dropPostgresDatabase(context.WithoutCancel(ctx), conn, in.SharedDatabaseName)
		return cloneSharedDatabaseOut{}, fmt.Errorf("connecting to cloned database: %w", err)
	}
	if _, err := targetConn.Exec(ctx, fmt.Sprintf("REASSIGN OWNED BY %s TO %s",
		quoteIdentifier(in.SourceUsername), quoteIdentifier(in.SharedUsername))); err != nil {
		targetConn.Close(context.WithoutCancel(ctx))
		_ = dropPostgresDatabase(context.WithoutCancel(ctx), conn, in.SharedDatabaseName)
		return cloneSharedDatabaseOut{}, fmt.Errorf("transferring cloned database ownership: %w", err)
	}
	targetConn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS true", quoteIdentifier(in.SourceDbName))); err != nil {
		_ = dropPostgresDatabase(context.WithoutCancel(ctx), conn, in.SharedDatabaseName)
		return cloneSharedDatabaseOut{}, fmt.Errorf("resuming connections to %s: %w", in.SourceDbName, err)
	}
	paused = false
	return cloneSharedDatabaseOut{DatabaseCreated: true}, nil
}

func undoCloneSharedDatabase(ctx context.Context, in cloneSharedDatabaseIn, out cloneSharedDatabaseOut) error {
	if !out.DatabaseCreated {
		return nil
	}
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return dropPostgresDatabase(ctx, conn, in.SharedDatabaseName)
}

func registerCloneSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	b := saga.Define("clone-shared-postgresql").Using(fw)
	saga.UsingAs[dbsaga.ServerCounter](b, pgServerCounter{})
	return b.
		Action(decodeSharedCloneSource).Undo(undoDecodeSharedCloneSource).
		Action(generateCloneCredentials).Undo(undoGenerateCloneCredentials).
		Action(CreateSharedUser).Undo(UndoCreateSharedUser).
		Action(cloneSharedDatabase).Undo(undoCloneSharedDatabase).
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
	Password         string
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
		DatabaseName: data.DatabaseName, Username: data.Username, Password: server.SuperuserPassword,
	}, nil
}

func undoDecodeDedicatedCloneSource(context.Context, decodeDedicatedCloneSourceIn, decodeDedicatedCloneSourceOut) error {
	return nil
}

type generateDedicatedCloneNameIn struct {
	AppName             string
	TargetAssociationID string
}
type generateDedicatedCloneNameOut struct{ ServerName string }

func generateDedicatedCloneName(_ context.Context, in generateDedicatedCloneNameIn) (generateDedicatedCloneNameOut, error) {
	suffix := in.TargetAssociationID
	if i := strings.LastIndexByte(suffix, '/'); i >= 0 {
		suffix = suffix[i+1:]
	}
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	return generateDedicatedCloneNameOut{ServerName: fmt.Sprintf("pg-%s-%s", in.AppName, suffix)}, nil
}
func undoGenerateDedicatedCloneName(context.Context, generateDedicatedCloneNameIn, generateDedicatedCloneNameOut) error {
	return nil
}

const (
	baseBackupTimeout  = 30 * time.Minute
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
	Password         string
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
			"SOURCE_PASSWORD=" + in.Password,
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
