package commands

import (
	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func SandboxMetrics(ctx *Context, opts struct {
	Server  string `long:"server" description:"Server address to connect to" default:"localhost:8444"`
	Sandbox string `short:"s" long:"sandbox" description:"Sandbox name to get metrics for"`
}) error {
	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return err
	}

	client, err := cs.Connect(opts.Server, "dev.miren.runtime/sandbox.metrics")
	if err != nil {
		return err
	}

	mc := metric_v1alpha.NewSandboxMetricsClient(client)
	res, err := mc.Snapshot(ctx, opts.Sandbox)
	if err != nil {
		return err
	}

	spew.Dump(res.Metrics())

	return nil

}
