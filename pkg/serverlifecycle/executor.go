package serverlifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"miren.dev/runtime/pkg/release"
)

// Options tunes an Executor. Start from DefaultOptions; a zero Options is not
// a working configuration.
type Options struct {
	ServiceName string
	// StateDir is the daemon's state directory, passed through to the
	// resource-limit refresh that precedes a restart.
	StateDir     string
	InstallPath  string
	TempDir      string
	ArtifactType release.ArtifactType
	// ReadyTimeout bounds how long a restarted server gets to report ready
	// before the operation fails (and, for upgrades, rolls back).
	ReadyTimeout  time.Duration
	ProbeInterval time.Duration
	AutoRollback  bool
	// PathSymlink, when set, is kept pointing at InstallPath after a
	// successful upgrade so the CLI on $PATH tracks the server.
	PathSymlink string
}

func DefaultOptions() Options {
	return Options{
		ServiceName:   "miren",
		StateDir:      release.DefaultManagerOptions().StateDir,
		InstallPath:   release.DefaultManagerOptions().InstallPath,
		TempDir:       os.TempDir(),
		ArtifactType:  release.ArtifactTypeBase,
		ReadyTimeout:  3 * time.Minute,
		ProbeInterval: 2 * time.Second,
		AutoRollback:  true,
		PathSymlink:   release.SystemCLIPath,
	}
}

// Executor drives an operation through its phases, persisting each
// transition. Everything needed to resume is in the Operation record.
type Executor struct {
	store      *Store
	opts       Options
	log        *slog.Logger
	downloader release.Downloader
	installer  release.Installer
	restarter  Restarter
	prober     Prober

	// downloaded is deliberately not persisted: a resumed run downloads again
	// rather than trusting a temp file that may be gone.
	downloaded *release.DownloadedArtifact
}

func NewExecutor(store *Store, opts Options, log *slog.Logger) *Executor {
	return &Executor{
		store:      store,
		opts:       opts,
		log:        log,
		downloader: release.NewDownloader(),
		installer:  release.NewInstaller(release.InstallOptions{InstallPath: opts.InstallPath, BackupSuffix: ".old"}),
		restarter:  SystemdRestarter{Unit: opts.ServiceName, StateDir: opts.StateDir},
		prober:     NewHealthProber(DefaultHealthURL, ""),
	}
}

func (e *Executor) WithDownloader(d release.Downloader) *Executor { e.downloader = d; return e }
func (e *Executor) WithInstaller(i release.Installer) *Executor   { e.installer = i; return e }
func (e *Executor) WithRestarter(r Restarter) *Executor           { e.restarter = r; return e }
func (e *Executor) WithProber(p Prober) *Executor                 { e.prober = p; return e }

