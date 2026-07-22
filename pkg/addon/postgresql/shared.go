package postgresql

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
	sharedServerName = "pg-shared"
	sharedDiskName   = "pg-shared-data"
	poolReadyTimeout = 5 * time.Minute
)

// --- EnsureSharedServerSaga Actions ---
//
// When no active shared server exists, this saga creates one:
//   step 1: Create PostgresServer entity       → compensate: Delete entity
//   step 2: Create SandboxPool                 → compensate: Delete pool
//   step 3: Wait for pool sandbox to reach RUNNING
//   step 4: Create Service                     → compensate: Delete service
//   step 5: Wait for service address
//   step 6: Activate PostgresServer entity (set refs, status: active)
//
// The saga captures its output into ensureServerCapture so the calling
// action can return ServerID, SuperuserPassword, and ServiceHost.

type CreateSharedServerEntityIn struct {
	SuperuserPassword string
	DiskName          string
	VariantConfig     map[string]string
}

type CreateSharedServerEntityOut struct {
	ServerID entity.Id
}

func CreateSharedServerEntity(ctx context.Context, in CreateSharedServerEntityIn) (CreateSharedServerEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.PostgresServer{
		AddonName:         AddonName,
		Variant:           "shared",
		Image:             in.VariantConfig[addon.ConfigImage],
		Status:            "provisioning",
		AssociationCount:  0,
		SuperuserPassword: in.SuperuserPassword,
		DiskName:          in.DiskName,
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
	SuperuserPassword string
	DiskName          string
	VariantConfig     map[string]string
}

type CreateSharedPoolOut struct {
	PoolID entity.Id
}

// sharedDiskNameForPassword derives the legacy disk name from the superuser
// password. New servers no longer use this: they store an explicit disk_name
// generated independently of the password (see newSharedDiskName). It survives
// only to reproduce — and back-fill — the name of servers provisioned before
// disk_name was tracked. Deriving the disk identity from the password is what
// made the password effectively immutable, since rotating it moved the disk out
// from under the existing data.
func sharedDiskNameForPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return sharedDiskName + "-" + hex.EncodeToString(h[:4])
}

// newSharedDiskName generates a fresh, unique disk name for a new shared-server
// generation. Uniqueness — so a re-created server never attaches a prior
// generation's not-yet-deleted disk — now comes from a random nonce rather than
// the superuser password, leaving the password free to rotate without moving
// the disk identity.
func newSharedDiskName() string {
	h := sha256.Sum256([]byte(idgen.Gen("pgdisk")))
	return sharedDiskName + "-" + hex.EncodeToString(h[:4])
}

// resolveSharedDiskName returns the server's stored disk name, falling back to
// the legacy password-derived name for servers provisioned before disk_name was
// tracked.
func resolveSharedDiskName(server *addon_v1alpha.PostgresServer) string {
	if server.DiskName != "" {
		return server.DiskName
	}
	return sharedDiskNameForPassword(server.SuperuserPassword)
}

