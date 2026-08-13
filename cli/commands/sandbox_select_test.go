package commands

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

func candidateIDs(candidates []sandboxCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID.String())
	}
	return ids
}

func TestNarrowSandboxCandidates(t *testing.T) {
	const (
		active = entity.Id("app_version/v2")
		old    = entity.Id("app_version/v1")
	)

	tests := []struct {
		name          string
		candidates    []sandboxCandidate
		activeVersion entity.Id
		service       string
		appHasWeb     bool
		want          []string
	}{
		{
			name: "drops the previous version when the active one is running",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "web", Version: old},
				{ID: "sandbox/b", Service: "web", Version: active},
			},
			activeVersion: active,
			appHasWeb:     true,
			want:          []string{"sandbox/b"},
		},
		{
			// Mid-deploy the new pool may have nothing up yet. Landing in the
			// old version beats refusing to exec.
			name: "keeps the previous version when the active one has nothing running",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "web", Version: old},
				{ID: "sandbox/b", Service: "web", Version: old},
			},
			activeVersion: active,
			appHasWeb:     true,
			want:          []string{"sandbox/a", "sandbox/b"},
		},
		{
			name: "an app with no active version keeps everything",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "web", Version: old},
				{ID: "sandbox/b", Service: "web", Version: active},
			},
			appHasWeb: true,
			want:      []string{"sandbox/a", "sandbox/b"},
		},
		{
			name: "picks web over the app's other services",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "worker", Version: active},
				{ID: "sandbox/b", Service: "web", Version: active},
				{ID: "sandbox/c", Service: "cron", Version: active},
			},
			activeVersion: active,
			appHasWeb:     true,
			want:          []string{"sandbox/b"},
		},
		{
			name: "an app without a web service keeps its other services",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "worker", Version: active},
				{ID: "sandbox/b", Service: "cron", Version: active},
			},
			activeVersion: active,
			want:          []string{"sandbox/a", "sandbox/b"},
		},
		{
			// --service is a hard filter applied against the pools, so by the
			// time we get here the web preference must not narrow further.
			name: "an explicit service disables the web preference",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "worker", Version: active},
				{ID: "sandbox/b", Service: "worker", Version: active},
			},
			activeVersion: active,
			service:       "worker",
			want:          []string{"sandbox/a", "sandbox/b"},
		},
		{
			// The case that matters most. Web binds a port to go ready and
			// workers don't, so a rolling deploy routinely has worker on the
			// new version while web is still on the old one. Preferring the
			// version first would hand over a worker, silently, since only one
			// candidate survives and the notice stays quiet at one.
			name: "a rolling deploy gives you old web, never a new worker",
			candidates: []sandboxCandidate{
				{ID: "sandbox/a", Service: "web", Version: old},
				{ID: "sandbox/b", Service: "worker", Version: active},
			},
			activeVersion: active,
			appHasWeb:     true,
			want:          []string{"sandbox/a"},
		},
		{
			name:          "no candidates stays empty",
			candidates:    nil,
			activeVersion: active,
			want:          []string{},
		},
		{
			name: "result is sorted so only the caller's pick is random",
			candidates: []sandboxCandidate{
				{ID: "sandbox/c", Service: "web", Version: active},
				{ID: "sandbox/a", Service: "web", Version: active},
				{ID: "sandbox/b", Service: "web", Version: active},
			},
			activeVersion: active,
			appHasWeb:     true,
			want:          []string{"sandbox/a", "sandbox/b", "sandbox/c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := narrowSandboxCandidates(tt.candidates, tt.activeVersion, tt.service, tt.appHasWeb)
			require.False(t, got.MissingDefault)
			require.Equal(t, tt.want, candidateIDs(got.Candidates))
		})
	}
}

// The input is a decoded snapshot the caller may still be holding; narrowing
// must not reorder it underneath them.
func TestNarrowSandboxCandidatesLeavesInputAlone(t *testing.T) {
	candidates := []sandboxCandidate{
		{ID: "sandbox/c", Service: "web"},
		{ID: "sandbox/a", Service: "web"},
	}

	narrowSandboxCandidates(candidates, "", "", true)

	require.Equal(t, []string{"sandbox/c", "sandbox/a"}, candidateIDs(candidates))
}

// The substitution phinze caught in review: exec must not answer "your web
// service has nothing up" by handing over a worker.
func TestNarrowSandboxCandidatesRefusesToSubstituteAService(t *testing.T) {
	const active = entity.Id("app_version/v2")

	candidates := []sandboxCandidate{
		{ID: "sandbox/a", Service: "worker", Version: active},
		{ID: "sandbox/b", Service: "cron", Version: active},
	}

	got := narrowSandboxCandidates(candidates, active, "", true)

	require.True(t, got.MissingDefault)
	require.Empty(t, got.Candidates)
	require.Equal(t, []string{"cron", "worker"}, got.Running)
}

