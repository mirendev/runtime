package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	runapi "miren.dev/runtime/api/run"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
)

// ConsoleTask is the task name `miren app run` resolves when none is given.
//
// The convention predates tasks: servers/exec already looked for a service
// literally called "console". Moving it to [tasks.console] also fixes an
// overload nobody wanted, since declaring [services.console] used to get you
// both a long-running service the launcher keeps up and the command app run
// executes -- and only the second was ever the point.
const ConsoleTask = "console"

// ConsoleMaxConcurrent is the ceiling on simultaneous console runs.
//
// It is a new limit on an existing command rather than a limit on new
// functionality, so it is set well past what anyone reaches by hand. An app
// that genuinely wants more can declare [tasks.console] and say so.
const ConsoleMaxConcurrent = 10

var _ app_v1alpha.Runs = &AppInfo{}

// CreateRun records a run and returns immediately.
//
// Admission and launching belong to the run controller, so a caller never
// blocks on capacity here: a run that cannot start yet simply stays pending.
// That is also what lets deploy- and schedule-triggered runs share one gate
// with a manual invoke without any of them polling.
func (r *AppInfo) CreateRun(ctx context.Context, state *app_v1alpha.RunsCreateRun) error {
	args := state.Args()

	// CreateRun executes a command inside the app's image with its credentials,
	// so this is the gate that matters most of the four.
	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, args.App(), &appRec); err != nil {
		return fmt.Errorf("app %s not found: %w", args.App(), err)
	}
	if appRec.ActiveVersion == "" {
		return fmt.Errorf("app %s has no active version to run against", args.App())
	}

	appName, err := r.appName(ctx, appRec.ID, args.App())
	if err != nil {
		return err
	}

	cfgSpec, err := r.resolveActiveConfig(ctx, appRec.ActiveVersion)
	if err != nil {
		return err
	}

	taskName := args.Task()
	if taskName == "" {
		taskName = ConsoleTask
	}

	task := findTask(cfgSpec, taskName)

	// The console task is a convention rather than a declaration: an app that
	// never mentions it still gets one, resolved from the image the same way it
	// always was. Any other name has to exist.
	if task == nil && taskName != ConsoleTask {
		return fmt.Errorf("app %s declares no task named %q", args.App(), taskName)
	}

	command, err := resolveCommand(task, args.Command())
	if err != nil {
		return err
	}

	timeout := ""
	if task != nil {
		timeout = task.Timeout
	}

	// One attempt, whatever the task declares. Retries exist for triggers
	// nobody is watching; a manual run that fails just fails and the caller
	// decides. The controller enforces this too, so setting it here keeps the
	// stored run honest rather than carrying a budget that is never spent.
	const manualMaxAttempts = 1

	name := fmt.Sprintf("%s-%s-%s", appName, taskName, idgen.Gen(""))
	run := &run_v1alpha.Run{
		App:         appRec.ID,
		Version:     appRec.ActiveVersion,
		Task:        taskName,
		Trigger:     run_v1alpha.MANUAL,
		Command:     command,
		Status:      run_v1alpha.PENDING,
		Timeout:     timeout,
		MaxAttempts: manualMaxAttempts,
	}

	id, err := r.EC.Create(ctx, name, run)
	if err != nil {
		return fmt.Errorf("creating run: %w", err)
	}

	r.Log.Info("created run",
		"run", id, "app", appName, "task", taskName, "trigger", "manual")

	state.Results().SetId(id.String())
	// Derived from the run and the attempt by the same helper the controller
	// uses, so a client can attach without waiting to observe the sandbox being
	// created -- and the two cannot drift apart.
	state.Results().SetSandboxName(runapi.SandboxName(id, 1).String())
	return nil
}

