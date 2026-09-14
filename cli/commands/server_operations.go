package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// ServerOperationsRun is the executor entry point the transient systemd unit
// invokes.
func ServerOperationsRun(ctx *Context, opts struct {
	Operation string `long:"operation" required:"true" description:"Operation id to execute or resume"`
	Dir       string `long:"dir" description:"Operation directory" default:"/var/lib/miren/server/lifecycle"`
	HealthURL string `long:"health-url" description:"Health endpoint to verify readiness against (default: derived from the server config)"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	ex := serverlifecycle.NewExecutor(store, serverlifecycle.DefaultOptions(), ctx.Log)
	cfg, cfgErr := serverconfig.Load("", nil, ctx.Log)
	if cfgErr != nil {
		// Restarts can still probe the default URL. Upgrades cannot guess
		// whether there is an etcd to snapshot, so they fail at backing_up
		// with this error rather than proceed without one.
		ctx.Log.Warn("could not load server config", "error", cfgErr)
		cfg = serverconfig.DefaultConfig()
	}
	healthURL := opts.HealthURL
	if healthURL == "" {
		healthURL = serverlifecycle.HealthURL(cfg.Ingress.GetMode(), cfg.Ingress.GetAddress())
	}
	ex.WithProber(serverlifecycle.NewHealthProber(healthURL, ""))
	switch {
	case cfgErr != nil:
		ex.WithDataBackup(unavailableBackup{fmt.Errorf("load server config: %w", cfgErr)})
	case cfg.Etcd.GetStartEmbedded():
		ex.WithDataBackup(etcd.Backup{
			Dir:      filepath.Join(store.Dir(), "backups"),
			Endpoint: fmt.Sprintf("https://localhost:%d", cfg.Etcd.GetClientPort()),
			TLS:      &etcd.TLSConfig{CertsDir: coordinate.EtcdCertsDir(cfg.Server.GetDataPath())},
			Log:      ctx.Log,
		})
	default:
		ctx.Log.Info("etcd is not embedded; upgrades will not snapshot it and rollback restores only the binary")
	}

	op, err := ex.Run(ctx, opts.Operation)
	if err != nil {
		return err
	}
	if !op.Succeeded() {
		return fmt.Errorf("operation %s %s: %s", op.ID, op.Phase, op.Error)
	}
	return nil
}

// unavailableBackup fails the backing_up phase with the reason no backup
// could be arranged, so an upgrade never proceeds on a guess.
type unavailableBackup struct{ err error }

func (u unavailableBackup) Backup(context.Context, string) (string, error) { return "", u.err }

type operationJSON struct {
	ID              string           `json:"id"`
	Action          string           `json:"action"`
	Phase           string           `json:"phase"`
	RequestedBy     string           `json:"requested_by,omitempty"`
	TargetVersion   string           `json:"target_version,omitempty"`
	ResolvedVersion string           `json:"resolved_version,omitempty"`
	PreviousVersion string           `json:"previous_version,omitempty"`
	NewVersion      string           `json:"new_version,omitempty"`
	Error           string           `json:"error,omitempty"`
	Progress        string           `json:"progress,omitempty"`
	BackupRef       string           `json:"backup_ref,omitempty"`
	DataRestore     *dataRestoreJSON `json:"data_restore,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	FinishedAt      *time.Time       `json:"finished_at,omitempty"`
}

type dataRestoreJSON struct {
	BackupRef  string     `json:"backup_ref"`
	RestoredAt *time.Time `json:"restored_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func toOperationJSON(op *serverlifecycle.Operation) operationJSON {
	out := operationJSON{
		ID: op.ID, Action: string(op.Action), Phase: string(op.Phase), RequestedBy: op.RequestedBy,
		TargetVersion: op.TargetVersion, ResolvedVersion: op.ResolvedVersion,
		PreviousVersion: op.PreviousVersion, NewVersion: op.NewVersion,
		Error: op.Error, Progress: op.Progress, BackupRef: op.BackupRef,
		CreatedAt: op.CreatedAt, FinishedAt: op.FinishedAt,
	}
	if op.DataRestore != nil {
		out.DataRestore = &dataRestoreJSON{BackupRef: op.DataRestore.BackupRef, RestoredAt: op.DataRestore.RestoredAt, Error: op.DataRestore.Error}
	}
	return out
}

// operationSource reads the ledger from wherever it can be reached: the
// server's RPC first, since that works from anywhere, and the files on this
// host when the server cannot answer, which is exactly the situation while
// an operation restarts it. An explicit --dir reads files only.
type operationSource struct {
	ctx *Context
	dir string
}

// openStore opens an existing ledger directory for reading. NewStore would
// create it, and a read must not turn a misspelled --dir into an empty
// directory that quietly answers "no operations".
func openStore(dir string) (*serverlifecycle.Store, error) {
	if info, err := os.Stat(dir); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	return serverlifecycle.NewStore(dir)
}

// local opens this host's ledger as a fallback, but only when the server the
// CLI just failed to reach is this host. With -C naming a remote cluster, the
// files here belong to some other server and must not stand in for it.
func (s operationSource) local() (*serverlifecycle.Store, bool) {
	if !s.ctx.targetsLocalServer() {
		return nil, false
	}
	store, err := openStore(s.dir)
	return store, err == nil
}

func (s operationSource) list() ([]*serverlifecycle.Operation, error) {
	if s.dir != serverlifecycle.DefaultDir {
		store, err := openStore(s.dir)
		if err != nil {
			return nil, err
		}
		return store.List()
	}
	ops, err := listRemoteOperations(s.ctx)
	if err == nil {
		return ops, nil
	}
	store, ok := s.local()
	if !ok {
		return nil, err
	}
	s.ctx.Log.Debug("reading lifecycle operations from disk; server did not answer", "error", err)
	return store.List()
}

func (s operationSource) get(id string) (*serverlifecycle.Operation, error) {
	if s.dir != serverlifecycle.DefaultDir {
		store, err := openStore(s.dir)
		if err != nil {
			return nil, err
		}
		return store.Get(id)
	}
	op, err := getRemoteOperation(s.ctx, id)
	if err == nil {
		return op, nil
	}
	store, ok := s.local()
	if !ok {
		return nil, err
	}
	s.ctx.Log.Debug("reading lifecycle operation from disk; server did not answer", "error", err)
	return store.Get(id)
}

func ServerOperationsList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Dir string `long:"dir" description:"Read operation records from this directory instead of asking the server" default:"/var/lib/miren/server/lifecycle"`
}) error {
	ops, err := operationSource{ctx: ctx, dir: opts.Dir}.list()
	if err != nil {
		return err
	}
	if opts.IsJSON() {
		items := make([]operationJSON, 0, len(ops))
		for _, op := range ops {
			items = append(items, toOperationJSON(op))
		}
		return PrintJSON(items)
	}
	if len(ops) == 0 {
		ctx.Info("No operations recorded.")
		return nil
	}
	rows := make([][]string, 0, len(ops))
	for _, op := range ops {
		rows = append(rows, []string{
			op.ID, string(op.Action), string(op.Phase), operationVersions(op),
			op.CreatedAt.Local().Format("2006-01-02 15:04:05"), op.Error,
		})
	}
	ctx.DisplayTable([]string{"ID", "ACTION", "PHASE", "VERSIONS", "STARTED", "ERROR"}, rows)
	return nil
}

func ServerOperationsShow(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Dir string `long:"dir" description:"Read operation records from this directory instead of asking the server" default:"/var/lib/miren/server/lifecycle"`
	ID  string `position:"0" usage:"Operation id"`
}) error {
	op, err := operationSource{ctx: ctx, dir: opts.Dir}.get(opts.ID)
	if err != nil {
		return err
	}
	if opts.IsJSON() {
		return PrintJSON(toOperationJSON(op))
	}
	ctx.Printf("Operation: %s\n", op.ID)
	ctx.Printf("Action:    %s\n", op.Action)
	ctx.Printf("Phase:     %s\n", op.Phase)
	if op.RequestedBy != "" {
		ctx.Printf("Requested: %s\n", op.RequestedBy)
	}
	if op.TargetVersion != "" {
		ctx.Printf("Target:    %s", op.TargetVersion)
		if op.ResolvedVersion != "" && op.ResolvedVersion != op.TargetVersion {
			ctx.Printf(" (%s)", op.ResolvedVersion)
		}
		ctx.Printf("\n")
	}
	if v := operationVersions(op); v != "" {
		ctx.Printf("Versions:  %s\n", v)
	}
	if op.PreviousInstanceID != "" || op.NewInstanceID != "" {
		ctx.Printf("Instances: %s -> %s\n", op.PreviousInstanceID, op.NewInstanceID)
	}
	if op.BackupRef != "" {
		ctx.Printf("Backup:    %s\n", op.BackupRef)
	}
	if op.DataRestore != nil {
		ctx.Printf("Restore:   %s\n", describeDataRestore(op.DataRestore))
	}
	if op.Progress != "" {
		ctx.Printf("Progress:  %s\n", op.Progress)
	}
	if op.Error != "" {
		ctx.Printf("Error:     %s\n", op.Error)
	}
	ctx.Printf("Started:   %s\n", op.CreatedAt.Local().Format(time.RFC3339))
	if op.FinishedAt != nil {
		ctx.Printf("Finished:  %s\n", op.FinishedAt.Local().Format(time.RFC3339))
	}
	return nil
}

// ServerOperationsAbandon ends an operation nobody will finish: an executor
// that died mid-operation, or a rollback whose data restore keeps the
// server from booting.
func ServerOperationsAbandon(ctx *Context, opts struct {
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/server/lifecycle"`
	ID  string `position:"0" usage:"Operation id"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	op, err := serverlifecycle.Abandon(store, opts.ID)
	if err != nil {
		if errors.Is(err, serverlifecycle.ErrLocked) {
			return fmt.Errorf("%w; wait for it to finish or stop unit %s first", err, serverlifecycle.UnitName(opts.ID))
		}
		return err
	}
	ctx.Info("Abandoned %s operation %s (%s)", op.Action, op.ID, op.Phase)
	if op.DataRestore != nil {
		ctx.Info("The server will start on its data as it is, without restoring %s.", op.DataRestore.BackupRef)
		ctx.Info("If the service is down, 'systemctl restart miren' brings it back.")
	}
	return nil
}

func describeDataRestore(r *serverlifecycle.DataRestore) string {
	switch {
	case r.Error != "":
		return "failed: " + r.Error
	case r.RestoredAt != nil:
		return "restored " + r.RestoredAt.Local().Format(time.RFC3339)
	default:
		return "requested"
	}
}

func operationVersions(op *serverlifecycle.Operation) string {
	switch {
	case op.PreviousVersion != "" && op.NewVersion != "":
		if op.PreviousVersion == op.NewVersion {
			return op.NewVersion
		}
		return op.PreviousVersion + " -> " + op.NewVersion
	case op.PreviousVersion != "":
		return op.PreviousVersion
	default:
		return op.NewVersion
	}
}

// runOperation records op, launches it under systemd-run, and follows
// it to a terminal phase.
func runOperation(ctx *Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("%s requires root privileges (use sudo)", op.Action)
	}
	// Resolved before the record exists so a lookup failure cannot strand a
	// pending op.
	exe, err := serverlifecycle.ExecutorBinary()
	if err != nil {
		return nil, err
	}
	store, err := serverlifecycle.NewStore(serverlifecycle.DefaultDir)
	if err != nil {
		return nil, err
	}
	op, _, err = serverlifecycle.Start(ctx, store, serverlifecycle.SystemdLauncher{Binary: exe}, op)
	if err != nil {
		if errors.Is(err, serverlifecycle.ErrBusy) {
			return nil, fmt.Errorf("%w; see 'miren server operations list'", err)
		}
		return nil, err
	}
	ctx.Info("Started %s operation %s (unit %s)", op.Action, op.ID, serverlifecycle.UnitName(op.ID))

	return followOperation(ctx, store, op.ID, serverlifecycle.UnitName(op.ID))
}

// Interrupting the follow does not stop the operation; it is its own process.
func followOperation(ctx *Context, store *serverlifecycle.Store, id, unit string) (*serverlifecycle.Operation, error) {
	var lastPhase serverlifecycle.Phase
	var lastProgress string
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	readErrors := 0
	for {
		op, err := store.Get(id)
		if err != nil {
			// The executor rewrites the record atomically, so a read error is
			// a hiccup, not the operation vanishing. Give it a few polls.
			if readErrors++; readErrors > 10 {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, err
			case <-ticker.C:
			}
			continue
		}
		readErrors = 0
		if op.Phase != lastPhase {
			ctx.Info("  %s", describePhase(op))
			lastPhase = op.Phase
			lastProgress = ""
		}
		if op.Progress != "" && op.Progress != lastProgress && !op.Done() {
			ctx.Info("    %s", op.Progress)
			lastProgress = op.Progress
		}
		if op.Done() {
			return op, nil
		}
		// An executor that exits without reaching a terminal phase would
		// otherwise leave us polling a record nobody will update again.
		if unit != "" && !serverlifecycle.UnitActive(ctx, unit) {
			// The executor writes its terminal record before exiting, so
			// re-read once: the unit may have finished between the two checks.
			if latest, err := store.Get(id); err == nil && latest.Done() {
				continue
			}
			return op, fmt.Errorf("executor unit %s exited with operation %s still %s; see 'journalctl -u %s'", unit, id, op.Phase, unit)
		}
		select {
		case <-ctx.Done():
			return op, fmt.Errorf("stopped following operation %s; it continues in the background (miren server operations show %s)", id, id)
		case <-ticker.C:
		}
	}
}

func describePhase(op *serverlifecycle.Operation) string {
	switch op.Phase {
	case serverlifecycle.PhasePending:
		return "pending"
	case serverlifecycle.PhaseDownloading:
		if op.ResolvedVersion != "" {
			return "downloading " + op.ResolvedVersion
		}
		return "resolving " + op.TargetVersion
	case serverlifecycle.PhaseBackingUp:
		return "backing up data"
	case serverlifecycle.PhaseInstalling:
		return "installing " + op.ResolvedVersion
	case serverlifecycle.PhaseRestarting:
		return "restarting miren service"
	case serverlifecycle.PhaseVerifying:
		return "waiting for the server to report ready"
	case serverlifecycle.PhaseRollingBack:
		return "rolling back: " + op.Error
	case serverlifecycle.PhaseSucceeded:
		if op.Progress != "" {
			return "done: " + op.Progress
		}
		return "done: " + operationVersions(op)
	case serverlifecycle.PhaseRolledBack:
		return "rolled back to " + op.NewVersion + ": " + op.Error
	case serverlifecycle.PhaseFailed:
		return "failed: " + op.Error
	}
	return strings.ToLower(string(op.Phase))
}
