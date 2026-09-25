package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestRenderServiceHealth(t *testing.T) {
	code := int64(0)
	health := []serviceHealth{
		{Service: "db", Running: 0, Dead: 3, CrashStreak: 7, CrashLooping: true, CooldownSeconds: 240, LastExitCode: &code, LastFailureLog: "connection refused"},
		{Service: "web", Running: 1},
	}
	output := renderServiceHealth(health)
	for _, fragment := range []string{"db: crash-looping, 0 running, 3 dead; 7 crashes in current streak, retry in", "Last exit code: 0", "connection refused", "web: 1 running, 0 dead"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("status output %q missing %q", output, fragment)
		}
	}
	if strings.Contains(output[strings.Index(output, "web:"):], "crash") {
		t.Fatalf("healthy service reports a crash: %q", output)
	}
	encoded, err := json.Marshal(health[0])
	if err != nil || !strings.Contains(string(encoded), `"last_exit_code":0`) || !strings.Contains(string(encoded), `"crash_streak":7`) {
		t.Fatalf("JSON status = %s, err = %v", encoded, err)
	}
}

func TestDoctorResourceChecks(t *testing.T) {
	env := verdictEnv(remoteHost, nil, probeOpen, probeOpen)
	env.resources = &doctorResources{
		disks:   []storage_v1alpha.Disk{{ID: entity.Id("disk-1"), Status: storage_v1alpha.ERROR}},
		volumes: []storage_v1alpha.DiskVolume{{ID: entity.Id("volume-1"), ActualState: storage_v1alpha.DV_ERROR, ErrorMessage: "attach failed"}},
	}
	crashed := &app_v1alpha.AppInfo{}
	crashed.SetName("demo")
	crashed.SetHealth("crashed")
	crashed.SetCrashCount(7)
	crashed.SetCooldownSeconds(240)
	healthy := &app_v1alpha.AppInfo{}
	healthy.SetName("web")
	healthy.SetHealth("healthy")
	env.apps = []*app_v1alpha.AppInfo{healthy, crashed}
	if got := checkApps(env); got.Status != checkFail || !strings.Contains(got.Summary, "demo (7 crashes") || got.Problem == nil {
		t.Fatalf("apps check = %+v", got)
	}
	if got := checkDisks(env); got.Status != checkFail || !strings.Contains(got.Summary, "attach failed") {
		t.Fatalf("disk check = %+v", got)
	}
	env.apps = []*app_v1alpha.AppInfo{healthy}
	if got := checkApps(env); got.Status != checkOK {
		t.Fatalf("healthy apps check = %+v", got)
	}
	env.appsErr = context.DeadlineExceeded
	if got := checkApps(env); got.Status != checkSkip {
		t.Fatalf("incomplete app scan reported health: %+v", got)
	}
	env.resources = nil
	env.resourcesErr = context.DeadlineExceeded
	if got := checkDisks(env); got.Status != checkSkip {
		t.Fatalf("incomplete disk scan reported health: %+v", got)
	}
}
