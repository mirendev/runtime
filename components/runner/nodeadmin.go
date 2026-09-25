package runner

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/lbdmod/ctrbuild"
	"miren.dev/runtime/pkg/rpc"
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

	if err := requireCoordinator(ctx); err != nil {
		s.log.Warn("rejected a disk accelerator install", "error", err)
		res.SetError(err.Error())
		return nil
	}

	// Pinned to the cluster's toolchain tag family. The image's entrypoint
	// runs here and its output is loaded into this kernel as root, so a
	// reference pointing at any other tag is not something to act on even from a
	// caller that got past the check above.
	image := req.Args().Image()
	if !lbdmod.IsBuilderImage(image) {
		s.log.Warn("rejected a disk accelerator install naming a foreign image", "image", image)
		res.SetError(fmt.Sprintf("%q is not this cluster's lbd toolchain image", image))
		return nil
	}

	installer := &lbdmod.Installer{
		Log: s.log,
		Builder: ctrbuild.New(s.deps.CC, s.log, ctrbuild.WithClusterRegistry(&ctrbuild.ClusterRegistry{
			Resolver: s.deps.Resolver,
			Issuer:   s.deps.WorkloadIssuer,
		})),
		Options: lbdmod.HostOptions(s.deps.DataPath),
		Image:   image,
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
		res.SetError(fmt.Sprintf("lbd installed but is not usable: %v", err))
		return nil
	}

	res.SetKernelRelease(status.Host.KernelRelease)
	res.SetLbdVersion(lbdmod.SourceVersion())
	return nil
}

// requireCoordinator refuses anyone but the coordinator.
//
// The listener verifies client certificates against the cluster CA. The
// coordinator dials with its API certificate, whose subject separates it from
// other cluster members that can also reach this port.
func requireCoordinator(ctx context.Context) error {
	identity := rpc.IdentityFromContext(ctx)
	if identity == nil || identity.Method == rpc.AuthMethodAnonymous {
		return fmt.Errorf("installing a kernel module requires the coordinator's certificate, and this caller presented none")
	}
	if identity.Method != rpc.AuthMethodCert {
		return fmt.Errorf("installing a kernel module requires a certificate, got %q", identity.Method)
	}
	if identity.Subject != rpc.CoordinatorCertSubject {
		return fmt.Errorf("only the coordinator may install a kernel module, not %q", identity.Subject)
	}
	return nil
}
