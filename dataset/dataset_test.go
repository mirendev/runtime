package dataset_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/dataset"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
)

func TestDatasetManager(t *testing.T) {
	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	t.Run("can create and get dataset", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tmpDir := t.TempDir()
		manager, err := dataset.NewManager(log, tmpDir, "")
		r.NoError(err)

		// Create dataset
		mc := &dataset.DataSetsClient{
			NetworkClient: rpc.LocalClient(dataset.AdaptDataSets(manager)),
		}

		info := &dataset.DataSetInfo{}
		info.SetName("test-dataset")

		cres, err := mc.Create(ctx, info)
		r.NoError(err)
		r.NotNil(cres.Dataset())

		// Verify dataset directory and info file were created
		infoPath := filepath.Join(tmpDir, "test-dataset", "info.json")
		_, err = os.Stat(infoPath)
		r.NoError(err)

		// Read and verify info file contents
		data, err := os.ReadFile(infoPath)
		r.NoError(err)

		var savedInfo dataset.DataSetInfo
		err = json.Unmarshal(data, &savedInfo)
		r.NoError(err)
		r.Equal("test-dataset", savedInfo.Name())

		// Get dataset
		gres, err := mc.Get(ctx, "test-dataset")
		r.NoError(err)
		r.NotNil(gres.Dataset())
	})

	t.Run("can list datasets", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tmpDir := t.TempDir()
		manager, err := dataset.NewManager(log, tmpDir, "")
		r.NoError(err)

		mc := &dataset.DataSetsClient{
			NetworkClient: rpc.LocalClient(dataset.AdaptDataSets(manager)),
		}

		// Create multiple datasets
		datasets := []string{"dataset1", "dataset2", "dataset3"}
		for _, name := range datasets {
			info := &dataset.DataSetInfo{}
			info.SetName(name)

			_, err = mc.Create(ctx, info)
			r.NoError(err)
		}

		// List datasets
		lres, err := mc.List(ctx)
		r.NoError(err)

		// Verify list results
		results := lres.Datasets()
		r.Len(results, len(datasets))

		names := make([]string, len(results))
		for i, info := range results {
			names[i] = info.Name()
		}

		for _, expected := range datasets {
			r.Contains(names, expected)
		}
	})

	t.Run("can delete dataset", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tmpDir := t.TempDir()
		manager, err := dataset.NewManager(log, tmpDir, "")
		r.NoError(err)

		mc := &dataset.DataSetsClient{
			NetworkClient: rpc.LocalClient(dataset.AdaptDataSets(manager)),
		}

		// Create dataset
		info := &dataset.DataSetInfo{}
		info.SetName("delete-test")

		_, err = mc.Create(ctx, info)
		r.NoError(err)

		// Delete dataset
		_, err = mc.Delete(ctx, "delete-test")
		r.NoError(err)

		// Verify dataset directory was removed
		_, err = os.Stat(filepath.Join(tmpDir, "delete-test"))
		r.True(os.IsNotExist(err))

		// Verify dataset cannot be retrieved
		gres, err := mc.Get(ctx, "delete-test")
		r.Error(err)
		r.Nil(gres)
		r.Contains(err.Error(), "not found")
	})

	t.Run("can write and read segments", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tmpDir := t.TempDir()
		manager, err := dataset.NewManager(log, tmpDir, "")
		r.NoError(err)

		mc := &dataset.DataSetsClient{
			NetworkClient: rpc.LocalClient(dataset.AdaptDataSets(manager)),
		}

		// Create dataset
		info := &dataset.DataSetInfo{}
		info.SetName("segment-test")

		cres, err := mc.Create(ctx, info)
		r.NoError(err)
		ds := cres.Dataset()

		// Create new segment
		res, err := ds.NewSegment(ctx, "seg1", nil)
		r.NoError(err)
		writer := res.Writer()

		// Write data to segment
		testData := []byte("test segment data")

		wres, err := writer.WriteAt(ctx, 0, testData)
		r.NoError(err)
		r.Equal(int64(len(testData)), wres.Count())

		// Close segment writer
		_, err = writer.Close(ctx)
		r.NoError(err)

		// List segments
		lres, err := ds.ListSegments(ctx)
		r.NoError(err)
		segments := lres.Segments()
		r.Contains(segments, "seg1")

		// Read segment
		sres, err := ds.ReadSegment(ctx, "seg1")
		r.NoError(err)
		r.NotNil(sres.Reader())
	})
}
