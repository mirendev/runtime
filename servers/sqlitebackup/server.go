// Package sqlitebackup stores the LTX transaction files that runners replicate
// from SQLite-provider disks. It is the coordinator half of the replication
// loop: runners drive it through a litestream ReplicaClient in
// components/sqlitedisk.
//
// The on-disk layout deliberately matches litestream's own file replica client
// (<root>/<key>/level-<n>/<minTXID>-<maxTXID>.ltx, with each file's mtime set
// to the transaction timestamp from its LTX header) so the two can be compared
// directly when diagnosing replication problems.
package sqlitebackup

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/superfly/ltx"

	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644

	// tmpFilePrefix marks a partially-written file. It deliberately cannot
	// parse as an LTX filename, so ListLTXFiles skips it.
	tmpFilePrefix = "tmp-"

	// staleTmpAge is how long a temp file must have gone untouched before it
	// is treated as debris from a crashed write rather than one in flight.
	// No legitimate upload comes close to this.
	staleTmpAge = time.Hour
)

// backupKeyPattern constrains a backup key to a single, safe path segment.
// Keys arrive from runners over RPC and are used directly as directory names,
// so anything that could escape the root — a slash, a leading dot, "..", an
// absolute path — must be rejected rather than sanitized.
var backupKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)

// Server implements the SqliteBackup RPC interface over a directory tree.
type Server struct {
	log  *slog.Logger
	root string
}

var _ sqlitebackup_v1alpha.SqliteBackup = (*Server)(nil)

// NewServer returns a Server storing backups beneath root, creating it if needed.
func NewServer(log *slog.Logger, root string) (*Server, error) {
	if root == "" {
		return nil, fmt.Errorf("sqlite backup root path required")
	}
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create sqlite backup root %s: %w", root, err)
	}
	return &Server{
		log:  log.With("module", "sqlite-backup"),
		root: root,
	}, nil
}

func (s *Server) keyDir(key string) (string, error) {
	if !backupKeyPattern.MatchString(key) {
		return "", fmt.Errorf("invalid sqlite backup key %q", key)
	}
	return filepath.Join(s.root, key), nil
}

func (s *Server) levelDir(key string, level int32) (string, error) {
	dir, err := s.keyDir(key)
	if err != nil {
		return "", err
	}
	if level < 0 {
		return "", fmt.Errorf("invalid compaction level %d", level)
	}
	return filepath.Join(dir, fmt.Sprintf("level-%d", level)), nil
}

func (s *Server) ltxPath(key string, level int32, minTXID, maxTXID ltx.TXID) (string, error) {
	dir, err := s.levelDir(key, level)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ltx.FormatFilename(minTXID, maxTXID)), nil
}

// ListLTXFiles reports the files held at a level, ordered by transaction ID.
// A level that has never been written is reported as empty rather than missing,
// matching how litestream's file client treats an absent directory.
func (s *Server) ListLTXFiles(ctx context.Context, state *sqlitebackup_v1alpha.SqliteBackupListLTXFiles) error {
	args := state.Args()
	key, level, seek := args.Key(), args.Level(), ltx.TXID(args.SeekTxid())

	dir, err := s.levelDir(key, level)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		state.Results().SetFiles(nil)
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to list sqlite backup level dir %s: %w", dir, err)
	}

	files := make([]*sqlitebackup_v1alpha.LTXFile, 0, len(entries))
	for _, entry := range entries {
		minTXID, maxTXID, err := ltx.ParseFilename(entry.Name())
		if err != nil {
			// Unique temp names mean a crashed write is no longer overwritten
			// by the retry that follows it, so its debris is reclaimed here.
			// This scan is already walking the directory, so the check is free,
			// and litestream lists often enough to keep the level tidy.
			s.removeIfStaleTmp(dir, entry)
			continue // temp files and anything else unrecognized
		}
		if minTXID < seek {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted between listing and stat
			}
			return fmt.Errorf("failed to stat sqlite backup file %s: %w", entry.Name(), err)
		}

		// mtime is the transaction timestamp, restored by WriteLTXFile.
		files = append(files, newLTXFile(level, minTXID, maxTXID, info.Size(), info.ModTime().UTC()))
	}

	sort.Slice(files, func(i, j int) bool { return files[i].MinTxid() < files[j].MinTxid() })

	state.Results().SetFiles(files)
	return nil
}

// removeIfStaleTmp deletes a leftover temp file that is old enough to be debris
// from a crashed write rather than one currently in flight.
//
// The age threshold is what keeps this safe: an in-progress upload is minutes
// old at most, so a file untouched for staleTmpAge cannot belong to a live
// write. Failures are ignored — this is opportunistic cleanup, and a listing
// should not fail because a stray file could not be removed.
func (s *Server) removeIfStaleTmp(dir string, entry os.DirEntry) {
	if entry.IsDir() || !strings.HasPrefix(entry.Name(), tmpFilePrefix) {
		return
	}

	info, err := entry.Info()
	if err != nil || time.Since(info.ModTime()) < staleTmpAge {
		return
	}

	path := filepath.Join(dir, entry.Name())
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		s.log.Debug("failed to remove stale sqlite backup temp file", "path", path, "error", err)
		return
	}
	s.log.Debug("removed stale sqlite backup temp file", "path", path)
}

