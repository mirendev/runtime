package runner

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/lbdmod/ctrbuild"
)

// nodeAdminServer handles work the coordinator asks this node to do to itself.
//
// It lives on the runner rather than the coordinator because the work is about
// this host: a kernel module has to be compiled against the kernel actually
// running here, and loaded into it.
type nodeAdminServer struct {
	log  *slog.Logger
	deps lbdDeps
}

// InstallDiskAccelerator builds and loads the lbd kernel module on this node.
//
// Failures come back in the result rather than as an RPC error, so the
// operator sees why the install did not happen instead of a transport-level
// message. The call itself only fails when the node could not be reached.
func (s *nodeAdminServer) InstallDiskAccelerator(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminInstallDiskAccelerator) error {
	res := req.Results()

	installer := &lbdmod.Installer{
		Log: s.log,
		Builder: ctrbuild.New(s.deps.CC, s.log, ctrbuild.WithClusterRegistry(&ctrbuild.ClusterRegistry{
			Resolver: s.deps.Resolver,
			Issuer:   s.deps.WorkloadIssuer,
		})),
		Options: lbdmod.HostOptions(s.deps.DataPath),
		Image:   req.Args().Image(),
	}

	status, err := installer.Install(ctx, req.Args().Force())
	if err != nil {
		s.log.Warn("installing the lbd kernel module failed", "error", err)
		res.SetError(err.Error())
		return nil
	}

	// The disk controller reads the mode once at startup, so a node that just
	// gained accelerator mode keeps serving loop devices until it restarts.
	// Say so rather than letting the operator discover it from a disk that
	// came up the old way.
	if err := diskio.EnsureLbdDevices(ctx, s.log); err != nil {
		s.log.Warn("lbd installed but is not usable yet", "error", err)
	}

	res.SetKernelRelease(status.Host.KernelRelease)
	res.SetLbdVersion(lbdmod.SourceVersion())
	return nil
}
