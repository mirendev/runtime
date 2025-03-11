package dataset

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/units"
)

type SegmentAccess struct {
	log      *slog.Logger
	datasets *DataSetsClient
	formats  []string
	host     string
}

type segmentOpts struct {
	host string
}

type SegmentOption func(*segmentOpts)

func WithHost(host string) SegmentOption {
	return func(opts *segmentOpts) {
		opts.host = host
	}
}

func NewSegmentAccess(log *slog.Logger, datasets *DataSetsClient, formats []string, opts ...SegmentOption) *SegmentAccess {
	var so segmentOpts

	for _, opt := range opts {
		opt(&so)
	}

	return &SegmentAccess{
		log:      log,
		datasets: datasets,
		formats:  formats,
		host:     so.host,
	}
}

var _ lsvd.SegmentAccess = (*SegmentAccess)(nil)

func (sa *SegmentAccess) InitContainer(ctx context.Context) error {
	// Container initialization is handled by dataset.Manager
	return nil
}

func (sa *SegmentAccess) InitVolume(ctx context.Context, vol *lsvd.VolumeInfo) error {
	info := &DataSetInfo{}
	info.SetName(vol.Name)
	info.SetSize(vol.Size.Int64())
	info.SetFormats(append([]string{
		"block/raw",
		"fs/ext4",
	}, sa.formats...))

	_, err := sa.datasets.Create(ctx, info)
	if err != nil {
		return errors.Wrap(err, "create dataset")
	}

	return nil
}

func (sa *SegmentAccess) ListVolumes(ctx context.Context) ([]string, error) {
	res, err := sa.datasets.List(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "list datasets")
	}

	datasets := res.Datasets()
	names := make([]string, len(datasets))
	for i, ds := range datasets {
		names[i] = ds.Name()
	}

	return names, nil
}

func (sa *SegmentAccess) RemoveSegment(ctx context.Context, seg lsvd.SegmentId) error {
	// Since segments are managed within volumes, this is a no-op at the container level
	return nil
}

func (sa *SegmentAccess) OpenVolume(ctx context.Context, vol string) (lsvd.Volume, error) {
	res, err := sa.datasets.Get(ctx, vol)
	if err != nil {
		return nil, errors.Wrap(err, "get dataset")
	}

	return &volumeAdapter{
		log:     sa.log,
		sa:      sa,
		dataset: res.Dataset(),
	}, nil
}

func (sa *SegmentAccess) GetVolumeInfo(ctx context.Context, vol string) (*lsvd.VolumeInfo, error) {
	res, err := sa.datasets.Get(ctx, vol)
	if err != nil {
		return nil, errors.Wrap(err, "get dataset")
	}

	info, err := res.Dataset().GetInfo(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "get dataset info")
	}

	data := info.Info()

	return &lsvd.VolumeInfo{
		Name: data.Name(),
		Size: units.Bytes(data.Size()),
	}, nil
}

type volumeAdapter struct {
	log     *slog.Logger
	sa      *SegmentAccess
	dataset *DataSetClient
}

func (v *volumeAdapter) Info(ctx context.Context) (*lsvd.VolumeInfo, error) {
	res, err := v.dataset.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	data := res.Info()

	return &lsvd.VolumeInfo{
		Name: data.Name(),
		Size: units.Bytes(data.Size()),
	}, nil
}

func (v *volumeAdapter) ListSegments(ctx context.Context) ([]lsvd.SegmentId, error) {
	res, err := v.dataset.ListSegments(ctx)
	if err != nil {
		return nil, err
	}

	segments := make([]lsvd.SegmentId, len(res.Segments()))
	for i, seg := range res.Segments() {
		sid, err := lsvd.ParseSegment(seg)
		if err != nil {
			return nil, err
		}
		segments[i] = sid
	}

	return segments, nil
}

type segmentReaderAdapter struct {
	ctx    context.Context
	log    *slog.Logger
	stream *SegmentReaderClient
}

func (s *segmentReaderAdapter) ReadAt(p []byte, off int64) (int, error) {
	data, err := s.stream.ReadAt(s.ctx, off, int64(len(p)))
	if err != nil {
		return 0, err
	}

	n := copy(p, data.Data())
	return n, nil
}

func (s *segmentReaderAdapter) Close() error {
	_, err := s.stream.Close(s.ctx)
	return err
}

func (s *segmentReaderAdapter) Layout(ctx context.Context) (*lsvd.SegmentLayout, error) {
	res, err := s.stream.Layout(ctx)
	if err != nil {
		return nil, err
	}

	var diskExtents []lsvd.ExternalExtentHeader

	for _, dsext := range res.Layout().Extents() {
		var ext lsvd.ExternalExtentHeader

		ext.SetLba(dsext.Lba())
		ext.SetBlocks(dsext.Blocks())
		ext.SetOffset(dsext.Offset())
		ext.SetSize(dsext.Size())
		ext.SetRawSize(dsext.RawSize())

		diskExtents = append(diskExtents, ext)
	}

	var layout lsvd.SegmentLayout
	layout.SetExtents(diskExtents)

	return &layout, nil
}

