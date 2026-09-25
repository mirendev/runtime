package runner

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

// InstallDiskAccelerator builds and loads the lbd kernel module on a runner.
//
// The work splits across two machines. The toolchain image is built once,
// here, because the coordinator is where BuildKit and the registry live. The
// module itself is built on the target node, because it has to be compiled
// against the kernel running there and loaded into it.
//
// Failures come back in the result rather than as an RPC error, so an operator
// sees why the install did not happen.
func (s *RegistrationServer) InstallDiskAccelerator(ctx context.Context, req *runner_v1alpha.RunnerRegistrationInstallDiskAccelerator) error {
	args := req.Args()
	results := req.Results()

	if !args.HasQuery() || args.Query() == "" {
		results.SetError("runner name or ID is required")
		return nil
	}
	query := args.Query()

	if s.LbdBuilder == nil {
		results.SetError("this cluster cannot build the lbd toolchain image, so accelerator mode is unavailable")
		return nil
	}
	if s.RPC == nil {
		results.SetError("no rpc state to reach the runner with")
		return nil
	}

	node, _, err := s.findNodeByQuery(ctx, query)
	if err != nil {
		s.Log.Error("failed to find runner", "query", query, "error", err)
		results.SetError(err.Error())
		return nil
	}
	if node == nil {
		results.SetError(fmt.Sprintf("runner %q not found", query))
		return nil
	}
	if node.ApiAddress == "" {
		results.SetError(fmt.Sprintf("runner %q has no address to reach it on", query))
		return nil
	}

	// Built before dialing: a node that pulls an image the cluster has not
	// published yet fails with a registry error that says nothing about why.
	image, err := s.LbdBuilder.EnsureLbdBuilderImage(ctx)
	if err != nil {
		s.Log.Error("failed to build the lbd toolchain image", "error", err)
		results.SetError(fmt.Sprintf("building the lbd toolchain image: %v", err))
		return nil
	}

	s.Log.Info("installing the lbd kernel module on a runner",
		"node", node.ID, "address", node.ApiAddress, "image", image)

	cl, err := s.RPC.Connect(node.ApiAddress, string(rpc.ServiceNodeAdmin))
	if err != nil {
		results.SetError(fmt.Sprintf("connecting to runner %q at %s: %v", query, node.ApiAddress, err))
		return nil
	}
	defer cl.Close()

	nc := &nodeadmin_v1alpha.NodeAdminClient{Client: cl}
	res, err := nc.InstallDiskAccelerator(ctx, image, args.HasForce() && args.Force())
	if err != nil {
		results.SetError(fmt.Sprintf("installing on runner %q: %v", query, err))
		return nil
	}

	if res.HasError() && res.Error() != "" {
		results.SetError(res.Error())
		return nil
	}

	results.SetName(node.Name)
	results.SetKernelRelease(res.KernelRelease())
	results.SetLbdVersion(res.LbdVersion())
	return nil
}

// LbdBuilderImageEnsurer builds the lbd toolchain image into the cluster
// registry if it is not already there. servers/build.Builder implements it;
// this is an interface so the runner registration server does not depend on
// the whole build server to ask one question of it.
type LbdBuilderImageEnsurer interface {
	EnsureLbdBuilderImage(ctx context.Context) (string, error)
}
