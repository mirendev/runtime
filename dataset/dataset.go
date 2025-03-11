package dataset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/asm/autoreg"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg dataset -input rpc.yml -output rpc.gen.go

// Manager handles dataset operations and maintains dataset state.
type Manager struct {
	Log           *slog.Logger
	DataDir       string `asm:"dataset-data-path,optional"`
	ServerDataDir string `asm:"data-path"`

	dataAddr string
	datasets map[string]*dsAccess
	mu       sync.RWMutex

	serv *http.ServeMux
}

var _ = autoreg.Register[Manager]()

func (m *Manager) Populated() error {
	m.datasets = make(map[string]*dsAccess)

	if m.DataDir == "" {
		m.DataDir = filepath.Join(m.ServerDataDir, "datasets")
	}

	if err := os.MkdirAll(m.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	srv := http.NewServeMux()

	srv.Handle("GET /segment/{dataset}/{id}", http.HandlerFunc(m.serveSegment))
	srv.Handle("/segments", http.HandlerFunc(m.listSegments))

	m.serv = srv

	return nil
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.Log.Debug("serving dataset manager", "method", r.Method, "url", r.URL)
	m.serv.ServeHTTP(w, r)
}

func (m *Manager) ReconstructFromState(state *rpc.InterfaceState) (*rpc.Interface, error) {
	switch state.Interface {
	case "DataSet":
		rs := &dsAccessRS{}

		if err := state.Decode(rs); err != nil {
			return nil, fmt.Errorf("failed to decode restore state: %w", err)
		}

		ds, err := m.getDS(rs.ID)
		if err != nil {
			return nil, err
		}

		return AdaptDataSet(ds), nil
	case "SegmentReader":
		rs := &dsSegmentReaderRS{}

		if err := state.Decode(rs); err != nil {
			return nil, fmt.Errorf("failed to decode restore state: %w", err)
		}

		ds, err := m.getDS(rs.Segment)
		if err != nil {
			return nil, err
		}

		sr, err := ds.openSegment(rs.ID)
		if err != nil {
			return nil, err
		}

		return AdaptSegmentReader(sr), nil
	default:
		return nil, nil
	}
}

var _ DataSets = (*Manager)(nil)

func (m *Manager) Create(ctx context.Context, state *DataSetsCreate) error {
	info := state.Args().Info()

	ds, err := m.CreateDataSet(ctx, info)
	if err != nil {
		return err
	}

	m.datasets[info.Name()] = ds.(*dsAccess)

	state.Results().SetDataset(ds)

	return nil
}

func (m *Manager) CreateDataSet(ctx context.Context, info *DataSetInfo) (DataSet, error) {
	if info.Name() == "" {
		return nil, fmt.Errorf("dataset name must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.datasets[info.Name()]; ok {
		return nil, fmt.Errorf("dataset %q already exists", info.Name())
	}

	dir := filepath.Join(m.DataDir, info.Name())

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dataset directory: %w", err)
	}

	infof, err := os.Create(filepath.Join(dir, "info.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to create dataset info file: %w", err)
	}

	defer infof.Close()

	if err := json.NewEncoder(infof).Encode(info); err != nil {
		return nil, fmt.Errorf("failed to write dataset info: %w", err)
	}

	ds := &dsAccess{
		log: m.Log,
		m:   m,
		id:  info.Name(),
		dir: dir,
	}

	m.datasets[info.Name()] = ds

	return ds, nil
}

// ListVolumes
func (m *Manager) List(ctx context.Context, state *DataSetsList) error {
	ents, err := os.ReadDir(m.DataDir)
	if err != nil {
		return fmt.Errorf("failed to list datasets: %w", err)
	}

	var datasets []*DataSetInfo

	for _, ent := range ents {
		if ent.IsDir() {
			f, err := os.Open(filepath.Join(m.DataDir, ent.Name(), "info.json"))
			if err != nil {
				return fmt.Errorf("failed to open dataset info file: %w", err)
			}

			defer f.Close()

			var info DataSetInfo

			if err := json.NewDecoder(f).Decode(&info); err != nil {
				return fmt.Errorf("failed to read dataset info: %w", err)
			}

			datasets = append(datasets, &info)
		}
	}

	state.Results().SetDatasets(datasets)

	return nil
}

// GetVolume

func (m *Manager) getDS(id string) (*dsAccess, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ds, ok := m.datasets[id]
	if !ok {
		dir := filepath.Join(m.DataDir, id)
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("dataset %q not found", id)
		}

		ds = &dsAccess{
			log: m.Log,
			m:   m,
			id:  id,
			dir: dir,
		}

		m.datasets[id] = ds
	}

	return ds, nil
}

func (m *Manager) Get(ctx context.Context, state *DataSetsGet) error {
	ds, err := m.getDS(state.Args().Id())
	if err != nil {
		return err
	}

	state.Results().SetDataset(ds)
	return nil
}

