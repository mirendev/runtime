package build

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/lbdmod"
)

func TestEnsureLbdBuilderImageSkipsAnExistingImage(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	// The artifact's entity name is the tag it was pushed under, so a prior
	// build shows up under exactly this name.
	_, err := inmem.Client.Create(ctx, lbdmod.BuilderTag(),
		&core_v1alpha.Artifact{Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	// BuildKit is deliberately nil: finding the image must short-circuit
	// before anything tries to build, or every install would rebuild.
	b := &LbdToolchain{Log: log, EC: entityserver.NewClient(log, inmem.EAC)}

	ref, err := b.EnsureLbdBuilderImage(ctx)
	require.NoError(t, err)
	assert.Equal(t, lbdmod.BuilderImage(ocireg.Host), ref)
}

func TestEnsureLbdBuilderImageRebuildsAnArchivedImage(t *testing.T) {
	// An archived artifact has had, or is about to have, its blobs collected,
	// so it cannot be pulled. Treating it as present would hand nodes a
	// reference that fails at pull time.
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	_, err := inmem.Client.Create(ctx, lbdmod.BuilderTag(),
		&core_v1alpha.Artifact{Status: core_v1alpha.ARCHIVED})
	require.NoError(t, err)

	b := &LbdToolchain{Log: log, EC: entityserver.NewClient(log, inmem.EAC)}

	_, err = b.EnsureLbdBuilderImage(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no buildkit", "it should have tried to rebuild")
}

func TestEnsureLbdBuilderImageGivesUpWhenTheCallerDoes(t *testing.T) {
	// A toolchain build takes minutes, so a second caller has to be able to
	// walk away rather than pinning an RPC handler until the first one lands.
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	lbdBuilderLock <- struct{}{}
	defer func() { <-lbdBuilderLock }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	log := testutils.TestLogger(t)
	b := &LbdToolchain{Log: log, EC: entityserver.NewClient(log, inmem.EAC)}

	done := make(chan error, 1)
	go func() {
		_, err := b.EnsureLbdBuilderImage(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("it blocked on the build lock instead of honouring the cancelled context")
	}
}

func TestEnsureLbdBuilderImageNeedsBuildkit(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &LbdToolchain{Log: testutils.TestLogger(t), EC: entityserver.NewClient(testutils.TestLogger(t), inmem.EAC)}

	_, err := b.EnsureLbdBuilderImage(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no buildkit available")
}
