package postgresql

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

const postgresPort = 5432

func postgresContainerPorts() []compute_v1alpha.SandboxSpecContainerPort {
	return []compute_v1alpha.SandboxSpecContainerPort{
		{
			Name:     "postgres",
			Port:     postgresPort,
			Protocol: compute_v1alpha.SandboxSpecContainerPortTCP,
		},
	}
}

// resultCapture is injected as a saga dependency so the final action
// can pass the ProvisionResult back to the caller.
type resultCapture struct {
	Result *addon.ProvisionResult
}

// --- Dedicated Provisioning Saga Actions ---

type GenerateCredentialsIn struct {
	AppName string
}

type GenerateCredentialsOut struct {
	Password     string
	DatabaseName string
	Username     string
	ServiceName  string
	ServerName   string
}

func GenerateCredentials(ctx context.Context, in GenerateCredentialsIn) (GenerateCredentialsOut, error) {
	return GenerateCredentialsOut{
		Password:     idgen.Gen("pw"),
		DatabaseName: sanitizeIdentifier(in.AppName),
		Username:     sanitizeIdentifier(in.AppName),
		ServiceName:  fmt.Sprintf("%s-postgresql", in.AppName),
		ServerName:   fmt.Sprintf("pg-%s-%s", in.AppName, idgen.Gen("s")),
	}, nil
}

func UndoGenerateCredentials(ctx context.Context, in GenerateCredentialsIn, out GenerateCredentialsOut) error {
	return nil
}

type CreatePostgresServerIn struct {
	ServerName  string
	VariantName string
	Password    string
}

type CreatePostgresServerOut struct {
	ServerID entity.Id
}

func CreatePostgresServer(ctx context.Context, in CreatePostgresServerIn) (CreatePostgresServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.PostgresServer{
		AddonName:         AddonName,
		Variant:           in.VariantName,
		Status:            "provisioning",
		AssociationCount:  1,
		SuperuserPassword: in.Password,
	}

	serverID, err := fw.EC.Create(ctx, in.ServerName, server)
	if err != nil {
		return CreatePostgresServerOut{}, fmt.Errorf("creating postgres server entity: %w", err)
	}

	return CreatePostgresServerOut{ServerID: serverID}, nil
}

func UndoCreatePostgresServer(ctx context.Context, in CreatePostgresServerIn, out CreatePostgresServerOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Delete(ctx, out.ServerID)
}

type CreateDedicatedPoolIn struct {
	ServerName    string
	AppName       string
	DatabaseName  string
	Username      string
	Password      string
	VariantConfig map[string]string
}

type CreateDedicatedPoolOut struct {
	PoolID entity.Id
}

func CreateDedicatedPool(ctx context.Context, in CreateDedicatedPoolIn) (CreateDedicatedPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	labels := types.LabelSet(
		"addon", AddonName,
		"app", in.AppName,
		"server", in.ServerName,
	)

	sizeGb := addon.ParseStorageGb(in.VariantConfig[ConfigStorage])
	diskName := fmt.Sprintf("pg-%s-data", in.ServerName)
	mountPath := "/var/lib/postgresql/data"

	env := []string{
		"POSTGRES_DB=" + in.DatabaseName,
		"POSTGRES_USER=" + in.Username,
		"POSTGRES_PASSWORD=" + in.Password,
		"PGDATA=" + mountPath + "/pgdata",
	}

	image := in.VariantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}

	poolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		Name:             in.ServerName,
		Image:            image,
		Env:              env,
		Ports:            postgresContainerPorts(),
		DesiredInstances: 1,
		Labels:           labels,
		SandboxPrefix:    fmt.Sprintf("%s-pg", in.AppName),
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
				SizeGb:       sizeGb,
				Filesystem:   "ext4",
				LeaseTimeout: "5m",
			},
		},
	})
	if err != nil {
		return CreateDedicatedPoolOut{}, fmt.Errorf("creating sandbox pool: %w", err)
	}

	return CreateDedicatedPoolOut{PoolID: poolID}, nil
}

func UndoCreateDedicatedPool(ctx context.Context, in CreateDedicatedPoolIn, out CreateDedicatedPoolOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.DeleteSandboxPool(ctx, out.PoolID); err != nil {
		return err
	}
	diskName := fmt.Sprintf("pg-%s-data", in.ServerName)
	return fw.DeleteDiskByName(ctx, diskName)
}

