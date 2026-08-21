package sqlitedisk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/benbjohnson/litestream"
	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
)

// replication is one database being replicated, shared by every disk attached
// to the same backup key.
//
// A database can legitimately have more than one owner: two services of the
// same app that declare the same sqlite id resolve to one key, and each of
// their sandboxes registers it. Replication must therefore outlive any single
// owner and stop only when the last one releases it.
type replication struct {
	dbPath string

	// owners holds the sandbox ids currently attached to this database.
	owners map[string]struct{}

	// ready is closed once the open attempt below finishes. Registrations that
	// arrive while a database is still opening wait on it rather than assuming
	// the in-flight attempt will succeed.
	ready chan struct{}
	db    *litestream.DB
	err   error
}

// Manager owns the replicated databases for every SQLite-provider disk attached
// on this node: one litestream.DB per backup key, each replicating to the
// coordinator.
//
// A nil *Manager is usable and does nothing. That keeps the runner's wiring
// simple when the coordinator backup service is unreachable — disks still
// attach and apps still run, they just are not backed up.
type Manager struct {
	log    *slog.Logger
	client *sqlitebackup_v1alpha.SqliteBackupClient

	mu    sync.Mutex
	repls map[string]*replication // keyed by backup key
	// owners maps an owner (a sandbox id) to the backup keys it registered, so
	// teardown can release them without re-deriving the keys. By the time a
	// sandbox is cleaned up its app version entity may already be deleted, so
	// anything that needs a lookup to find the key is too late.
	owners map[string]map[string]struct{}
}

// NewManager returns a Manager replicating through client.
func NewManager(log *slog.Logger, client *sqlitebackup_v1alpha.SqliteBackupClient) *Manager {
	return &Manager{
		log:    log.With("module", "sqlite-disk"),
		client: client,
		repls:  make(map[string]*replication),
		owners: make(map[string]map[string]struct{}),
	}
}

// Register attaches owner to the database at dbPath, restoring it from the
// coordinator first when this node has no copy, and begins replicating it.
//
// Registering a key that is already replicated adds owner to it rather than
// starting a second replicator: the database keeps replicating until every
// owner has released it. Re-registering the same owner is a no-op.
//
// Restore happens before replication starts, so a database that exists locally
// is never overwritten by a backup — EnsureExists only acts on a missing file,
// and succeeds when there is no backup to restore (a genuinely new disk).
func (m *Manager) Register(ctx context.Context, owner, key, dbPath string) error {
	if m == nil {
		return nil
	}
	// An unowned registration could never be released, so it would pin
	// replication for the life of the process.
	if owner == "" {
		return fmt.Errorf("sqlite disk %s: owner required to register", key)
	}

	m.mu.Lock()
	if r, ok := m.repls[key]; ok {
		// Two disks resolving to one key must agree on the file, or one of them
		// would believe a database it never writes to is being backed up.
		if r.dbPath != dbPath {
			m.mu.Unlock()
			return fmt.Errorf("sqlite disk %s is already replicating %s, cannot also replicate %s",
				key, r.dbPath, dbPath)
		}
		r.owners[owner] = struct{}{}
		m.trackOwnerLocked(owner, key)
		ready := r.ready
		m.mu.Unlock()

		// The open may still be in flight; its outcome is this caller's too.
		select {
		case <-ready:
		case <-ctx.Done():
			m.releaseOwner(context.WithoutCancel(ctx), key, owner)
			return ctx.Err()
		}

		m.mu.Lock()
		err := r.err
		m.mu.Unlock()
		if err != nil {
			return fmt.Errorf("sqlite disk %s failed to start replicating: %w", key, err)
		}

		m.log.Debug("sqlite disk already replicating", "key", key, "path", dbPath, "owner", owner)
		return nil
	}

	r := &replication{
		dbPath: dbPath,
		owners: map[string]struct{}{owner: {}},
		ready:  make(chan struct{}),
	}
	m.repls[key] = r
	m.trackOwnerLocked(owner, key)
	m.mu.Unlock()

	db, err := m.open(ctx, key, dbPath)

	abandoned := m.finishOpen(key, r, db, err)

	if err != nil {
		return err
	}

	if abandoned {
		m.log.Info("sqlite disk released while opening, closing it", "key", key, "path", dbPath)
		if cerr := db.Close(ctx); cerr != nil {
			return fmt.Errorf("closing abandoned sqlite disk %s: %w", key, cerr)
		}
		return nil
	}

	m.log.Info("replicating sqlite disk", "key", key, "path", dbPath, "owner", owner)
	return nil
}

