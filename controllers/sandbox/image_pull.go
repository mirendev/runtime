package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/containerdx"
)

// ensureImage returns the image for ref, pulling it if containerd does not
// already have it.
//
// A pull failure is written to the sandbox's log stream before being
// returned. The pull runs on whichever node hosts the sandbox, so without
// this the reason lives only in that node's journal: the sandbox is marked
// DEAD, the pool crash-loops, and `miren logs` shows nothing because no
// container ever produced output (MIR-1541).
func (c *SandboxController) ensureImage(ctx context.Context, sb *compute.Sandbox, shortID, ref string, timing *ContainerStartupTiming) (containerd.Image, error) {
	start := time.Now()
	img, err := c.CC.GetImage(ctx, ref)
	if timing != nil {
		timing.DurationsNS["image_lookup"] = time.Since(start)
		switch {
		case err == nil:
			timing.ImageCache = "hit"
		case errdefs.IsNotFound(err):
			timing.ImageCache = "miss"
		}
	}
	if err == nil {
		return c.logImageReady(ctx, img)
	}
	// Only a missing image justifies a pull. Anything else (a containerd RPC
	// failure, a cancelled context) would just fail again and be misreported
	// as a pull failure.
	if !errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get image %s: %w", ref, err)
	}

	start = time.Now()
	_, err = containerdx.PullImage(ctx, c.Log, c.CC, ref, containerd.WithPullUnpack, containerd.WithResolver(c.resolver()))
	if timing != nil {
		timing.DurationsNS["image_pull_unpack"] = time.Since(start)
	}
	if err != nil {
		// A cancelled context means we were superseded or are shutting down,
		// not that the pull failed. Skip the event so the app's logs don't
		// report a failure (and, via the url.Error wrapper, a network hint)
		// for something that never went wrong. Same guard as the port wait.
		if ctx.Err() == nil {
			c.EmitSandboxEvent(sb, shortID, describePullFailure(ref, err))
		}
		return nil, fmt.Errorf("failed to pull image %s: %w", ref, err)
	}

	start = time.Now()
	img, err = c.CC.GetImage(ctx, ref)
	if timing != nil {
		timing.DurationsNS["image_lookup"] += time.Since(start)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get image %s: %w", ref, err)
	}

	return c.logImageReady(ctx, img)
}

func (c *SandboxController) logImageReady(ctx context.Context, img containerd.Image) (containerd.Image, error) {
	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)
	return img, nil
}

// registryUnreachableHint is appended to a pull failure when the image lives
// on the cluster registry and the error is a network one. The raw containerd
// error names an IP and port but not what is listening there, and the
// distributed-runner case that motivated MIR-1541 is exactly a runner that
// cannot open port 5000 on the coordinator.
const registryUnreachableHint = "check that this node can reach the cluster image registry at " +
	ocireg.Host + " on the coordinator"

// describePullFailure builds the user-facing line for a failed image pull.
// The containerd error is kept verbatim because its tail carries the root
// cause (a dial timeout, a 404, an auth failure); the hint is only added when
// the failure is a network error against the cluster registry.
func describePullFailure(ref string, err error) string {
	msg := fmt.Sprintf("failed to pull image %s: %v", ref, err)

	var netErr net.Error
	if isClusterRegistryRef(ref) && errors.As(err, &netErr) {
		msg += "; " + registryUnreachableHint
	}

	return msg
}

// isClusterRegistryRef reports whether ref is served by the cluster registry.
// It accepts the same two host spellings SandboxController.resolver routes
// there: with the port (what the build pipeline writes) and without it.
func isClusterRegistryRef(ref string) bool {
	host, _, ok := strings.Cut(ref, "/")
	if !ok {
		return false
	}
	return host == ocireg.Host || host == "cluster.local"
}
