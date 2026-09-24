package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestSummarizeServiceHealth(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	pools := []compute_v1alpha.SandboxPool{
		{ID: "pool-db", Service: "db", DesiredInstances: 1, ConsecutiveCrashCount: 2, LastCrashTime: now.Add(-time.Minute), CooldownUntil: now.Add(time.Minute)},
		{ID: "pool-web", Service: "web"},
	}
	sandboxes := []sandboxHealthRecord{
		{Pool: "pool-web", Sandbox: compute_v1alpha.Sandbox{ID: "web-1", Status: compute_v1alpha.RUNNING}},
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "db-1", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 0, At: now.Add(-9 * time.Minute)}}},
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "db-2", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 17, At: now.Add(-2 * time.Minute)}}},
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "db-3", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 5, At: now.Add(-11 * time.Minute)}}},
		{Pool: "pool-other", Sandbox: compute_v1alpha.Sandbox{ID: "foreign", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 99, At: now}}},
	}
	got := summarizeServiceHealth(pools, sandboxes, now)
	if len(got) != 2 || got[0].Service != "db" || got[0].Dead != 3 || got[0].Running != 0 || got[0].CrashStreak != 2 || !got[0].CrashLooping || got[0].LastExitCode == nil || *got[0].LastExitCode != 17 || got[0].lastSandbox != "db-2" {
		t.Fatalf("db summary = %+v", got)
	}
	if got[1].Service != "web" || got[1].Running != 1 || got[1].CrashLooping {
		t.Fatalf("web summary = %+v", got[1])
	}
	got[0].LastFailureLog = "database unavailable\nconnection refused"
	output := renderServiceHealth(got)
	for _, fragment := range []string{"db: crash-looping, 0 running, 3 dead; 2 crashes in current streak", "Last exit code: 17", "      connection refused", "web: 1 running, 0 dead"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("status output %q missing %q", output, fragment)
		}
	}
	if strings.Contains(output[strings.Index(output, "web:"):], "crash") {
		t.Fatalf("healthy web service reports a crash: %q", output)
	}
	encoded, err := json.Marshal(got[0])
	if err != nil || !strings.Contains(string(encoded), `"last_exit_code":17`) || !strings.Contains(string(encoded), `"crash_streak":2`) {
		t.Fatalf("JSON status = %s, err = %v", encoded, err)
	}
	zero := summarizeServiceHealth(pools, []sandboxHealthRecord{{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "db-zero", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 0, At: now.Add(-time.Minute)}}}}, now)[0]
	if zero.LastExitCode == nil || *zero.LastExitCode != 0 {
		t.Fatalf("zero exit code must be present: %+v", zero)
	}
	early := summarizeServiceHealth(pools, []sandboxHealthRecord{{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "db-early", Status: compute_v1alpha.DEAD}, Updated: now.Add(-time.Minute)}}, now)[0]
	if early.CrashStreak != 2 || early.LastExitCode != nil || early.lastSandbox != "" {
		t.Fatalf("early failure without an exit record = %+v", early)
	}
	latest := summarizeServiceHealth(pools, []sandboxHealthRecord{
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "success", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 0, At: now.Add(-time.Minute)}}},
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "failure", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 17, At: now.Add(-2 * time.Minute)}}},
		{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{ID: "retired", Status: compute_v1alpha.DEAD}, Updated: now},
	}, now)[0]
	if latest.LastExitCode == nil || *latest.LastExitCode != 0 || latest.lastSandbox != "failure" || !latest.lastFailure.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("newer successful exit hid earlier failure or was replaced by retirement: %+v", latest)
	}
	if idle := summarizeServiceHealth([]compute_v1alpha.SandboxPool{{ID: "pool-db", Service: "db", DesiredInstances: 0, ConsecutiveCrashCount: 4, LastCrashTime: now.Add(-time.Minute), CooldownUntil: now.Add(time.Minute)}}, sandboxes, now)[0]; idle.CrashStreak != 0 || idle.CrashLooping {
		t.Fatalf("scaled-to-zero service should not appear to be failing: %+v", idle)
	}
}

func TestScaleDownIsNotACrashLoop(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	retired := make([]sandboxHealthRecord, 3)
	for i := range retired {
		retired[i] = sandboxHealthRecord{Pool: "pool-web", Sandbox: compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD}, Updated: now.Add(-time.Minute)}
	}
	for _, desired := range []int64{1, 0, 1} { // 4→1, scaled to zero, then woken
		pool := compute_v1alpha.SandboxPool{ID: "pool-web", Service: "web", DesiredInstances: desired}
		got := summarizeServiceHealth([]compute_v1alpha.SandboxPool{pool}, retired, now)[0]
		if got.Dead != 3 || got.CrashStreak != 0 || got.CrashLooping {
			t.Fatalf("desired %d: %+v", desired, got)
		}
	}
	env := verdictEnv(remoteHost, nil, probeOpen, probeOpen)
	env.resources = &doctorResources{pools: []compute_v1alpha.SandboxPool{{ID: "pool-web", Service: "web", DesiredInstances: 1}}, sandboxes: retired}
	if got := checkSandboxes(env); got.Status != checkOK {
		t.Fatalf("doctor treated scale-down as a failure: %+v", got)
	}
	// A later intentional retirement must not replace the last known failed exit.
	pool := compute_v1alpha.SandboxPool{ID: "pool-web", Service: "web", DesiredInstances: 1, ConsecutiveCrashCount: 1, LastCrashTime: now.Add(-2 * time.Minute)}
	retired = append(retired, sandboxHealthRecord{Pool: "pool-web", Sandbox: compute_v1alpha.Sandbox{ID: "failed", Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 17, At: now.Add(-2 * time.Minute)}}})
	got := summarizeServiceHealth([]compute_v1alpha.SandboxPool{pool}, retired, now)[0]
	if got.LastExitCode == nil || *got.LastExitCode != 17 || got.lastSandbox != "failed" {
		t.Fatalf("known failure lost to retirement: %+v", got)
	}
}

