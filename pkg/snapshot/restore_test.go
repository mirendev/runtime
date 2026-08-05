package snapshot

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoreImage(t *testing.T) {
	t.Run("round trip with mixed data", func(t *testing.T) {
		// 128KB: first half zeros, second half data
		srcData := make([]byte, 128*1024)
		for i := 64 * 1024; i < len(srcData); i++ {
			srcData[i] = byte(i % 251)
		}

		snapBuf := &bufferSeeker{}
		_, err := Backup(snapBuf, bytes.NewReader(srcData), "test", int64(len(srcData)), "ext4")
		require.NoError(t, err)

		snapReader := bytes.NewReader(snapBuf.Bytes())
		meta, err := ReadHeader(snapReader)
		require.NoError(t, err)

		outFile, err := os.CreateTemp(t.TempDir(), "restore-*")
		require.NoError(t, err)
		defer outFile.Close()

		require.NoError(t, outFile.Truncate(meta.SizeBytes))

		err = RestoreImage(outFile, snapReader, meta)
		require.NoError(t, err)

		_, err = outFile.Seek(0, io.SeekStart)
		require.NoError(t, err)
		restored, err := io.ReadAll(outFile)
		require.NoError(t, err)
		assert.Equal(t, srcData, restored)
	})

	t.Run("round trip all non-zero", func(t *testing.T) {
		srcData := make([]byte, 32*1024)
		for i := range srcData {
			srcData[i] = byte(i%254 + 1) // no zeros
		}

		snapBuf := &bufferSeeker{}
		_, err := Backup(snapBuf, bytes.NewReader(srcData), "test", int64(len(srcData)), "ext4")
		require.NoError(t, err)

		snapReader := bytes.NewReader(snapBuf.Bytes())
		meta, err := ReadHeader(snapReader)
		require.NoError(t, err)

		outFile, err := os.CreateTemp(t.TempDir(), "restore-*")
		require.NoError(t, err)
		defer outFile.Close()
		require.NoError(t, outFile.Truncate(meta.SizeBytes))

		require.NoError(t, RestoreImage(outFile, snapReader, meta))

		_, err = outFile.Seek(0, io.SeekStart)
		require.NoError(t, err)
		restored, err := io.ReadAll(outFile)
		require.NoError(t, err)
		assert.Equal(t, srcData, restored)
	})

	t.Run("round trip all zeros", func(t *testing.T) {
		srcData := make([]byte, 64*1024)

		snapBuf := &bufferSeeker{}
		_, err := Backup(snapBuf, bytes.NewReader(srcData), "test", int64(len(srcData)), "ext4")
		require.NoError(t, err)

		snapReader := bytes.NewReader(snapBuf.Bytes())
		meta, err := ReadHeader(snapReader)
		require.NoError(t, err)

		outFile, err := os.CreateTemp(t.TempDir(), "restore-*")
		require.NoError(t, err)
		defer outFile.Close()
		require.NoError(t, outFile.Truncate(meta.SizeBytes))

		require.NoError(t, RestoreImage(outFile, snapReader, meta))

		_, err = outFile.Seek(0, io.SeekStart)
		require.NoError(t, err)
		restored, err := io.ReadAll(outFile)
		require.NoError(t, err)
		assert.Equal(t, srcData, restored)
	})

	t.Run("round trip data-zeros-data pattern", func(t *testing.T) {
		// 3 blocks: data, zeros, data
		srcData := make([]byte, 12288)
		for i := range 4096 {
			srcData[i] = 0xAA
		}
		// middle 4096 stays zero
		for i := 8192; i < 12288; i++ {
			srcData[i] = 0xBB
		}

		snapBuf := &bufferSeeker{}
		_, err := Backup(snapBuf, bytes.NewReader(srcData), "test", int64(len(srcData)), "ext4")
		require.NoError(t, err)

		snapReader := bytes.NewReader(snapBuf.Bytes())
		meta, err := ReadHeader(snapReader)
		require.NoError(t, err)

		outFile, err := os.CreateTemp(t.TempDir(), "restore-*")
		require.NoError(t, err)
		defer outFile.Close()
		require.NoError(t, outFile.Truncate(meta.SizeBytes))

		require.NoError(t, RestoreImage(outFile, snapReader, meta))

		_, err = outFile.Seek(0, io.SeekStart)
		require.NoError(t, err)
		restored, err := io.ReadAll(outFile)
		require.NoError(t, err)
		assert.Equal(t, srcData, restored)
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		srcData := []byte("hello world test data for checksum")

		snapBuf := &bufferSeeker{}
		_, err := Backup(snapBuf, bytes.NewReader(srcData), "test", int64(len(srcData)), "ext4")
		require.NoError(t, err)

		snapReader := bytes.NewReader(snapBuf.Bytes())
		meta, err := ReadHeader(snapReader)
		require.NoError(t, err)
		meta.Checksum = "0000000000000000000000000000000000000000000000000000000000000000"

		outFile, err := os.CreateTemp(t.TempDir(), "restore-*")
		require.NoError(t, err)
		defer outFile.Close()
		require.NoError(t, outFile.Truncate(meta.SizeBytes))

		err = RestoreImage(outFile, snapReader, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}

func TestSparseWrite(t *testing.T) {
	t.Run("all zeros seeks past", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		require.NoError(t, f.Truncate(8192))

		data := make([]byte, 8192)
		err = sparseWrite(f, data)
		require.NoError(t, err)

		pos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(8192), pos)

		// Read back — should be all zeros (from truncate)
		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("all non-zero writes everything", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		data := make([]byte, 8192)
		for i := range data {
			data[i] = 0xFF
		}

		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("leading zeros", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		require.NoError(t, f.Truncate(8192))

		data := make([]byte, 8192)
		for i := 4096; i < 8192; i++ {
			data[i] = 0xAB
		}

		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("trailing zeros", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		require.NoError(t, f.Truncate(8192))

		data := make([]byte, 8192)
		for i := range 4096 {
			data[i] = 0xCD
		}

		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("leading and trailing zeros", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		require.NoError(t, f.Truncate(12288))

		// 3 blocks: zeros, data, zeros
		data := make([]byte, 12288)
		for i := 4096; i < 8192; i++ {
			data[i] = 0xEF
		}

		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("sub-block data written as-is", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		// Smaller than block size — no block-aligned detection possible
		data := []byte{0x00, 0x00, 0x01, 0x02, 0x00}
		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("multiple non-zero blocks surrounded by zeros", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sparse-*")
		require.NoError(t, err)
		defer f.Close()

		// 5 blocks: zeros, data, data, data, zeros
		require.NoError(t, f.Truncate(5*4096))
		data := make([]byte, 5*4096)
		for i := 4096; i < 4*4096; i++ {
			data[i] = byte(i % 253)
		}

		err = sparseWrite(f, data)
		require.NoError(t, err)

		_, err = f.Seek(0, io.SeekStart)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})
}

func TestAllZero(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		assert.True(t, allZero(nil))
	})

	t.Run("empty slice", func(t *testing.T) {
		assert.True(t, allZero([]byte{}))
	})

	t.Run("all zeros", func(t *testing.T) {
		assert.True(t, allZero(make([]byte, 4096)))
	})

	t.Run("non-zero at start", func(t *testing.T) {
		b := make([]byte, 4096)
		b[0] = 1
		assert.False(t, allZero(b))
	})

	t.Run("non-zero at end", func(t *testing.T) {
		b := make([]byte, 4096)
		b[4095] = 1
		assert.False(t, allZero(b))
	})

	t.Run("non-zero in middle", func(t *testing.T) {
		b := make([]byte, 4096)
		b[2048] = 1
		assert.False(t, allZero(b))
	})

	t.Run("single byte zero", func(t *testing.T) {
		assert.True(t, allZero([]byte{0}))
	})

	t.Run("single byte non-zero", func(t *testing.T) {
		assert.False(t, allZero([]byte{1}))
	})
}
