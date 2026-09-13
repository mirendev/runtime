package etcd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const snapshotSuffix = ".etcd.db"

// DefaultBackupsToKeep bounds the pre-upgrade snapshots kept on disk. More
// than the one a rollback needs, because an upgrade that passed readiness
// can still turn out bad later and the snapshot is what an operator would
// reach for; few enough that the etcd quota bounds the disk they take.
const DefaultBackupsToKeep = 3

// Backup is the lifecycle executor's DataBackup for a server with embedded
// etcd: one snapshot per operation under Dir, named by the operation id so
// the directory lists in operation order. The reference it hands back is
// the snapshot's path, which is what the server's data-restore component
// expects to find in a DataRestore request.
type Backup struct {
	Dir      string
	Endpoint string
	TLS      *TLSConfig
	// Keep is how many snapshots survive, newest first; 0 means
	// DefaultBackupsToKeep.
	Keep int
	Log  *slog.Logger
}

func (b Backup) Backup(ctx context.Context, opID string) (string, error) {
	path := filepath.Join(b.Dir, opID+snapshotSuffix)
	if err := Snapshot(ctx, b.Log, b.Endpoint, b.TLS, path); err != nil {
		return "", err
	}
	if err := b.prune(); err != nil {
		// The snapshot that matters is on disk; old ones are a warning.
		b.Log.Warn("could not prune old etcd snapshots", "dir", b.Dir, "error", err)
	}
	return path, nil
}

// prune deletes all but the newest Keep snapshots. Operation ids are ULIDs,
// so name order is creation order.
func (b Backup) prune() error {
	keep := b.Keep
	if keep <= 0 {
		keep = DefaultBackupsToKeep
	}
	entries, err := os.ReadDir(b.Dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), snapshotSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		name := names[0]
		names = names[1:]
		if err := os.Remove(filepath.Join(b.Dir, name)); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
		b.Log.Info("pruned old etcd snapshot", "path", filepath.Join(b.Dir, name))
	}
	return nil
}