func (m *Manager) Delete(ctx context.Context, state *DataSetsDelete) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.datasets[state.Args().Id()]
	if ok {
		delete(m.datasets, state.Args().Id())
	}

	dir := filepath.Join(m.DataDir, state.Args().Id())
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete dataset directory: %w", err)
	}

	return nil
}

// NewManager creates a new dataset manager that stores datasets in the specified directory.
func NewManager(log *slog.Logger, dataDir, dataAddr string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	srv := http.NewServeMux()

	m := &Manager{
		Log:      log,
		DataDir:  dataDir,
		dataAddr: dataAddr,
		datasets: make(map[string]*dsAccess),
		serv:     srv,
	}

	srv.Handle("GET /segment/{dataset}/{id}", http.HandlerFunc(m.serveSegment))
	srv.Handle("/segments", http.HandlerFunc(m.listSegments))

	return m, nil
}

type dsAccess struct {
	log *slog.Logger
	m   *Manager
	id  string
	dir string
}

var _ DataSet = (*dsAccess)(nil)
var _ rpc.HasRestoreState = (*dsAccess)(nil)

type dsAccessRS struct {
	ID string `json:"id"`
}

func (d *dsAccess) RestoreState(iface any) (any, error) {
	return &dsAccessRS{ID: d.id}, nil
}

// GetInfo
func (d *dsAccess) GetInfo(ctx context.Context, state *DataSetGetInfo) error {
	f, err := os.Open(filepath.Join(d.dir, "info.json"))
	if err != nil {
		return fmt.Errorf("failed to open dataset info file: %w", err)
	}

	defer f.Close()

	var info DataSetInfo

	if err := json.NewDecoder(f).Decode(&info); err != nil {
		return fmt.Errorf("failed to read dataset info: %w", err)
	}

	d.log.Debug("returning dataset info", "info", info)

	state.Results().SetInfo(&info)
	return nil
}

// ListSegments
func (d *dsAccess) ListSegments(ctx context.Context, state *DataSetListSegments) error {
	ents, err := os.ReadDir(d.dir)
	if err != nil {
		return fmt.Errorf("failed to list segments: %w", err)
	}

	var segments []string

	for _, ent := range ents {
		if ent.Type().IsRegular() && strings.HasSuffix(ent.Name(), ".segment") {
			segments = append(segments, strings.TrimSuffix(ent.Name(), ".segment"))
		}
	}

	state.Results().SetSegments(segments)

	return nil
}

type dsSegmentWriter struct {
	rpc.ForbidRestore

	f      *os.File
	closed bool
}

var _ SegmentWriter = (*dsSegmentWriter)(nil)

// Close
func (w *dsSegmentWriter) Close(ctx context.Context, state *SegmentWriterClose) error {
	if w.closed {
		return nil
	}

	w.closed = true
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("failed to close segment file: %w", err)
	}

	return os.Rename(w.f.Name(), strings.TrimSuffix(w.f.Name(), ".tmp"))
}

// WriteAt
func (w *dsSegmentWriter) WriteAt(ctx context.Context, state *SegmentWriterWriteAt) error {
	n, err := w.f.WriteAt(state.Args().Data(), state.Args().Offset())
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}

		return fmt.Errorf("failed to write segment data: %w", err)
	}

	state.Results().SetCount(int64(n))
	return nil
}

// NewSegment
func (d *dsAccess) NewSegment(ctx context.Context, state *DataSetNewSegment) error {
	f, err := os.Create(filepath.Join(d.dir, state.Args().Id()+".segment.tmp"))
	if err != nil {
		return fmt.Errorf("failed to create segment file: %w", err)
	}

	layout := state.Args().Layout()
	if layout != nil {
		g, err := os.Create(filepath.Join(d.dir, state.Args().Id()+".segment.layout.cbor"))
		if err != nil {
			return fmt.Errorf("failed to create segment layout file: %w", err)
		}

		defer g.Close()

		if err := cbor.NewEncoder(g).Encode(layout); err != nil {
			return fmt.Errorf("failed to write segment layout: %w", err)
		}
	}

	state.Results().SetWriter(&dsSegmentWriter{f: f})
	return nil
}

func (m *Manager) listSegments(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var segments []string
	for id := range m.datasets {
		ents, err := os.ReadDir(filepath.Join(m.DataDir, id))
		if err != nil {
			http.Error(w, "failed to list segments", http.StatusInternalServerError)
			return
		}

		for _, ent := range ents {
			if ent.Type().IsRegular() && strings.HasSuffix(ent.Name(), ".segment") {
				segments = append(segments, id+"/"+strings.TrimSuffix(ent.Name(), ".segment"))
			}
		}
	}

	if err := json.NewEncoder(w).Encode(segments); err != nil {
		http.Error(w, "failed to encode segments", http.StatusInternalServerError)
		return
	}
}

