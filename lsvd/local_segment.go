package lsvd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/pkg/errors"
)

type LocalFile struct {
	f   *os.File
	log *slog.Logger
}

func (l *LocalFile) ReadAt(b []byte, off int64) (int, error) {
	l.log.Debug("read at", "off", off, "len", len(b), "path", l.f.Name())
	n, err := l.f.ReadAt(b, off)
	l.log.Debug("read at done", "off", off, "len", len(b), "path", l.f.Name(), "n", n, "err", err)

	if n > 0 {
		return n, nil
	}

	return n, err
}

func OpenLocalFile(path string, log *slog.Logger) (*LocalFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return &LocalFile{f: f, log: log}, nil
}

func (l *LocalFile) Close() error {
	return l.f.Close()
}

type LocalFileAccess struct {
	Log *slog.Logger
	Dir string
}

var _ SegmentAccess = (*LocalFileAccess)(nil)

func (l *LocalFileAccess) OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error) {
	return OpenLocalFile(
		filepath.Join(l.Dir, "segments", "segment."+ulid.ULID(seg).String()), l.Log)
}

func ReadSegments(f io.Reader) ([]SegmentId, error) {
	var out []SegmentId

	br := bufio.NewReader(f)

	for {
		var seg SegmentId

		_, err := br.Read(seg[:])
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		out = append(out, seg)
	}

	return out, nil
}

func (l *LocalFileAccess) ListSegments(ctx context.Context, vol string) ([]SegmentId, error) {
	f, err := os.Open(filepath.Join(l.Dir, "volumes", vol, "segments"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	defer f.Close()

	return ReadSegments(f)
}

func (l *LocalFileAccess) WriteMetadata(ctx context.Context, vol, name string) (io.WriteCloser, error) {
	f, err := os.Create(filepath.Join(l.Dir, "volumes", vol, name))
	return f, err
}

func (l *LocalFileAccess) ReadMetadata(ctx context.Context, vol, name string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(l.Dir, "volumes", vol, name))
	return f, err
}

func (l *LocalFileAccess) RemoveSegment(ctx context.Context, seg SegmentId) error {
	return os.Remove(
		filepath.Join(l.Dir, "segments", "segment."+ulid.ULID(seg).String()))
}

func (l *LocalFileAccess) WriteSegment(ctx context.Context, seg SegmentId) (io.WriteCloser, error) {
	path := filepath.Join(l.Dir, "segments", "segment."+ulid.ULID(seg).String())
	return os.Create(path)
}

func (l *LocalFileAccess) UploadSegment(ctx context.Context, seg SegmentId, f *os.File) error {
	path := filepath.Join(l.Dir, "segments", "segment."+ulid.ULID(seg).String())
	dest, err := os.Create(path)
	if err != nil {
		return err
	}

	defer dest.Close()

	n, err := io.Copy(dest, f)
	if err != nil {
		l.Log.Error("error saving segment", "error", err, "path", path)
	} else {
		l.Log.Debug("saved segment", "path", path, "size", n)
	}

	return err
}

func (l *LocalFileAccess) AppendToSegments(ctx context.Context, vol string, seg SegmentId) error {
	segments, err := l.ListSegments(ctx, vol)
	if err != nil {
		return err
	}

	path := filepath.Join(l.Dir, "volumes", vol, "segments")

	segments = append(segments, seg)

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer f.Close()

	bw := bufio.NewWriter(f)

	defer bw.Flush()

	for _, seg := range segments {
		_, err = bw.Write(seg[:])
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *LocalFileAccess) RemoveSegmentFromVolume(ctx context.Context, vol string, seg SegmentId) error {
	var buf bytes.Buffer

	segmentsPath := filepath.Join(l.Dir, "volumes", vol, "segments")
	f, err := os.OpenFile(segmentsPath, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}

	defer f.Close()

	var cur SegmentId

	for {
		_, err = f.Read(cur[:])
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return err
		}

		if seg != cur {
			buf.Write(cur[:])
		}
	}

	f.Close()

	f, err = os.Create(segmentsPath)
	if err != nil {
		return err
	}

	io.Copy(f, &buf)

	return nil
}

func (l *LocalFileAccess) InitContainer(ctx context.Context) error {
	for _, p := range []string{"segments", "volumes"} {
		path := filepath.Join(l.Dir, p)
		fi, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			err = os.Mkdir(path, 0755)
			if err != nil {
				return err
			}
		} else if !fi.Mode().IsDir() {
			return fmt.Errorf("sub path not a directory: %s", path)
		}
	}

	return nil
}

func (l *LocalFileAccess) InitVolume(ctx context.Context, vol *VolumeInfo) error {
	if vol.Name == "" {
		return fmt.Errorf("volume name must not be empty")
	}

	path := filepath.Join(l.Dir, "volumes", vol.Name)

	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	err = os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(path, "info.json"))
	if err != nil {
		return err
	}

	defer f.Close()

	return json.NewEncoder(f).Encode(&vol)
}

func (l *LocalFileAccess) ListVolumes(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(l.Dir, "volumes"))
	if err != nil {
		return nil, err
	}

	var volumes []string

	for _, ent := range entries {
		volumes = append(volumes, ent.Name())
	}

	return volumes, nil
}

func (l *LocalFileAccess) GetVolumeInfo(ctx context.Context, vol string) (*VolumeInfo, error) {
	f, err := os.Open(filepath.Join(l.Dir, "volumes", vol, "info.json"))
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var vi VolumeInfo
	err = json.NewDecoder(f).Decode(&vi)
	if err != nil {
		return nil, err
	}

	return &vi, nil
}
