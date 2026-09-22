package ocireg

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobStorePutAndOpen(t *testing.T) {
	store := NewBlobStore(t.TempDir())
	contents := []byte("static artifact")

	digest, size, err := store.Put(bytes.NewReader(contents))
	require.NoError(t, err)
	assert.Equal(t, int64(len(contents)), size)
	assert.Equal(t, "sha256:6eeeb8b0edb6d556c032d00d385658103e20b03112b6dc7e3db0efdc8471df76", digest)

	secondDigest, secondSize, err := store.Put(bytes.NewReader(contents))
	require.NoError(t, err)
	assert.Equal(t, digest, secondDigest)
	assert.Equal(t, size, secondSize)

	blob, err := store.Open(digest)
	require.NoError(t, err)
	defer blob.Close()
	got, err := io.ReadAll(blob)
	require.NoError(t, err)
	assert.Equal(t, contents, got)
}

func TestBlobStoreOpenRejectsInvalidDigest(t *testing.T) {
	store := NewBlobStore(t.TempDir())

	_, err := store.Open("../outside")
	require.Error(t, err)
}
