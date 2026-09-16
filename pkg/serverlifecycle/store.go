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

// DefaultDir is the server's ledger; RunnerDir is the runner's. They are
// separate ledgers with separate busy slots: a host that runs both daemons
// (the coordinator in standalone mode does not, but nothing forbids it) can
// have one operation on each.
const (
	DefaultDir = "/var/lib/miren/server/lifecycle"
	RunnerDir  = "/var/lib/miren/runner/lifecycle"
)

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

// CreateOrGet records op unless a record with its id already exists, in
// which case that record is returned instead and created is false. The
// lookup and the write share the directory lock, so two callers carrying one
// id cannot both miss and cannot both create; exactly one of them creates.
// Like Create, it refuses to create while another operation is running.
func (s *Store) CreateOrGet(op *Operation) (existing *Operation, created bool, err error) {
	if err := op.validate(); err != nil {
		return nil, false, err
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	existing, err = s.Get(op.ID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	if active, err := s.Active(); err != nil {
		return nil, false, err
	} else if active != nil {
		return nil, false, fmt.Errorf("%w: %s (%s, %s)", ErrBusy, active.ID, active.Action, active.Phase)
	}
	if err := s.write(op); err != nil {
		return nil, false, err
	}
	return op, true, nil
}

// Remove deletes a record. It exists for one case: a record that was
// created but whose executor never started and whose failure could not be
// written, which would otherwise hold the busy slot forever.
func (s *Store) Remove(id string) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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

// List returns every operation, oldest first. A record that cannot be read
// is left out rather than hiding the rest; callers that must not miss one
// use listStrict.
func (s *Store) List() ([]*Operation, error) {
	return s.list(false)
}

// listStrict is List for a caller whose answer changes if a single record
// is unreadable: it reports the first read or decode error instead.
func (s *Store) listStrict() ([]*Operation, error) {
	return s.list(true)
}

func (s *Store) list(strict bool) ([]*Operation, error) {
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
			if strict {
				return nil, err
			}
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

// PendingRestore returns the operation whose rollback asked the server to
// restore data and has not been answered, or nil. The server calls this
// early in boot, before the data it would restore is in use.
//
// The request outlives its operation on purpose. An executor that waited on
// a server refusing to boot marks the operation failed and exits, and if
// that ended the request the very next boot would start on the data the
// rollback was meant to replace. Only a restore or Abandon settles it.
//
// Records are read strictly: a request in a record that cannot be decoded
// is still a request, and the caller fails closed on the error.
func (s *Store) PendingRestore() (*Operation, error) {
	ops, err := s.listStrict()
	if err != nil {
		return nil, err
	}
	var pending *Operation
	for _, op := range ops {
		if op.DataRestore == nil {
			continue
		}
		result, err := s.ReadRestoreResult(op.ID)
		switch {
		case errors.Is(err, ErrNotFound):
			pending = op
		case err != nil:
			return nil, err
		case !result.Settled():
			pending = op
		}
	}
	return pending, nil
}

func (s *Store) restoreResultPath(id string) string {
	return filepath.Join(s.dir, "restores", id+".json")
}

// WriteRestoreResult records the server's answer to a DataRestore request.
func (s *Store) WriteRestoreResult(result *RestoreResult) error {
	if result.OperationID == "" {
		return fmt.Errorf("restore result has no operation id")
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.restoreResultPath(result.OperationID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dir, s.restoreResultPath(result.OperationID), result.OperationID, data)
}

// RecordRestoreAttempt writes the outcome of a restore attempt, unless an
// operator abandoned the request while the attempt ran: a failed attempt
// must not reopen a request that was just settled, or the server would go
// back to refusing to boot right after being told not to. A successful
// attempt is always recorded. It returns the result now on disk.
func (s *Store) RecordRestoreAttempt(result *RestoreResult) (*RestoreResult, error) {
	if result.Error != "" {
		existing, err := s.ReadRestoreResult(result.OperationID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if existing != nil && existing.Abandoned {
			return existing, nil
		}
	}
	if err := s.WriteRestoreResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ReadRestoreResult(id string) (*RestoreResult, error) {
	data, err := os.ReadFile(s.restoreResultPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: restore result for %s", ErrNotFound, id)
		}
		return nil, err
	}
	var result RestoreResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode restore result %s: %w", id, err)
	}
	return &result, nil
}

func (s *Store) write(op *Operation) error {
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.dir, s.path(op.ID), op.ID, data)
}

func writeFileAtomic(dir, path, id string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+id+".*.tmp")
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
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	// The rename is durable only once the directory entry is: a restore
	// request that vanished with a power cut would leave the next boot on
	// the wrong data.
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
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
