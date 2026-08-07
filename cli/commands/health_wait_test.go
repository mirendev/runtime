package commands

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/apphealth"
)

// fakeGetter returns a canned ApplicationStatus (and an optional error) for
// every poll.
type fakeGetter struct {
	status *app_v1alpha.ApplicationStatus
	err    error
}

func (f fakeGetter) AppInfo(ctx context.Context, application string) (*app_v1alpha.ApplicationStatus, error) {
	return f.status, f.err
}

// logLine is a canned log entry with an optional source.
type logLine struct {
	line   string
	source string
}

// fakeTailer returns canned log lines. lines (source-less) and sourced are both
// emitted, in that order.
type fakeTailer struct {
	lines   []string
	sourced []logLine
	err     error
}

func (f fakeTailer) RecentLogs(ctx context.Context, application string) ([]*app_v1alpha.LogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	var entries []*app_v1alpha.LogEntry
	for _, l := range f.lines {
		var e app_v1alpha.LogEntry
		e.SetLine(l)
		entries = append(entries, &e)
	}
	for _, l := range f.sourced {
		var e app_v1alpha.LogEntry
		e.SetLine(l.line)
		e.SetSource(l.source)
		entries = append(entries, &e)
	}
	return entries, nil
}

func pollerContext(base context.Context) (*Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	ctx := &Context{
		Context: base,
		Stdout:  buf,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return ctx, buf
}

func status(active, health string, ready, desired int32) *app_v1alpha.ApplicationStatus {
	var s app_v1alpha.ApplicationStatus
	s.SetActiveVersion(active)
	s.SetHealth(health)
	s.SetReadyInstances(ready)
	s.SetDesiredInstances(desired)
	return &s
}

func TestDecideActivation(t *testing.T) {
	cases := []struct {
		name string
		snap healthSnapshot
		want activationDecision
	}{
		{"not active yet", healthSnapshot{versionActive: false, health: apphealth.Healthy, ready: 1}, decisionWait},
		{"crashed wins over counts", healthSnapshot{versionActive: true, health: apphealth.Crashed, ready: 1, desired: 1}, decisionCrashed},
		{"scaled to zero", healthSnapshot{versionActive: true, health: apphealth.Idle, desired: 0}, decisionScaledToZero},
		// Without its own arm this falls to ready > 0, which a task-only app
		// never satisfies -- the deploy would time out despite succeeding.
		{"task-only app is deployed and invokable", healthSnapshot{versionActive: true, health: apphealth.Ready, ready: 0, desired: 0}, decisionTaskOnly},
		{"one of many serving", healthSnapshot{versionActive: true, health: apphealth.Degraded, ready: 1, desired: 3}, decisionHealthy},
		{"fully healthy", healthSnapshot{versionActive: true, health: apphealth.Healthy, ready: 2, desired: 2}, decisionHealthy},
		{"active but nothing serving", healthSnapshot{versionActive: true, health: apphealth.Starting, ready: 0, desired: 1}, decisionWait},
		{"old server with no health reporting falls back to active", healthSnapshot{versionActive: true, health: "", ready: 0, desired: 0}, decisionHealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, decideActivation(tc.snap))
		})
	}
}

func TestWaitForActivation_HealthyServing(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	getter := fakeGetter{status: status("app-v2", apphealth.Healthy, 2, 2)}

	err := waitForActivation(ctx, getter, fakeTailer{}, "app", "app-v2", "app-v2")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "live and serving")
}

func TestWaitForActivation_LegacyServerSucceeds(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	// An older server doesn't populate health/ready, so the active version is
	// all we can confirm. We must report success, not wait out the timeout.
	getter := fakeGetter{status: status("app-v2", "", 0, 0)}

	err := waitForActivation(ctx, getter, fakeTailer{}, "app", "app-v2", "app-v2")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "doesn't report health")
}

func TestWaitForActivation_PartiallyReadyIsSuccess(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	getter := fakeGetter{status: status("app-v2", apphealth.Degraded, 1, 3)}

	err := waitForActivation(ctx, getter, fakeTailer{}, "app", "app-v2", "app-v2")

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "live and serving")
	assert.Contains(t, out, "1/3 instances ready")
}

func TestWaitForActivation_ScaledToZero(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	getter := fakeGetter{status: status("app-v2", apphealth.Idle, 0, 0)}

	err := waitForActivation(ctx, getter, fakeTailer{}, "app", "app-v2", "app-v2")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "scaled to zero")
}

func TestWaitForActivation_CrashedFailsWithLogs(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	s := status("app-v2", apphealth.Crashed, 0, 1)
	s.SetCrashCount(3)
	s.SetCooldownSeconds(10)
	getter := fakeGetter{status: s}
	tailer := fakeTailer{lines: []string{"panic: boom", "exit status 2"}}

	err := waitForActivation(ctx, getter, tailer, "app", "app-v2", "app-v2")

	require.Error(t, err)
	out := buf.String()
	assert.Contains(t, out, "never became healthy")
	assert.Contains(t, out, "crash-looped")
	assert.Contains(t, out, "3 consecutive crash")
	assert.Contains(t, out, "panic: boom")
}

func TestWaitForActivation_CrashLogsFilterBuildNoise(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	s := status("app-v2", apphealth.Crashed, 0, 1)
	getter := fakeGetter{status: s}
	tailer := fakeTailer{sourced: []logLine{
		{line: "[phase] Building Go application", source: "build"},
		{line: "panic: boom", source: "sb-1tD"},
		{line: "starting up...", source: "system"},
	}}

	err := waitForActivation(ctx, getter, tailer, "app", "app-v2", "app-v2")

	require.Error(t, err)
	out := buf.String()
	assert.Contains(t, out, "panic: boom")
	assert.NotContains(t, out, "Building Go application")
	assert.NotContains(t, out, "starting up...")
}

func TestWaitForActivation_CancelledStops(t *testing.T) {
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	buf := &bytes.Buffer{}
	ctx := &Context{
		Context: cancelCtx,
		Stdout:  buf,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	// Active but nothing serving yet, so decideActivation returns wait and the
	// loop falls through to the cancelled context.
	getter := fakeGetter{status: status("app-v2", apphealth.Starting, 0, 1)}

	err := waitForActivation(ctx, getter, fakeTailer{}, "app", "app-v2", "app-v2")

	assert.ErrorIs(t, err, context.Canceled)
}

func TestPollHealth_NormalizesEntityID(t *testing.T) {
	ctx, _ := pollerContext(context.Background())
	getter := fakeGetter{status: status("go-server-v3", apphealth.Healthy, 1, 1)}

	// CLI passes an entity ID; AppInfo reports the short version string.
	snap := pollHealth(ctx, getter, "app", "app_version/go-server-v3")

	assert.True(t, snap.versionActive)
	assert.Equal(t, apphealth.Healthy, snap.health)
}

func TestReportBoundPortDivergence(t *testing.T) {
	ctx, buf := pollerContext(context.Background())

	var bp app_v1alpha.BoundPort
	bp.SetPort(8080)
	snap := healthSnapshot{boundPorts: []*app_v1alpha.BoundPort{&bp}}

	reportBoundPortDivergence(ctx, snap)

	out := buf.String()
	assert.Contains(t, out, "8080")
	assert.Contains(t, out, "$PORT")
}

func TestReportBoundPortDivergence_NoneIsQuiet(t *testing.T) {
	ctx, buf := pollerContext(context.Background())
	reportBoundPortDivergence(ctx, healthSnapshot{})
	assert.Empty(t, strings.TrimSpace(buf.String()))
}
