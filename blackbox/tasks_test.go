//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

type runEntry struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Trigger  string `json:"trigger"`
	Status   string `json:"status"`
	ExitCode *int32 `json:"exit_code"`
}

func listRuns(t *testing.T, m *harness.Miren, app string) []runEntry {
	t.Helper()

	r := m.MustRun("app", "runs", "-a", app, "--format", "json")

	var runs []runEntry
	if err := json.Unmarshal([]byte(r.Stdout), &runs); err != nil {
		t.Fatalf("failed to parse app runs JSON: %v\n%s", err, r.Stdout)
	}
	return runs
}

func findRun(runs []runEntry, id string) (runEntry, bool) {
	for _, r := range runs {
		if r.ID == id {
			return r, true
		}
	}
	return runEntry{}, false
}

// waitForRun polls until a run reaches a terminal status.
func waitForRun(t *testing.T, m *harness.Miren, app, id string) runEntry {
	t.Helper()

	var final runEntry
	harness.Poll(t, fmt.Sprintf("run %s finishes", id), 3*time.Minute, 2*time.Second, func() (bool, string) {
		run, ok := findRun(listRuns(t, m, app), id)
		if !ok {
			return false, fmt.Sprintf("run %s not listed yet", id)
		}
		switch run.Status {
		case "succeeded", "failed", "timed_out", "canceled":
			final = run
			return true, ""
		default:
			return false, fmt.Sprintf("run %s status: %s", id, run.Status)
		}
	})
	return final
}

