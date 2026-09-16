package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/pkg/ui"
)

// lifecycleDaemon is what the operations and upgrade commands need to know
// about the daemon they act on: the server or the runner.
type lifecycleDaemon struct {
	// name is the subcommand group and the word in messages: "server", "runner".
	name    string
	dir     string
	unit    string
	command []string
	// manager describes the install the daemon runs from, for version checks.
	manager func() release.ManagerOptions
}

// title is name capitalized, for the start of a sentence.
func (d lifecycleDaemon) title() string {
	return strings.ToUpper(d.name[:1]) + d.name[1:]
}

var (
	serverDaemon = lifecycleDaemon{
		name:    "server",
		dir:     serverlifecycle.DefaultDir,
		unit:    serverlifecycle.DefaultOptions().ServiceName,
		command: serverlifecycle.ServerExecutorCommand,
		manager: release.DefaultManagerOptions,
	}
	runnerDaemon = lifecycleDaemon{
		name:    "runner",
		dir:     serverlifecycle.RunnerDir,
		unit:    serverlifecycle.RunnerOptions().ServiceName,
		command: serverlifecycle.RunnerExecutorCommand,
		manager: release.RunnerManagerOptions,
	}
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
	if op.HandedOff() {
		// The server takes it from here; this process only has to be sure
		// it did. Runner steps then show up on the record, which the CLI
		// that started the operation is still following.
		op, err = serverlifecycle.AwaitAdoption(ctx, store, op.ID, serverlifecycle.AdoptionTimeout, 2*time.Second)
		if err != nil {
			return err
		}
		if op.Adopted() {
			ctx.Log.Info("server took over the operation to upgrade the runners", "operation", op.ID, "server_instance", op.DrivenBy)
			return nil
		}
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
	ID              string            `json:"id"`
	Action          string            `json:"action"`
	Phase           string            `json:"phase"`
	RequestedBy     string            `json:"requested_by,omitempty"`
	TargetVersion   string            `json:"target_version,omitempty"`
	ResolvedVersion string            `json:"resolved_version,omitempty"`
	PreviousVersion string            `json:"previous_version,omitempty"`
	NewVersion      string            `json:"new_version,omitempty"`
	Components      map[string]string `json:"components,omitempty"`
	Error           string            `json:"error,omitempty"`
	Progress        string            `json:"progress,omitempty"`
	BackupRef       string            `json:"backup_ref,omitempty"`
	DataRestore     *dataRestoreJSON  `json:"data_restore,omitempty"`
	DrivenBy        string            `json:"driven_by,omitempty"`
	Nodes           []nodeStepJSON    `json:"nodes,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	FinishedAt      *time.Time        `json:"finished_at,omitempty"`
}

type nodeStepJSON struct {
	Name            string     `json:"name"`
	RunnerID        string     `json:"runner_id"`
	OperationID     string     `json:"operation_id,omitempty"`
	Phase           string     `json:"phase"`
	Error           string     `json:"error,omitempty"`
	Progress        string     `json:"progress,omitempty"`
	PreviousVersion string     `json:"previous_version,omitempty"`
	NewVersion      string     `json:"new_version,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
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
		PreviousVersion: op.PreviousVersion, NewVersion: op.NewVersion, Components: op.Components,
		Error: op.Error, Progress: op.Progress, BackupRef: op.BackupRef, DrivenBy: op.DrivenBy,
		CreatedAt: op.CreatedAt, FinishedAt: op.FinishedAt,
	}
	if op.DataRestore != nil {
		out.DataRestore = &dataRestoreJSON{BackupRef: op.DataRestore.BackupRef, RestoredAt: op.DataRestore.RestoredAt, Error: op.DataRestore.Error}
	}
	for _, step := range op.Nodes {
		out.Nodes = append(out.Nodes, nodeStepJSON{
			Name: step.Name, RunnerID: step.RunnerID, OperationID: step.OperationID, Phase: string(step.Phase),
			Error: step.Error, Progress: step.Progress, PreviousVersion: step.PreviousVersion, NewVersion: step.NewVersion,
			StartedAt: step.StartedAt, FinishedAt: step.FinishedAt,
		})
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
	return printOperationList(ctx, opts.FormatOptions, ops)
}

func printOperationList(ctx *Context, opts FormatOptions, ops []*serverlifecycle.Operation) error {
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
	headers := []string{"ID", "ACTION", "PHASE", "VERSIONS", "STARTED", "ERROR"}
	rows := make([]ui.Row, 0, len(ops))
	for _, op := range ops {
		rows = append(rows, ui.Row{
			op.ID, string(op.Action), phaseStyle(op.Phase).Render(string(op.Phase)), operationVersions(op),
			op.CreatedAt.Local().Format("2006-01-02 15:04:05"), op.Error,
		})
	}
	// ID is what gets pasted into 'show' and STARTED is a fixed-width
	// timestamp, so neither is worth anything truncated. PHASE is styled,
	// and a styled cell renders whole, so honoring its width is what keeps
	// the row aligned. VERSIONS and ERROR can lose their tails on a narrow
	// terminal; 'show' has the full text.
	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0, 2, 4))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())
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
	return printOperation(ctx, opts.FormatOptions, op)
}