func TestActiveServicePoolsUsesVersionMembership(t *testing.T) {
	pools := []compute_v1alpha.SandboxPool{
		{ID: "current", SandboxSpec: compute_v1alpha.SandboxSpec{Version: "app-version-456"}, ReferencedByVersions: []entity.Id{"app-version-456", "app-version-123"}},
		{ID: "old", SandboxSpec: compute_v1alpha.SandboxSpec{Version: "app-version-123"}, ReferencedByVersions: []entity.Id{"app-version-456"}},
		{ID: "legacy", SandboxSpec: compute_v1alpha.SandboxSpec{Version: "app-version-789"}},
	}
	got := activeServicePools(pools, "app-version-123")
	if len(got) != 1 || got[0].ID != "current" {
		t.Fatalf("pools for active version = %+v", got)
	}
	if got := activeServicePools(pools, "app-version-789"); len(got) != 1 || got[0].ID != "legacy" {
		t.Fatalf("legacy pool membership = %+v", got)
	}
	if got := activeServicePools(pools, ""); len(got) != 3 {
		t.Fatalf("pools before first deployment = %+v", got)
	}
}

func TestDoctorResourceChecks(t *testing.T) {
	now := time.Now()
	env := verdictEnv(remoteHost, nil, probeOpen, probeOpen)
	env.resources = &doctorResources{
		pools:   []compute_v1alpha.SandboxPool{{ID: "pool-db", Service: "db", DesiredInstances: 1, ReadyInstances: 0, ConsecutiveCrashCount: 3, LastCrashTime: now.Add(-time.Minute), CooldownUntil: now.Add(time.Minute)}},
		disks:   []storage_v1alpha.Disk{{ID: entity.Id("disk-1"), Status: storage_v1alpha.ERROR}},
		volumes: []storage_v1alpha.DiskVolume{{ID: entity.Id("volume-1"), ActualState: storage_v1alpha.DV_ERROR, ErrorMessage: "attach failed"}},
	}
	env.orphans = 2
	for i := 0; i < 3; i++ {
		env.resources.sandboxes = append(env.resources.sandboxes, sandboxHealthRecord{Pool: "pool-db", Sandbox: compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{At: now.Add(-time.Duration(i+1) * time.Minute)}}})
	}
	for _, tc := range []struct {
		name     string
		check    func(*doctorEnv) checkResult
		want     checkStatus
		fragment string
	}{
		{"indexes", checkEntityIndexes, checkWarn, "2 orphaned"},
		{"sandboxes", checkSandboxes, checkFail, "3 consecutive crashes"},
		{"pools", checkPools, checkFail, "db"},
		{"disks", checkDisks, checkFail, "attach failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.check(env)
			if got.Status != tc.want || !strings.Contains(got.Summary, tc.fragment) || got.Problem == nil || len(got.Problem.Actions) == 0 {
				t.Fatalf("check = %+v, want %s with action and %q", got, tc.want, tc.fragment)
			}
		})
	}
	if got := checkPools(env); got.Problem == nil || got.Problem.Actions[0].Command != "miren sandbox-pool list" {
		t.Fatalf("pool diagnostic suggests invalid command: %+v", got)
	}
	env.indexErr = context.DeadlineExceeded
	if got := checkEntityIndexes(env); got.Status != checkWarn {
		t.Fatalf("timed-out index check = %+v, want warning", got)
	}
	resources := env.resources
	env.resources = nil
	env.resourcesErr = context.DeadlineExceeded
	env.indexErr = nil
	for _, check := range []func(*doctorEnv) checkResult{checkSandboxes, checkPools, checkDisks} {
		if got := check(env); got.Status != checkSkip {
			t.Fatalf("incomplete resource scan reported health: %+v", got)
		}
	}
	if got := checkEntityIndexes(env); got.Status != checkWarn {
		t.Fatalf("independent index check = %+v, want warning for two orphans", got)
	}
	env.resourcesErr = nil
	env.resources = resources
	if got := checkPools(env); got.Status != checkFail {
		t.Fatalf("pool check = %+v, want independent failure", got)
	}
	// Two apps with a service called db must not combine into a false alert.
	env.resources.pools[0].App = "app-a"
	env.resources.pools[0].ConsecutiveCrashCount = 2
	env.resources.pools = append(env.resources.pools, compute_v1alpha.SandboxPool{ID: "pool-other", App: "app-b", Service: "db"})
	env.resources.sandboxes = env.resources.sandboxes[:2]
	env.resources.sandboxes = append(env.resources.sandboxes, sandboxHealthRecord{Pool: "pool-other", Sandbox: compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{At: now.Add(-time.Minute)}}})
	if got := checkSandboxes(env); got.Status != checkOK {
		t.Fatalf("distinct app failures = %+v, want no aggregate alert", got)
	}
}