// A task-only app has no long-running process at all. Deploying one exercises
// the whole point of the feature: it must build, activate, and report a health
// that means "deployed and invokable" rather than "scaled to zero".
func TestTaskOnlyAppDeploys(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "list", "--format", "json")

	var apps []struct {
		Name   string `json:"name"`
		Health string `json:"health"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &apps); err != nil {
		t.Fatalf("failed to parse app list JSON: %v", err)
	}

	var health string
	for _, a := range apps {
		if a.Name == name {
			health = a.Health
		}
	}

	if health != "ready" {
		t.Fatalf("task-only app reported health %q, want \"ready\" (an app doing exactly what it was configured to do must not read as asleep)", health)
	}
}

// A detached run returns its id immediately and keeps going without a client,
// which is what makes --detach mean only "don't attach my terminal".
func TestDetachedRunSucceeds(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--task", "hello", "--detach")
	id := strings.TrimSpace(r.Stdout)
	if id == "" {
		t.Fatal("expected a run id on stdout")
	}

	run := waitForRun(t, m, name, id)
	if run.Status != "succeeded" {
		t.Fatalf("run %s status %q, want succeeded", id, run.Status)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("run %s exit code %v, want 0", id, run.ExitCode)
	}
	if run.Trigger != "manual" {
		t.Fatalf("run %s trigger %q, want manual", id, run.Trigger)
	}
}

// A failing task must record its real exit code. This is what the whole Exit
// component exists for: before it, the code was logged and thrown away.
func TestFailingRunRecordsItsExitCode(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--task", "fail", "--detach")
	id := strings.TrimSpace(r.Stdout)

	run := waitForRun(t, m, name, id)
	if run.Status != "failed" {
		t.Fatalf("run %s status %q, want failed", id, run.Status)
	}
	if run.ExitCode == nil || *run.ExitCode != 3 {
		t.Fatalf("run %s exit code %v, want 3", id, run.ExitCode)
	}
}

// A run's output is findable by run id, which works because the run controller
// tags the sandbox spec's log attributes and the sandbox controller copies them
// onto every entry.
func TestRunLogsAreReadableByRunID(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--task", "hello", "--detach")
	id := strings.TrimSpace(r.Stdout)
	waitForRun(t, m, name, id)

	harness.Poll(t, "run output is readable", time.Minute, 3*time.Second, func() (bool, string) {
		out := m.Run("logs", "run", id, "-a", name)
		if !out.Success() {
			return false, fmt.Sprintf("logs run failed: %s", out.Stderr)
		}
		if !strings.Contains(out.Stdout, "hello from a task") {
			return false, "run output not visible yet"
		}
		return true, ""
	})
}

// Cancelling ends a run early. It is the only thing that does -- a client going
// away deliberately does not.
func TestRunCancellation(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--task", "slow", "--detach")
	id := strings.TrimSpace(r.Stdout)

	// Wait until it is actually going, so cancellation has something to stop.
	harness.Poll(t, "slow run starts", 2*time.Minute, 2*time.Second, func() (bool, string) {
		run, ok := findRun(listRuns(t, m, name), id)
		if !ok {
			return false, "run not listed yet"
		}
		if run.Status != "running" {
			return false, fmt.Sprintf("run status: %s", run.Status)
		}
		return true, ""
	})

	m.MustRun("app", "runs", "cancel", id, "-a", name)

	run := waitForRun(t, m, name, id)
	if run.Status != "canceled" {
		t.Fatalf("run %s status %q, want canceled", id, run.Status)
	}
	// No exit was observed, so none should be invented.
	if run.ExitCode != nil {
		t.Fatalf("run %s reported exit code %v; a canceled run observed none", id, *run.ExitCode)
	}
}

// An attached run propagates its exit code to the caller's shell, which is the
// behavior `miren app run` had before runs existed and must keep.
func TestAttachedRunPropagatesExitCode(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.Run("app", "run", "-a", name, "--task", "fail")
	r.RequireExitCode(t, 3)
}

// Bare `miren app run` -- no --task -- is the oldest surface this command has,
// and the one absorbing it into runs broke. The command resolved to the empty
// string, which the sandbox turns into "no process args at all", so runc
// refused to start the container and the run failed before executing anything.
func TestBareRunOpensAConsole(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--detach")
	id := strings.TrimSpace(r.Stdout)
	if id == "" {
		t.Fatal("expected a run id on stdout")
	}

	// A shell with nobody typing at it just waits, which is the point: reaching
	// running at all means it had a command to execute.
	harness.Poll(t, fmt.Sprintf("run %s starts", id), 3*time.Minute, 2*time.Second, func() (bool, string) {
		run, ok := findRun(listRuns(t, m, name), id)
		if !ok {
			return false, "not listed yet"
		}
		if run.Status == "running" {
			return true, ""
		}
		if run.Status == "failed" {
			t.Fatalf("bare run %s failed instead of opening a console; it resolved no command to execute", id)
		}
		return false, "status: " + run.Status
	})

	m.MustRun("app", "runs", "cancel", "-a", name, id)
	waitForRun(t, m, name, id)
}

// Arguments reach the container as the argv the caller typed. They are joined
// into one string for "sh -c", so anything that does not survive a round of
// shell parsing arrives as a different command than the one that was run.
func TestRunPreservesArgumentBoundaries(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--", "echo", "hello   world")

	if !strings.Contains(r.Stdout, "hello   world") {
		t.Fatalf("argument boundaries were lost; want \"hello   world\" in output, got:\n%s", r.Stdout)
	}
}

// [tasks.<name>.env] has to reach the container. A task names no service, so
// its env travels a path of its own, and it had none: the values were stored at
// build time and read back nowhere.
func TestRunCarriesDeclaredTaskEnv(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	r := m.MustRun("app", "run", "-a", name, "--task", "showenv")

	if !strings.Contains(r.Stdout, "TASK_SECRET=carried") {
		t.Fatalf("declared task env did not reach the container; got:\n%s", r.Stdout)
	}
}

// An attached run must not return before the command does. The attach used to
// end when the *client's* stdin ended, which non-interactively is immediately:
// the command kept running while the CLI reported success, so a script gating
// on `miren app run` proceeded as though a task that was still going had
// passed.
func TestAttachedRunWaitsForTheCommand(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "task-app"})

	start := time.Now()
	r := m.Run("app", "run", "-a", name, "--task", "brief")
	elapsed := time.Since(start)

	r.RequireExitCode(t, 7)

	if elapsed < 3*time.Second {
		t.Fatalf("attached run returned after %s, before the command it was watching could finish", elapsed)
	}
	if !strings.Contains(r.Stdout, "brief task done") {
		t.Fatalf("attached run missed output written before the command exited; got:\n%s", r.Stdout)
	}
}

// A failing deploy task must fail the deploy. The gate sits before any rollout
// begins, so the app should end up with no active version at all rather than a
// half-promoted one.
func TestDeployTaskGatesTheDeploy(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "deploy-gate")
	t.Cleanup(func() {
		m.Run("app", "delete", name, "-f")
	})

	dir := m.ContainerPath(fmt.Sprintf("%s/task-deploy-gate", c.TestdataDir))
	r := m.Run("deploy", "-a", name, "-d", dir, "-f")

	if r.Success() {
		t.Fatal("deploy succeeded despite its deploy task failing; the gate did not hold")
	}
	if !strings.Contains(r.Stdout+r.Stderr, "migrate") {
		t.Fatalf("deploy failure should name the task that failed:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}

	// The run is recorded even though the deploy failed -- that is where its
	// logs live and how you find out why.
	runs := listRuns(t, m, name)
	var found bool
	for _, run := range runs {
		if run.Task == "migrate" && run.Trigger == "deploy" {
			found = true
			if run.Status != "failed" {
				t.Errorf("deploy-triggered run status %q, want failed", run.Status)
			}
		}
	}
	if !found {
		t.Errorf("expected a deploy-triggered run for migrate, got %+v", runs)
	}
}
