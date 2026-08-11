package mysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

const (
	sharedServerName = "my-shared"
	sharedDiskName   = "my-shared-data"
	poolReadyTimeout = 5 * time.Minute
)

// --- EnsureSharedServerSaga Actions ---

type CreateSharedServerEntityIn struct {
	RootPassword  string
	DiskName      string
	VariantConfig map[string]string
}

type CreateSharedServerEntityOut struct {
	ServerID entity.Id
}

func CreateSharedServerEntity(ctx context.Context, in CreateSharedServerEntityIn) (CreateSharedServerEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MysqlServer{
		AddonName:        AddonName,
		Variant:          "shared",
		Image:            in.VariantConfig[addon.ConfigImage],
		Status:           "provisioning",
		AssociationCount: 0,
		RootPassword:     in.RootPassword,
		DiskName:         in.DiskName,
	}

	serverID, err := fw.EC.Create(ctx, sharedServerName, server)
	if err != nil {
		return CreateSharedServerEntityOut{}, fmt.Errorf("creating shared server entity: %w", err)
	}

	return CreateSharedServerEntityOut{ServerID: serverID}, nil
}

func UndoCreateSharedServerEntity(ctx context.Context, in CreateSharedServerEntityIn, out CreateSharedServerEntityOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Delete(ctx, out.ServerID)
}

type CreateSharedPoolIn struct {
	RootPassword  string
	DiskName      string
	VariantConfig map[string]string
}

type CreateSharedPoolOut struct {
	PoolID entity.Id
}

// sharedDiskNameForPassword derives the legacy disk name from the root password.
// New servers no longer use this: they store an explicit disk_name generated
// independently of the password (see newSharedDiskName). It survives only to
// reproduce, and back-fill, the name of servers provisioned before disk_name was
// tracked. Deriving the disk identity from the password is what made the root
// password effectively immutable, since rotating it moved the disk out from
// under the existing data.
func sharedDiskNameForPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return sharedDiskName + "-" + hex.EncodeToString(h[:4])
}

// newSharedDiskName generates a fresh, unique disk name for a new shared-server
// generation. Uniqueness now comes from a random nonce rather than the root
// password, leaving the password free to rotate without moving the disk
// identity.
func newSharedDiskName() string {
	h := sha256.Sum256([]byte(idgen.Gen("mydisk")))
	return sharedDiskName + "-" + hex.EncodeToString(h[:4])
}

// resolveSharedDiskName returns the server's stored disk name, falling back to
// the legacy password-derived name for servers provisioned before disk_name was
// tracked.
func resolveSharedDiskName(server *addon_v1alpha.MysqlServer) string {
	if server.DiskName != "" {
		return server.DiskName
	}
	return sharedDiskNameForPassword(server.RootPassword)
}

func CreateSharedPool(ctx context.Context, in CreateSharedPoolIn) (CreateSharedPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	labels := types.LabelSet(
		"addon", AddonName,
		"server", sharedServerName,
		"shared", "true",
	)

	mountPath := "/var/lib/mysql"

	env := []string{
		"MYSQL_ROOT_PASSWORD=" + in.RootPassword,
	}

	diskName := in.DiskName

	image := in.VariantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}

	poolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		Name:             sharedServerName,
		Image:            image,
		Env:              env,
		Ports:            mysqlContainerPorts(),
		DesiredInstances: 1,
		Labels:           labels,
		SandboxPrefix:    "my-shared",
		// MySQL 8 first-boot cold init runs initialize, a temporary setup
		// server, then the real server — ~20s on loaded dev hardware. 60s
		// gives ~3x headroom over the default 15s port-bind budget.
		PortWaitTimeout: "60s",
		Mounts: []compute_v1alpha.SandboxSpecContainerMount{
			{Source: "mydata", Destination: mountPath},
		},
		Volumes: []compute_v1alpha.SandboxSpecVolume{
			{
				Name:         "mydata",
				Provider:     "miren",
				DiskName:     diskName,
				MountPath:    mountPath,
				SizeGb:       sharedDefaultStorageGb,
				Filesystem:   "ext4",
				LeaseTimeout: "5m",
			},
		},
	})
	if err != nil {
		return CreateSharedPoolOut{}, fmt.Errorf("creating shared pool: %w", err)
	}

	return CreateSharedPoolOut{PoolID: poolID}, nil
}

