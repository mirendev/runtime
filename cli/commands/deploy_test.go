package commands

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc"
)

func TestDeployMessageChecksNewerServerNotJustDeploymentTracking(t *testing.T) {
	ctx := context.Background()
	buildClient := rpc.LocalClient(rpc.NewInterface([]rpc.Method{{
		Name: "buildFromTar", InterfaceName: "Builder", Params: []string{"deployment"},
		Handler: func(context.Context, rpc.Call) error { return nil },
	}}, struct{}{}))
	if !buildClient.HasMethodParam(ctx, "buildFromTar", "deployment") {
		t.Fatal("test server must advertise the preexisting deployment tracking capability")
	}

	for _, tc := range []struct {
		name      string
		params    []string
		wantError bool
	}{
		{"pre-message server", []string{"app_name", "app_version_id"}, true},
		{"message-aware server", []string{"app_name", "app_version_id", "message"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			depClient := rpc.LocalClient(rpc.NewInterface([]rpc.Method{{
				Name: "DeployVersion", InterfaceName: "Deployment", Params: tc.params,
				Handler: func(context.Context, rpc.Call) error { return nil },
			}}, struct{}{}))
			err := requireDeploymentMessageSupport(ctx, depClient)
			if (err != nil) != tc.wantError {
				t.Errorf("message support check returned %v, wantError=%v", err, tc.wantError)
			}
		})
	}
}

func TestDeployMessageRequiresDeploymentRecord(t *testing.T) {
	for _, opts := range []deployOpts{
		{Message: "ship it", Ephemeral: "preview"},
		{Message: "ship it", Analyze: true},
	} {
		err := runDeploy(&Context{}, opts, &deploySummary{}, nil)
		if err == nil || !strings.Contains(err.Error(), "--message cannot be used") {
			t.Errorf("opts=%+v: expected flag incompatibility, got %v", opts, err)
		}
	}
}

func TestDeployRejectsOversizedMessageBeforeConnecting(t *testing.T) {
	message := strings.Repeat("界", 342)
	err := runDeploy(&Context{}, deployOpts{Message: message}, &deploySummary{}, nil)
	if err == nil || !strings.Contains(err.Error(), "--message must be at most 1024 bytes (got 1026)") {
		t.Fatalf("expected actionable local message length error, got %v", err)
	}
}

func TestDeploymentHistoryShowsMessageSeparatelyFromCommit(t *testing.T) {
	dep := &deployment_v1alpha.DeploymentInfo{}
	dep.SetStatus("succeeded")
	dep.SetMessage("release to staging\nwith cache reset")
	git := &deployment_v1alpha.GitInfo{}
	git.SetSha("abcdef0123456789")
	git.SetCommitMessage("fix parser bug")
	dep.SetGitInfo(git)

	for _, detailed := range []bool{false, true} {
		headers, rows, _ := buildDeploymentTable([]*deployment_v1alpha.DeploymentInfo{dep}, historyDisplayOpts{detailed: detailed})
		if got := rows[0][len(rows[0])-1]; got != "release to staging" {
			t.Errorf("detailed=%v: message = %q", detailed, got)
		}
		if headers[len(headers)-1] != "MESSAGE" {
			t.Errorf("detailed=%v: last column = %q", detailed, headers[len(headers)-1])
		}
		if detailed && rows[0][len(rows[0])-2] != "fix parser bug" {
			t.Errorf("commit message changed: %q", rows[0][len(rows[0])-2])
		}
	}
	dep = &deployment_v1alpha.DeploymentInfo{}
	_, rows, _ := buildDeploymentTable([]*deployment_v1alpha.DeploymentInfo{dep}, historyDisplayOpts{})
	if rows[0][len(rows[0])-1] != "-" {
		t.Errorf("missing message should display -, got %q", rows[0][len(rows[0])-1])
	}
}

