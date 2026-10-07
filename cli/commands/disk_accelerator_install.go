package commands

import (
	"fmt"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
)

// DiskAcceleratorInstall asks the cluster to install on the named node, or
// infers the node from this host's running server or runner configuration.
func DiskAcceleratorInstall(ctx *Context, opts struct {
	ConfigCentric

	Force bool   `short:"f" long:"force" description:"Rebuild even when the module is already current"`
	Node  string `position:"0" usage:"Runner to install on (name, ID, or short ID); omit to infer this host's node"`
}) error {
	var runner *runnerconfig.Config
	var localClusterName string
	if opts.Node == "" {
		if opts.Cluster != "" {
			return fmt.Errorf("cannot infer this host's node for cluster %q; pass a node name or ID", opts.Cluster)
		}
		var err error
		opts.Node, runner, localClusterName, err = currentDiskAcceleratorNode(ctx, runnerconfig.DefaultConfigPath, "/etc/systemd/system/miren.service", "")
		if err != nil {
			return err
		}
	}

	var (
		client *rpc.NetworkClient
		err    error
	)
	if runner != nil {
		state, err := rpc.NewState(ctx,
			rpc.WithLogger(ctx.Log),
			rpc.WithBindAddr("[::]:0"),
			rpc.WithCertPEMs([]byte(runner.ClientCert), []byte(runner.ClientKey)),
			rpc.WithCertificateVerification([]byte(runner.CACert)),
		)
		if err != nil {
			return fmt.Errorf("connecting with local runner credentials: %w", err)
		}
		defer state.Close()
		client, err = state.Connect(runner.CoordinatorAddress, rpc.ServiceRunner)
		if err != nil {
			return err
		}
	} else if localClusterName != "" {
		cluster, err := localDiskAcceleratorCluster(ctx, localClusterName, "")
		if err != nil {
			return err
		}
		state, err := cluster.State(ctx, clientconfig.NewConfig(), rpc.WithLogger(ctx.Log))
		if err != nil {
			return fmt.Errorf("connecting to local server cluster %q: %w", localClusterName, err)
		}
		defer state.Close()
		client, err = state.Client(rpc.ServiceRunner)
		if err != nil {
			return err
		}
	} else {
		client, err = ctx.RPCClient(rpc.ServiceRunner)
	}
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	ctx.Begin("Installing the lbd kernel module on %s", opts.Node)

	res, err := rc.InstallDiskAccelerator(ctx, opts.Node, opts.Force)
	if err != nil {
		return err
	}
	if res.Error() != "" {
		return fmt.Errorf("%s", res.Error())
	}

	ctx.Completed("Accelerator mode is ready on %s, kernel %s", res.Name(), res.KernelRelease())
	ctx.Info("Restart that node's miren service to pick it up")
	return nil
}