func (r *AppInfo) ListRuns(ctx context.Context, state *app_v1alpha.RunsListRuns) error {
	args := state.Args()

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, args.App(), &appRec); err != nil {
		return fmt.Errorf("app %s not found: %w", args.App(), err)
	}

	results, err := r.EC.List(ctx, entity.Ref(run_v1alpha.RunAppId, appRec.ID))
	if err != nil {
		return fmt.Errorf("listing runs: %w", err)
	}

	var runs []*app_v1alpha.RunInfo
	for results.Next() {
		var run run_v1alpha.Run
		results.Read(&run)

		if args.Task() != "" && run.Task != args.Task() {
			continue
		}
		runs = append(runs, runInfo(&run))
	}

	// Failures first, then newest. The reason someone opens this list is almost
	// always a failure, and a list where they are buried under successful
	// console sessions is a list nobody reads.
	slices.SortStableFunc(runs, func(a, b *app_v1alpha.RunInfo) int {
		if af, bf := isFailure(a.Status()), isFailure(b.Status()); af != bf {
			if af {
				return -1
			}
			return 1
		}
		// cmp.Compare rather than subtraction: these are Unix milliseconds, and
		// their difference does not fit an int on a 32-bit build.
		return cmp.Compare(b.StartedAt(), a.StartedAt())
	})

	if limit := int(args.Limit()); limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	state.Results().SetRuns(runs)
	return nil
}

func (r *AppInfo) GetRun(ctx context.Context, state *app_v1alpha.RunsGetRun) error {
	run, _, err := r.lookupRun(ctx, state.Args().Id())
	if err != nil {
		return err
	}
	if err := r.authorizeRun(ctx, run); err != nil {
		return err
	}

	state.Results().SetRun(runInfo(run))
	return nil
}

// CancelRun records a cancellation request. The controller owns every status
// transition, so this deliberately does not write CANCELED itself -- a second
// writer would race the reconcile that is also deciding this run's fate.
func (r *AppInfo) CancelRun(ctx context.Context, state *app_v1alpha.RunsCancelRun) error {
	run, _, err := r.lookupRun(ctx, state.Args().Id())
	if err != nil {
		return err
	}
	if err := r.authorizeRun(ctx, run); err != nil {
		return err
	}

	if isRunTerminal(run.Status) {
		state.Results().SetCanceled(false)
		return nil
	}

	if err := r.EC.Patch(ctx, run.ID, 0,
		entity.Time(run_v1alpha.RunCancelRequestedAtId, time.Now()),
	); err != nil {
		return fmt.Errorf("requesting cancellation: %w", err)
	}

	r.Log.Info("cancellation requested for run", "run", run.ID, "task", run.Task)
	state.Results().SetCanceled(true)
	return nil
}

// authorizeRun resolves the app a run belongs to and applies the same guard the
// app-named methods use.
//
// GetRun and CancelRun take a bare run id, so without this an app-scoped caller
// could read or cancel any run in the cluster. The app is resolved from the run
// itself rather than anything caller-supplied.
func (r *AppInfo) authorizeRun(ctx context.Context, run *run_v1alpha.Run) error {
	var md core_v1alpha.Metadata
	if err := r.EC.GetById(ctx, run.App, &md); err != nil {
		// The owning app can't be resolved, so the guard can't be evaluated.
		// Refuse rather than defaulting open.
		return rpc.AppAccessError(ctx, run.App.String())
	}

	if !rpc.AllowApp(ctx, md.Name) {
		return rpc.AppAccessError(ctx, md.Name)
	}
	return nil
}

func (r *AppInfo) lookupRun(ctx context.Context, id string) (*run_v1alpha.Run, entity.Id, error) {
	if id == "" {
		return nil, "", fmt.Errorf("run id is required")
	}

	var run run_v1alpha.Run
	err := r.EC.GetById(ctx, entity.Id(id), &run)
	if err != nil && errors.Is(err, cond.ErrNotFound{}) && !strings.Contains(id, "/") {
		// Accept a bare name as well as a full id.
		err = r.EC.GetById(ctx, entity.Id("run/"+id), &run)
	}
	if err != nil {
		return nil, "", fmt.Errorf("run %s not found: %w", id, err)
	}

	return &run, run.ID, nil
}

// appName resolves the app's metadata name, which is what a run's entity name
// and its sandbox labels are built from.
func (r *AppInfo) appName(ctx context.Context, appID entity.Id, fallback string) (string, error) {
	var md core_v1alpha.Metadata
	if err := r.EC.GetById(ctx, appID, &md); err != nil {
		return fallback, nil
	}
	if md.Name == "" {
		return fallback, nil
	}
	return md.Name, nil
}

