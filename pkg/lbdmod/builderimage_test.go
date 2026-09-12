package lbdmod

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/entity"
)

func TestBuilderVersionIsStable(t *testing.T) {
	// The tag is a content hash, so it must not move between calls -- a tag
	// that changed per process would rebuild and re-push on every install.
	first := BuilderVersion()
	assert.Equal(t, first, BuilderVersion())
	assert.Len(t, first, 16)
	assert.NotEmpty(t, first)
}

func TestBuilderImageReference(t *testing.T) {
	ref := BuilderImage("cluster.local:5000")
	assert.Equal(t, "cluster.local:5000/"+BuilderRepository+":"+BuilderTag(), ref)
	assert.Contains(t, ref, BuilderVersion(), "the tag carries the content hash")
}

func TestBuilderTagSurvivesArtifactGC(t *testing.T) {
	// An artifact's entity name is the tag it was pushed under, and that name
	// is all artifact GC has to tell a system image from an orphan. If this
	// ever stops holding, the toolchain image is collected within the hour and
	// its blobs deleted underneath the nodes still pulling it.
	assert.True(t, core_v1alpha.IsSystemArtifact(entity.Id("artifact/"+BuilderTag())))
	assert.False(t, core_v1alpha.IsSystemArtifact(entity.Id("artifact/orphan")))
}

func TestHashFSChangesWithContent(t *testing.T) {
	base := fstest.MapFS{
		"b/Dockerfile": {Data: []byte("FROM ubuntu:24.04\n")},
		"b/build.sh":   {Data: []byte("echo hi\n")},
	}
	changed := fstest.MapFS{
		"b/Dockerfile": {Data: []byte("FROM ubuntu:24.04\n")},
		"b/build.sh":   {Data: []byte("echo bye\n")},
	}

	baseSum, err := hashFS(base, "b")
	require.NoError(t, err)
	changedSum, err := hashFS(changed, "b")
	require.NoError(t, err)

	assert.NotEqual(t, baseSum, changedSum, "changing the build script must change the tag")

	// Same content hashes the same, so an unchanged toolchain is never rebuilt.
	again, err := hashFS(fstest.MapFS{
		"b/build.sh":   {Data: []byte("echo hi\n")},
		"b/Dockerfile": {Data: []byte("FROM ubuntu:24.04\n")},
	}, "b")
	require.NoError(t, err)
	assert.Equal(t, baseSum, again, "the hash must not depend on walk order")
}

func TestHashFSSeparatesNamesFromContent(t *testing.T) {
	// Without length-prefixing, a rename could be cancelled out by a content
	// change that shifts the same bytes across the boundary.
	a, err := hashFS(fstest.MapFS{"b/ab": {Data: []byte("c")}}, "b")
	require.NoError(t, err)
	b, err := hashFS(fstest.MapFS{"b/a": {Data: []byte("bc")}}, "b")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestMaterializeBuilderWritesABuildContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ctx")
	require.NoError(t, MaterializeBuilder(dir))

	// BuildKit's dockerfile frontend takes the filename relative to the
	// context root, so the Dockerfile has to sit at the top of it.
	dockerfile, err := os.ReadFile(filepath.Join(dir, BuilderDockerfile))
	require.NoError(t, err)
	assert.Contains(t, string(dockerfile), "FROM ubuntu:24.04")
	assert.Contains(t, string(dockerfile), "build-lbd")

	script, err := os.Stat(filepath.Join(dir, "build.sh"))
	require.NoError(t, err)
	// embed.FS drops the executable bit, and the image COPYs this in and runs
	// it, so materializing has to put it back.
	assert.NotZero(t, script.Mode().Perm()&0100, "build.sh must be executable")
}

func TestMaterializeBuilderIsRepeatable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ctx")
	require.NoError(t, MaterializeBuilder(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, BuilderDockerfile), []byte("clobbered"), 0644))

	require.NoError(t, MaterializeBuilder(dir))
	data, err := os.ReadFile(filepath.Join(dir, BuilderDockerfile))
	require.NoError(t, err)
	assert.NotEqual(t, "clobbered", string(data))
}

func TestIsBuilderImageRejectsForeignReferences(t *testing.T) {
	// This gates what a node will pull, run, and load into its kernel, so it
	// has to reject anything outside the cluster's own toolchain repository.
	for _, ref := range []string{
		"docker.io/library/ubuntu:24.04",
		"evil.example.com/miren-system/lbd-builder:v1",
		"cluster.local:5000/someapp:latest",
		// A prefix match on the host alone is not enough.
		"cluster.local:5000/miren-system/lbd-builder-evil:v1",
		// No tag at all.
		"cluster.local:5000/" + BuilderRepository,
		"cluster.local:5000/" + BuilderRepository + ":",
		"",
	} {
		assert.False(t, IsBuilderImage(ref), "should have rejected %q", ref)
	}
}

func TestIsBuilderImageAcceptsOurOwn(t *testing.T) {
	assert.True(t, IsBuilderImage(BuilderImage(ocireg.Host)))

	// A coordinator on a newer miren carries a different content hash, and
	// asking a node to build with it is legitimate.
	assert.True(t, IsBuilderImage(ocireg.Host+"/"+BuilderRepository+":miren-system-lbd-builder-0000000000000000"))
}
