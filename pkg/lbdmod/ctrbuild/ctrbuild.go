// Package ctrbuild runs the lbd builder image on containerd.
//
// It is separate from pkg/lbdmod so that package can stay free of container
// runtime dependencies: components/diskio and controllers/disk import lbdmod
// only to ask whether lbd is usable, and should not pull containerd in behind
// that question.
package ctrbuild

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/pkg/lbdmod"
)

// DefaultNamespace is the containerd namespace miren's own containers live in.
const DefaultNamespace = "miren"

// cleanupTimeout bounds the teardown of a container after the work is done,
// including the case where the caller's context has already been cancelled.
const cleanupTimeout = 30 * time.Second

// outputTailLines is how much of the build output a failure quotes back.
const outputTailLines = 40

// Builder runs a build container on containerd.
//
// Nothing in components/ does this: every component there is a supervised
// daemon with a restart policy. A build is the opposite -- it runs to
// completion, its exit code is the answer, and it must leave nothing behind.
type Builder struct {
	cc  *containerd.Client
	log *slog.Logger
}

// New returns a Builder that runs containers on cc.
func New(cc *containerd.Client, log *slog.Logger) *Builder {
	return &Builder{cc: cc, log: log}
}

// Build pulls the image, runs the container to completion, and tears
// everything down. It returns an error unless the container exited zero.
func (b *Builder) Build(ctx context.Context, spec lbdmod.BuildSpec) error {
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	image, err := b.resolveImage(ctx, spec.Image)
	if err != nil {
		return err
	}

	// A previous run that died before its own cleanup leaves the container
	// behind and its name taken.
	if existing, err := b.cc.LoadContainer(ctx, spec.Name); err == nil {
		b.log.Info("removing a container left by an earlier build", "container", spec.Name)
		b.removeContainer(ctx, existing)
	}

	// Deliberately not privileged. Compiling C against read-only bind mounts
	// needs no extra capabilities -- runc's default masked and read-only
	// paths cover /proc and /sys, not /lib/modules or /usr/src -- and this
	// container runs on every node that turns accelerator mode on. Verified
	// against a real containerd for both the host-headers and header-fetch
	// paths; if that ever stops holding, add the one capability that is
	// missing rather than all of them.
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithProcessArgs(spec.Args...),
		oci.WithEnv(spec.Env),
		oci.WithMounts(ociMounts(spec.Mounts)),
	}
	if spec.HostNetwork {
		opts = append(opts, oci.WithHostNamespace(specs.NetworkNamespace), oci.WithHostResolvconf)
	}

	container, err := b.cc.NewContainer(ctx, spec.Name,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(spec.Name+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return fmt.Errorf("creating the build container: %w", err)
	}
	defer b.removeContainer(context.WithoutCancel(ctx), container)

	output := newTailWriter(b.log, spec.Name, outputTailLines)
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, output, output)))
	if err != nil {
		return fmt.Errorf("creating the build task: %w", err)
	}
	defer func() {
		output.flush()
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if _, err := task.Delete(cleanupCtx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
			b.log.Warn("failed to delete the build task", "error", err)
		}
	}()

	// Establish the exit channel before starting, so the exit event cannot be
	// missed by a build that finishes immediately.
	exitCh, err := task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting on the build task: %w", err)
	}

	if err := task.Start(ctx); err != nil {
		return fmt.Errorf("starting the build: %w", err)
	}

	select {
	case status := <-exitCh:
		output.flush()
		if err := status.Error(); err != nil {
			return fmt.Errorf("the build did not report a result: %w", err)
		}
		if code := status.ExitCode(); code != 0 {
			return &lbdmod.BuildFailedError{ExitCode: code, Output: output.Tail()}
		}
		return nil

	case <-ctx.Done():
		// Kill rather than leaving a compile running against a directory we
		// are about to delete.
		if err := task.Kill(context.WithoutCancel(ctx), syscall.SIGKILL); err != nil && !errdefs.IsNotFound(err) {
			b.log.Warn("failed to kill the build task", "error", err)
		}
		return ctx.Err()
	}
}

// resolveImage returns the builder image, pulling it only if it is not already
// in the local store.
//
// Preferring the local copy is what lets a node that has already built once
// rebuild without a registry, and what makes a side-loaded image usable at all
// -- `ctr images import` plus --image is the only way to run a builder on a
// node with no reachable registry. It is safe here because the builder
// reference is pinned to a tag that never moves.
func (b *Builder) resolveImage(ctx context.Context, ref string) (containerd.Image, error) {
	if img, err := b.cc.GetImage(ctx, ref); err == nil {
		b.log.Info("using the lbd builder image already on this node", "image", ref)
		return img, nil
	}

	b.log.Info("pulling the lbd builder image", "image", ref)
	img, err := b.cc.Pull(ctx, ref, containerd.WithPullUnpack)
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", ref, err)
	}
	return img, nil
}

// removeContainer deletes a container and its snapshot, tolerating a container
// that is already gone.
func (b *Builder) removeContainer(ctx context.Context, container containerd.Container) {
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(ctx, DefaultNamespace), cleanupTimeout)
	defer cancel()

	if task, err := container.Task(ctx, nil); err == nil {
		if _, err := task.Delete(ctx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
			b.log.Warn("failed to delete a leftover build task", "error", err)
		}
	}

	if err := container.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
		b.log.Warn("failed to delete the build container", "error", err, "container", container.ID())
	}
}

// ociMounts converts the runtime-agnostic mounts into OCI bind mounts.
func ociMounts(mounts []lbdmod.Mount) []specs.Mount {
	out := make([]specs.Mount, 0, len(mounts))
	for _, m := range mounts {
		access := "rw"
		if m.ReadOnly {
			access = "ro"
		}
		out = append(out, specs.Mount{
			Destination: m.Destination,
			Type:        "bind",
			Source:      m.Source,
			Options:     []string{"rbind", access},
		})
	}
	return out
}

// tailWriter forwards container output to a logger a line at a time and keeps
// the last few lines, so a failure can quote what actually went wrong instead
// of just its exit code.
type tailWriter struct {
	log   *slog.Logger
	name  string
	limit int

	mu      sync.Mutex
	partial []byte
	lines   []string
}

func newTailWriter(log *slog.Logger, name string, limit int) *tailWriter {
	return &tailWriter{log: log, name: name, limit: limit}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.partial = append(w.partial, p...)
	for {
		idx := strings.IndexByte(string(w.partial), '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(w.partial[:idx]), "\r")
		w.partial = w.partial[idx+1:]
		w.record(line)
	}
	return len(p), nil
}

// flush emits whatever the container left without a trailing newline.
func (w *tailWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.partial) > 0 {
		w.record(strings.TrimRight(string(w.partial), "\r"))
		w.partial = nil
	}
}

// record must be called with w.mu held.
//
// Build output goes to Debug, not Info. A compile is thirty-odd lines of make
// output that would be identical the next thousand times it runs, and the
// automatic rebuild after a kernel upgrade emits them into the daemon log with
// nobody watching. The outcome is logged at Info by the installer, and a
// failure carries the tail in BuildFailedError regardless of level -- so the
// case that actually needs this output never depended on it being Info.
// Operators watching a build can see it with -v.
func (w *tailWriter) record(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	w.log.Debug(line, "source", w.name)
	w.lines = append(w.lines, line)
	if len(w.lines) > w.limit {
		w.lines = w.lines[len(w.lines)-w.limit:]
	}
}

// Tail returns the retained lines as a single block.
func (w *tailWriter) Tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.lines, "\n")
}