func (r *AppInfo) resolveActiveConfig(ctx context.Context, versionID entity.Id) (*core_v1alpha.ConfigSpec, error) {
	var ver core_v1alpha.AppVersion
	if err := r.EC.GetById(ctx, versionID, &ver); err != nil {
		return nil, fmt.Errorf("reading active version: %w", err)
	}

	cfgSpec, err := coreutil.ResolveConfig(ctx, r.EC.EAC(), &ver)
	if err != nil {
		return nil, fmt.Errorf("resolving config: %w", err)
	}
	return cfgSpec, nil
}

func findTask(cfgSpec *core_v1alpha.ConfigSpec, name string) *core_v1alpha.ConfigSpecTasks {
	if cfgSpec == nil {
		return nil
	}
	for i := range cfgSpec.Tasks {
		if cfgSpec.Tasks[i].Name == name {
			return &cfgSpec.Tasks[i]
		}
	}
	return nil
}

// resolveCommand picks what a run executes: an explicit override, else the
// task's declared command, else nothing -- which leaves the resolution to the
// runner's console fallback chain, since what a bare `miren app run` should do
// depends on what the image supplies and cannot be written down as a default.
func resolveCommand(task *core_v1alpha.ConfigSpecTasks, override []string) (string, error) {
	if len(override) > 0 {
		return strings.Join(override, " "), nil
	}
	if task != nil {
		return task.Command, nil
	}
	return "", nil
}

func runInfo(run *run_v1alpha.Run) *app_v1alpha.RunInfo {
	var info app_v1alpha.RunInfo
	info.SetId(run.ID.String())
	info.SetTask(run.Task)
	info.SetTrigger(strings.TrimPrefix(string(run.Trigger), "trigger."))
	info.SetStatus(strings.TrimPrefix(string(run.Status), "status."))
	info.SetCommand(run.Command)
	info.SetAttempt(int32(clampInt32(run.Attempt)))
	info.SetSandbox(run.Sandbox.String())
	info.SetVersion(run.Version.String())

	// An exit code is reported only when the command's own exit is what ended
	// the run.
	//
	// A canceled or timed-out run does produce an observed code -- the platform
	// killed the process and the kernel reported something -- but that number is
	// teardown noise, not the task's outcome, and showing it beside a "canceled"
	// status invites reading it as an application error. The observation stays
	// on the entity; it just isn't the run's result. The status carries the
	// meaning in those cases.
	if reportsExitCode(run.Status) && !run.Result.At.IsZero() {
		// The entity stores int64 and the wire field is int32. Real exit codes
		// are a byte, so this only matters for a corrupt or hostile value --
		// but silently wrapping one into a plausible-looking code is the worst
		// available outcome, so clamp instead.
		info.SetExitCode(int32(clampInt32(run.Result.Code)))
	}

	if !run.StartedAt.IsZero() {
		info.SetStartedAt(run.StartedAt.UnixMilli())
	}
	if !run.EndedAt.IsZero() {
		info.SetEndedAt(run.EndedAt.UnixMilli())
	}

	return &info
}

// clampInt32 saturates rather than wrapping at the wire boundary.
//
// The entity fields are int64 and the RPC ones int32. Real exit codes are a
// byte and real attempt counts are single digits, so this only matters for a
// corrupt or hostile value -- but wrapping one into a small plausible-looking
// number is the worst available outcome, since nothing downstream can tell it
// apart from a genuine result.
func clampInt32(v int64) int64 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return v
}

// reportsExitCode says whether a run ended by its command exiting, which is the
// only case where an exit code describes the task rather than its teardown.
func reportsExitCode(s run_v1alpha.RunStatus) bool {
	switch s {
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED:
		return true
	case run_v1alpha.PENDING, run_v1alpha.RUNNING, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		return false
	}
	return false
}

func isFailure(status string) bool {
	switch status {
	case "failed", "timed_out", "canceled":
		return true
	}
	return false
}

func isRunTerminal(s run_v1alpha.RunStatus) bool {
	switch s {
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		return true
	case run_v1alpha.PENDING, run_v1alpha.RUNNING:
		return false
	}
	return false
}
