//go:build linux
// +build linux

package commands

import (
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/httpingress"
)

func Dev(ctx *Context, opts struct {
	Address       string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	EtcdEndpoints []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix    string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId      string   `short:"r" long:"runner-id" description:"Runner ID" default:"dev"`
}) error {
	eg, sub := errgroup.WithContext(ctx)

	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:       opts.Address,
		EtcdEndpoints: opts.EtcdEndpoints,
		Prefix:        opts.EtcdPrefix,
	})

	err := co.Start(sub)
	if err != nil {
		ctx.Log.Error("failed to start coordinator", "error", err)
		return err
	}

	time.Sleep(time.Second)

	r := runner.NewRunner(ctx.Log, ctx.Server, runner.RunnerConfig{
		Id:            opts.RunnerId,
		ServerAddress: opts.Address,
		Workers:       runner.DefaulWorkers,
	})

	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		ctx.Log.Error("failed to create RPC client", "error", err)
		return err
	}

	err = r.Start(sub)
	if err != nil {
		return err
	}

	client, err := rs.Connect(opts.Address, "entities")
	if err != nil {
		ctx.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := &entityserver_v1alpha.EntityAccessClient{Client: client}

	sch, err := scheduler.NewScheduler(sub, ctx.Log, eac)
	if err != nil {
		ctx.Log.Error("failed to create scheduler", "error", err)
		return err
	}

	eg.Go(func() error {
		return sch.Watch(sub, eac)
	})

	hs := httpingress.NewServer(ctx.Log, eac)

	go func() {
		err := http.ListenAndServe(":8989", hs)
		if err != nil {
			ctx.Log.Error("failed to start HTTP server", "error", err)
		}
	}()

	ctx.Log.Info("Starting dev mode", "address", opts.Address, "etcd_endpoints", opts.EtcdEndpoints, "etcd_prefix", opts.EtcdPrefix, "runner_id", opts.RunnerId)

	return eg.Wait()
}