func UndoCreateSharedPool(ctx context.Context, in CreateSharedPoolIn, out CreateSharedPoolOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.DeleteSandboxPool(ctx, out.PoolID)
}

type ActivateSharedServerIn struct {
	ServerID     entity.Id
	PoolID       entity.Id
	ServiceID    entity.Id
	RootPassword string
	DiskName     string
	ServiceHost  string
}

type ActivateSharedServerOut struct {
	Activated bool
}

func ActivateSharedServer(ctx context.Context, in ActivateSharedServerIn) (ActivateSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MysqlServer{
		AddonName:        AddonName,
		Variant:          "shared",
		Status:           "active",
		AssociationCount: 0,
		RootPassword:     in.RootPassword,
		DiskName:         in.DiskName,
		SandboxPool:      in.PoolID,
		Service:          in.ServiceID,
	}
	server.ID = in.ServerID

	if err := fw.EC.Update(ctx, server); err != nil {
		return ActivateSharedServerOut{}, fmt.Errorf("activating shared server: %w", err)
	}

	return ActivateSharedServerOut{Activated: true}, nil
}

func UndoActivateSharedServer(ctx context.Context, in ActivateSharedServerIn, out ActivateSharedServerOut) error {
	return nil
}

func RegisterEnsureSharedServerSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, SharedServerName: sharedServerName, Port: mysqlPort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("ensure-shared-mysql-server").
		Using(fw).
		Using(cfg).
		Action(CreateSharedServerEntity).Undo(UndoCreateSharedServerEntity).
		Action(CreateSharedPool).Undo(UndoCreateSharedPool).
		Action(dbsaga.WaitForSharedPool).Undo(dbsaga.UndoWaitForSharedPool).
		Action(dbsaga.CreateSharedService).Undo(dbsaga.UndoCreateSharedService).
		Action(dbsaga.WaitForSharedService).Undo(dbsaga.UndoWaitForSharedService).
		Action(ActivateSharedServer).Undo(UndoActivateSharedServer).
		RegisterTo(registry)
}

// --- Shared Provisioning Saga Actions ---

type FindOrCreateSharedServerIn struct {
	AppName       string
	VariantConfig map[string]string
}

type FindOrCreateSharedServerOut struct {
	ServerID     entity.Id
	RootPassword string
	ServiceHost  string
}

func isNotExist(err error) bool {
	return errors.Is(err, cond.ErrNotFound{})
}

func cleanupStaleSharedServer(fw *addon.ProviderFramework, ctx context.Context, server *addon_v1alpha.MysqlServer) error {
	if server.Service != "" {
		if err := fw.DeleteService(ctx, server.Service); err != nil && !isNotExist(err) {
			return fmt.Errorf("deleting stale shared service: %w", err)
		}
	}
	if server.SandboxPool != "" {
		if err := fw.DeleteSandboxPool(ctx, server.SandboxPool); err != nil && !isNotExist(err) {
			return fmt.Errorf("deleting stale shared pool: %w", err)
		}
	}
	if server.DiskName != "" || server.RootPassword != "" {
		diskName := resolveSharedDiskName(server)
		if err := fw.DeleteDiskByName(ctx, diskName); err != nil {
			return fmt.Errorf("deleting stale shared data disk: %w", err)
		}
	}
	return fw.EC.Delete(ctx, server.ID)
}

