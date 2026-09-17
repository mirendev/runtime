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

// DataBackup snapshots the server's data before an upgrade. The executor
// does not know what the data is: it stores the reference it gets back on
// the operation, and on rollback hands it to the restarted server, which
// does know how to put it back before serving.
type DataBackup interface {
	// Backup takes a snapshot for the operation and returns its reference.
	Backup(ctx context.Context, opID string) (string, error)
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
	// backup is optional: without one, upgrades skip the backing_up phase
	// and rollback restores only the binary.
	backup DataBackup

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
func (e *Executor) WithDataBackup(b DataBackup) *Executor         { e.backup = b; return e }

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
	case PhaseBackingUp:
		return e.backUp(ctx, op)
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
	return e.transition(op, PhaseBackingUp)
}

// backUp takes the snapshot rollback will restore. An upgrade that cannot be
// backed up does not proceed: without the snapshot, a rollback would leave
// the previous build running against whatever the new one did to the data.
// NoRollback opts out, since that is the one case the snapshot has no use.
func (e *Executor) backUp(ctx context.Context, op *Operation) error {
	if e.backup == nil || op.NoRollback {
		return e.transition(op, PhaseInstalling)
	}
	ref, err := e.backup.Backup(ctx, op.ID)
	if err != nil {
		return e.fail(ctx, op, fmt.Errorf("back up data before upgrade: %w", err))
	}
	op.BackupRef = ref
	e.log.Info("backed up data before upgrade", "operation", op.ID, "backup_ref", ref)
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
		// Downloading again also takes the snapshot again, which is fine: the
		// fresher one wins and it is stored under the same reference.
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
	op.Components = snap.Components
	op.Progress = ""
	if op.Action == ActionUpgrade {
		e.ensurePathSymlink(op)
	}
	return e.transition(op, PhaseSucceeded)
}

// rollback restores the binary, then asks the server to restore the data on
// its way back up. The executor cannot restore data itself: the data store
// is not a child of the server process and may outlive it (a graceful stop
// takes it down, an unclean death does not), so the one moment it can be
// replaced safely is inside the booting server, before anything reads it.
// The request is recorded on the operation before the restart, and the
// server's answer is read back after it.
func (e *Executor) rollback(ctx context.Context, op *Operation) error {
	// The restore request goes down before the binary does. The moment the
	// previous binary is back on disk, systemd's Restart=always can launch
	// it without waiting for us, and a launch that finds no request boots on
	// the data the rollback was meant to replace. The request names the
	// build it is for so that the build being rolled back from, which may
	// still be crash looping in the same window, leaves it alone.
	if op.BackupRef != "" && op.DataRestore == nil {
		op.DataRestore = &DataRestore{BackupRef: op.BackupRef, ForVersion: op.PreviousVersion, ForCommit: op.PreviousCommit}
		if err := e.store.Update(op); err != nil {
			return err
		}
	}
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
		// A server that refuses to boot because the restore failed says so
		// in its answer; that reason belongs on the record, not just in the
		// journal. The request stays pending, so the server keeps refusing
		// until the restore works or an operator abandons the operation.
		if result, _ := e.readRestoreResult(op); result != nil && result.Error != "" {
			op.DataRestore.Error = result.Error
			return e.fail(ctx, op, fmt.Errorf("%s; server not ready after rollback: %w; data restore failed: %s (the server retries on every boot; 'miren server operations abandon %s' starts it on the data as it is)",
				op.Error, err, result.Error, op.ID))
		}
		return e.fail(ctx, op, fmt.Errorf("%s; server not ready after rollback: %w", op.Error, err))
	}
	op.NewInstanceID = snap.InstanceID
	op.NewVersion = snap.Version
	op.Components = snap.Components
	if op.DataRestore != nil {
		// The server is up, so either it restored the data or it never saw
		// the request (a build that predates data restore, say). The old
		// build on new data is exactly what this phase exists to prevent, so
		// that is recorded as a failure, not a rollback.
		result, err := e.readRestoreResult(op)
		if err != nil {
			return err
		}
		if result == nil {
			return e.fail(ctx, op, fmt.Errorf("%s; rolled back to %s but it did not restore %s (does that build predate data restore?)",
				op.Error, snap.Version, op.DataRestore.BackupRef))
		}
		if result.Error != "" {
			op.DataRestore.Error = result.Error
			return e.fail(ctx, op, fmt.Errorf("%s; rolled back to %s but restoring %s failed: %s",
				op.Error, snap.Version, op.DataRestore.BackupRef, result.Error))
		}
		restoredAt := result.RestoredAt
		op.DataRestore.RestoredAt = &restoredAt
	}
	op.Progress = "rolled back to " + snap.Version
	return e.transition(op, PhaseRolledBack)
}

// readRestoreResult returns nil, nil when the server has not answered.
func (e *Executor) readRestoreResult(op *Operation) (*RestoreResult, error) {
	if op.DataRestore == nil {
		return nil, nil
	}
	result, err := e.store.ReadRestoreResult(op.ID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return result, err
}

// ErrNothingToAbandon is returned by Abandon for an operation that is
// finished and left nothing pending.
var ErrNothingToAbandon = errors.New("operation is finished and left nothing pending")

// Abandon is the operator's way out of an operation nobody will finish: an
// executor that died mid-operation, or a rollback whose data restore keeps
// failing and keeps the server from booting. It marks an unfinished
// operation failed and settles a pending restore request as abandoned, so
// the next boot starts on the data as it is. It refuses while an executor
// still holds the operation.
func Abandon(store *Store, id string) (*Operation, error) {
	unlock, err := store.LockOperation(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	op, err := store.Get(id)
	if err != nil {
		return nil, err
	}
	const reason = "abandoned by operator"
	changed := false
	if op.DataRestore != nil {
		result, err := store.ReadRestoreResult(id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if result == nil || !result.Settled() {
			if err := store.WriteRestoreResult(&RestoreResult{
				OperationID: id, BackupRef: op.DataRestore.BackupRef, Error: reason, Abandoned: true,
			}); err != nil {
				return nil, err
			}
			op.DataRestore.Error = appendReason(op.DataRestore.Error, reason)
			changed = true
		}
	}
	if !op.Done() {
		op.Phase = PhaseFailed
		op.Error = appendReason(op.Error, reason)
		changed = true
	}
	if !changed {
		return op, fmt.Errorf("%w: %s", ErrNothingToAbandon, id)
	}
	return op, store.Update(op)
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

func appendReason(existing, reason string) string {
	if existing == "" {
		return reason
	}
	return existing + "; " + reason
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