// Run drives the operation to a terminal phase or until ctx ends. A finished
// operation is returned unchanged; an interrupted one resumes where it was.
func (e *Executor) Run(ctx context.Context, id string) (*Operation, error) {
	unlock, err := e.store.LockOperation(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	op, err := e.store.Get(id)
	if err != nil {
		return nil, err
	}
	if err := op.validate(); err != nil {
		return op, err
	}
	for !op.Done() {
		if err := ctx.Err(); err != nil {
			return op, err
		}
		before := op.Phase
		if err := e.step(ctx, op); err != nil {
			// Only store and ctx failures surface here; phase outcomes are
			// recorded on the operation.
			return op, err
		}
		if op.Phase != before {
			e.log.Info("lifecycle operation phase changed", "operation", op.ID, "action", op.Action, "from", before, "to", op.Phase)
		}
	}
	return op, nil
}

func (e *Executor) step(ctx context.Context, op *Operation) error {
	switch op.Phase {
	case PhasePending:
		return e.begin(ctx, op)
	case PhaseDownloading:
		return e.download(ctx, op)
	case PhaseInstalling:
		return e.install(ctx, op)
	case PhaseRestarting:
		return e.restart(ctx, op)
	case PhaseVerifying:
		return e.verify(ctx, op)
	case PhaseRollingBack:
		return e.rollback(ctx, op)
	case PhaseSucceeded, PhaseFailed, PhaseRolledBack:
		return nil
	}
	return e.fail(ctx, op, fmt.Errorf("unknown phase %q", op.Phase))
}

// begin records the process we are about to replace so verification can
// insist on a different one afterwards.
func (e *Executor) begin(ctx context.Context, op *Operation) error {
	snap, err := e.prober.Probe(ctx)
	if err != nil {
		// Still worth restarting; we just lose the "is this a new process" check.
		e.log.Warn("could not identify running server before operation", "operation", op.ID, "error", err)
	} else {
		op.PreviousInstanceID = snap.InstanceID
		op.PreviousVersion = snap.Version
		op.PreviousCommit = snap.Commit
		if snap.InstallKind == "container" {
			return e.fail(ctx, op, errors.New("server runs in a container; container installs cannot be upgraded or restarted this way yet (see MIR-882)"))
		}
	}
	switch op.Action {
	case ActionRestart:
		return e.transition(op, PhaseRestarting)
	case ActionUpgrade:
		return e.transition(op, PhaseDownloading)
	}
	return e.fail(ctx, op, fmt.Errorf("unknown action %q", op.Action))
}

func (e *Executor) download(ctx context.Context, op *Operation) error {
	metadata, err := e.downloader.GetVersionMetadata(ctx, op.TargetVersion)
	if err != nil {
		return e.fail(ctx, op, fmt.Errorf("resolve version %q: %w", op.TargetVersion, err))
	}
	op.ResolvedVersion = metadata.Version
	op.ResolvedCommit = metadata.Commit

	if op.PreviousVersion != "" && sameBuild(op.PreviousVersion, op.PreviousCommit, metadata.Version, metadata.Commit) {
		// Already running the target; succeed without restarting for show.
		op.NewInstanceID = op.PreviousInstanceID
		op.NewVersion = op.PreviousVersion
		op.Progress = "already running " + op.ResolvedVersion
		return e.transition(op, PhaseSucceeded)
	}

	artifactType := e.opts.ArtifactType
	if op.ArtifactType != "" {
		artifactType = release.ArtifactType(op.ArtifactType)
	}
	artifact := release.NewArtifact(artifactType, op.TargetVersion)
	progress := &progressRecorder{store: e.store, op: op}
	downloaded, err := e.downloader.Download(ctx, artifact, release.DownloadOptions{
		TargetDir:      e.opts.TempDir,
		ProgressWriter: progress,
	})
	if err != nil {
		return e.fail(ctx, op, fmt.Errorf("download %s %s: %w", artifact.Type, op.ResolvedVersion, err))
	}
	e.downloaded = downloaded
	op.Progress = ""
	return e.transition(op, PhaseInstalling)
}

func (e *Executor) install(ctx context.Context, op *Operation) error {
	// A resumed run may find the install already done (died between the
	// rename and the checkpoint); the binary on disk is the truth.
	if onDisk, err := e.installer.GetCurrentVersion(ctx); err == nil &&
		sameBuild(onDisk.Version, onDisk.Commit, op.ResolvedVersion, op.ResolvedCommit) {
		return e.transition(op, PhaseRestarting)
	}
	if e.downloaded == nil {
		return e.transition(op, PhaseDownloading)
	}
	if err := e.installer.Install(ctx, e.downloaded); err != nil {
		return e.fail(ctx, op, fmt.Errorf("install: %w", err))
	}
	return e.transition(op, PhaseRestarting)
}

func (e *Executor) restart(ctx context.Context, op *Operation) error {
	// Resuming after a restart that took: do not bounce the server again.
	if op.PreviousInstanceID != "" {
		if snap, err := e.prober.Probe(ctx); err == nil && snap.InstanceID != op.PreviousInstanceID {
			return e.transition(op, PhaseVerifying)
		}
	}
	if err := e.restarter.Restart(ctx); err != nil {
		return e.failOrRollback(ctx, op, fmt.Errorf("restart: %w", err))
	}
	return e.transition(op, PhaseVerifying)
}

func (e *Executor) verify(ctx context.Context, op *Operation) error {
	snap, err := e.awaitReady(ctx, op, func(s Snapshot) bool {
		if op.Action == ActionUpgrade && !sameBuild(s.Version, s.Commit, op.ResolvedVersion, op.ResolvedCommit) {
			return false
		}
		return true
	})
	if err != nil {
		return e.failOrRollback(ctx, op, err)
	}
	op.NewInstanceID = snap.InstanceID
	op.NewVersion = snap.Version
	op.Progress = ""
	if op.Action == ActionUpgrade {
		e.ensurePathSymlink(op)
	}
	return e.transition(op, PhaseSucceeded)
}

func (e *Executor) rollback(ctx context.Context, op *Operation) error {
	// Rollback consumes the .old backup. A resumed run that died between the
	// restore and the restart finds no backup but the previous build already
	// on disk; that is a restore that took, not a missing one.
	restored := false
	if !e.installer.HasBackup() {
		if onDisk, err := e.installer.GetCurrentVersion(ctx); err == nil &&
			op.PreviousVersion != "" && sameBuild(onDisk.Version, onDisk.Commit, op.PreviousVersion, op.PreviousCommit) {
			restored = true
		}
	}
	if !restored {
		if err := e.installer.Rollback(ctx); err != nil {
			return e.fail(ctx, op, fmt.Errorf("%s; rollback failed: %w", op.Error, err))
		}
	}
	if err := e.restarter.Restart(ctx); err != nil {
		return e.fail(ctx, op, fmt.Errorf("%s; restart after rollback failed: %w", op.Error, err))
	}
	snap, err := e.awaitReady(ctx, op, func(Snapshot) bool { return true })
	if err != nil {
		return e.fail(ctx, op, fmt.Errorf("%s; server not ready after rollback: %w", op.Error, err))
	}
	op.NewInstanceID = snap.InstanceID
	op.NewVersion = snap.Version
	op.Progress = "rolled back to " + snap.Version
	return e.transition(op, PhaseRolledBack)
}

// awaitReady polls until a ready instance with a new id satisfies accept, or
// the timeout passes.
func (e *Executor) awaitReady(ctx context.Context, op *Operation, accept func(Snapshot) bool) (Snapshot, error) {
	timeout := e.opts.ReadyTimeout
	if op.ReadyTimeoutSeconds > 0 {
		timeout = time.Duration(op.ReadyTimeoutSeconds) * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last, detail string
	for attempt := 0; ; attempt++ {
		// Always probe once; after that, stop as soon as the deadline passes
		// rather than paying for one more probe.
		if attempt > 0 && time.Now().After(deadline) {
			break
		}
		snap, err := e.prober.Probe(ctx)
		switch {
		case err != nil:
			last = "waiting for the server to answer"
			detail = err.Error()
		case op.PreviousInstanceID != "" && snap.InstanceID == op.PreviousInstanceID:
			last = "still the previous server instance " + snap.InstanceID
		case !snap.Ready:
			last = "server " + snap.InstanceID + " is starting"
		case !accept(snap):
			last = fmt.Sprintf("server %s is ready but reports version %s", snap.InstanceID, snap.Version)
		default:
			return snap, nil
		}
		if op.Progress != last {
			op.Progress = last
			_ = e.store.Update(op)
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-time.After(e.opts.ProbeInterval):
		}
	}
	if detail != "" {
		return Snapshot{}, fmt.Errorf("server not ready within %s: %s (%s)", timeout, last, detail)
	}
	return Snapshot{}, fmt.Errorf("server not ready within %s: %s", timeout, last)
}

// failOrRollback and fail record an outcome, unless the run was cancelled: a
// cancelled phase stays as it was so a later run can resume it.
func (e *Executor) failOrRollback(ctx context.Context, op *Operation, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if op.Action == ActionUpgrade && e.opts.AutoRollback && !op.NoRollback && e.installer.HasBackup() {
		op.Error = cause.Error()
		e.log.Warn("lifecycle operation failed, rolling back", "operation", op.ID, "error", cause)
		return e.transition(op, PhaseRollingBack)
	}
	return e.fail(ctx, op, cause)
}

func (e *Executor) fail(ctx context.Context, op *Operation, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op.Error = cause.Error()
	e.log.Error("lifecycle operation failed", "operation", op.ID, "action", op.Action, "error", cause)
	return e.transition(op, PhaseFailed)
}

func (e *Executor) transition(op *Operation, to Phase) error {
	op.Phase = to
	return e.store.Update(op)
}

// Best-effort: the upgrade already succeeded, so a symlink problem is a log
// line, not a rollback.
func (e *Executor) ensurePathSymlink(op *Operation) {
	if e.opts.PathSymlink == "" {
		return
	}
	switch err := release.EnsurePathSymlink(e.opts.InstallPath, e.opts.PathSymlink); {
	case errors.Is(err, release.ErrPathManagedElsewhere):
		e.log.Info("leaving CLI path alone, managed by another tool", "path", e.opts.PathSymlink)
	case err != nil:
		e.log.Warn("could not update CLI symlink", "path", e.opts.PathSymlink, "error", err)
	}
}

// sameBuild: commits decide when both are known, otherwise version strings.
func sameBuild(versionA, commitA, versionB, commitB string) bool {
	if commitA != "" && commitA != "unknown" && commitB != "" && commitB != "unknown" {
		return commitA == commitB
	}
	return versionA == versionB
}

// progressRecorder writes download progress onto the operation, throttled so
// the store is not rewritten on every chunk.
type progressRecorder struct {
	store *Store
	op    *Operation

	mu      sync.Mutex
	total   int64
	current int64
	last    time.Time
}

func (p *progressRecorder) SetTotal(total int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
}

func (p *progressRecorder) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current += int64(len(data))
	if time.Since(p.last) < time.Second {
		return len(data), nil
	}
	p.last = time.Now()
	if p.total > 0 {
		p.op.Progress = fmt.Sprintf("downloading %d%% (%.1f/%.1f MB)", 100*p.current/p.total,
			float64(p.current)/(1024*1024), float64(p.total)/(1024*1024))
	} else {
		p.op.Progress = fmt.Sprintf("downloading %.1f MB", float64(p.current)/(1024*1024))
	}
	_ = p.store.Update(p.op)
	return len(data), nil
}