// finishOpen publishes the result of an open to everyone waiting on r and tears
// the registration down if it failed. It reports whether r was abandoned while
// the open was in flight, which the caller must handle by closing the database
// itself.
//
// Both the teardown and the abandoned check hinge on r still being the
// registration installed under key. A release that ran during the open drops the
// entry, and a later register can install a fresh one in its place; in neither
// case may this open delete the map entry or untrack owners, because they belong
// to whoever holds the slot now.
func (m *Manager) finishOpen(key string, r *replication, db *litestream.DB, err error) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	r.db, r.err = db, err
	close(r.ready)

	// Every owner may have left while this open was running, in which case the
	// release could not close a database that did not exist yet and left it to
	// us. Without this the database would keep replicating with no owner and no
	// entry in the map, invisible even to Close.
	if m.repls[key] != r {
		return true
	}

	if err != nil {
		// Drop the whole registration, including any owners that joined while
		// the open was running — none of them are being replicated.
		delete(m.repls, key)
		for o := range r.owners {
			m.untrackOwnerLocked(o, key)
		}
	}
	return false
}

func (m *Manager) open(ctx context.Context, key, dbPath string) (*litestream.DB, error) {
	log := m.log.With("key", key, "path", dbPath)

	db := litestream.NewDB(dbPath)
	db.SetLogger(quietLogger(log))
	db.Replica = litestream.NewReplicaWithClient(db, NewReplicaClient(log, m.client, key))

	// EnsureExists only reports whether it failed, not whether it restored, so
	// note beforehand whether there was anything here. A database appearing is
	// the one lifecycle event in this function an operator would want at Info.
	_, existedBefore := os.Stat(dbPath)

	if err := db.EnsureExists(ctx); err != nil {
		return nil, fmt.Errorf("restore sqlite disk %s from coordinator: %w", key, err)
	}

	if existedBefore != nil {
		if _, err := os.Stat(dbPath); err == nil {
			m.log.Info("restored sqlite disk from coordinator", "key", key, "path", dbPath)
		}
	}

	// EnsureExists returns successfully without creating anything when there is
	// no backup to restore, which is the normal case for a brand new disk. The
	// database has to exist and be in WAL mode before litestream opens it, so
	// create it here rather than leaving the app to find an empty directory.
	if err := EnsureDatabase(dbPath); err != nil {
		return nil, fmt.Errorf("initialize sqlite disk %s: %w", key, err)
	}

	if err := db.Open(); err != nil {
		return nil, fmt.Errorf("start replicating sqlite disk %s: %w", key, err)
	}

	// Open only starts the monitor goroutine; the SQLite handle is opened
	// lazily on the first sync. Without this, a disk attached and detached
	// inside one monitor interval would never replicate at all — Close skips
	// its final flush when the handle was never opened. Syncing here also
	// means a freshly attached disk is backed up immediately.
	if err := db.Sync(ctx); err != nil {
		if cerr := db.Close(ctx); cerr != nil {
			m.log.Warn("failed to close sqlite disk after failed initial sync", "key", key, "error", cerr)
		}
		return nil, fmt.Errorf("initial sync of sqlite disk %s: %w", key, err)
	}

	return db, nil
}