type UpdateDedicatedServerIn struct {
	ServerID    entity.Id
	PoolID      entity.Id
	ServiceID   entity.Id
	ServiceHost string
	VariantName string
	Password    string
}

type UpdateDedicatedServerOut struct {
	Updated bool
}

func UpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn) (UpdateDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.PostgresServer{
		AddonName:         AddonName,
		Variant:           in.VariantName,
		Status:            "active",
		AssociationCount:  1,
		SuperuserPassword: in.Password,
		SandboxPool:       in.PoolID,
		Service:           in.ServiceID,
	}
	server.ID = in.ServerID

	if err := fw.EC.Update(ctx, server); err != nil {
		return UpdateDedicatedServerOut{}, fmt.Errorf("updating postgres server: %w", err)
	}

	return UpdateDedicatedServerOut{Updated: true}, nil
}

func UndoUpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn, out UpdateDedicatedServerOut) error {
	return nil
}

type BuildDedicatedResultIn struct {
	ServiceHost  string
	Username     string
	Password     string
	DatabaseName string
	ServerID     entity.Id
}

type BuildDedicatedResultOut struct {
	Done bool
}

func BuildDedicatedResult(ctx context.Context, in BuildDedicatedResultIn) (BuildDedicatedResultOut, error) {
	rc := saga.Get[*resultCapture](ctx)

	host := in.ServiceHost
	envVars := buildEnvVars(host, postgresPort, in.Username, in.Password, in.DatabaseName)

	dedicatedData := &addon_v1alpha.PostgresqlDedicatedData{
		PostgresServer: in.ServerID,
		DatabaseName:   in.DatabaseName,
		Username:       in.Username,
	}

	rc.Result = &addon.ProvisionResult{
		EnvVars: envVars,
		Attrs:   dedicatedData.Encode(),
	}

	return BuildDedicatedResultOut{Done: true}, nil
}

func UndoBuildDedicatedResult(ctx context.Context, in BuildDedicatedResultIn, out BuildDedicatedResultOut) error {
	return nil
}

// RegisterDedicatedSaga registers the dedicated PostgreSQL provisioning saga.
func RegisterDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *resultCapture) error {
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, Port: postgresPort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("provision-dedicated-postgresql").
		Using(fw).
		Using(rc).
		Using(cfg).
		Action(GenerateCredentials).Undo(UndoGenerateCredentials).
		Action(CreatePostgresServer).Undo(UndoCreatePostgresServer).
		Action(CreateDedicatedPool).Undo(UndoCreateDedicatedPool).
		Action(dbsaga.WaitForDedicatedPool).Undo(dbsaga.UndoWaitForDedicatedPool).
		Action(dbsaga.CreateDedicatedService).Undo(dbsaga.UndoCreateDedicatedService).
		Action(dbsaga.WaitForDedicatedService).Undo(dbsaga.UndoWaitForDedicatedService).
		Action(UpdateDedicatedServer).Undo(UndoUpdateDedicatedServer).
		Action(BuildDedicatedResult).Undo(UndoBuildDedicatedResult).
		RegisterTo(registry)
}

// --- Dedicated Deprovisioning Saga Actions ---

type DecodeDedicatedAttrsIn struct {
	AssocEntity *entity.Entity `saga:"assocentity"`
}

type DecodeDedicatedAttrsOut struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

func DecodeDedicatedAttrs(ctx context.Context, in DecodeDedicatedAttrsIn) (DecodeDedicatedAttrsOut, error) {
	var data addon_v1alpha.PostgresqlDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}

	if data.PostgresServer == "" {
		return DecodeDedicatedAttrsOut{}, fmt.Errorf("no postgres server ref found")
	}

	return DecodeDedicatedAttrsOut{DedicatedServerID: data.PostgresServer}, nil
}

func UndoDecodeDedicatedAttrs(ctx context.Context, in DecodeDedicatedAttrsIn, out DecodeDedicatedAttrsOut) error {
	return nil
}

type LookupDedicatedServerIn struct {
	DedicatedServerID entity.Id
}

type LookupDedicatedServerOut struct {
	DedicatedServiceID  entity.Id
	DedicatedPoolID     entity.Id
	DedicatedServerName string
}