func (m *Manager) serveSegment(w http.ResponseWriter, r *http.Request) {
	ds := r.PathValue("dataset")
	id := r.PathValue("id")

	path := filepath.Join(m.DataDir, ds, id+".segment")

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "failed to open segment file: "+path, http.StatusInternalServerError)
		return
	}

	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "failed to stat segment file", http.StatusInternalServerError)
		return
	}

	rng := r.Header.Get("Range")

	m.Log.Debug("serving segment", "dataset", ds, "id", id, "range", rng)

	/*
		if rng != "" {
			if strings.HasPrefix(rng, "bytes=") {
				rng = strings.TrimPrefix(rng, "bytes=")
				parts := strings.Split(rng, "-")
				if len(parts) != 2 {
					http.Error(w, "invalid range", http.StatusBadRequest)
					return
				}

				start, end := parts[0], parts[1]

				if start == "" {
					start = "0"
				}

				if end == "" {
					end = fmt.Sprintf("%d", stat.Size()-1)
				}

				si, err := strconv.ParseInt(start, 10, 64)
				if err != nil {
					http.Error(w, "invalid range start", http.StatusBadRequest)
					return
				}

				ei, err := strconv.ParseInt(end, 10, 64)
				if err != nil {
					http.Error(w, "invalid range end", http.StatusBadRequest)
					return
				}

				_, err = f.Seek(si, io.SeekStart)
				if err != nil {
					http.Error(w, "failed to seek to range start", http.StatusInternalServerError)
					return
				}

				m.log.Debug("serving range", "start", si, "end", ei)

				io.CopyN(w, f, ei-si+1)
				return
			}
		}
	*/

	http.ServeContent(w, r, id+".segment", stat.ModTime(), f)
}

type dsSegmentReader struct {
	log *slog.Logger
	f   *os.File

	m *Manager

	id      string
	segment string
}

var _ SegmentReader = (*dsSegmentReader)(nil)
var _ rpc.HasRestoreState = (*dsSegmentReader)(nil)

// Close
func (r *dsSegmentReader) Close(ctx context.Context, state *SegmentReaderClose) error {
	if err := r.f.Close(); err != nil {
		return fmt.Errorf("failed to close segment file: %w", err)
	}

	return nil
}

// ReadAt
func (r *dsSegmentReader) ReadAt(ctx context.Context, state *SegmentReaderReadAt) error {
	data := make([]byte, state.Args().Size())

	n, err := r.f.ReadAt(data, state.Args().Offset())
	if err != nil && n == 0 {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}

		return fmt.Errorf("failed to read segment data: %w", err)
	}

	state.Results().SetData(data[:n])

	return nil
}

func (r *dsSegmentReader) Layout(ctx context.Context, state *SegmentReaderLayout) error {
	f, err := os.Open(r.f.Name() + ".layout.cbor")
	if err != nil {
		return fmt.Errorf("failed to open segment layout file: %w", err)
	}

	defer f.Close()

	var layout SegmentLayout

	if err := cbor.NewDecoder(f).Decode(&layout); err != nil {
		return fmt.Errorf("failed to read segment layout: %w", err)
	}

	state.Results().SetLayout(&layout)

	return nil
}

func (r *dsSegmentReader) DataPath(ctx context.Context, state *SegmentReaderDataPath) error {
	var dp DataPathAccess

	if r.m.dataAddr == "" {
		return nil
	}

	dp.SetUrl(fmt.Sprintf("http://%s/segment/%s/%s", r.m.dataAddr, r.segment, r.id))
	dp.SetTtl(standard.ToDuration(24 * 265 * time.Hour))

	r.log.Debug("data path url", "url", dp.Url())

	state.Results().SetDataPath(&dp)

	return nil
}

func (d *dsAccess) openSegment(id string) (SegmentReader, error) {
	f, err := os.Open(filepath.Join(d.dir, id+".segment"))
	if err != nil {
		return nil, fmt.Errorf("failed to open segment file: %w", err)
	}

	return &dsSegmentReader{
		log: d.log,
		f:   f,

		m: d.m,

		segment: d.id,
		id:      id,
	}, nil
}

func (d *dsAccess) ReadSegment(ctx context.Context, state *DataSetReadSegment) error {
	sr, err := d.openSegment(state.Args().Id())
	if err != nil {
		return err
	}

	state.Results().SetReader(sr)
	return nil
}

type dsSegmentReaderRS struct {
	ID      string `json:"id"`
	Segment string `json:"segment"`
}

func (r *dsSegmentReader) RestoreState(iface any) (any, error) {
	return &dsSegmentReaderRS{
		ID:      r.id,
		Segment: r.segment,
	}, nil
}

// ReadBytes
func (d *dsAccess) ReadBytes(ctx context.Context, state *DataSetReadBytes) error {
	return nil
}