type dataPathAdapter struct {
	ctx    context.Context
	log    *slog.Logger
	stream *SegmentReaderClient

	url string
	ttl time.Duration
}

func (s *dataPathAdapter) ReadAt(p []byte, off int64) (int, error) {
	s.log.Info("reading from data path", "url", s.url, "offset", off, "length", len(p))

	req, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		s.log.Error("failed to create request", "error", err)
		return 0, err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p)-1)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.log.Error("failed to perform request", "error", err)
		return 0, err
	}

	defer resp.Body.Close()

	n, err := io.ReadFull(resp.Body, p)
	if err == io.ErrUnexpectedEOF {
		err = nil
	}

	if err != nil {
		s.log.Error("failed to read response body", "error", err, "n", n)
	}

	s.log.Info("read from data path", "n", n)

	return n, err
}

func (s *dataPathAdapter) Close() error {
	_, err := s.stream.Close(s.ctx)
	return err
}

func (s *dataPathAdapter) Layout(ctx context.Context) (*lsvd.SegmentLayout, error) {
	res, err := s.stream.Layout(ctx)
	if err != nil {
		return nil, err
	}

	var diskExtents []lsvd.ExternalExtentHeader

	for _, dsext := range res.Layout().Extents() {
		var ext lsvd.ExternalExtentHeader

		ext.SetLba(dsext.Lba())
		ext.SetBlocks(dsext.Blocks())
		ext.SetOffset(dsext.Offset())
		ext.SetSize(dsext.Size())
		ext.SetRawSize(dsext.RawSize())

		diskExtents = append(diskExtents, ext)
	}

	var layout lsvd.SegmentLayout
	layout.SetExtents(diskExtents)

	return &layout, nil
}

func (v *volumeAdapter) OpenSegment(ctx context.Context, seg lsvd.SegmentId) (lsvd.SegmentReader, error) {
	res, err := v.dataset.ReadSegment(ctx, seg.String())
	if err != nil {
		return nil, err
	}

	reader := res.Reader()

	dpres, err := reader.DataPath(ctx)
	if err != nil {
		v.log.Warn("failed to get data path", "error", err)
	}

	if dpres.HasDataPath() {
		dataPath := dpres.DataPath()

		url := dataPath.Url()

		if strings.HasPrefix(url, "/") && v.sa.host != "" {
			url = v.sa.host + url
		}

		dur := standard.FromDuration(dataPath.Ttl())

		v.log.Debug("detected data path for segment", "url", url, "ttl", dur)

		return &dataPathAdapter{
			ctx:    ctx,
			log:    v.log,
			stream: res.Reader(),
			url:    url,
			ttl:    dur,
		}, nil
	}

	return &segmentReaderAdapter{
		ctx:    ctx,
		log:    v.log,
		stream: res.Reader(),
	}, nil
}

func (v *volumeAdapter) AppendSegment(ctx context.Context, seg lsvd.SegmentId, f *os.File) error {
	panic("no")
	res, err := v.dataset.NewSegment(ctx, seg.String(), nil)
	if err != nil {
		return err
	}

	writer := res.Writer()

	defer writer.Close(ctx)

	// Copy the file contents to the segment
	buf := make([]byte, 32*1024)
	offset := int64(0)

	for {
		n, err := f.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read source file: %w", err)
		}

		wres, err := writer.WriteAt(ctx, offset, buf[:n])
		if err != nil {
			return fmt.Errorf("write segment: %w", err)
		}

		offset += wres.Count()
	}

	_, err = writer.Close(ctx)
	if err != nil {
		return fmt.Errorf("close segment: %w", err)
	}

	return nil
}

func (v *volumeAdapter) NewSegment(
	ctx context.Context,
	seg lsvd.SegmentId,
	layout *lsvd.SegmentLayout,
	f *os.File,
) error {
	var extents []Extent
	for _, ext := range layout.Extents() {
		var e Extent

		e.SetLba(ext.Lba())
		e.SetBlocks(ext.Blocks())
		e.SetOffset(ext.Offset())
		e.SetSize(ext.Size())
		e.SetRawSize(ext.RawSize())

		extents = append(extents, e)
	}

	var sl SegmentLayout
	sl.SetExtents(extents)

	res, err := v.dataset.NewSegment(ctx, seg.String(), &sl)
	if err != nil {
		return err
	}

	writer := res.Writer()

	defer writer.Close(ctx)

	// Copy the file contents to the segment
	buf := make([]byte, 1024*1024)
	offset := int64(0)

	for {
		n, err := f.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read source file: %w", err)
		}

		wres, err := writer.WriteAt(ctx, offset, buf[:n])
		if err != nil {
			return fmt.Errorf("write segment: %w", err)
		}

		offset += wres.Count()
	}

	_, err = writer.Close(ctx)
	if err != nil {
		return fmt.Errorf("close segment: %w", err)
	}

	return nil
}

func (v *volumeAdapter) RemoveSegment(ctx context.Context, seg lsvd.SegmentId) error {
	// The dataset API doesn't provide direct segment removal
	// This would need to be added to the RPC interface
	return nil
}
