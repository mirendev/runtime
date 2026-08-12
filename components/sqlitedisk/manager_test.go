package sqlitedisk

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	sqlitebackupsrv "miren.dev/runtime/servers/sqlitebackup"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestManager wires a Manager to an in-process coordinator backup server and
// returns the directory the server stores files in.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()

	backupRoot := t.TempDir()
	srv, err := sqlitebackupsrv.NewServer(testLogger(), backupRoot)
	require.NoError(t, err)

	client := &sqlitebackup_v1alpha.SqliteBackupClient{
		Client: rpc.LocalClient(sqlitebackup_v1alpha.AdaptSqliteBackup(srv)),
	}
	return NewManager(testLogger(), client), backupRoot
}

// openDB opens a SQLite database in WAL mode, which litestream requires.
func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	require.NoError(t, err)
	return db
}

func seedRows(t *testing.T, path string, values ...string) {
	t.Helper()

	db := openDB(t, path)
	defer db.Close()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS notes (body TEXT)`)
	require.NoError(t, err)
	for _, v := range values {
		_, err := db.Exec(`INSERT INTO notes (body) VALUES (?)`, v)
		require.NoError(t, err)
	}
}

func readRows(t *testing.T, path string) []string {
	t.Helper()

	db := openDB(t, path)
	defer db.Close()

	rows, err := db.Query(`SELECT body FROM notes ORDER BY rowid`)
	require.NoError(t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var body string
		require.NoError(t, rows.Scan(&body))
		out = append(out, body)
	}
	require.NoError(t, rows.Err())
	return out
}

// removeDB deletes the database and its WAL sidecars, standing in for a node
// that lost its local copy.
func removeDB(t *testing.T, path string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Remove(path + suffix)
		if err != nil && !os.IsNotExist(err) {
			require.NoError(t, err)
		}
	}
	require.NoFileExists(t, path)
}

// The whole point of the feature: data written on one node comes back after the
// local copy is gone.
func TestReplicateThenRestore(t *testing.T) {
	ctx := context.Background()
	mgr, backupRoot := newTestManager(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	key := BackupKey("app/demo", "state")

	seedRows(t, dbPath, "first", "second")

	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	seedRows(t, dbPath, "third")
	// Deregister performs a final sync, so everything above is replicated.
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))

	entries, err := os.ReadDir(filepath.Join(backupRoot, key))
	require.NoError(t, err, "expected backup files under the key's directory")
	require.NotEmpty(t, entries)

	removeDB(t, dbPath)

	// Re-registering restores from the coordinator before replication resumes.
	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	t.Cleanup(func() { _ = mgr.DeregisterOwner(ctx, "sandbox/test") })

	require.FileExists(t, dbPath, "EnsureExists should have restored the database")
	require.Equal(t, []string{"first", "second", "third"}, readRows(t, dbPath))
}

// A disk with no backup yet must not fail to attach; it is simply a new disk.
func TestRegisterWithNoBackupSucceeds(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "only")

	key := BackupKey("app/fresh", "state")
	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))

	require.Equal(t, []string{"only"}, readRows(t, dbPath))
}

// Sandbox reconcile can attach the same disk more than once.
func TestRegisterIsIdempotent(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "a")
	key := BackupKey("app/demo", "state")

	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))

	// Teardown runs unconditionally, so an unknown key must be harmless.
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/never-registered"))
}

// Restoring must never clobber a database that is already present locally.
func TestRegisterDoesNotOverwriteExistingDatabase(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	key := BackupKey("app/demo", "state")

	seedRows(t, dbPath, "backed-up")
	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))

	// Local writes made while replication was stopped must survive re-attach.
	seedRows(t, dbPath, "local-only")

	require.NoError(t, mgr.Register(ctx, "sandbox/test", key, dbPath))
	t.Cleanup(func() { _ = mgr.DeregisterOwner(ctx, "sandbox/test") })

	require.Equal(t, []string{"backed-up", "local-only"}, readRows(t, dbPath))
}

func TestCloseStopsAllDatabases(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		dbPath := filepath.Join(dir, name+".db")
		seedRows(t, dbPath, name)
		require.NoError(t, mgr.Register(ctx, "sandbox/test", BackupKey("app/demo", name), dbPath))
	}

	require.NoError(t, mgr.Close(ctx))

	// Close should have emptied the registry, so re-registering is clean.
	dbPath := filepath.Join(dir, "one.db")
	require.NoError(t, mgr.Register(ctx, "sandbox/test", BackupKey("app/demo", "one"), dbPath))
	require.NoError(t, mgr.Close(ctx))
}

// A nil Manager stands in for "backups not configured" and must be inert.
func TestNilManagerIsInert(t *testing.T) {
	ctx := context.Background()
	var mgr *Manager

	require.NoError(t, mgr.Register(ctx, "sandbox/test", "key", "/nonexistent/data.db"))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/test"))
	require.NoError(t, mgr.Close(ctx))
}

func TestBackupKey(t *testing.T) {
	// Keys must satisfy what the coordinator accepts: a single path segment
	// starting with an alphanumeric.
	key := BackupKey("app/demo", "state")
	require.NotContains(t, key, "/")
	require.Regexp(t, `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`, key)
	require.LessOrEqual(t, len(key), maxKeyLen)

	// Stable for the same input, distinct for different disks.
	require.Equal(t, key, BackupKey("app/demo", "state"))
	require.NotEqual(t, key, BackupKey("app/demo", "other"))
	require.NotEqual(t, key, BackupKey("app/other", "state"))

	// Inputs that sanitize to the same prefix must still not collide.
	require.NotEqual(t, BackupKey("app/a-b", "c"), BackupKey("app/a", "b-c"))

	// Awkward inputs still produce a legal key.
	for _, tc := range []struct{ app, vol string }{
		{"", ""},
		{"...", "..."},
		{"/leading", "trailing/"},
		{"ünïcode", "名前"},
	} {
		got := BackupKey(tc.app, tc.vol)
		require.Regexp(t, `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`, got,
			"app=%q vol=%q produced illegal key %q", tc.app, tc.vol, got)
		require.LessOrEqual(t, len(got), maxKeyLen)
	}
}

// Teardown releases by sandbox id because the entities a key was derived from
// may already be deleted by the time cleanup runs.
func TestDeregisterOwner(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dir := t.TempDir()
	const owner = "sandbox/demo"

	keys := make([]string, 0, 2)
	for _, name := range []string{"one", "two"} {
		dbPath := filepath.Join(dir, name+".db")
		seedRows(t, dbPath, name)
		key := BackupKey("app/demo", name)
		keys = append(keys, key)
		require.NoError(t, mgr.Register(ctx, owner, key, dbPath))
	}

	// A disk belonging to a different sandbox must survive.
	otherPath := filepath.Join(dir, "other.db")
	seedRows(t, otherPath, "other")
	otherKey := BackupKey("app/other", "state")
	require.NoError(t, mgr.Register(ctx, "sandbox/other", otherKey, otherPath))

	require.NoError(t, mgr.DeregisterOwner(ctx, owner))

	mgr.mu.Lock()
	_, ownerStillTracked := mgr.owners[owner]
	remaining := len(mgr.repls)
	_, otherStillReplicating := mgr.repls[otherKey]
	mgr.mu.Unlock()

	require.False(t, ownerStillTracked, "owner should be forgotten")
	require.Equal(t, 1, remaining, "only the other sandbox's disk should remain")
	require.True(t, otherStillReplicating)

	for _, key := range keys {
		mgr.mu.Lock()
		_, ok := mgr.repls[key]
		mgr.mu.Unlock()
		require.False(t, ok, "key %s should have been released", key)
	}

	// Releasing an owner that registered nothing is harmless — teardown runs
	// unconditionally, including for sandboxes with no sqlite disks.
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/never-seen"))
	require.NoError(t, mgr.DeregisterOwner(ctx, ""))

	var nilMgr *Manager
	require.NoError(t, nilMgr.DeregisterOwner(ctx, owner))
}

// Two services of one app may declare the same sqlite id, which resolves to one
// backup key. Replication must survive one of them stopping.
func TestSharedKeyKeepsReplicatingUntilLastOwnerLeaves(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "shared")
	key := BackupKey("app/demo", "shared")

	require.NoError(t, mgr.Register(ctx, "sandbox/a", key, dbPath))
	require.NoError(t, mgr.Register(ctx, "sandbox/b", key, dbPath))

	// A leaves; B is still running and must keep being backed up.
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/a"))

	mgr.mu.Lock()
	r, stillReplicating := mgr.repls[key]
	var remainingOwners int
	if stillReplicating {
		remainingOwners = len(r.owners)
	}
	mgr.mu.Unlock()

	require.True(t, stillReplicating, "replication must outlive the first owner")
	require.Equal(t, 1, remainingOwners)

	// Writes made after A left still reach the coordinator.
	seedRows(t, dbPath, "after-a-left")

	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/b"))

	mgr.mu.Lock()
	_, stillReplicating = mgr.repls[key]
	mgr.mu.Unlock()
	require.False(t, stillReplicating, "last owner leaving must stop replication")

	// The final sync on B's release must have captured the later write.
	removeDB(t, dbPath)
	require.NoError(t, mgr.Register(ctx, "sandbox/c", key, dbPath))
	t.Cleanup(func() { _ = mgr.DeregisterOwner(ctx, "sandbox/c") })
	require.Equal(t, []string{"shared", "after-a-left"}, readRows(t, dbPath))
}

// Re-registering the same owner must not inflate the reference count, or the
// database would never close.
func TestRegisterSameOwnerTwiceReleasesCleanly(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "a")
	key := BackupKey("app/demo", "state")

	require.NoError(t, mgr.Register(ctx, "sandbox/a", key, dbPath))
	require.NoError(t, mgr.Register(ctx, "sandbox/a", key, dbPath))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/a"))

	mgr.mu.Lock()
	_, stillReplicating := mgr.repls[key]
	mgr.mu.Unlock()
	require.False(t, stillReplicating, "one release should match one owner")
}

// Two disks sharing a key but naming different files would leave one of them
// silently unreplicated, so the second registration is refused.
func TestRegisterRejectsConflictingPathForSameKey(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "data.db")
	second := filepath.Join(dir, "other.db")
	seedRows(t, first, "a")
	seedRows(t, second, "b")
	key := BackupKey("app/demo", "state")

	require.NoError(t, mgr.Register(ctx, "sandbox/a", key, first))
	t.Cleanup(func() { _ = mgr.DeregisterOwner(ctx, "sandbox/a") })

	err := mgr.Register(ctx, "sandbox/b", key, second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot also replicate")
}

func TestRegisterRequiresOwner(t *testing.T) {
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "a")

	err := mgr.Register(context.Background(), "", BackupKey("app/demo", "state"), dbPath)
	require.Error(t, err, "an unowned registration could never be released")
	require.Contains(t, err.Error(), "owner required")
}

// A failed open must not leave a second caller believing it is replicating.
func TestRegisterFailurePropagatesToConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	// A directory in place of the database file makes EnsureDatabase fail.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	require.NoError(t, os.MkdirAll(dbPath, 0o755))
	key := BackupKey("app/demo", "broken")

	require.Error(t, mgr.Register(ctx, "sandbox/a", key, dbPath))

	mgr.mu.Lock()
	_, leaked := mgr.repls[key]
	_, ownerLeaked := mgr.owners["sandbox/a"]
	mgr.mu.Unlock()

	require.False(t, leaked, "a failed open must not leave a registration behind")
	require.False(t, ownerLeaked, "a failed open must not leave owner tracking behind")

	// And the failure must be reported again rather than reported as success.
	require.Error(t, mgr.Register(ctx, "sandbox/b", key, dbPath))
}

// A database opened after its last owner left must not keep replicating. The
// release cannot close it (there is nothing to close yet), so the Register that
// opened it has to notice it was abandoned and close it itself.
func TestRegisterClosesDatabaseAbandonedDuringOpen(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTestManager(t)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedRows(t, dbPath, "a")
	key := BackupKey("app/demo", "state")

	// Claim the key the way Register does before its slow open, then release it
	// while that open is still notionally in flight.
	r := &replication{
		dbPath: dbPath,
		owners: map[string]struct{}{"sandbox/a": {}},
		ready:  make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.repls[key] = r
	mgr.trackOwnerLocked("sandbox/a", key)
	mgr.mu.Unlock()

	require.NoError(t, mgr.releaseOwner(ctx, key, "sandbox/a"))

	mgr.mu.Lock()
	_, stillRegistered := mgr.repls[key]
	mgr.mu.Unlock()
	require.False(t, stillRegistered, "release should have dropped the entry")

	// Now a fresh Register completes. It must leave nothing behind: neither a
	// map entry nor an open database.
	require.NoError(t, mgr.Register(ctx, "sandbox/b", key, dbPath))
	require.NoError(t, mgr.DeregisterOwner(ctx, "sandbox/b"))

	mgr.mu.Lock()
	remaining := len(mgr.repls)
	owners := len(mgr.owners)
	mgr.mu.Unlock()
	require.Zero(t, remaining, "no replication should be left behind")
	require.Zero(t, owners, "no owner tracking should be left behind")
}
