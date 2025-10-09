package lsvd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

type mockVolume struct {
	segments       []SegmentId
	segmentData    map[SegmentId][]byte
	segmentLayouts map[SegmentId]*SegmentLayout
	listErr        error
	openErr        error
	newErr         error
}

func (m *mockVolume) Info(ctx context.Context) (*VolumeInfo, error) {
	return &VolumeInfo{Name: "test", Size: 1024}, nil
}

func (m *mockVolume) ListSegments(ctx context.Context) ([]SegmentId, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.segments, nil
}

func (m *mockVolume) OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	data, ok := m.segmentData[seg]
	if !ok {
		return nil, os.ErrNotExist
	}
	layout := m.segmentLayouts[seg]
	return &mockSegmentReader{data: data, layout: layout}, nil
}

func (m *mockVolume) NewSegment(ctx context.Context, seg SegmentId, layout *SegmentLayout, data *os.File) error {
	if m.newErr != nil {
		return m.newErr
	}
	// Read all data from file
	content, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	m.segmentData[seg] = content
	m.segmentLayouts[seg] = layout
	m.segments = append(m.segments, seg)
	return nil
}

func (m *mockVolume) RemoveSegment(ctx context.Context, seg SegmentId) error {
	return nil
}

type mockSegmentReader struct {
	data   []byte
	layout *SegmentLayout
	offset int64
}

func (m *mockSegmentReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n = copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *mockSegmentReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.offset = offset
	case io.SeekCurrent:
		m.offset += offset
	case io.SeekEnd:
		m.offset = int64(len(m.data)) + offset
	}
	return m.offset, nil
}

func (m *mockSegmentReader) Read(p []byte) (n int, err error) {
	n, err = m.ReadAt(p, m.offset)
	m.offset += int64(n)
	return n, err
}

func (m *mockSegmentReader) Close() error {
	return nil
}

func (m *mockSegmentReader) Layout(ctx context.Context) (*SegmentLayout, error) {
	return m.layout, nil
}

func TestSegmentReconciler(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	log := slog.Default()

	// Create test segments
	baseTime := uint64(1704067200000) // 2024-01-01
	seg1 := SegmentId(ulid.MustNew(baseTime+1000, testEntropy))
	seg2 := SegmentId(ulid.MustNew(baseTime+2000, testEntropy))
	seg3 := SegmentId(ulid.MustNew(baseTime+3000, testEntropy))
	seg4 := SegmentId(ulid.MustNew(baseTime+4000, testEntropy))

	t.Run("no missing segments", func(t *testing.T) {
		primary := &mockVolume{
			segments:       []SegmentId{seg1, seg2},
			segmentData:    make(map[SegmentId][]byte),
			segmentLayouts: make(map[SegmentId]*SegmentLayout),
		}
		replica := &mockVolume{
			segments:       []SegmentId{seg1, seg2},
			segmentData:    make(map[SegmentId][]byte),
			segmentLayouts: make(map[SegmentId]*SegmentLayout),
		}

		reconciler := NewSegmentReconciler(log, primary, replica)
		result, err := reconciler.Reconcile(ctx)
		r.NoError(err)
		r.Equal(2, result.TotalPrimary)
		r.Equal(2, result.TotalReplica)
		r.Equal(0, result.Missing)
		r.Equal(0, result.Uploaded)
		r.Equal(0, result.Failed)
	})

	t.Run("missing segments", func(t *testing.T) {
		layout1 := &SegmentLayout{}
		layout2 := &SegmentLayout{}

		primary := &mockVolume{
			segments: []SegmentId{seg1, seg2, seg3, seg4},
			segmentData: map[SegmentId][]byte{
				seg1: []byte("segment1-data"),
				seg2: []byte("segment2-data"),
				seg3: []byte("segment3-data"),
				seg4: []byte("segment4-data"),
			},
			segmentLayouts: map[SegmentId]*SegmentLayout{
				seg1: layout1,
				seg2: layout2,
				seg3: layout1,
				seg4: layout2,
			},
		}

		replica := &mockVolume{
			segments: []SegmentId{seg1, seg2},
			segmentData: map[SegmentId][]byte{
				seg1: []byte("segment1-data"),
				seg2: []byte("segment2-data"),
			},
			segmentLayouts: map[SegmentId]*SegmentLayout{
				seg1: layout1,
				seg2: layout2,
			},
		}

		reconciler := NewSegmentReconciler(log, primary, replica)
		result, err := reconciler.Reconcile(ctx)
		r.NoError(err)
		r.Equal(4, result.TotalPrimary)
		r.Equal(2, result.TotalReplica)
		r.Equal(2, result.Missing)
		r.Equal(2, result.Uploaded)
		r.Equal(0, result.Failed)

		// Verify segments were uploaded
		r.Contains(replica.segmentData, seg3)
		r.Contains(replica.segmentData, seg4)
		r.Equal([]byte("segment3-data"), replica.segmentData[seg3])
		r.Equal([]byte("segment4-data"), replica.segmentData[seg4])
		r.Equal(layout1, replica.segmentLayouts[seg3])
		r.Equal(layout2, replica.segmentLayouts[seg4])
	})

	t.Run("empty primary", func(t *testing.T) {
		primary := &mockVolume{
			segments:       []SegmentId{},
			segmentData:    make(map[SegmentId][]byte),
			segmentLayouts: make(map[SegmentId]*SegmentLayout),
		}
		replica := &mockVolume{
			segments:       []SegmentId{seg1},
			segmentData:    make(map[SegmentId][]byte),
			segmentLayouts: make(map[SegmentId]*SegmentLayout),
		}

		reconciler := NewSegmentReconciler(log, primary, replica)
		result, err := reconciler.Reconcile(ctx)
		r.NoError(err)
		r.Equal(0, result.TotalPrimary)
		r.Equal(1, result.TotalReplica)
		r.Equal(0, result.Missing)
		r.Equal(0, result.Uploaded)
	})

	t.Run("empty replica", func(t *testing.T) {
		layout := &SegmentLayout{}
		primary := &mockVolume{
			segments: []SegmentId{seg1, seg2},
			segmentData: map[SegmentId][]byte{
				seg1: []byte("data1"),
				seg2: []byte("data2"),
			},
			segmentLayouts: map[SegmentId]*SegmentLayout{
				seg1: layout,
				seg2: layout,
			},
		}
		replica := &mockVolume{
			segments:       []SegmentId{},
			segmentData:    make(map[SegmentId][]byte),
			segmentLayouts: make(map[SegmentId]*SegmentLayout),
		}

		reconciler := NewSegmentReconciler(log, primary, replica)
		result, err := reconciler.Reconcile(ctx)
		r.NoError(err)
		r.Equal(2, result.TotalPrimary)
		r.Equal(0, result.TotalReplica)
		r.Equal(2, result.Missing)
		r.Equal(2, result.Uploaded)
		r.Equal(0, result.Failed)

		// Verify both segments were uploaded
		r.Len(replica.segmentData, 2)
		r.Equal([]byte("data1"), replica.segmentData[seg1])
		r.Equal([]byte("data2"), replica.segmentData[seg2])
	})
}
