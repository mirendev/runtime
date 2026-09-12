package disk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/snapshot"
)

// diskImage builds content that compresses to something big enough to be worth
// resuming, and that a wrong byte would show up in.
func diskImage(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

// A resumed backup must reuse the snapshot it already compressed. Compressing
// again would read the image as it is now, which for a live disk is not the
// image the client already holds half of, and the two halves would not be a
// snapshot of anything.
func TestBackupResumeReusesTheStagedSnapshot(t *testing.T) {
	content := diskImage(256 * 1024)
	s, _, _, imagePath := newTestServer(t, content)
	prog := s.newProgress(context.Background(), nil)

	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)

	first, meta, err := s.stageForClient("t1", prog, target)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	require.NotZero(t, meta.CompressedSize)

	// Change the image underneath, as a live disk would.
	require.NoError(t, os.WriteFile(imagePath, diskImage(256*1024+7777), 0644))

	second, meta2, err := s.stageForClient("t1", prog, target)
	require.NoError(t, err)
	require.NoError(t, second.Close())

	assert.Equal(t, *meta, *meta2, "a resumed transfer must describe the same snapshot")

	// A different transfer id is a different backup, and does see the new image.
	third, meta3, err := s.stageForClient("t2", prog, target)
	require.NoError(t, err)
	require.NoError(t, third.Close())
	assert.NotEqual(t, meta.Checksum, meta3.Checksum)
}

// The point of the whole exercise: bytes delivered in two pieces, split at an
// arbitrary offset, must restore to exactly the original image.
func TestBackupSplitAcrossTwoAttemptsRestoresIdentically(t *testing.T) {
	content := diskImage(512 * 1024)
	s, _, _, _ := newTestServer(t, content)
	prog := s.newProgress(context.Background(), nil)

	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)

	staged, meta, err := s.stageForClient("t1", prog, target)
	require.NoError(t, err)
	defer staged.Close()

	// First attempt gets a third of the way and dies.
	cut := meta.CompressedSize / 3
	require.Positive(t, cut)

	firstHalf := make([]byte, cut)
	_, err = io.ReadFull(staged, firstHalf)
	require.NoError(t, err)

	// Second attempt resumes from exactly what arrived.
	resumed, meta2, err := s.stageForClient("t1", prog, target)
	require.NoError(t, err)
	defer resumed.Close()
	require.Equal(t, meta.CompressedSize, meta2.CompressedSize)

	_, err = resumed.Seek(cut, io.SeekStart)
	require.NoError(t, err)
	secondHalf, err := io.ReadAll(resumed)
	require.NoError(t, err)

	stitched := append(append([]byte{}, firstHalf...), secondHalf...)
	require.Len(t, stitched, int(meta.CompressedSize))

	// And what the client ends up with is a snapshot that restores to the
	// original image, checksum and all.
	src := bytes.NewReader(stitched)
	hdr, err := snapshot.ReadHeader(src)
	require.NoError(t, err)
	assert.Equal(t, meta.Checksum, hdr.Checksum)

	restored := filepath.Join(t.TempDir(), "restored.img")
	require.NoError(t, s.writeImage(restored, src, 0, hdr, prog))

	got, err := os.ReadFile(restored)
	require.NoError(t, err)
	assert.Equal(t, content, got, "a resumed backup must restore byte for byte")
}

// The mirror case: an upload that arrives in two pieces leaves the server
// holding exactly the file the client sent.
func TestUploadSplitAcrossTwoAttemptsIsReassembled(t *testing.T) {
	content := diskImage(256 * 1024)
	s, _, _, _ := newTestServer(t, content)
	prog := s.newProgress(context.Background(), nil)

	// A real snapshot, so what is reassembled can be checked by restoring it.
	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)
	staged, meta, err := s.stageForClient("src", prog, target)
	require.NoError(t, err)
	snapBytes, err := io.ReadAll(staged)
	require.NoError(t, err)
	require.NoError(t, staged.Close())
	require.Len(t, snapBytes, int(meta.CompressedSize))

	cut := len(snapBytes) / 2

	// First attempt delivers half, then the connection dies.
	f, err := s.openTransfer("up1", 0)
	require.NoError(t, err)
	_, err = f.Write(snapBytes[:cut])
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())

	// The client asks where to pick up, and is told what actually landed.
	path, err := s.transferPath("up1")
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(cut), info.Size())

	// Second attempt sends the rest from there.
	f, err = s.openTransfer("up1", int64(cut))
	require.NoError(t, err)
	_, err = f.Write(snapBytes[cut:])
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, snapBytes, got, "the reassembled upload must match what was sent")

	// And it really is a usable snapshot.
	src := bytes.NewReader(got)
	hdr, err := snapshot.ReadHeader(src)
	require.NoError(t, err)

	restored := filepath.Join(t.TempDir(), "restored.img")
	require.NoError(t, s.writeImage(restored, src, 0, hdr, prog))

	back, err := os.ReadFile(restored)
	require.NoError(t, err)
	assert.Equal(t, content, back)
}

// A completed backup releases its staging, so a transfer that finished does not
// keep a copy of the disk around.
func TestFinishedBackupReleasesItsStaging(t *testing.T) {
	s, _, _, _ := newTestServer(t, diskImage(64*1024))
	prog := s.newProgress(context.Background(), nil)

	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)

	staged, _, err := s.stageForClient("t1", prog, target)
	require.NoError(t, err)
	require.NoError(t, staged.Close())

	path, err := s.transferPath("t1")
	require.NoError(t, err)
	metaPath, err := s.stagingMetaPath("t1")
	require.NoError(t, err)
	require.FileExists(t, path)
	require.FileExists(t, metaPath)

	s.discardStaging("t1")

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(metaPath)
	assert.True(t, os.IsNotExist(err))
}

// An interrupted upload is exactly what the transfer file exists for and must
// survive. A refused one never will be picked up, because the client does not
// retry a refusal, so leaving it would keep a copy of the snapshot around for
// every attempt until the sweep.
func TestOnlyRefusedUploadsAreDiscarded(t *testing.T) {
	interrupted := errors.New("connection reset by peer")
	refused := cond.ValidationFailure("disk-backup", `disk "data" is in use`)

	assert.True(t, shouldDiscardUpload("t1", "", refused))
	assert.False(t, shouldDiscardUpload("t1", "", interrupted))

	// A restore point came from the cloud, so there is no upload to discard.
	assert.False(t, shouldDiscardUpload("t1", "point-1", refused))

	// And nothing to discard without an id.
	assert.False(t, shouldDiscardUpload("", "", refused))
}