func printOperation(ctx *Context, opts FormatOptions, op *serverlifecycle.Operation) error {
	if opts.IsJSON() {
		return PrintJSON(toOperationJSON(op))
	}
	items := []ui.NamedValue{
		ui.NewNamedValue("Operation", op.ID),
		ui.NewNamedValue("Action", string(op.Action)),
		ui.NewStyledValue("Phase", string(op.Phase), phaseStyle(op.Phase)),
	}
	if op.RequestedBy != "" {
		items = append(items, ui.NewNamedValue("Requested", op.RequestedBy))
	}
	if op.TargetVersion != "" {
		target := op.TargetVersion
		if op.ResolvedVersion != "" && op.ResolvedVersion != op.TargetVersion {
			target += " (" + op.ResolvedVersion + ")"
		}
		items = append(items, ui.NewNamedValue("Target", target))
	}
	if v := operationVersions(op); v != "" {
		items = append(items, ui.NewNamedValue("Versions", v))
	}
	if op.PreviousInstanceID != "" || op.NewInstanceID != "" {
		items = append(items, ui.NewNamedValue("Instances", op.PreviousInstanceID+" -> "+op.NewInstanceID))
	}
	if len(op.Components) > 0 {
		items = append(items, ui.NewNamedValue("Components", describeComponents(op.Components)))
	}
	if op.BackupRef != "" {
		items = append(items, ui.NewNamedValue("Backup", op.BackupRef))
	}
	if op.DataRestore != nil {
		items = append(items, ui.NewNamedValue("Restore", describeDataRestore(op.DataRestore)))
	}
	if op.Progress != "" {
		items = append(items, ui.NewNamedValue("Progress", op.Progress))
	}
	if op.Error != "" {
		items = append(items, ui.NewStyledValue("Error", op.Error, infoRed))
	}
	items = append(items, ui.NewNamedValue("Started", op.CreatedAt.Local().Format(time.RFC3339)))
	if op.FinishedAt != nil {
		items = append(items, ui.NewNamedValue("Finished", op.FinishedAt.Local().Format(time.RFC3339)))
	}
	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	if len(op.Nodes) > 0 {
		headers := []string{"RUNNER", "PHASE", "VERSIONS", "DETAIL"}
		rows := make([]ui.Row, 0, len(op.Nodes))
		for _, step := range op.Nodes {
			rows = append(rows, ui.Row{step.Name, phaseStyle(step.Phase).Render(string(step.Phase)), stepVersions(step), stepDetail(step)})
		}
		columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0, 1))
		ctx.Printf("\nRunners:\n%s\n", ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows)).Render())
	}
	return nil
}

// describeComponents renders "containerd v2.0.4, runc 1.2.2", sorted so the
// line is stable across runs.
func describeComponents(components map[string]string) string {
	parts := make([]string, 0, len(components))
	for _, name := range slices.Sorted(maps.Keys(components)) {
		parts = append(parts, name+" "+components[name])
	}
	return strings.Join(parts, ", ")
}

func stepVersions(step *serverlifecycle.NodeStep) string {
	switch {
	case step.PreviousVersion != "" && step.NewVersion != "" && step.PreviousVersion != step.NewVersion:
		return step.PreviousVersion + " -> " + step.NewVersion
	case step.NewVersion != "":
		return step.NewVersion
	default:
		return step.PreviousVersion
	}
}

func stepDetail(step *serverlifecycle.NodeStep) string {
	if step.Error != "" {
		return step.Error
	}
	return step.Progress
}

// describeStep is one line for a runner's step as the follow prints it.
func describeStep(step *serverlifecycle.NodeStep) string {
	switch {
	case step.Phase == serverlifecycle.StepSkipped:
		return "skipped: " + step.Error
	case step.Done() && !step.Succeeded():
		return string(step.Phase) + ": " + step.Error
	case step.Succeeded():
		if step.Progress != "" {
			return "done: " + step.Progress
		}
		return "done: " + stepVersions(step)
	case step.Progress != "":
		return string(step.Phase) + ": " + step.Progress
	default:
		return string(step.Phase)
	}
}

// operationReporter prints an operation as it changes: each phase, the
// progress note within it, and each runner's step once the walk begins.
type operationReporter struct {
	ctx    *Context
	daemon lifecycleDaemon

	lastPhase    serverlifecycle.Phase
	lastProgress string
	steps        map[string]string
}

