package coordinate

import (
	"context"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	esv1 "miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/entityserver"
)

type CoordinatorConfig struct {
	Address       string   `json:"address" yaml:"address"`
	EtcdEndpoints []string `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix        string   `json:"prefix" yaml:"prefix"`
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
}

func (c *Coordinator) Start(ctx context.Context) error {
	c.Log.Info("starting coordinator", "address", c.Address, "etcd_endpoints", c.EtcdEndpoints, "prefix", c.Prefix)
	defer c.Log.Info("coordinator stopped")

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBindAddr(c.Address))
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}

	server := rs.Server()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:        c.EtcdEndpoints,
		AutoSyncInterval: time.Minute,
	})
	if err != nil {
		c.Log.Error("failed to create etcd client", "error", err)
		return err
	}

	etcdStore, err := entity.NewEtcdStore(ctx, client, c.Prefix)
	if err != nil {
		c.Log.Error("failed to create etcd store", "error", err)
		return err
	}

	err = schema.Apply(ctx, etcdStore)
	if err != nil {
		c.Log.Error("failed to apply schema", "error", err)
		return err
	}

	var ess entityserver.EntityServer
	ess.Log = c.Log
	ess.Store = etcdStore

	server.ExposeValue("entities", esv1.AdaptEntityAccess(&ess))

	c.Log.Info("started RPC server")
	return nil
}
