package serverlifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const DefaultDir = "/var/lib/miren/server/lifecycle"

var ErrNotFound = errors.New("lifecycle operation not found")

var ErrBusy = errors.New("another lifecycle operation is in progress")

// ErrLocked is returned when another executor already holds an operation.
var ErrLocked = errors.New("lifecycle operation is being executed by another process")

// Store keeps one JSON file per operation, written atomically so a process
// dying mid-write leaves the previous record intact.
type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create lifecycle dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// Create refuses while another operation is running: two executors racing
// on one binary and one systemd unit cannot both be right. The check and the
// write happen under a directory lock so two concurrent callers cannot both
// pass it.
func (s *Store) Create(op *Operation) error {
	if err := op.validate(); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(s.path(op.ID)); err == nil {
		return fmt.Errorf("operation %s already exists", op.ID)
	}
	if active, err := s.Active(); err != nil {
		return err
	} else if active != nil {
		return fmt.Errorf("%w: %s (%s, %s)", ErrBusy, active.ID, active.Action, active.Phase)
	}
	return s.write(op)
}

func (s *Store) Get(id string) (*Operation, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, err
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("decode operation %s: %w", id, err)
	}
	return &op, nil
}

func (s *Store) Update(op *Operation) error {
	op.UpdatedAt = time.Now().UTC()
	if op.Done() && op.FinishedAt == nil {
		t := op.UpdatedAt
		op.FinishedAt = &t
	}
	return s.write(op)
}

// List returns every operation, oldest first.
func (s *Store) List() ([]*Operation, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ops []*Operation
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		op, err := s.Get(strings.TrimSuffix(name, ".json"))
		if err != nil {
			// A half-written or foreign file should not hide the rest.
			continue
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
	return ops, nil
}

func (s *Store) Active() (*Operation, error) {
	ops, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if !op.Done() {
			return op, nil
		}
	}
	return nil, nil
}

func (s *Store) write(op *Operation) error {
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "."+op.ID+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path(op.ID)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// lock takes an exclusive flock on the store directory's lock file.
func (s *Store) lock() (func(), error) {
	return flockFile(filepath.Join(s.dir, ".lock"), 0)
}

// LockOperation claims id for one executor. It does not wait: a second
// executor on the same operation gets ErrLocked immediately.
func (s *Store) LockOperation(id string) (func(), error) {
	unlock, err := flockFile(filepath.Join(s.dir, "."+id+".lock"), syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: %s", ErrLocked, id)
	}
	return unlock, err
}

func flockFile(path string, flags int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|flags); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
