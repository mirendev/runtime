package build

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tonistiigi/fsutil"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/workloadidentity"
)

// lbdBuilderLock serializes the toolchain build within one coordinator. Two
// concurrent builds would both succeed -- the registry dedupes by manifest
// digest -- but they would each spend a full image build to get there.
//
// It is a channel rather than a sync.Mutex so a caller can give up: an image
// build takes minutes, and the RPC handler waiting behind one has to stay
// cancellable.
var lbdBuilderLock = make(chan struct{}, 1)

// LbdToolchain builds the lbd toolchain image into the cluster registry.
//
// It takes only what that needs rather than hanging off the app Builder: the
// coordinator exposes the runner endpoints well before workload control boots,
// and that is where the install RPC lives, so depending on the app builder
// would order this after the thing that uses it.
type LbdToolchain struct {
	Log      *slog.Logger
	BuildKit BuildKitProvider
	Issuer   *workloadidentity.Issuer
	EC       *entityserver.Client
	TempDir  string
}

// EnsureLbdBuilderImage makes sure the lbd toolchain image is in the cluster
// registry and returns the reference nodes should pull.
//
// The image is a base plus a handful of build packages; it carries no lbd
// source. Building it here rather than publishing one means there is no
// released artifact to version and no public registry to depend on, and adding
// a builder for another distribution later costs only a Dockerfile.
//
// It is keyed by a content hash of the embedded Dockerfile and build script, so
// this is a no-op on every call after the first until that content changes.
func (b *LbdToolchain) EnsureLbdBuilderImage(ctx context.Context) (string, error) {
	ref := lbdmod.BuilderImage(ocireg.Host)

	if b.present(ctx) {
		return ref, nil
	}

	select {
	case lbdBuilderLock <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for another lbd toolchain build to finish: %w", ctx.Err())
	}
	defer func() { <-lbdBuilderLock }()

	// Another call may have finished the build while this one waited.
	if b.present(ctx) {
		return ref, nil
	}

	if b.BuildKit == nil {
		return "", fmt.Errorf("no buildkit available to build the lbd toolchain image")
	}

	dir, err := os.MkdirTemp(b.TempDir, "lbd-builder-")
	if err != nil {
		return "", fmt.Errorf("creating a build context directory: %w", err)
	}
	defer os.RemoveAll(dir)

	if err := lbdmod.MaterializeBuilder(dir); err != nil {
		return "", err
	}

	// The dockerfile frontend takes its context from a real directory, and
	// resolves "filename" relative to that root, so the Dockerfile has to sit
	// inside the context we just wrote.
	dfs, err := fsutil.NewFS(dir)
	if err != nil {
		return "", fmt.Errorf("opening the build context %s: %w", dir, err)
	}

	bkc, err := b.BuildKit.Client(ctx)
	if err != nil {
		return "", fmt.Errorf("connecting to buildkit: %w", err)
	}
	defer bkc.Close()

	b.Log.Info("building the lbd toolchain image", "image", ref)

	bk := &Buildkit{Client: bkc, Log: b.Log, WorkloadIssuer: b.Issuer}
	res, err := bk.BuildImage(ctx, dfs, BuildStack{
		Stack: "dockerfile",
		Input: lbdmod.BuilderDockerfile,
	}, lbdmod.BuilderRepository, ref)
	if err != nil {
		return "", fmt.Errorf("building the lbd toolchain image: %w", err)
	}

	b.Log.Info("built the lbd toolchain image", "image", ref, "digest", res.ManifestDigest)
	return ref, nil
}

// present reports whether the toolchain image for this content hash is already
// in the registry. An artifact's entity name is the tag it was pushed under,
// so the tag is the lookup key.
func (b *LbdToolchain) present(ctx context.Context) bool {
	if b.EC == nil {
		return false
	}

	var artifact core_v1alpha.Artifact
	if err := b.EC.Get(ctx, lbdmod.BuilderTag(), &artifact); err != nil {
		return false
	}
	// An archived artifact has had, or is about to have, its blobs collected,
	// so it cannot be pulled and has to be rebuilt.
	return artifact.Status == core_v1alpha.ACTIVE
}