// OpenLTXFile streams one file back to the caller. A size of 0 means read to
// the end; offset and size together let litestream resume a partial read.
func (s *Server) OpenLTXFile(ctx context.Context, state *sqlitebackup_v1alpha.SqliteBackupOpenLTXFile) error {
	args := state.Args()

	path, err := s.ltxPath(args.Key(), args.Level(), ltx.TXID(args.MinTxid()), ltx.TXID(args.MaxTxid()))
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		// litestream branches on "missing" versus "broken" when restoring, so
		// this has to arrive as a typed not-found rather than a generic
		// failure. cond.NotFound survives the RPC hop as cond.ErrNotFound.
		return cond.NotFound("ltx-file", path)
	} else if err != nil {
		return fmt.Errorf("failed to open sqlite backup file %s: %w", path, err)
	}
	defer f.Close()

	if offset := args.Offset(); offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek sqlite backup file %s: %w", path, err)
		}
	}

	var src io.Reader = f
	if size := args.Size(); size > 0 {
		src = io.LimitReader(f, size)
	}

	w := stream.ToWriter(ctx, args.Data())
	if _, err := io.Copy(w, src); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to stream sqlite backup file %s: %w", path, err)
	}
	// Closing the stream signals EOF to the caller, so its error matters.
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close sqlite backup stream for %s: %w", path, err)
	}
	return nil
}

// WriteLTXFile stores a file, replacing any existing file with the same
// transaction range. The write lands on a temp file and is fsynced before being
// renamed into place, so a crash mid-transfer cannot leave a torn file that
// ListLTXFiles would report as complete.
func (s *Server) WriteLTXFile(ctx context.Context, state *sqlitebackup_v1alpha.SqliteBackupWriteLTXFile) error {
	args := state.Args()
	key, level := args.Key(), args.Level()
	minTXID, maxTXID := ltx.TXID(args.MinTxid()), ltx.TXID(args.MaxTxid())

	path, err := s.ltxPath(key, level, minTXID, maxTXID)
	if err != nil {
		return err
	}

	// The stream is owned by the RPC call, not by us; closing it here would
	// tear down the caller's client (see servers/build, which also leaves it open).
	rc := stream.ToReader(ctx, args.Data())

	// The transaction timestamp lives in the LTX header; PeekHeader hands back
	// a reader with the header bytes prepended so nothing is consumed.
	hdr, rd, err := ltx.PeekHeader(rc)
	if err != nil {
		return fmt.Errorf("failed to read LTX header for %s: %w", path, err)
	}
	timestamp := time.UnixMilli(hdr.Timestamp).UTC()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("failed to create sqlite backup dir %s: %w", dir, err)
	}

	// A unique temp name, rather than a fixed one derived from the final path,
	// so two writers racing on the same transaction range cannot truncate or
	// delete each other's in-flight file. Both then rename onto the same final
	// path, which is atomic, so the last one to land wins with a whole file.
	f, err := os.CreateTemp(dir, tmpFilePrefix+"*")
	if err != nil {
		return fmt.Errorf("failed to create sqlite backup temp file in %s: %w", dir, err)
	}
	tmpPath := f.Name()

	// CreateTemp opens 0600; keep the mode consistent with the stored files.
	if err := f.Chmod(filePerm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to set mode on sqlite backup temp file %s: %w", tmpPath, err)
	}

	// Once renamed the temp name no longer exists, so cleanup is only for the
	// paths that never got that far.
	committed := false
	defer func() {
		_ = f.Close() // no-op after the explicit Close below
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	size, err := io.Copy(f, rd)
	if err != nil {
		return fmt.Errorf("failed to write sqlite backup file %s: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync sqlite backup file %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close sqlite backup file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename sqlite backup file %s: %w", path, err)
	}
	committed = true
	if err := fsyncDir(dir); err != nil {
		return fmt.Errorf("failed to sync sqlite backup dir %s: %w", dir, err)
	}
	// ListLTXFiles reads the transaction timestamp back out of the mtime.
	if err := os.Chtimes(path, timestamp, timestamp); err != nil {
		return fmt.Errorf("failed to set mtime on sqlite backup file %s: %w", path, err)
	}

	s.log.Debug("stored ltx file",
		"key", key, "level", level, "min_txid", minTXID, "max_txid", maxTXID, "size", size)

	file := newLTXFile(level, minTXID, maxTXID, size, timestamp)
	state.Results().SetFile(&file)
	return nil
}

// DeleteLTXFiles removes specific files. Files that are already gone are not an
// error: litestream retries retention passes, and a partially-applied delete
// must be able to complete on the next attempt.
func (s *Server) DeleteLTXFiles(ctx context.Context, state *sqlitebackup_v1alpha.SqliteBackupDeleteLTXFiles) error {
	args := state.Args()
	key := args.Key()

	for _, file := range args.Files() {
		path, err := s.ltxPath(key, file.Level(), ltx.TXID(file.MinTxid()), ltx.TXID(file.MaxTxid()))
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete sqlite backup file %s: %w", path, err)
		}
	}
	return nil
}

// DeleteAll discards every file held for a key.
func (s *Server) DeleteAll(ctx context.Context, state *sqlitebackup_v1alpha.SqliteBackupDeleteAll) error {
	key := state.Args().Key()

	dir, err := s.keyDir(key)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete sqlite backups for %s: %w", key, err)
	}

	s.log.Info("deleted all sqlite backups for key", "key", key)
	return nil
}

func newLTXFile(level int32, minTXID, maxTXID ltx.TXID, size int64, createdAt time.Time) *sqlitebackup_v1alpha.LTXFile {
	var file sqlitebackup_v1alpha.LTXFile
	file.SetLevel(level)
	file.SetMinTxid(uint64(minTXID))
	file.SetMaxTxid(uint64(maxTXID))
	file.SetSize(size)
	file.SetCreatedAt(standard.ToTimestamp(createdAt))
	return &file
}

// fsyncDir flushes a directory entry so a rename survives a crash.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