func LookupDedicatedServer(ctx context.Context, in LookupDedicatedServerIn) (LookupDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.PostgresServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LookupDedicatedServerOut{}, fmt.Errorf("looking up postgres server: %w", err)
	}

	// Get the entity name (used to derive the disk name for cleanup)
	var meta core_v1alpha.Metadata
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &meta); err != nil {
		return LookupDedicatedServerOut{}, fmt.Errorf("looking up server metadata: %w", err)
	}

	return LookupDedicatedServerOut{
		DedicatedServiceID:  server.Service,
		DedicatedPoolID:     server.SandboxPool,
		DedicatedServerName: meta.Name,
	}, nil
}

func UndoLookupDedicatedServer(ctx context.Context, in LookupDedicatedServerIn, out LookupDedicatedServerOut) error {
	return nil
}

type DeleteDedicatedServerEntityIn struct {
	DedicatedServerID   entity.Id
	DedicatedServerName string

	// PoolCleanedUp forces this action to run after DeleteDedicatedPool,
	// ensuring the server entity is deleted last.
	PoolCleanedUp saga.Edge `saga:"dedicated_pool_deleted"`
}

type DeleteDedicatedServerEntityOut struct {
	ServerDeleted bool
}

func DeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn) (DeleteDedicatedServerEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	// Delete the data disk before removing the server entity. If this fails,
	// abort so the saga retries — once the server entity is gone we lose the
	// stable name needed to derive the disk name.
	if in.DedicatedServerName != "" {
		diskName := fmt.Sprintf("pg-%s-data", in.DedicatedServerName)
		if err := fw.DeleteDiskByName(ctx, diskName); err != nil {
			return DeleteDedicatedServerEntityOut{}, fmt.Errorf("deleting dedicated data disk %s: %w", diskName, err)
		}
	}

	if err := fw.EC.Delete(ctx, in.DedicatedServerID); err != nil {
		return DeleteDedicatedServerEntityOut{}, fmt.Errorf("deleting postgres server: %w", err)
	}

	return DeleteDedicatedServerEntityOut{ServerDeleted: true}, nil
}

func UndoDeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn, out DeleteDedicatedServerEntityOut) error {
	return nil
}

// RegisterDeprovisionDedicatedSaga registers the dedicated deprovisioning saga.
func RegisterDeprovisionDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	return saga.Define("deprovision-dedicated-postgresql").
		Using(fw).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LookupDedicatedServer).Undo(UndoLookupDedicatedServer).
		Action(dbsaga.DeleteDedicatedService).Undo(dbsaga.UndoDeleteDedicatedService).
		Action(dbsaga.DeleteDedicatedPool).Undo(dbsaga.UndoDeleteDedicatedPool).
		Action(DeleteDedicatedServerEntity).Undo(UndoDeleteDedicatedServerEntity).
		RegisterTo(registry)
}

// maxPgIdentLen is the maximum length of a PostgreSQL identifier (NAMEDATALEN-1).
const maxPgIdentLen = 63

func sanitizeIdentifier(name string) string {
	return addon.SanitizeIdentifier(name, maxPgIdentLen)
}

func (p *Provider) provisionDedicated(ctx context.Context, assoc addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.Log.Info("provisioning dedicated PostgreSQL",
		"app", app.Name,
		"variant", variant.Name)

	rc := &resultCapture{}
	registry := saga.NewRegistry()

	if err := RegisterDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("provision-dedicated-postgresql").
		WithID(addon.ProvisionExecutionID(assoc.ID)).
		Input("appname", app.Name).
		Input("variantname", variant.Name).
		Input("variantconfig", variant.Config).
		Execute(ctx)
	if err != nil {
		return nil, err
	}

	if rc.Result == nil {
		return nil, fmt.Errorf("saga completed but no result was captured")
	}

	p.Log.Info("dedicated PostgreSQL provisioned", "app", app.Name)
	return rc.Result, nil
}

func (p *Provider) deprovisionDedicated(ctx context.Context, assoc addon.AddonAssociation) error {
	p.Log.Info("deprovisioning dedicated PostgreSQL", "assoc", assoc.ID)

	registry := saga.NewRegistry()

	if err := RegisterDeprovisionDedicatedSaga(registry, p.Fw); err != nil {
		return fmt.Errorf("registering deprovision saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("deprovision-dedicated-postgresql").
		WithID(addon.DeprovisionExecutionID(assoc.ID)).
		Input("assocentity", assoc.Entity).
		Execute(ctx)
	if err != nil {
		return err
	}

	p.Log.Info("dedicated PostgreSQL deprovisioned", "assoc", assoc.ID)
	return nil
}