func FindOrCreateSharedServer(ctx context.Context, in FindOrCreateSharedServerIn) (FindOrCreateSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MysqlServer
	err := fw.EC.Get(ctx, sharedServerName, &server)
	if err == nil {
		switch server.Status {
		case "active":
			// Back-fill disk_name for servers provisioned before it was tracked,
			// recording the current (password-derived) name so the disk identity
			// stops depending on the root password. This must land before any
			// password rotation, or the rotation would move the disk.
			if server.DiskName == "" {
				legacy := sharedDiskNameForPassword(server.RootPassword)
				if err := fw.EC.Patch(ctx, server.ID, 0,
					entity.String(addon_v1alpha.MysqlServerDiskNameId, legacy),
				); err != nil {
					fw.Log.Warn("backfilling shared server disk_name failed",
						"server", server.ID, "error", err)
				} else {
					server.DiskName = legacy
				}
			}

			serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
			if err != nil {
				if !errors.Is(err, cond.ErrNotFound{}) {
					return FindOrCreateSharedServerOut{}, fmt.Errorf("resolving existing shared service address: %w", err)
				}
				fw.Log.Warn("shared server active but service gone, cleaning up stale server",
					"server", server.ID, "error", err)
				if delErr := cleanupStaleSharedServer(fw, ctx, &server); delErr != nil {
					return FindOrCreateSharedServerOut{}, fmt.Errorf("cleaning up stale shared server: %w", delErr)
				}
				break
			}
			requestedImage := in.VariantConfig[addon.ConfigImage]
			if server.Image != "" && requestedImage != "" && server.Image != requestedImage {
				return FindOrCreateSharedServerOut{}, fmt.Errorf(
					"shared MySQL server is running %s but version %s was requested; shared servers do not support mixed versions",
					server.Image, requestedImage)
			}
			return FindOrCreateSharedServerOut{
				ServerID:     server.ID,
				RootPassword: server.RootPassword,
				ServiceHost:  serviceHost,
			}, nil
		default:
			resp, lookupErr := fw.EAC.Get(ctx, server.ID.String())
			if lookupErr != nil {
				return FindOrCreateSharedServerOut{}, fmt.Errorf("looking up shared server entity: %w", lookupErr)
			}
			age := time.Since(resp.Entity().Entity().GetCreatedAt())
			if age < 10*time.Minute {
				return FindOrCreateSharedServerOut{}, fmt.Errorf("shared server exists but has status %q (age %s); retry later", server.Status, age.Truncate(time.Second))
			}
			fw.Log.Warn("shared server stale, cleaning up",
				"server", server.ID, "status", server.Status, "age", age.Truncate(time.Second))
			if delErr := cleanupStaleSharedServer(fw, ctx, &server); delErr != nil {
				return FindOrCreateSharedServerOut{}, fmt.Errorf("cleaning up stale shared server: %w", delErr)
			}
		}
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return FindOrCreateSharedServerOut{}, fmt.Errorf("looking up shared server: %w", err)
	}

	rootPassword := idgen.Gen("rt")
	diskName := newSharedDiskName()

	result, err := saga.RunNested(ctx, "ensure-shared-mysql-server",
		saga.WithNestedInput("rootpassword", rootPassword),
		saga.WithNestedInput("diskname", diskName),
		saga.WithNestedInput("variantconfig", in.VariantConfig),
	)
	if err != nil {
		return FindOrCreateSharedServerOut{}, fmt.Errorf("ensuring shared server: %w", err)
	}

	var serverID entity.Id
	if err := result.Get("serverid", &serverID); err != nil {
		return FindOrCreateSharedServerOut{}, fmt.Errorf("reading server ID from nested result: %w", err)
	}

	var serviceHost string
	if err := result.Get("servicehost", &serviceHost); err != nil {
		return FindOrCreateSharedServerOut{}, fmt.Errorf("reading service host from nested result: %w", err)
	}

	return FindOrCreateSharedServerOut{
		ServerID:     serverID,
		RootPassword: rootPassword,
		ServiceHost:  serviceHost,
	}, nil
}

func UndoFindOrCreateSharedServer(ctx context.Context, in FindOrCreateSharedServerIn, out FindOrCreateSharedServerOut) error {
	return nil
}

type GenerateSharedCredentialsIn struct {
	AppName string
}

type GenerateSharedCredentialsOut struct {
	SharedPassword          string
	SharedDatabaseName      string
	GeneratedSharedUsername string
}

func GenerateSharedCredentials(ctx context.Context, in GenerateSharedCredentialsIn) (GenerateSharedCredentialsOut, error) {
	return GenerateSharedCredentialsOut{
		SharedPassword:          idgen.Gen("pw"),
		SharedDatabaseName:      sanitizeIdentifier(in.AppName),
		GeneratedSharedUsername: sanitizeIdentifier(in.AppName),
	}, nil
}

func UndoGenerateSharedCredentials(ctx context.Context, in GenerateSharedCredentialsIn, out GenerateSharedCredentialsOut) error {
	return nil
}

type CreateSharedUserIn struct {
	ServiceHost             string
	RootPassword            string
	GeneratedSharedUsername string
	SharedPassword          string
}

type CreateSharedUserOut struct {
	SharedUsername string
}

