package memcache

import (
	"context"
	"fmt"
	"time"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

const (
	memcachePort     = 11211
	poolReadyTimeout = 5 * time.Minute
)

func memcacheContainerPorts() []compute_v1alpha.SandboxSpecContainerPort {
	return []compute_v1alpha.SandboxSpecContainerPort{
		{
			Name:     "memcache",
			Port:     memcachePort,
			Protocol: compute_v1alpha.SandboxSpecContainerPortTCP,
		},
	}
}

type resultCapture struct {
	Result *addon.ProvisionResult
}

// --- Dedicated Provisioning Saga Actions ---

type GenerateNamesIn struct {
	AppName string
}

type GenerateNamesOut struct {
	ServiceName string `saga:"servicename"`
	ServerName  string `saga:"servername"`
}

func GenerateNames(ctx context.Context, in GenerateNamesIn) (GenerateNamesOut, error) {
	return GenerateNamesOut{
		ServiceName: fmt.Sprintf("%s-memcache", in.AppName),
		ServerName:  fmt.Sprintf("mc-%s-%s", in.AppName, idgen.Gen("s")),
	}, nil
}

func UndoGenerateNames(ctx context.Context, in GenerateNamesIn, out GenerateNamesOut) error {
	return nil
}

type CreateMemcacheServerIn struct {
	ServerName  string `saga:"servername"`
	VariantName string `saga:"variantname"`
}

type CreateMemcacheServerOut struct {
	ServerID entity.Id `saga:"serverid"`
}

func CreateMemcacheServer(ctx context.Context, in CreateMemcacheServerIn) (CreateMemcacheServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MemcacheServer{
		AddonName:        AddonName,
		Variant:          in.VariantName,
		Status:           "provisioning",
		AssociationCount: 1,
	}

	serverID, err := fw.EC.Create(ctx, in.ServerName, server)
	if err != nil {
		return CreateMemcacheServerOut{}, fmt.Errorf("creating memcache server entity: %w", err)
	}

	return CreateMemcacheServerOut{ServerID: serverID}, nil
}

func UndoCreateMemcacheServer(ctx context.Context, in CreateMemcacheServerIn, out CreateMemcacheServerOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Delete(ctx, out.ServerID)
}

type CreateDedicatedPoolIn struct {
	ServerName    string            `saga:"servername"`
	AppName       string            `saga:"appname"`
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

	memory := in.VariantConfig[ConfigMemory]
	if memory == "" {
		return CreateDedicatedPoolOut{}, fmt.Errorf("missing required config: %s", ConfigMemory)
	}

	image := in.VariantConfig[addon.ConfigImage]
	if image == "" {
		image = BaseImage + ":" + DefaultVersion
	}

	poolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		Name:             in.ServerName,
		Image:            image,
		Command:          fmt.Sprintf("memcached -m %s", memory),
		Ports:            memcacheContainerPorts(),
		DesiredInstances: 1,
		Labels:           labels,
		SandboxPrefix:    fmt.Sprintf("%s-mc", in.AppName),
	})
	if err != nil {
		return CreateDedicatedPoolOut{}, fmt.Errorf("creating sandbox pool: %w", err)
	}

	return CreateDedicatedPoolOut{PoolID: poolID}, nil
}

func UndoCreateDedicatedPool(ctx context.Context, in CreateDedicatedPoolIn, out CreateDedicatedPoolOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.DeleteSandboxPool(ctx, out.PoolID)
}

type UpdateDedicatedServerIn struct {
	ServerID    entity.Id `saga:"serverid"`
	PoolID      entity.Id `saga:"poolid"`
	ServiceID   entity.Id `saga:"serviceid"`
	ServiceHost string    `saga:"servicehost"`
	VariantName string    `saga:"variantname"`
}

type UpdateDedicatedServerOut struct {
	Updated bool
}

func UpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn) (UpdateDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	server := &addon_v1alpha.MemcacheServer{
		AddonName:        AddonName,
		Variant:          in.VariantName,
		Status:           "active",
		AssociationCount: 1,
		SandboxPool:      in.PoolID,
		Service:          in.ServiceID,
	}
	server.ID = in.ServerID

	if err := fw.EC.Update(ctx, server); err != nil {
		return UpdateDedicatedServerOut{}, fmt.Errorf("updating memcache server: %w", err)
	}

	return UpdateDedicatedServerOut{Updated: true}, nil
}

func UndoUpdateDedicatedServer(ctx context.Context, in UpdateDedicatedServerIn, out UpdateDedicatedServerOut) error {
	return nil
}

type BuildDedicatedResultIn struct {
	ServiceHost string    `saga:"servicehost"`
	ServerID    entity.Id `saga:"serverid"`
}

type BuildDedicatedResultOut struct {
	Done bool
}

