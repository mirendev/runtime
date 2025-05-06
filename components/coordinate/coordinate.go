package coordinate

import (
	"context"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	"miren.dev/runtime/servers/entityserver"
	execproxy "miren.dev/runtime/servers/exec_proxy"
)

type CoordinatorConfig struct {
	Address       string              `json:"address" yaml:"address"`
	EtcdEndpoints []string            `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix        string              `json:"prefix" yaml:"prefix"`
	Resolver      netresolve.Resolver `json:"resolver" yaml:"resolver"`
	TempDir       string              `json:"temp_dir" yaml:"temp_dir"`

	Mem *metrics.MemoryUsage
	Cpu *metrics.CPUUsage
}

func NewCoordinator(log *slog.Logger, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{
		CoordinatorConfig: cfg,
		Log:               log,
	}
}

type Coordinator struct {
	CoordinatorConfig

	Log *slog.Logger

	state *rpc.State

	aa activator.AppActivator
}

func (c *Coordinator) Activator() activator.AppActivator {
	return c.aa
}

func (c *Coordinator) Start(ctx context.Context) error {
	c.Log.Info("starting coordinator", "address", c.Address, "etcd_endpoints", c.EtcdEndpoints, "prefix", c.Prefix)

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBindAddr(c.Address))
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}
	c.state = rs

	server := rs.Server()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:        c.EtcdEndpoints,
		AutoSyncInterval: time.Minute,
	})
	if err != nil {
		c.Log.Error("failed to create etcd client", "error", err)
		return err
	}

	etcdStore, err := entity.NewEtcdStore(ctx, c.Log, client, c.Prefix)
	if err != nil {
		c.Log.Error("failed to create etcd store", "error", err)
		return err
	}

	err = schema.Apply(ctx, etcdStore)
	if err != nil {
		c.Log.Error("failed to apply schema", "error", err)
		return err
	}

	ess, err := entityserver.NewEntityServer(c.Log, etcdStore)
	if err != nil {
		c.Log.Error("failed to create entity server", "error", err)
		return err
	}

	server.ExposeValue("entities", esv1.AdaptEntityAccess(ess))

	loopback, err := rs.Connect(c.Address, "entities")
	if err != nil {
		c.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(loopback)

	var (
		defPro core_v1alpha.Project
		defNet entityserver_v1alpha.Entity
	)

	defNet.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: "default",
		}).Encode,
		defPro.Encode,
		entity.Ident, "project/default",
	))

	_, err = eac.Put(ctx, &defNet)
	if err != nil {
		c.Log.Error("failed to create default project", "error", err)
		return err
	}

	aa := activator.NewLocalActivator(ctx, c.Log, eac)
	c.aa = aa

	eps := execproxy.NewServer(c.Log, eac, rs, aa)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(eps))

	bs := build.NewBuilder(c.Log, eac, c.Resolver, c.TempDir)
	server.ExposeValue("dev.miren.runtime/build", build_v1alpha.AdaptBuilder(bs))

	ec := aes.NewClient(c.Log, eac)

	ai := app.NewAppInfo(c.Log, ec, c.Cpu, c.Mem)
	server.ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(ai))
	server.ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(ai))

	c.Log.Info("started RPC server")
	return nil
}

func (c *Coordinator) Server() *rpc.Server {
	return c.state.Server()
}
