package ocireg

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRepositoryName(t *testing.T) {
	valid := []string{
		"myapp",
		"my-app",
		"my_app",
		"my__app",
		"my.app",
		"team/myapp",
		"a/b/c/d",
		"app123",
		strings.Repeat("a", maxRepositoryNameLen),
	}

	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			assert.NoError(t, validateRepositoryName(name))
		})
	}

	invalid := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"dot dot", ".."},
		{"traversal", "../../etc"},
		{"embedded traversal", "team/../../etc"},
		{"leading slash", "/myapp"},
		{"trailing slash", "myapp/"},
		{"empty segment", "team//myapp"},
		{"uppercase", "MyApp"},
		{"colon", "app:tag"},
		{"null byte", "app\x00"},
		{"newline", "app\nmore"},
		{"too long", strings.Repeat("a", maxRepositoryNameLen+1)},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			assert.Error(t, validateRepositoryName(tc.value))
		})
	}
}

func TestValidateDigest(t *testing.T) {
	valid := []string{
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"sha512:abcdef",
		"multihash+base58:QmZZZ",
	}

	for _, digest := range valid {
		t.Run("valid/"+digest, func(t *testing.T) {
			assert.NoError(t, validateDigest(digest))
		})
	}

	invalid := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"no algorithm", "abcdef"},
		{"traversal", "../../etc/cron.d/pwn"},
		{"traversal with algorithm", "sha256:../../etc/passwd"},
		{"slash in value", "sha256:ab/cd"},
		{"uppercase algorithm", "SHA256:abcdef"},
		{"empty value", "sha256:"},
		{"null byte", "sha256:abc\x00"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			assert.Error(t, validateDigest(tc.value))
		})
	}
}

func TestValidateReference(t *testing.T) {
	// References are tags or digests; artifact ids from idgen are the common
	// case in this registry.
	valid := []string{
		"latest",
		"v1.2.3",
		"bK9vQmXr2sT4uW6yZ8aB1c",
		"artifact-1",
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	for _, reference := range valid {
		t.Run("valid/"+reference, func(t *testing.T) {
			assert.NoError(t, validateReference(reference))
		})
	}

	invalid := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"traversal", "../../etc/passwd"},
		{"dot dot", ".."},
		{"leading dot", ".hidden"},
		{"slash", "a/b"},
		{"too long", strings.Repeat("a", 129)},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			assert.Error(t, validateReference(tc.value))
		})
	}
}

func TestValidateUploadID(t *testing.T) {
	assert.NoError(t, validateUploadID("bK9vQmXr2sT4uW6yZ8aB1c"))
	assert.Error(t, validateUploadID(""))
	assert.Error(t, validateUploadID(".."))
	assert.Error(t, validateUploadID("../../etc/passwd"))
	assert.Error(t, validateUploadID("b-with-dash"))
}

// TestSplitEscapedPath pins the decoding behavior the routing depends on: an
// encoded slash must stay inside its segment rather than becoming a separator.
func TestSplitEscapedPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		want   []string
		wantOK bool
	}{
		{"version check", "/v2/", nil, true},
		{"simple", "/v2/app/blobs/sha256:abc", []string{"app", "blobs", "sha256:abc"}, true},
		{"nested name", "/v2/team/app/manifests/latest", []string{"team", "app", "manifests", "latest"}, true},
		{
			"encoded slash stays in segment",
			"/v2/%2e%2e%2f%2e%2e%2fetc/manifests/passwd",
			[]string{"../../etc", "manifests", "passwd"},
			true,
		},
		{"trailing slash keeps empty segment", "/v2/app/blobs/uploads/", []string{"app", "blobs", "uploads", ""}, true},
		{"not under v2", "/other/thing", nil, false},
		{"bad escape", "/v2/%zz/blobs/x", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := splitEscapedPath(tc.path)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestContainedPath(t *testing.T) {
	root := "/var/lib/miren/registry"

	t.Run("stays under root", func(t *testing.T) {
		got, err := containedPath(root, "blobs", "sha256:abc")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "blobs", "sha256:abc"), got)
	})

	t.Run("rejects escape", func(t *testing.T) {
		_, err := containedPath(root, "blobs", "../../../etc/passwd")
		assert.Error(t, err)
	})

	t.Run("rejects escape to sibling with shared prefix", func(t *testing.T) {
		// The containment check has to compare against root plus a separator,
		// or "/var/lib/miren/registry-evil" would look like a prefix match.
		_, err := containedPath(root, "..", "registry-evil", "blob")
		assert.Error(t, err)
	})

	t.Run("root itself is contained", func(t *testing.T) {
		got, err := containedPath(root)
		require.NoError(t, err)
		assert.Equal(t, root, got)
	})
}

func TestDigestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob")
	content := []byte("layer contents")
	require.NoError(t, os.WriteFile(path, content, 0644))

	sum := sha256.Sum256(content)
	want := "sha256:" + hex.EncodeToString(sum[:])

	t.Run("sha256", func(t *testing.T) {
		got, err := digestFile(path, want)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("sha512", func(t *testing.T) {
		sum512 := sha512.Sum512(content)
		want512 := "sha512:" + hex.EncodeToString(sum512[:])

		got, err := digestFile(path, want512)
		require.NoError(t, err)
		assert.Equal(t, want512, got)
	})

	// An algorithm we can't compute has to be an error, not a pass. Returning
	// nil here would let a client opt out of verification by naming something
	// exotic.
	t.Run("unsupported algorithm is an error", func(t *testing.T) {
		_, err := digestFile(path, "md5:abcdef")
		assert.Error(t, err)
	})

	t.Run("missing file is an error", func(t *testing.T) {
		_, err := digestFile(filepath.Join(t.TempDir(), "nope"), want)
		assert.Error(t, err)
	})
}
