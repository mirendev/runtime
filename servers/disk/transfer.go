package disk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
)

const (
	// transfersDir holds the partial snapshots of transfers that are still in
	// flight, beside the volumes they belong to so they land on the filesystem
	// sized for disk data.
	transfersDir = "transfers"

	// transferTTL is how long an abandoned transfer's bytes are kept. Long
	// enough to outlive an outage and a night's sleep, short enough that a
	// client that never comes back does not hoard a multi-gigabyte file
	// forever.
	transferTTL = 24 * time.Hour
)

// keyedLocks serializes work on any one key: a transfer id, or a disk name.
//
// The transfer files are shared mutable state, and nothing else guards them.
// Two calls carrying the same id can otherwise both pass openTransfer's offset
// check and interleave their appends, or one can truncate a staged snapshot
// while the other is streaming it. A well-behaved client never does this, since
// it picks a fresh id per invocation and retries in sequence, but the server
// should not be relying on that.
//
// Locks are reference counted so the map does not grow by one entry per backup
// forever. Dropping an entry while a caller still held a pointer to it would be
// worse than the leak: the next caller would mint a second lock for the same id
// and the two would run concurrently anyway.
type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

func newKeyedLocks() *keyedLocks {
	return &keyedLocks{locks: map[string]*keyedLock{}}
}

// acquire blocks until this key is free and returns the release function.
func (t *keyedLocks) acquire(id string) func() {
	t.mu.Lock()
	l, ok := t.locks[id]
	if !ok {
		l = &keyedLock{}
		t.locks[id] = l
	}
	l.refs++
	t.mu.Unlock()

	l.mu.Lock()

	return func() {
		l.mu.Unlock()

		t.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(t.locks, id)
		}
		t.mu.Unlock()
	}
}

// transferPath is where a transfer's bytes accumulate.
//
// The id comes from the client, so it is checked rather than trusted: it names
// a file under a directory this server owns, and a caller must not be able to
// aim that at something else.
func (s *Server) transferPath(id string) (string, error) {
	if id == "" {
		return "", refuse("a transfer id is required to resume an interrupted transfer")
	}
	if len(id) > 128 {
		return "", refuse("transfer id is too long")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return "", refuse("transfer id may only contain letters, digits, dashes and underscores")
		}
	}
	return filepath.Join(s.diskDataPath(), transfersDir, id+".part"), nil
}

// TransferOffset reports how many bytes of a transfer the server already holds.
//
// An id it has never seen reports zero rather than failing, so a client's first
// attempt and its retries take the same path.
func (s *Server) TransferOffset(ctx context.Context, state *disk_v1alpha.DiskBackupTransferOffset) error {
	path, err := s.transferPath(state.Args().TransferId())
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		state.Results().SetReceivedBytes(0)
		return nil
	case err != nil:
		return fmt.Errorf("checking transfer progress: %w", err)
	}

	state.Results().SetReceivedBytes(info.Size())
	return nil
}

// openTransfer opens a transfer's file for appending, creating it if this is
// the first attempt, and checks the caller is resuming from where the server
// actually is.
//
// Refusing a mismatched offset rather than seeking to it is deliberate. The two
// ends disagreeing about how much arrived is exactly the situation where
// writing anyway produces a file that is the right length and the wrong bytes,
// and nothing would notice until a restore months later.
func (s *Server) openTransfer(id string, offset int64) (*os.File, error) {
	path, err := s.transferPath(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating transfer directory: %w", err)
	}

	var have int64
	if info, serr := os.Stat(path); serr == nil {
		have = info.Size()
	} else if !os.IsNotExist(serr) {
		return nil, fmt.Errorf("checking transfer progress: %w", serr)
	}

	if offset != have {
		return nil, refuse(
			"this transfer is at %d bytes but the client offered to continue from %d — ask transferOffset where to resume, or use a new transfer id to start over",
			have, offset,
		)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening transfer file: %w", err)
	}
	return f, nil
}