// A failed deploy leaves ActiveVersion pointing at a version that never came
// up while the old instances keep serving. Refusing those would lock you out of
// a shell exactly when a deploy has just broken, so version stays a preference.
func TestNarrowSandboxCandidatesStillServesAFailedDeploy(t *testing.T) {
	const (
		active = entity.Id("app_version/v2")
		old    = entity.Id("app_version/v1")
	)

	candidates := []sandboxCandidate{
		{ID: "sandbox/a", Service: "web", Version: old},
	}

	got := narrowSandboxCandidates(candidates, active, "", true)

	require.False(t, got.MissingDefault)
	require.Equal(t, []string{"sandbox/a"}, candidateIDs(got.Candidates))
}

func TestNoDefaultServiceError(t *testing.T) {
	var buf bytes.Buffer
	noDefaultServiceError("myapp", []string{"worker", "cron"}).(*ui.Diagnostic).WriteForTerminal(&buf)

	out := buf.String()
	require.Contains(t, out, `no running web instance for app "myapp"`)
	require.Contains(t, out, "worker, cron")
	require.Contains(t, out, "miren sandbox exec -a myapp --service worker")
}

// With nothing else running there is no alternative service to offer, and the
// message must not invent one.
func TestNoDefaultServiceErrorWithNothingElseRunning(t *testing.T) {
	var buf bytes.Buffer
	noDefaultServiceError("myapp", nil).(*ui.Diagnostic).WriteForTerminal(&buf)

	out := buf.String()
	require.Contains(t, out, `no running web instance for app "myapp"`)
	require.NotContains(t, out, "Still running")
	require.NotContains(t, out, "exec -a myapp --service")
}

func TestSandboxExecOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    sandboxExecOptions
		wantErr string
	}{
		{name: "id alone", opts: sandboxExecOptions{Id: "sandbox/abc"}},
		{name: "app alone", opts: sandboxExecOptions{App: "myapp"}},
		{name: "app with service", opts: sandboxExecOptions{App: "myapp", Service: "worker"}},
		{name: "neither, id comes from a positional", opts: sandboxExecOptions{}},
		{
			name:    "app and id both name a sandbox",
			opts:    sandboxExecOptions{App: "myapp", Id: "sandbox/abc"},
			wantErr: "--app and --id",
		},
		{
			name:    "service without app has nothing to select among",
			opts:    sandboxExecOptions{Id: "sandbox/abc", Service: "worker"},
			wantErr: "it needs --app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestExecChoiceNotice(t *testing.T) {
	chosen := sandboxCandidate{ID: "sandbox/myapp-web-abc123", Brief: "abc", Service: "web"}

	t.Run("silent when there was no choice", func(t *testing.T) {
		require.Empty(t, execChoiceNotice(chosen, 1))
	})

	t.Run("names the sandbox when there was", func(t *testing.T) {
		require.Equal(t, "abc (web), picked from 3 running", execChoiceNotice(chosen, 3))
	})
}

// A failure against a sandbox we picked has to name the app the caller asked
// for, since the sandbox id is one they never typed.
func TestChosenSandboxExecError(t *testing.T) {
	cause := errors.New("failed to get entity sandbox/myapp-web-abc123: not found")
	err := chosenSandboxExecError("myapp", sandboxCandidate{Brief: "abc", Service: "web"}, cause)

	require.ErrorContains(t, err, `"myapp"`)
	require.ErrorIs(t, err, cause)

	var buf bytes.Buffer
	err.(*ui.Diagnostic).WriteForTerminal(&buf)
	require.Contains(t, buf.String(), "abc (web)")
	require.Contains(t, buf.String(), cause.Error())
}

func TestSandboxExecFlagParsing(t *testing.T) {
	t.Run("app takes every positional as the command", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"-a", "myapp", "--", "ls", "-la", "/app"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Equal(t, "myapp", opts.App)
		require.Empty(t, opts.Id)
		require.Equal(t, []string{"ls", "-la", "/app"}, opts.Args)
	})

	t.Run("without app the first positional is the id", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"sb_abc123", "--", "echo", "hi"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Empty(t, opts.App)
		require.Equal(t, []string{"sb_abc123", "echo", "hi"}, opts.Args)
	})

	t.Run("service is long-only", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"-a", "myapp", "--service", "worker"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Equal(t, "worker", opts.Service)
	})
}