func CreateSharedUser(ctx context.Context, in CreateSharedUserIn) (CreateSharedUserOut, error) {
	db, err := connectAsRoot(ctx, in.ServiceHost, in.RootPassword)
	if err != nil {
		return CreateSharedUserOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer db.Close()

	if err := createMysqlUser(ctx, db, in.GeneratedSharedUsername, in.SharedPassword); err != nil {
		return CreateSharedUserOut{}, err
	}

	return CreateSharedUserOut{SharedUsername: in.GeneratedSharedUsername}, nil
}

func UndoCreateSharedUser(ctx context.Context, in CreateSharedUserIn, out CreateSharedUserOut) error {
	if out.SharedUsername == "" {
		return nil
	}

	db, err := connectAsRoot(ctx, in.ServiceHost, in.RootPassword)
	if err != nil {
		return fmt.Errorf("connecting for user cleanup: %w", err)
	}
	defer db.Close()

	return dropMysqlUser(ctx, db, in.GeneratedSharedUsername)
}

type CreateSharedDatabaseIn struct {
	ServiceHost        string
	RootPassword       string
	SharedDatabaseName string
	SharedUsername     string
}

type CreateSharedDatabaseOut struct {
	DatabaseCreated bool
}

func CreateSharedDatabase(ctx context.Context, in CreateSharedDatabaseIn) (CreateSharedDatabaseOut, error) {
	db, err := connectAsRoot(ctx, in.ServiceHost, in.RootPassword)
	if err != nil {
		return CreateSharedDatabaseOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer db.Close()

	if err := createMysqlDatabase(ctx, db, in.SharedDatabaseName, in.SharedUsername); err != nil {
		return CreateSharedDatabaseOut{}, err
	}

	return CreateSharedDatabaseOut{DatabaseCreated: true}, nil
}

func UndoCreateSharedDatabase(ctx context.Context, in CreateSharedDatabaseIn, out CreateSharedDatabaseOut) error {
	if !out.DatabaseCreated {
		return nil
	}

	db, err := connectAsRoot(ctx, in.ServiceHost, in.RootPassword)
	if err != nil {
		return fmt.Errorf("connecting for database cleanup: %w", err)
	}
	defer db.Close()

	return dropMysqlDatabase(ctx, db, in.SharedDatabaseName)
}

type BuildSharedResultIn struct {
	ServerID           entity.Id
	ServiceHost        string
	SharedDatabaseName string
	SharedUsername     string
	SharedPassword     string
}

type BuildSharedResultOut struct {
	Done bool
}

func BuildSharedResult(ctx context.Context, in BuildSharedResultIn) (BuildSharedResultOut, error) {
	rc := saga.Get[*resultCapture](ctx)

	envVars := buildEnvVars(in.ServiceHost, mysqlPort, in.SharedUsername, in.SharedPassword, in.SharedDatabaseName)

	sharedData := &addon_v1alpha.MysqlSharedData{
		MysqlServer:  in.ServerID,
		DatabaseName: in.SharedDatabaseName,
		Username:     in.SharedUsername,
	}

	rc.Result = &addon.ProvisionResult{
		EnvVars: envVars,
		Attrs:   sharedData.Encode(),
	}

	return BuildSharedResultOut{Done: true}, nil
}

func UndoBuildSharedResult(ctx context.Context, in BuildSharedResultIn, out BuildSharedResultOut) error {
	return nil
}

func RegisterSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *resultCapture) error {
	if err := RegisterEnsureSharedServerSaga(registry, fw); err != nil {
		return err
	}

	cfg := &dbsaga.AddonConfig{AddonName: AddonName, SharedServerName: sharedServerName, Port: mysqlPort, ReadyTimeout: poolReadyTimeout}
	b := saga.Define("provision-shared-mysql").
		Using(fw).
		Using(rc).
		Using(cfg)
	saga.UsingAs[dbsaga.ServerCounter](b, mysqlServerCounter{})
	return b.
		Action(FindOrCreateSharedServer).Undo(UndoFindOrCreateSharedServer).
		Action(GenerateSharedCredentials).Undo(UndoGenerateSharedCredentials).
		Action(CreateSharedUser).Undo(UndoCreateSharedUser).
		Action(CreateSharedDatabase).Undo(UndoCreateSharedDatabase).
		Action(dbsaga.IncrementAssociationCount).Undo(dbsaga.UndoIncrementAssociationCount).
		Action(BuildSharedResult).Undo(UndoBuildSharedResult).
		RegisterTo(registry)
}

// --- Shared Deprovisioning Saga Actions ---

type DecodeSharedAttrsIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type DecodeSharedAttrsOut struct {
	SharedServerRef entity.Id
	SharedDbName    string
	SharedUserName  string
}

func DecodeSharedAttrs(ctx context.Context, in DecodeSharedAttrsIn) (DecodeSharedAttrsOut, error) {
	var data addon_v1alpha.MysqlSharedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}

	if data.MysqlServer == "" {
		return DecodeSharedAttrsOut{}, fmt.Errorf("no mysql server ref found")
	}

	return DecodeSharedAttrsOut{
		SharedServerRef: data.MysqlServer,
		SharedDbName:    data.DatabaseName,
		SharedUserName:  data.Username,
	}, nil
}