func TestDeploymentHistoryMessageDoesNotEmitTerminalControls(t *testing.T) {
	dep := &deployment_v1alpha.DeploymentInfo{}
	dep.SetMessage("Ship \x1b[2J\tcheckout\ninternal detail")
	for _, detailed := range []bool{false, true} {
		row := buildDeploymentRow(dep, historyDisplayOpts{detailed: detailed})
		got := row[len(row)-1]
		if got != "Ship �[2J�checkout" {
			t.Errorf("detailed=%v: message = %q", detailed, got)
		}
	}
}

func TestBuildPhaseSummary(t *testing.T) {
	duration := 250 * time.Millisecond

	t.Run("direct image names the upstream reference", func(t *testing.T) {
		got := buildPhaseSummary("docker.io/library/nginx:alpine", 0, 0, duration)
		if got.name != "Use image" {
			t.Errorf("name = %q, want %q", got.name, "Use image")
		}
		if got.details != "docker.io/library/nginx:alpine" {
			t.Errorf("details = %q, want normalized image reference", got.details)
		}
		if got.duration != duration {
			t.Errorf("duration = %s, want %s", got.duration, duration)
		}
	})

	t.Run("source build retains build summary", func(t *testing.T) {
		got := buildPhaseSummary("", 3, 0, duration)
		if got.name != "Build & push image" {
			t.Errorf("name = %q, want %q", got.name, "Build & push image")
		}
		if got.details != "3 steps completed" {
			t.Errorf("details = %q, want %q", got.details, "3 steps completed")
		}
	})
}

func TestBuildStepsSummary(t *testing.T) {
	cases := []struct {
		count, cached int
		want          string
	}{
		{0, 0, "cached"},
		{1, 0, "1 step completed"},
		{5, 0, "5 steps completed"},
		{5, 3, "5 steps, 3 cached"},
		{1, 1, "1 step, all cached"},
		{5, 5, "5 steps, all cached"},
	}
	for _, tc := range cases {
		if got := buildStepsSummary(tc.count, tc.cached); got != tc.want {
			t.Errorf("buildStepsSummary(%d, %d) = %q, want %q", tc.count, tc.cached, got, tc.want)
		}
	}
}

func TestEnrichUploadProgress(t *testing.T) {
	const total int64 = 1_000_000

	cases := []struct {
		name         string
		written      int64
		bytesRead    int64
		elapsed      time.Duration
		wantFraction float64
		wantETA      time.Duration
	}{
		{
			name:         "zero total skips entirely",
			written:      100,
			bytesRead:    50,
			elapsed:      5 * time.Second,
			wantFraction: 0,
			wantETA:      0,
		},
		{
			name:         "warmup period suppresses ETA",
			written:      10_000,
			bytesRead:    5_000,
			elapsed:      4 * time.Second,
			wantFraction: 0.01,
			wantETA:      0,
		},
		{
			name:         "steady-state ETA extrapolates from elapsed",
			written:      100_000,
			bytesRead:    50_000,
			elapsed:      10 * time.Second,
			wantFraction: 0.1,
			wantETA:      90 * time.Second,
		},
		{
			name:         "completed upload reports no ETA",
			written:      total,
			bytesRead:    500_000,
			elapsed:      60 * time.Second,
			wantFraction: 1.0,
			wantETA:      0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var written atomic.Int64
			written.Store(tc.written)

			p := upload.Progress{
				BytesRead: tc.bytesRead,
				Duration:  tc.elapsed,
			}
			totalArg := total
			if tc.name == "zero total skips entirely" {
				totalArg = 0
			}
			enrichUploadProgress(&p, &written, int64(totalArg))

			if p.Fraction != tc.wantFraction {
				t.Errorf("Fraction = %v, want %v", p.Fraction, tc.wantFraction)
			}
			if p.ETA != tc.wantETA {
				t.Errorf("ETA = %v, want %v", p.ETA, tc.wantETA)
			}
		})
	}
}
