package lbdmod

import (
	"context"
	"fmt"
)

// Mount is a bind mount into the builder.
type Mount struct {
	// Source is the path on the host.
	Source string

	// Destination is where it appears inside the builder.
	Destination string

	// ReadOnly keeps the builder from writing through the mount.
	ReadOnly bool
}

// BuildSpec describes one run of the builder image.
type BuildSpec struct {
	// Name is the container's id. It is fixed rather than random so a build
	// killed before its own cleanup leaves something the next run can find.
	Name string

	// Image is the builder image reference.
	Image string

	// Args replaces the image's entrypoint arguments.
	Args []string

	// Env is added to the image's environment as "KEY=value" pairs.
	Env []string

	// Mounts are bind mounts into the container.
	Mounts []Mount

	// HostNetwork gives the builder the host's network and resolver. It is
	// set only when the builder has to fetch kernel headers for itself; a
	// build against a host's own headers needs no network at all.
	HostNetwork bool
}

// Builder runs the builder image once and waits for it to finish. It is an
// interface so this package stays free of container runtime dependencies --
// components/diskio and controllers/disk import it only to ask whether lbd is
// available, and should not pull containerd along with them. The containerd
// implementation is pkg/lbdmod/ctrbuild.
type Builder interface {
	// Build runs the container to completion and returns nil only if it
	// exited zero. A non-zero exit should be reported as a BuildFailedError
	// so the build output survives.
	Build(ctx context.Context, spec BuildSpec) error
}

// BuildFailedError reports a builder container that ran but exited non-zero.
// It carries the tail of the build output, which is where the real explanation
// lives.
type BuildFailedError struct {
	ExitCode uint32
	Output   string
}

func (e *BuildFailedError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("the lbd build failed (exit %d)", e.ExitCode)
	}
	return fmt.Sprintf("the lbd build failed (exit %d):\n%s", e.ExitCode, e.Output)
}