func UndoDecodeSharedAttrs(ctx context.Context, in DecodeSharedAttrsIn, out DecodeSharedAttrsOut) error {
	return nil
}

type LookupSharedServerIn struct {
	SharedServerRef entity.Id
}

type LookupSharedServerOut struct {
	SharedRootPassword string
	SharedDiskName     string
	SharedServiceRef   entity.Id
	SharedPoolRef      entity.Id
	SharedAssocCount   int64
	SharedServiceHost  string
}

func LookupSharedServer(ctx context.Context, in LookupSharedServerIn) (LookupSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MysqlServer
	if err := fw.EC.GetById(ctx, in.SharedServerRef, &server); err != nil {
		return LookupSharedServerOut{}, fmt.Errorf("looking up shared server: %w", err)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LookupSharedServerOut{}, fmt.Errorf("resolving shared service address: %w", err)
	}

	return LookupSharedServerOut{
		SharedRootPassword: server.RootPassword,
		SharedDiskName:     resolveSharedDiskName(&server),
		SharedServiceRef:   server.Service,
		SharedPoolRef:      server.SandboxPool,
		SharedAssocCount:   server.AssociationCount,
		SharedServiceHost:  serviceHost,
	}, nil
}

func UndoLookupSharedServer(ctx context.Context, in LookupSharedServerIn, out LookupSharedServerOut) error {
	return nil
}

type TerminateConnectionsIn struct {
	SharedServiceHost  string
	SharedRootPassword string
	SharedDbName       string
}

type TerminateConnectionsOut struct {
	ConnectionsTerminated bool
}

func TerminateConnections(ctx context.Context, in TerminateConnectionsIn) (TerminateConnectionsOut, error) {
	db, err := connectAsRoot(ctx, in.SharedServiceHost, in.SharedRootPassword)
	if err != nil {
		return TerminateConnectionsOut{}, fmt.Errorf("connecting to terminate connections: %w", err)
	}
	defer db.Close()

	if err := terminateMysqlConnections(ctx, db, in.SharedDbName); err != nil {
		return TerminateConnectionsOut{}, err
	}

	return TerminateConnectionsOut{ConnectionsTerminated: true}, nil
}

func UndoTerminateConnections(ctx context.Context, in TerminateConnectionsIn, out TerminateConnectionsOut) error {
	return nil
}

type DropSharedDatabaseIn struct {
	SharedServiceHost     string
	SharedRootPassword    string
	SharedDbName          string
	ConnectionsTerminated bool
}

type DropSharedDatabaseOut struct {
	DatabaseDropped bool
}

func DropSharedDatabase(ctx context.Context, in DropSharedDatabaseIn) (DropSharedDatabaseOut, error) {
	db, err := connectAsRoot(ctx, in.SharedServiceHost, in.SharedRootPassword)
	if err != nil {
		return DropSharedDatabaseOut{}, fmt.Errorf("connecting to drop database: %w", err)
	}
	defer db.Close()

	if err := dropMysqlDatabase(ctx, db, in.SharedDbName); err != nil {
		return DropSharedDatabaseOut{}, err
	}

	return DropSharedDatabaseOut{DatabaseDropped: true}, nil
}

func UndoDropSharedDatabase(ctx context.Context, in DropSharedDatabaseIn, out DropSharedDatabaseOut) error {
	return nil
}

type DropSharedUserIn struct {
	SharedServiceHost     string
	SharedRootPassword    string
	SharedUserName        string
	ConnectionsTerminated bool
}

type DropSharedUserOut struct {
	UserDropped bool
}

func DropSharedUser(ctx context.Context, in DropSharedUserIn) (DropSharedUserOut, error) {
	db, err := connectAsRoot(ctx, in.SharedServiceHost, in.SharedRootPassword)
	if err != nil {
		return DropSharedUserOut{}, fmt.Errorf("connecting to drop user: %w", err)
	}
	defer db.Close()

	if err := dropMysqlUser(ctx, db, in.SharedUserName); err != nil {
		return DropSharedUserOut{}, err
	}

	return DropSharedUserOut{UserDropped: true}, nil
}