func BuildDedicatedResult(ctx context.Context, in BuildDedicatedResultIn) (BuildDedicatedResultOut, error) {
	rc := saga.Get[*resultCapture](ctx)

	envVars := buildEnvVars(in.ServiceHost, memcachePort)

	dedicatedData := &addon_v1alpha.MemcacheDedicatedData{
		MemcacheServer: in.ServerID,
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
	cfg := &dbsaga.AddonConfig{AddonName: AddonName, Port: memcachePort, ReadyTimeout: poolReadyTimeout}
	return saga.Define("provision-dedicated-memcache").
		Using(fw).
		Using(rc).
		Using(cfg).
		Action(GenerateNames).Undo(UndoGenerateNames).
		Action(CreateMemcacheServer).Undo(UndoCreateMemcacheServer).
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
	var data addon_v1alpha.MemcacheDedicatedData
	if in.AssocEntity != nil {
		data.Decode(in.AssocEntity)
	}

	if data.MemcacheServer == "" {
		return DecodeDedicatedAttrsOut{}, fmt.Errorf("no memcache server ref found")
	}

	return DecodeDedicatedAttrsOut{DedicatedServerID: data.MemcacheServer}, nil
}

func UndoDecodeDedicatedAttrs(ctx context.Context, in DecodeDedicatedAttrsIn, out DecodeDedicatedAttrsOut) error {
	return nil
}

type LookupDedicatedServerIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

type LookupDedicatedServerOut struct {
	DedicatedServiceID entity.Id `saga:"dedicatedserviceid"`
	DedicatedPoolID    entity.Id `saga:"dedicatedpoolid"`
}

func LookupDedicatedServer(ctx context.Context, in LookupDedicatedServerIn) (LookupDedicatedServerOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.MemcacheServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LookupDedicatedServerOut{}, fmt.Errorf("looking up memcache server: %w", err)
	}

	return LookupDedicatedServerOut{
		DedicatedServiceID: server.Service,
		DedicatedPoolID:    server.SandboxPool,
	}, nil
}

func UndoLookupDedicatedServer(ctx context.Context, in LookupDedicatedServerIn, out LookupDedicatedServerOut) error {
	return nil
}

type DeleteDedicatedServerEntityIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`

	ServiceCleanedUp saga.Edge `saga:"dedicated_service_deleted"`
	PoolCleanedUp    saga.Edge `saga:"dedicated_pool_deleted"`
}

type DeleteDedicatedServerEntityOut struct {
	ServerDeleted bool
}

func DeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn) (DeleteDedicatedServerEntityOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	if err := fw.EC.Delete(ctx, in.DedicatedServerID); err != nil {
		return DeleteDedicatedServerEntityOut{}, fmt.Errorf("deleting memcache server: %w", err)
	}

	return DeleteDedicatedServerEntityOut{ServerDeleted: true}, nil
}

func UndoDeleteDedicatedServerEntity(ctx context.Context, in DeleteDedicatedServerEntityIn, out DeleteDedicatedServerEntityOut) error {
	return nil
}

func RegisterDeprovisionDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework) error {
	return saga.Define("deprovision-dedicated-memcache").
		Using(fw).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LookupDedicatedServer).Undo(UndoLookupDedicatedServer).
		Action(dbsaga.DeleteDedicatedService).Undo(dbsaga.UndoDeleteDedicatedService).
		Action(dbsaga.DeleteDedicatedPool).Undo(dbsaga.UndoDeleteDedicatedPool).
		Action(DeleteDedicatedServerEntity).Undo(UndoDeleteDedicatedServerEntity).
		RegisterTo(registry)
}

func (p *Provider) provisionDedicated(ctx context.Context, assoc addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.Log.Info("provisioning dedicated Memcached",
		"app", app.Name,
		"variant", variant.Name)

	rc := &resultCapture{}
	registry := saga.NewRegistry()

	if err := RegisterDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering dedicated saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("provision-dedicated-memcache").
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

	p.Log.Info("dedicated Memcached provisioned", "app", app.Name)
	return rc.Result, nil
}

func (p *Provider) deprovisionDedicated(ctx context.Context, assoc addon.AddonAssociation) error {
	p.Log.Info("deprovisioning dedicated Memcached", "assoc", assoc.ID)

	registry := saga.NewRegistry()

	if err := RegisterDeprovisionDedicatedSaga(registry, p.Fw); err != nil {
		return fmt.Errorf("registering deprovision saga: %w", err)
	}

	storage := p.Fw.Storage
	executor := saga.NewExecutor(storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))

	err := executor.Start("deprovision-dedicated-memcache").
		WithID(addon.DeprovisionExecutionID(assoc.ID)).
		Input("assocentity", assoc.Entity).
		Execute(ctx)
	if err != nil {
		return err
	}

	p.Log.Info("dedicated Memcached deprovisioned", "assoc", assoc.ID)
	return nil
}