// DeregisterOwner releases everything an owner registered, stopping replication
// for any database it was the last owner of.
//
// Teardown calls this rather than deriving keys again, because the entities the
// keys came from may already be deleted. Unknown owners are ignored so teardown
// can run unconditionally.
func (m *Manager) DeregisterOwner(ctx context.Context, owner string) error {
	if m == nil || owner == "" {
		return nil
	}

	m.mu.Lock()
	keys := make([]string, 0, len(m.owners[owner]))
	for key := range m.owners[owner] {
		keys = append(keys, key)
	}
	m.mu.Unlock()

	var errs []error
	for _, key := range keys {
		if err := m.releaseOwner(ctx, key, owner); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// releaseOwner drops one owner's claim on a key, closing the database only once
// no owner is left.
func (m *Manager) releaseOwner(ctx context.Context, key, owner string) error {
	m.mu.Lock()
	r, ok := m.repls[key]
	if !ok {
		m.untrackOwnerLocked(owner, key)
		m.mu.Unlock()
		return nil
	}

	delete(r.owners, owner)
	m.untrackOwnerLocked(owner, key)

	if len(r.owners) > 0 {
		m.mu.Unlock()
		m.log.Debug("sqlite disk still in use by another sandbox, keeping replication",
			"key", key, "owner", owner, "remaining", len(r.owners))
		return nil
	}

	delete(m.repls, key)
	ready, db := r.ready, r.db
	m.mu.Unlock()

	// A registration still opening has no database to close yet. Removing it
	// from the map above is what tells Register, once its open finishes, that
	// the database it just opened is unwanted and must be closed.
	select {
	case <-ready:
	default:
		return nil
	}
	if db == nil {
		return nil
	}

	// Close performs a final sync before shutting replication down.
	if err := db.Close(ctx); err != nil {
		return fmt.Errorf("stop replicating sqlite disk %s: %w", key, err)
	}

	m.log.Info("stopped replicating sqlite disk", "key", key)
	return nil
}

// Close stops replicating every database. Errors are collected so one failing
// database cannot strand the others still running.
func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	repls := m.repls
	m.repls = make(map[string]*replication)
	m.owners = make(map[string]map[string]struct{})
	m.mu.Unlock()

	var errs []error
	for key, r := range repls {
		select {
		case <-r.ready:
		default:
			// Still opening. Replacing m.repls above already signalled to that
			// Register call that its database is unwanted, so it closes it.
			continue
		}
		if r.db == nil {
			continue
		}
		if err := r.db.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop replicating sqlite disk %s: %w", key, err))
		}
	}

	return errors.Join(errs...)
}

func (m *Manager) trackOwnerLocked(owner, key string) {
	if owner == "" {
		return
	}
	if m.owners[owner] == nil {
		m.owners[owner] = make(map[string]struct{})
	}
	m.owners[owner][key] = struct{}{}
}

func (m *Manager) untrackOwnerLocked(owner, key string) {
	if owner == "" {
		return
	}
	delete(m.owners[owner], key)
	if len(m.owners[owner]) == 0 {
		delete(m.owners, owner)
	}
}

// quietLogger demotes litestream's Info records to Debug.
//
// litestream is written for a CLI, where a line per sync and a line per
// uploaded file are reasonable. Here it runs inside a daemon with a one second
// sync interval, so at Info that is a record every second for every replicated
// database, for the life of the runner. Miren keeps Info for events an operator
// would act on, so those go to Debug; warnings and errors pass through
// untouched, and this package logs its own lifecycle events at Info.
func quietLogger(log *slog.Logger) *slog.Logger {
	return slog.New(demoteInfoHandler{log.Handler()})
}

type demoteInfoHandler struct{ slog.Handler }

func demoted(l slog.Level) slog.Level {
	if l == slog.LevelInfo {
		return slog.LevelDebug
	}
	return l
}

func (h demoteInfoHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.Handler.Enabled(ctx, demoted(l))
}

func (h demoteInfoHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Level = demoted(r.Level)
	return h.Handler.Handle(ctx, r)
}

func (h demoteInfoHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return demoteInfoHandler{h.Handler.WithAttrs(attrs)}
}

func (h demoteInfoHandler) WithGroup(name string) slog.Handler {
	return demoteInfoHandler{h.Handler.WithGroup(name)}
}

// EnsureDatabase makes sure a SQLite database exists at path and is in WAL
// mode, which litestream requires in order to replicate it.
//
// It is safe to call on an existing database: opening it and setting the
// journal mode it already has is a no-op, so a restored database is left
// exactly as it was.
func EnsureDatabase(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite database %s: %w", path, err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL;").Scan(&mode); err != nil {
		return fmt.Errorf("set WAL mode on %s: %w", path, err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("sqlite database %s is in %q mode, expected wal", path, mode)
	}

	// Setting the journal mode alone does not necessarily flush a header to
	// disk, and litestream needs a real file to open.
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("sqlite database %s was not created: %w", path, err)
	}

	return nil
}