func UndoDropSharedUser(ctx context.Context, in DropSharedUserIn, out DropSharedUserOut) error {
	return nil
}

type CleanupSharedServerIn struct {
	SharedServerRef  entity.Id
	SharedServiceRef entity.Id
	SharedPoolRef    entity.Id
	SharedDiskName   string
	RemainingCount   int64
}

type CleanupSharedServerOut struct {
	CleanedUp bool
}

func CleanupSharedServer(ctx context.Context, in CleanupSharedServerIn) (CleanupSharedServerOut, error) {
	if in.RemainingCount > 0 {
		return CleanupSharedServerOut{CleanedUp: false}, nil
	}

	fw := saga.Get[*addon.ProviderFramework](ctx)

	if in.SharedServiceRef != "" {
		if err := fw.DeleteService(ctx, in.SharedServiceRef); err != nil {
			return CleanupSharedServerOut{}, fmt.Errorf("deleting shared service: %w", err)
		}
	}

	if in.SharedPoolRef != "" {
		if err := fw.DeleteSandboxPool(ctx, in.SharedPoolRef); err != nil {
			return CleanupSharedServerOut{}, fmt.Errorf("deleting shared pool: %w", err)
		}
	}

	if in.SharedDiskName != "" {
		if err := fw.DeleteDiskByName(ctx, in.SharedDiskName); err != nil {
			return CleanupSharedServerOut{}, fmt.Errorf("deleting shared data disk %s: %w", in.SharedDiskName, err)
		}
	}

	if err := fw.EC.Delete(ctx, in.SharedServerRef); err != nil {
		return CleanupSharedServerOut{}, fmt.Errorf("deleting shared server: %w", err)
	}

	return CleanupSharedServerOut{CleanedUp: true}, nil
}

func UndoCleanupSharedServer(ctx context.Context, in CleanupSharedServerIn, out CleanupSharedServerOut) error {
	return nil
}

func RegisterDeprovisionSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	b := saga.Define("deprovision-shared-mysql").
		Using(fw)
	saga.UsingAs[dbsaga.ServerCounter](b, mysqlServerCounter{})
	return b.
		Action(DecodeSharedAttrs).Undo(UndoDecodeSharedAttrs).
		Action(LookupSharedServer).Undo(UndoLookupSharedServer).
		Action(TerminateConnections).Undo(UndoTerminateConnections).
		Action(DropSharedDatabase).Undo(UndoDropSharedDatabase).
		Action(DropSharedUser).Undo(UndoDropSharedUser).
		Action(dbsaga.DecrementAssociationCount).Undo(dbsaga.UndoDecrementAssociationCount).
		Action(CleanupSharedServer).Undo(UndoCleanupSharedServer).
		RegisterTo(registry)
}

func (p *Provider) provisionShared(ctx context.Context, assoc addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.Log.Info("provisioning shared MySQL",
		"app", app.Name,
		"variant", variant.Name)

	rc := &resultCapture{}
	registry := saga.NewRegistry()

	if err := RegisterSharedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering shared saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("provision-shared-mysql").
		WithID(addon.ProvisionExecutionID(assoc.ID)).
		Input("appname", app.Name).
		Input("variantconfig", variant.Config).
		Execute(ctx)
	if err != nil {
		return nil, err
	}

	if rc.Result == nil {
		return nil, fmt.Errorf("saga completed but no result was captured")
	}

	p.Log.Info("shared MySQL provisioned", "app", app.Name)
	return rc.Result, nil
}

func (p *Provider) deprovisionShared(ctx context.Context, assoc addon.AddonAssociation) error {
	p.Log.Info("deprovisioning shared MySQL", "assoc", assoc.ID)

	registry := saga.NewRegistry()

	if err := RegisterDeprovisionSharedSaga(registry, p.Fw); err != nil {
		return fmt.Errorf("registering deprovision saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("deprovision-shared-mysql").
		WithID(addon.DeprovisionExecutionID(assoc.ID)).
		Input("assocentity", assoc.Entity).
		Execute(ctx)
	if err != nil {
		return err
	}

	p.Log.Info("shared MySQL deprovisioned", "assoc", assoc.ID)
	return nil
}