// stagedSnapshot describes a compressed snapshot waiting to be collected.
//
// It exists because the checksum and the image's size are known only while the
// snapshot is being compressed, and a resumed transfer skips that step. Without
// it, a client that reconnected for the last megabyte would be told the backup
// had no checksum.
type stagedSnapshot struct {
	Disk           string `json:"disk"`
	ImageSize      int64  `json:"image_size"`
	CompressedSize int64  `json:"compressed_size"`
	Checksum       string `json:"checksum"`
}

func (s *Server) stagingMetaPath(id string) (string, error) {
	path, err := s.transferPath(id)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(path, ".part") + ".meta", nil
}

// loadStaging reports the finished snapshot waiting under this transfer id, or
// nil when there is none.
//
// The metadata file doubles as the completion marker: it is written only after
// compression finishes, so bytes left behind by a server that died partway
// through have no metadata and are correctly treated as nothing at all.
func (s *Server) loadStaging(id string) (*stagedSnapshot, error) {
	metaPath, err := s.stagingMetaPath(id)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metaPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading staged snapshot metadata: %w", err)
	}

	var meta stagedSnapshot
	if err := json.Unmarshal(data, &meta); err != nil {
		// Unreadable metadata means we cannot say what the staged bytes are, so
		// treat the transfer as absent rather than guess.
		s.log.Warn("discarding a staged snapshot with unreadable metadata", "transfer_id", id, "error", err)
		s.discardStaging(id)
		return nil, nil
	}

	path, err := s.transferPath(id)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		s.discardStaging(id)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checking staged snapshot: %w", err)
	}
	if info.Size() != meta.CompressedSize {
		s.log.Warn("discarding a staged snapshot that does not match its metadata",
			"transfer_id", id, "on_disk", info.Size(), "recorded", meta.CompressedSize)
		s.discardStaging(id)
		return nil, nil
	}

	return &meta, nil
}

func (s *Server) saveStaging(id string, meta *stagedSnapshot) error {
	metaPath, err := s.stagingMetaPath(id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encoding staged snapshot metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		return fmt.Errorf("writing staged snapshot metadata: %w", err)
	}
	return nil
}

// discardStaging removes a staged snapshot and its metadata.
func (s *Server) discardStaging(id string) {
	if metaPath, err := s.stagingMetaPath(id); err == nil {
		if rerr := os.Remove(metaPath); rerr != nil && !os.IsNotExist(rerr) {
			s.log.Warn("failed to remove staged snapshot metadata", "transfer_id", id, "error", rerr)
		}
	}
	s.discardTransfer(id)
}

// discardTransfer removes a transfer's bytes once they are no longer needed.
func (s *Server) discardTransfer(id string) {
	path, err := s.transferPath(id)
	if err != nil {
		return
	}
	if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
		s.log.Warn("failed to remove finished transfer", "transfer_id", id, "error", rerr)
	}
}

// sweepTransfers deletes transfers nobody came back for.
//
// This runs when a transfer starts rather than on a timer: it is the moment a
// transfer directory is known to be in use, and it keeps the cleanup on the
// path that creates the mess rather than in a goroutine that has to be owned
// and shut down.
func (s *Server) sweepTransfers() {
	dir := filepath.Join(s.diskDataPath(), transfersDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("could not sweep abandoned transfers", "error", err)
		}
		return
	}

	cutoff := time.Now().Add(-transferTTL)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Both halves of a staged backup are swept. Taking only the .part would
		// leave its .meta behind forever, since nothing else ever looks at a
		// metadata file whose bytes are gone.
		if !strings.HasSuffix(e.Name(), ".part") && !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if rerr := os.Remove(path); rerr != nil {
			s.log.Warn("failed to remove abandoned transfer", "path", path, "error", rerr)
			continue
		}
		s.log.Info("removed abandoned transfer",
			"path", path, "size", info.Size(), "idle_for", time.Since(info.ModTime()).Truncate(time.Minute))
	}
}
