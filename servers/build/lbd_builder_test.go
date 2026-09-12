package build

import (
	"context"
	"testing"

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

func TestEnsureLbdBuilderImageNeedsBuildkit(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &LbdToolchain{Log: testutils.TestLogger(t), EC: entityserver.NewClient(testutils.TestLogger(t), inmem.EAC)}

	_, err := b.EnsureLbdBuilderImage(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no buildkit available")
}
