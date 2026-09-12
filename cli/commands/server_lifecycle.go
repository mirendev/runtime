package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// ServerLifecycleRun is the executor entry point the transient systemd unit
// invokes.
func ServerLifecycleRun(ctx *Context, opts struct {
	Operation string `long:"operation" required:"true" description:"Operation id to execute or resume"`
	Dir       string `long:"dir" description:"Operation directory" default:"/var/lib/miren/server/lifecycle"`
	HealthURL string `long:"health-url" description:"Health endpoint to verify readiness against (default: derived from the server config)"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	ex := serverlifecycle.NewExecutor(store, serverlifecycle.DefaultOptions(), ctx.Log)
	healthURL := opts.HealthURL
	if healthURL == "" {
		healthURL = serverHealthURL(ctx)
	}
	ex.WithProber(serverlifecycle.NewHealthProber(healthURL, ""))

	op, err := ex.Run(ctx, opts.Operation)
	if err != nil {
		return err
	}
	if !op.Succeeded() {
		return fmt.Errorf("operation %s %s: %s", op.ID, op.Phase, op.Error)
	}
	return nil
}

// serverHealthURL derives the probe URL from the server config so behind-proxy
// ingress modes probe the right port.
func serverHealthURL(ctx *Context) string {
	cfg, err := serverconfig.Load("", nil, ctx.Log)
	if err != nil {
		ctx.Log.Warn("could not load server config for health probe, using default", "error", err)
		return serverlifecycle.DefaultHealthURL
	}
	return serverlifecycle.HealthURL(cfg.Ingress.GetMode(), cfg.Ingress.GetAddress())
}

type lifecycleOperationJSON struct {
	ID              string     `json:"id"`
	Action          string     `json:"action"`
	Phase           string     `json:"phase"`
	RequestedBy     string     `json:"requested_by,omitempty"`
	TargetVersion   string     `json:"target_version,omitempty"`
	ResolvedVersion string     `json:"resolved_version,omitempty"`
	PreviousVersion string     `json:"previous_version,omitempty"`
	NewVersion      string     `json:"new_version,omitempty"`
	Error           string     `json:"error,omitempty"`
	Progress        string     `json:"progress,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

func lifecycleJSON(op *serverlifecycle.Operation) lifecycleOperationJSON {
	return lifecycleOperationJSON{
		ID: op.ID, Action: string(op.Action), Phase: string(op.Phase), RequestedBy: op.RequestedBy,
		TargetVersion: op.TargetVersion, ResolvedVersion: op.ResolvedVersion,
		PreviousVersion: op.PreviousVersion, NewVersion: op.NewVersion,
		Error: op.Error, Progress: op.Progress, CreatedAt: op.CreatedAt, FinishedAt: op.FinishedAt,
	}
}

func ServerLifecycleList(ctx *Context, opts struct {
	FormatOptions
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/server/lifecycle"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	ops, err := store.List()
	if err != nil {
		return err
	}
	if opts.IsJSON() {
		items := make([]lifecycleOperationJSON, 0, len(ops))
		for _, op := range ops {
			items = append(items, lifecycleJSON(op))
		}
		return PrintJSON(items)
	}
	if len(ops) == 0 {
		ctx.Info("No lifecycle operations recorded.")
		return nil
	}
	rows := make([][]string, 0, len(ops))
	for _, op := range ops {
		rows = append(rows, []string{
			op.ID, string(op.Action), string(op.Phase), lifecycleVersions(op),
			op.CreatedAt.Local().Format("2006-01-02 15:04:05"), op.Error,
		})
	}
	ctx.DisplayTable([]string{"ID", "ACTION", "PHASE", "VERSIONS", "STARTED", "ERROR"}, rows)
	return nil
}

func ServerLifecycleShow(ctx *Context, opts struct {
	FormatOptions
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/server/lifecycle"`
	ID  string `position:"0" usage:"Operation id"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	op, err := store.Get(opts.ID)
	if err != nil {
		return err
	}
	if opts.IsJSON() {
		return PrintJSON(lifecycleJSON(op))
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
	if v := lifecycleVersions(op); v != "" {
		ctx.Printf("Versions:  %s\n", v)
	}
	if op.PreviousInstanceID != "" || op.NewInstanceID != "" {
		ctx.Printf("Instances: %s -> %s\n", op.PreviousInstanceID, op.NewInstanceID)
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

func lifecycleVersions(op *serverlifecycle.Operation) string {
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

// runLifecycleOperation records op, launches it under systemd-run, and follows
// it to a terminal phase.
func runLifecycleOperation(ctx *Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("%s requires root privileges (use sudo)", op.Action)
	}
	// Run the executor from this binary, not the installed server binary: an
	// older server may predate `server lifecycle run` entirely. Resolved
	// before the record exists so a lookup failure cannot strand a pending op.
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}

	store, err := serverlifecycle.NewStore(serverlifecycle.DefaultDir)
	if err != nil {
		return nil, err
	}
	if err := store.Create(op); err != nil {
		if errors.Is(err, serverlifecycle.ErrBusy) {
			return nil, fmt.Errorf("%w; see 'miren server lifecycle list'", err)
		}
		return nil, err
	}

	launcher := serverlifecycle.SystemdLauncher{Binary: exe}
	if err := launcher.Launch(ctx, op.ID); err != nil {
		op.Phase = serverlifecycle.PhaseFailed
		op.Error = "could not start executor: " + err.Error()
		_ = store.Update(op)
		return nil, err
	}
	ctx.Info("Started %s operation %s (unit %s)", op.Action, op.ID, serverlifecycle.UnitName(op.ID))

	return followLifecycleOperation(ctx, store, op.ID, serverlifecycle.UnitName(op.ID))
}

// Interrupting the follow does not stop the operation; it is its own process.
func followLifecycleOperation(ctx *Context, store *serverlifecycle.Store, id, unit string) (*serverlifecycle.Operation, error) {
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
			return op, fmt.Errorf("stopped following operation %s; it continues in the background (miren server lifecycle show %s)", id, id)
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
		return "done: " + lifecycleVersions(op)
	case serverlifecycle.PhaseRolledBack:
		return "rolled back to " + op.NewVersion + ": " + op.Error
	case serverlifecycle.PhaseFailed:
		return "failed: " + op.Error
	}
	return strings.ToLower(string(op.Phase))
}
