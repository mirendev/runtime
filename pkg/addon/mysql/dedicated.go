package mysql

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

const mysqlPort = 3306

func mysqlContainerPorts() []compute_v1alpha.SandboxSpecContainerPort {
	return []compute_v1alpha.SandboxSpecContainerPort{
		{
			Name:     "mysql",
			Port:     mysqlPort,
			Protocol: compute_v1alpha.SandboxSpecContainerPortTCP,
		},
	}
}

type resultCapture struct {
	Result *addon.ProvisionResult
}

// --- Dedicated Provisioning Saga Actions ---

type GenerateCredentialsIn struct {
	AppName string
}

type GenerateCredentialsOut struct {
	Password     string `saga:"password"`
	RootPassword string `saga:"rootpassword"`
	DatabaseName string `saga:"databasename"`
	Username     string `saga:"username"`
	ServiceName  string `saga:"servicename"`
	ServerName   string `saga:"servername"`
}

func GenerateCredentials(ctx context.Context, in GenerateCredentialsIn) (GenerateCredentialsOut, error) {
	return GenerateCredentialsOut{
		Password:     idgen.Gen("pw"),
		RootPassword: idgen.Gen("rt"),
		DatabaseName: sanitizeIdentifier(in.AppName),
		Username:     sanitizeIdentifier(in.AppName),
		ServiceName:  fmt.Sprintf("%s-mysql", in.AppName),
		ServerName:   fmt.Sprintf("my-%s-%s", in.AppName, idgen.Gen("s")),
	}, nil
}

func UndoGenerateCredentials(ctx context.Context, in GenerateCredentialsIn, out GenerateCredentialsOut) error {
	return nil
}

type CreateMysqlServerIn struct {
	ServerName   string `saga:"servername"`
	VariantName  string `saga:"variantname"`
	RootPassword string `saga:"rootpassword"`
}

type CreateMysqlServerOut struct {
	ServerID entity.Id `saga:"serverid"`
}

func CreateMysqlServer(ctx context.Context, in CreateMysqlServerIn) (CreateMysqlServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MysqlServer{
		AddonName:        AddonName,
		Variant:          in.VariantName,
		Status:           "provisioning",
		AssociationCount: 1,
		RootPassword:     in.RootPassword,
	}

	serverID, err := fw.EC.Create(ctx, in.ServerName, server)
	if err != nil {
		return CreateMysqlServerOut{}, fmt.Errorf("creating mysql server entity: %w", err)
	}

	return CreateMysqlServerOut{ServerID: serverID}, nil
}

func UndoCreateMysqlServer(ctx context.Context, in CreateMysqlServerIn, out CreateMysqlServerOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Delete(ctx, out.ServerID)
}

type CreateDedicatedPoolIn struct {
	ServerName    string            `saga:"servername"`
	AppName       string            `saga:"appname"`
	DatabaseName  string            `saga:"databasename"`
	Username      string            `saga:"username"`
	Password      string            `saga:"password"`
	RootPassword  string            `saga:"rootpassword"`
	VariantConfig map[string]string `saga:"variantconfig"`
}

type CreateDedicatedPoolOut struct {
	PoolID entity.Id `saga:"poolid"`
}

func CreateDedicatedPool(ctx context.Context, in CreateDedicatedPoolIn) (CreateDedicatedPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	labels := types.LabelSet(
		"addon", AddonName,
		"app", in.AppName,
		"server", in.ServerName,
	)

	sizeGb := addon.ParseStorageGb(in.VariantConfig[ConfigStorage])
	diskName := fmt.Sprintf("my-%s-data", in.ServerName)
	mountPath := "/var/lib/mysql"

	env := []string{
		"MYSQL_ROOT_PASSWORD=" + in.RootPassword,
		"MYSQL_DATABASE=" + in.DatabaseName,
		"MYSQL_USER=" + in.Username,
		"MYSQL_PASSWORD=" + in.Password,
	}

	image := in.VariantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}

	poolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		Name:             in.ServerName,
		Image:            image,
		Env:              env,
		Ports:            mysqlContainerPorts(),
		DesiredInstances: 1,
		Labels:           labels,
		SandboxPrefix:    fmt.Sprintf("%s-my", in.AppName),
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
	diskName := fmt.Sprintf("my-%s-data", in.ServerName)
	return fw.DeleteDiskByName(ctx, diskName)
}

type UpdateDedicatedServerIn struct {
	ServerID     entity.Id `saga:"serverid"`
	PoolID       entity.Id `saga:"poolid"`
	ServiceID    entity.Id `saga:"serviceid"`
	ServiceHost  string    `saga:"servicehost"`
	VariantName  string    `saga:"variantname"`
	RootPassword string    `saga:"rootpassword"`
}

type UpdateDedicatedServerOut struct {
	Updated bool
}

func UpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn) (UpdateDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MysqlServer{
		AddonName:        AddonName,
		Variant:          in.VariantName,
		Status:           "active",
		AssociationCount: 1,
		RootPassword:     in.RootPassword,
		SandboxPool:      in.PoolID,
		Service:          in.ServiceID,
	}
	server.ID = in.ServerID

	if err := fw.EC.Update(ctx, server); err != nil {
		return UpdateDedicatedServerOut{}, fmt.Errorf("updating mysql server: %w", err)
	}

	return UpdateDedicatedServerOut{Updated: true}, nil
}

func UndoUpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn, out UpdateDedicatedServerOut) error {
	return nil
}

type BuildDedicatedResultIn struct {
	ServiceHost  string    `saga:"servicehost"`
	Username     string    `saga:"username"`
	Password     string    `saga:"password"`
	DatabaseName string    `saga:"databasename"`
	ServerID     entity.Id `saga:"serverid"`
}

type BuildDedicatedResultOut struct {
	Done bool
}

func BuildDedicatedResult(ctx context.Context, in BuildDedicatedResultIn) (BuildDedicatedResultOut, error) {
	rc := saga.Get[*resultCapture](ctx)

	envVars := buildEnvVars(in.ServiceHost, mysqlPort, in.Username, in.Password, in.DatabaseName)

	dedicatedData := &addon_v1alpha.MysqlDedicatedData{
		MysqlServer:  in.ServerID,
		DatabaseName: in.DatabaseName,
		Username:     in.Username,
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

func RegisterDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *resultCapture) error {
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, Port: mysqlPort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("provision-dedicated-mysql").
		Using(fw).
		Using(rc).
		Using(cfg).
		Action(GenerateCredentials).Undo(UndoGenerateCredentials).
		Action(CreateMysqlServer).Undo(UndoCreateMysqlServer).
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
	var data addon_v1alpha.MysqlDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}

	if data.MysqlServer == "" {
		return DecodeDedicatedAttrsOut{}, fmt.Errorf("no mysql server ref found")
	}

	return DecodeDedicatedAttrsOut{DedicatedServerID: data.MysqlServer}, nil
}

func UndoDecodeDedicatedAttrs(ctx context.Context, in DecodeDedicatedAttrsIn, out DecodeDedicatedAttrsOut) error {
	return nil
}

type LookupDedicatedServerIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

type LookupDedicatedServerOut struct {
	DedicatedServiceID  entity.Id `saga:"dedicatedserviceid"`
	DedicatedPoolID     entity.Id `saga:"dedicatedpoolid"`
	DedicatedServerName string    `saga:"dedicatedservername"`
}

func LookupDedicatedServer(ctx context.Context, in LookupDedicatedServerIn) (LookupDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MysqlServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LookupDedicatedServerOut{}, fmt.Errorf("looking up mysql server: %w", err)
	}

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
	DedicatedServerID   entity.Id `saga:"dedicatedserverid"`
	DedicatedServerName string    `saga:"dedicatedservername"`

	PoolCleanedUp saga.Edge `saga:"dedicated_pool_deleted"`
}

type DeleteDedicatedServerEntityOut struct {
	ServerDeleted bool
}

func DeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn) (DeleteDedicatedServerEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	if in.DedicatedServerName != "" {
		diskName := fmt.Sprintf("my-%s-data", in.DedicatedServerName)
		if err := fw.DeleteDiskByName(ctx, diskName); err != nil {
			return DeleteDedicatedServerEntityOut{}, fmt.Errorf("deleting dedicated data disk %s: %w", diskName, err)
		}
	}

	if err := fw.EC.Delete(ctx, in.DedicatedServerID); err != nil {
		return DeleteDedicatedServerEntityOut{}, fmt.Errorf("deleting mysql server: %w", err)
	}

	return DeleteDedicatedServerEntityOut{ServerDeleted: true}, nil
}

func UndoDeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn, out DeleteDedicatedServerEntityOut) error {
	return nil
}

func RegisterDeprovisionDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	return saga.Define("deprovision-dedicated-mysql").
		Using(fw).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LookupDedicatedServer).Undo(UndoLookupDedicatedServer).
		Action(dbsaga.DeleteDedicatedService).Undo(dbsaga.UndoDeleteDedicatedService).
		Action(dbsaga.DeleteDedicatedPool).Undo(dbsaga.UndoDeleteDedicatedPool).
		Action(DeleteDedicatedServerEntity).Undo(UndoDeleteDedicatedServerEntity).
		RegisterTo(registry)
}

func (p *Provider) provisionDedicated(ctx context.Context, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.Log.Info("provisioning dedicated MySQL",
		"app", app.Name,
		"variant", variant.Name)

	rc := &resultCapture{}
	registry := saga.NewRegistry()

	if err := RegisterDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("provision-dedicated-mysql").
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

	p.Log.Info("dedicated MySQL provisioned", "app", app.Name)
	return rc.Result, nil
}

func (p *Provider) deprovisionDedicated(ctx context.Context, assoc addon.AddonAssociation) error {
	p.Log.Info("deprovisioning dedicated MySQL", "assoc", assoc.ID)

	registry := saga.NewRegistry()

	if err := RegisterDeprovisionDedicatedSaga(registry, p.Fw); err != nil {
		return fmt.Errorf("registering deprovision saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("deprovision-dedicated-mysql").
		Input("assocentity", assoc.Entity).
		Execute(ctx)
	if err != nil {
		return err
	}

	p.Log.Info("dedicated MySQL deprovisioned", "assoc", assoc.ID)
	return nil
}