func CreateSharedPool(ctx context.Context, in CreateSharedPoolIn) (CreateSharedPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	labels := types.LabelSet(
		"addon", AddonName,
		"server", sharedServerName,
		"shared", "true",
	)

	mountPath := "/var/lib/postgresql/data"

	env := []string{
		"POSTGRES_PASSWORD=" + in.SuperuserPassword,
		"PGDATA=" + mountPath + "/pgdata",
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
		Ports:            postgresContainerPorts(),
		DesiredInstances: 1,
		Labels:           labels,
		SandboxPrefix:    "pg-shared",
		// Postgres first-boot runs initdb before binding; can exceed the
		// default 15s on loaded dev hardware. 60s matches the MySQL budget.
		PortWaitTimeout: "60s",
		Mounts: []compute_v1alpha.SandboxSpecContainerMount{
			{Source: "pgdata", Destination: mountPath},
		},
		Volumes: []compute_v1alpha.SandboxSpecVolume{
			{
				Name:         "pgdata",
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
	ServerID          entity.Id
	PoolID            entity.Id
	ServiceID         entity.Id
	SuperuserPassword string
	DiskName          string
	ServiceHost       string
}

type ActivateSharedServerOut struct {
	Activated bool
}

func ActivateSharedServer(ctx context.Context, in ActivateSharedServerIn) (ActivateSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.PostgresServer{
		AddonName:         AddonName,
		Variant:           "shared",
		Status:            "active",
		AssociationCount:  0,
		SuperuserPassword: in.SuperuserPassword,
		DiskName:          in.DiskName,
		SandboxPool:       in.PoolID,
		Service:           in.ServiceID,
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

// RegisterEnsureSharedServerSaga registers the saga that creates the shared
// PostgreSQL server infrastructure (entity, pool, service).
func RegisterEnsureSharedServerSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, SharedServerName: sharedServerName, Port: postgresPort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("ensure-shared-server").
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

// Step 1: Find or create the shared PostgresServer.
// If no active shared server exists, executes the EnsureSharedServerSaga
// as a nested saga to create the server entity, sandbox pool, and service.

type FindOrCreateSharedServerIn struct {
	AppName       string
	VariantConfig map[string]string
}

type FindOrCreateSharedServerOut struct {
	ServerID          entity.Id
	SuperuserPassword string
	ServiceHost       string
}

// isNotExist returns true if the error indicates the entity doesn't exist.
func isNotExist(err error) bool {
	return errors.Is(err, cond.ErrNotFound{})
}

// cleanupStaleSharedServer removes a shared server and its infrastructure
// (pool, service, disk, entity) so a fresh one can be created. Not-found
// errors are ignored (the resource may already be gone); other errors abort
// cleanup to preserve the server entity for retry.
func cleanupStaleSharedServer(fw *addon.ProviderFramework, ctx context.Context, server *addon_v1alpha.PostgresServer) error {
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
	if server.DiskName != "" || server.SuperuserPassword != "" {
		diskName := resolveSharedDiskName(server)
		if err := fw.DeleteDiskByName(ctx, diskName); err != nil {
			return fmt.Errorf("deleting stale shared data disk: %w", err)
		}
	}
	return fw.EC.Delete(ctx, server.ID)
}

func FindOrCreateSharedServer(ctx context.Context, in FindOrCreateSharedServerIn) (FindOrCreateSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	// Try to find an existing shared server
	var server addon_v1alpha.PostgresServer
	err := fw.EC.Get(ctx, sharedServerName, &server)
	if err == nil {
		switch server.Status {
		case "active":
			// Back-fill disk_name for servers provisioned before it was tracked,
			// recording the current (password-derived) name so the disk identity
			// stops depending on the superuser password. This must land before
			// any password rotation, or the rotation would move the disk.
			if server.DiskName == "" {
				legacy := sharedDiskNameForPassword(server.SuperuserPassword)
				if err := fw.EC.Patch(ctx, server.ID, 0,
					entity.String(addon_v1alpha.PostgresServerDiskNameId, legacy),
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
					// Transient error (e.g. no IP assigned yet) — return so the
					// controller retries rather than destroying a live server.
					return FindOrCreateSharedServerOut{}, fmt.Errorf("resolving existing shared service address: %w", err)
				}
				// Service entity is gone — clean up the stale server so the
				// nested saga creates fresh infrastructure.
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
					"shared PostgreSQL server is running %s but version %s was requested; shared servers do not support mixed versions",
					server.Image, requestedImage)
			}
			return FindOrCreateSharedServerOut{
				ServerID:          server.ID,
				SuperuserPassword: server.SuperuserPassword,
				ServiceHost:       serviceHost,
			}, nil
		default:
			// Server in a non-terminal state (e.g. "provisioning"). This could
			// be a concurrent provision in progress or a stuck previous attempt.
			// Only clean up if the entity is old enough to be considered stale.
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

	// No shared server found (or stale one was removed) — create via nested saga.
	superuserPassword := idgen.Gen("su")
	diskName := newSharedDiskName()

	result, err := saga.RunNested(ctx, "ensure-shared-server",
		saga.WithNestedInput("superuserpassword", superuserPassword),
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
		ServerID:          serverID,
		SuperuserPassword: superuserPassword,
		ServiceHost:       serviceHost,
	}, nil
}

func UndoFindOrCreateSharedServer(ctx context.Context, in FindOrCreateSharedServerIn, out FindOrCreateSharedServerOut) error {
	// The shared server is intentionally not torn down if a later provisioning
	// step fails — it may be serving other applications. The EnsureSharedServerSaga
	// handles its own compensations if server creation fails.
	return nil
}

// Step 2: Generate credentials for the app's database.

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

// Step 3: Connect to the shared server and CREATE USER.

type CreateSharedUserIn struct {
	ServiceHost             string
	SuperuserPassword       string
	GeneratedSharedUsername string
	SharedPassword          string
}

type CreateSharedUserOut struct {
	SharedUsername string
}

func CreateSharedUser(ctx context.Context, in CreateSharedUserIn) (CreateSharedUserOut, error) {
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return CreateSharedUserOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer conn.Close(ctx)

	if err := createPostgresUser(ctx, conn, in.GeneratedSharedUsername, in.SharedPassword); err != nil {
		return CreateSharedUserOut{}, err
	}

	return CreateSharedUserOut{SharedUsername: in.GeneratedSharedUsername}, nil
}

func UndoCreateSharedUser(ctx context.Context, in CreateSharedUserIn, out CreateSharedUserOut) error {
	if out.SharedUsername == "" {
		return nil
	}

	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return fmt.Errorf("connecting for user cleanup: %w", err)
	}
	defer conn.Close(ctx)

	return dropPostgresUser(ctx, conn, in.GeneratedSharedUsername)
}

// Step 4: Connect to the shared server and CREATE DATABASE.

type CreateSharedDatabaseIn struct {
	ServiceHost        string
	SuperuserPassword  string
	SharedDatabaseName string
	SharedUsername     string
}

type CreateSharedDatabaseOut struct {
	DatabaseCreated bool
}

func CreateSharedDatabase(ctx context.Context, in CreateSharedDatabaseIn) (CreateSharedDatabaseOut, error) {
	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return CreateSharedDatabaseOut{}, fmt.Errorf("connecting to shared server: %w", err)
	}
	defer conn.Close(ctx)

	if err := createPostgresDatabase(ctx, conn, in.SharedDatabaseName, in.SharedUsername); err != nil {
		return CreateSharedDatabaseOut{}, err
	}

	return CreateSharedDatabaseOut{DatabaseCreated: true}, nil
}

func UndoCreateSharedDatabase(ctx context.Context, in CreateSharedDatabaseIn, out CreateSharedDatabaseOut) error {
	if !out.DatabaseCreated {
		return nil
	}

	conn, err := connectAsSuperuser(ctx, in.ServiceHost, in.SuperuserPassword)
	if err != nil {
		return fmt.Errorf("connecting for database cleanup: %w", err)
	}
	defer conn.Close(ctx)

	return dropPostgresDatabase(ctx, conn, in.SharedDatabaseName)
}

// Step 5 (IncrementAssociationCount) is in dbsaga.

// Step 6: Build the ProvisionResult.

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

	envVars := buildEnvVars(in.ServiceHost, postgresPort, in.SharedUsername, in.SharedPassword, in.SharedDatabaseName)

	sharedData := &addon_v1alpha.PostgresqlSharedData{
		PostgresServer: in.ServerID,
		DatabaseName:   in.SharedDatabaseName,
		Username:       in.SharedUsername,
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

// RegisterSharedSaga registers the shared PostgreSQL provisioning saga.
// This also registers the nested ensure-shared-server saga in the same registry.
func RegisterSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *resultCapture) error {
	if err := RegisterEnsureSharedServerSaga(registry, fw); err != nil {
		return err
	}

	cfg := &dbsaga.AddonConfig{AddonName: AddonName, SharedServerName: sharedServerName, Port: postgresPort, ReadyTimeout: poolReadyTimeout}
	b := saga.Define("provision-shared-postgresql").
		Using(fw).
		Using(rc).
		Using(cfg)
	saga.UsingAs[dbsaga.ServerCounter](b, pgServerCounter{})
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
	var data addon_v1alpha.PostgresqlSharedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}

	if data.PostgresServer == "" {
		return DecodeSharedAttrsOut{}, fmt.Errorf("no postgres server ref found")
	}

	return DecodeSharedAttrsOut{
		SharedServerRef: data.PostgresServer,
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
	SharedSuperuserPassword string
	SharedDiskName          string
	SharedServiceRef        entity.Id
	SharedPoolRef           entity.Id
	SharedAssocCount        int64
	SharedServiceHost       string
}

func LookupSharedServer(ctx context.Context, in LookupSharedServerIn) (LookupSharedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, in.SharedServerRef, &server); err != nil {
		return LookupSharedServerOut{}, fmt.Errorf("looking up shared server: %w", err)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LookupSharedServerOut{}, fmt.Errorf("resolving shared service address: %w", err)
	}

	return LookupSharedServerOut{
		SharedSuperuserPassword: server.SuperuserPassword,
		SharedDiskName:          resolveSharedDiskName(&server),
		SharedServiceRef:        server.Service,
		SharedPoolRef:           server.SandboxPool,
		SharedAssocCount:        server.AssociationCount,
		SharedServiceHost:       serviceHost,
	}, nil
}

func UndoLookupSharedServer(ctx context.Context, in LookupSharedServerIn, out LookupSharedServerOut) error {
	return nil
}

type TerminateConnectionsIn struct {
	SharedServiceHost       string
	SharedSuperuserPassword string
	SharedDbName            string
}

type TerminateConnectionsOut struct {
	ConnectionsTerminated bool
}

func TerminateConnections(ctx context.Context, in TerminateConnectionsIn) (TerminateConnectionsOut, error) {
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return TerminateConnectionsOut{}, fmt.Errorf("connecting to terminate connections: %w", err)
	}
	defer conn.Close(ctx)

	if err := terminatePostgresConnections(ctx, conn, in.SharedDbName); err != nil {
		return TerminateConnectionsOut{}, err
	}

	return TerminateConnectionsOut{ConnectionsTerminated: true}, nil
}

func UndoTerminateConnections(ctx context.Context, in TerminateConnectionsIn, out TerminateConnectionsOut) error {
	return nil
}

type DropSharedDatabaseIn struct {
	SharedServiceHost       string
	SharedSuperuserPassword string
	SharedDbName            string
	ConnectionsTerminated   bool
}

type DropSharedDatabaseOut struct {
	DatabaseDropped bool
}

func DropSharedDatabase(ctx context.Context, in DropSharedDatabaseIn) (DropSharedDatabaseOut, error) {
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return DropSharedDatabaseOut{}, fmt.Errorf("connecting to drop database: %w", err)
	}
	defer conn.Close(ctx)

	if err := dropPostgresDatabase(ctx, conn, in.SharedDbName); err != nil {
		return DropSharedDatabaseOut{}, err
	}

	return DropSharedDatabaseOut{DatabaseDropped: true}, nil
}

func UndoDropSharedDatabase(ctx context.Context, in DropSharedDatabaseIn, out DropSharedDatabaseOut) error {
	return nil
}

type DropSharedUserIn struct {
	SharedServiceHost       string
	SharedSuperuserPassword string
	SharedUserName          string
	ConnectionsTerminated   bool
}

type DropSharedUserOut struct {
	UserDropped bool
}

func DropSharedUser(ctx context.Context, in DropSharedUserIn) (DropSharedUserOut, error) {
	conn, err := connectAsSuperuser(ctx, in.SharedServiceHost, in.SharedSuperuserPassword)
	if err != nil {
		return DropSharedUserOut{}, fmt.Errorf("connecting to drop user: %w", err)
	}
	defer conn.Close(ctx)

	if err := dropPostgresUser(ctx, conn, in.SharedUserName); err != nil {
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

	// Delete the data disk so that a future shared server starts fresh. If this
	// fails, abort so the saga retries — once the server entity is gone we lose
	// the disk name needed to find it.
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

// RegisterDeprovisionSharedSaga registers the shared deprovisioning saga.
func RegisterDeprovisionSharedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	b := saga.Define("deprovision-shared-postgresql").
		Using(fw)
	saga.UsingAs[dbsaga.ServerCounter](b, pgServerCounter{})
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

func (p *Provider) provisionShared(ctx context.Context, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.Log.Info("provisioning shared PostgreSQL",
		"app", app.Name,
		"variant", variant.Name)

	rc := &resultCapture{}
	registry := saga.NewRegistry()

	if err := RegisterSharedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering shared saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("provision-shared-postgresql").
		Input("appname", app.Name).
		Input("variantconfig", variant.Config).
		Execute(ctx)
	if err != nil {
		return nil, err
	}

	if rc.Result == nil {
		return nil, fmt.Errorf("saga completed but no result was captured")
	}

	p.Log.Info("shared PostgreSQL provisioned", "app", app.Name)
	return rc.Result, nil
}

func (p *Provider) deprovisionShared(ctx context.Context, assoc addon.AddonAssociation) error {
	p.Log.Info("deprovisioning shared PostgreSQL", "assoc", assoc.ID)

	registry := saga.NewRegistry()

	if err := RegisterDeprovisionSharedSaga(registry, p.Fw); err != nil {
		return fmt.Errorf("registering deprovision saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("deprovision-shared-postgresql").
		Input("assocentity", assoc.Entity).
		Execute(ctx)
	if err != nil {
		return err
	}

	p.Log.Info("shared PostgreSQL deprovisioned", "assoc", assoc.ID)
	return nil
}