func (r *operationReporter) report(op *serverlifecycle.Operation) {
	if op.Phase != r.lastPhase {
		r.ctx.Info("  %s", describePhase(r.daemon, op))
		r.lastPhase = op.Phase
		r.lastProgress = ""
	}
	if op.Progress != "" && op.Progress != r.lastProgress && !op.Done() {
		r.ctx.Info("    %s", op.Progress)
		r.lastProgress = op.Progress
	}
	if r.steps == nil {
		r.steps = map[string]string{}
	}
	for _, step := range op.Nodes {
		line := describeStep(step)
		if line == r.steps[step.Name] {
			continue
		}
		r.steps[step.Name] = line
		r.ctx.Info("      %s: %s", step.Name, line)
	}
}

// phaseStyle colors a phase by what it means for the operator: green is done
// and good, red is done and bad, yellow is a rollback in either state, and
// blue is still moving.
func phaseStyle(phase serverlifecycle.Phase) lipgloss.Style {
	switch phase {
	case serverlifecycle.PhaseSucceeded:
		return infoGreen
	case serverlifecycle.PhaseFailed:
		return infoRed
	case serverlifecycle.PhaseRollingBack, serverlifecycle.PhaseRolledBack:
		return infoYellow
	case serverlifecycle.PhasePending, serverlifecycle.StepSkipped:
		return infoGray
	case serverlifecycle.PhaseDownloading, serverlifecycle.PhaseBackingUp, serverlifecycle.PhaseInstalling,
		serverlifecycle.PhaseRestarting, serverlifecycle.PhaseVerifying, serverlifecycle.PhaseUpgradingRunners:
		return infoLabel
	}
	return lipgloss.NewStyle()
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
func runOperation(ctx *Context, daemon lifecycleDaemon, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("%s requires root privileges (use sudo)", op.Action)
	}
	// Resolved before the record exists so a lookup failure cannot strand a
	// pending op.
	exe, err := serverlifecycle.ExecutorBinary()
	if err != nil {
		return nil, err
	}
	store, err := serverlifecycle.NewStore(daemon.dir)
	if err != nil {
		return nil, err
	}
	launcher := serverlifecycle.SystemdLauncher{Binary: exe, Command: daemon.command}
	op, _, err = serverlifecycle.Start(ctx, store, launcher, op)
	if err != nil {
		if errors.Is(err, serverlifecycle.ErrBusy) {
			return nil, fmt.Errorf("%w; see 'miren %s operations list'", err, daemon.name)
		}
		return nil, err
	}
	ctx.Info("Started %s operation %s (unit %s)", op.Action, op.ID, serverlifecycle.UnitName(op.ID))

	return followOperation(ctx, daemon, store, op.ID, serverlifecycle.UnitName(op.ID))
}

// Interrupting the follow does not stop the operation; it is its own process.
func followOperation(ctx *Context, daemon lifecycleDaemon, store *serverlifecycle.Store, id, unit string) (*serverlifecycle.Operation, error) {
	reporter := &operationReporter{ctx: ctx, daemon: daemon}
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
		reporter.report(op)
		if op.Done() {
			return op, nil
		}
		// An executor that exits without reaching a terminal phase would
		// otherwise leave us polling a record nobody will update again. Once
		// it has handed off, the server writes the record and the unit is
		// expected to be gone.
		if unit != "" && !op.HandedOff() && !serverlifecycle.UnitActive(ctx, unit) {
			// The executor writes its terminal record before exiting, so
			// re-read once: the unit may have finished between the two checks.
			if latest, err := store.Get(id); err == nil && latest.Done() {
				continue
			}
			return op, fmt.Errorf("executor unit %s exited with operation %s still %s; see 'journalctl -u %s'", unit, id, op.Phase, unit)
		}
		select {
		case <-ctx.Done():
			return op, fmt.Errorf("stopped following operation %s; it continues in the background (miren %s operations show %s)", id, daemon.name, id)
		case <-ticker.C:
		}
	}
}

func describePhase(daemon lifecycleDaemon, op *serverlifecycle.Operation) string {
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
		return "restarting " + daemon.unit + " service"
	case serverlifecycle.PhaseVerifying:
		return "waiting for the " + daemon.name + " to report ready"
	case serverlifecycle.PhaseUpgradingRunners:
		if !op.Adopted() {
			return "server is up on " + op.NewVersion + "; handing the runners to it"
		}
		return "upgrading runners"
	case serverlifecycle.PhaseRollingBack:
		return "rolling back: " + op.Error
	case serverlifecycle.PhaseSucceeded:
		if op.Progress != "" {
			return "done: " + op.Progress
		}
		return "done: " + operationVersions(op)
	case serverlifecycle.PhaseRolledBack:
		return "rolled back to " + op.NewVersion + ": " + op.Error
	case serverlifecycle.StepSkipped:
		return "skipped: " + op.Error
	case serverlifecycle.PhaseFailed:
		return "failed: " + op.Error
	}
	return strings.ToLower(string(op.Phase))
}
