package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
)

func verdictEnv(host string, connErr error, tcp, udp probeOutcome) *doctorEnv {
	return &doctorEnv{
		cfg:          &clientconfig.Config{},
		cluster:      &clientconfig.ClusterConfig{Hostname: host},
		clusterName:  "prod",
		clusterCount: 1,
		connErr:      connErr,
		tcp:          probe{Outcome: tcp},
		udp:          probe{Outcome: udp},
	}
}

const remoteHost = "cluster.example.com:8443"

func unreachableErr(host string) error {
	return rpc.NewResolveUnreachableError("entities", host, 2*time.Second, errors.New("timeout"))
}

// The verdict is the cross product of the RPC result and the two probes. This
// is the table the old doctor gathered evidence for and then ignored.
func TestServerVerdictTable(t *testing.T) {
	tests := []struct {
		name        string
		connErr     error
		tcp         probeOutcome
		udp         probeOutcome
		wantStatus  checkStatus
		wantSummary string
	}{
		{
			name:        "connected",
			connErr:     nil,
			wantStatus:  checkOK,
			wantSummary: "connected to",
		},
		{
			// The host answered and refused: the machine is up, the server
			// isn't. This is the case the old code called "may be starting up".
			name:        "tcp refused means nothing is running",
			connErr:     unreachableErr(remoteHost),
			tcp:         probeRefused,
			udp:         probeSilent,
			wantStatus:  checkFail,
			wantSummary: "not running",
		},
		{
			name:        "tcp open but udp silent means the API port isn't answering",
			connErr:     unreachableErr(remoteHost),
			tcp:         probeOpen,
			udp:         probeSilent,
			wantStatus:  checkFail,
			wantSummary: "API port not answering",
		},
		{
			name:        "silence on both means the host is unreachable",
			connErr:     unreachableErr(remoteHost),
			tcp:         probeSilent,
			udp:         probeSilent,
			wantStatus:  checkFail,
			wantSummary: "unreachable",
		},
		{
			name:        "went silent mid-request",
			connErr:     rpc.NewResolveWentSilentError("entities", remoteHost, 30*time.Second, errors.New("timeout")),
			tcp:         probeOpen,
			udp:         probeOpen,
			wantStatus:  checkFail,
			wantSummary: "stopped responding",
		},
		{
			name:        "accepted but never answered",
			connErr:     rpc.NewResolveNoAnswerError("entities", remoteHost, 8*time.Second, errors.New("deadline")),
			tcp:         probeOpen,
			udp:         probeOpen,
			wantStatus:  checkFail,
			wantSummary: "not answering",
		},
		{
			// The server replied, so it's healthy; it just hasn't registered
			// the capability. That's a booting server, which is a warning
			// rather than a failure because it resolves itself.
			name:        "answered without the capability is a warning",
			connErr:     rpc.NewResolveLookupError("entities", remoteHost, "unknown object: entities"),
			tcp:         probeOpen,
			udp:         probeOpen,
			wantStatus:  checkWarn,
			wantSummary: "not ready",
		},
		{
			name:        "credentials refused",
			connErr:     rpc.NewResolveStatusError("entities", remoteHost, 401),
			tcp:         probeOpen,
			udp:         probeOpen,
			wantStatus:  checkFail,
			wantSummary: "access denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkServer(verdictEnv(remoteHost, tt.connErr, tt.tcp, tt.udp))

			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", got.Status, tt.wantStatus)
			}
			if !strings.Contains(got.Summary, tt.wantSummary) {
				t.Errorf("Summary = %q, want it to contain %q", got.Summary, tt.wantSummary)
			}
			if tt.wantStatus != checkOK && got.Problem == nil {
				t.Error("a failing check must explain itself")
			}
			if tt.wantStatus == checkOK && got.Problem != nil {
				t.Error("a passing check must not report a problem")
			}
		})
	}
}

// The original bug in doctor: a stopped local server was reported as one that
// might be starting up, contradicting its own probe output.
func TestStoppedServerIsNotReportedAsStarting(t *testing.T) {
	got := checkServer(verdictEnv("localhost:8443", unreachableErr("localhost:8443"), probeRefused, probeSilent))

	if got.Problem == nil {
		t.Fatal("expected a problem for a stopped server")
	}
	text := got.Problem.Summary + " " + got.Problem.Detail
	if strings.Contains(strings.ToLower(text), "starting up") {
		t.Errorf("stopped server still blamed on startup: %q", text)
	}
	if !strings.Contains(strings.ToLower(text), "isn't running") {
		t.Errorf("stopped server not identified: %q", text)
	}
}

// A firewall is only a plausible explanation when UDP is blocked, and it should
// never be volunteered for a server that simply isn't running.
func TestFirewallOnlyBlamedWhenUDPIsBlocked(t *testing.T) {
	notRunning := checkServer(verdictEnv("localhost:8443", unreachableErr("localhost:8443"), probeRefused, probeSilent))
	text := notRunning.Problem.Summary + " " + notRunning.Problem.Detail
	if strings.Contains(strings.ToLower(text), "firewall") {
		t.Errorf("firewall blamed for a server that isn't running: %q", text)
	}

	blocked := checkServer(verdictEnv(remoteHost, unreachableErr(remoteHost), probeOpen, probeSilent))
	blockedText := blocked.Problem.Summary + " " + blocked.Problem.Detail
	if !strings.Contains(strings.ToLower(blockedText), "firewall") {
		t.Errorf("UDP-blocked case does not mention a firewall: %q", blockedText)
	}
}

// Advice has to be for the machine the user is on. The old doctor told everyone
// to run systemctl, including macOS users, where `server install` refuses to
// run at all and points at the container instead.
func TestRemoteClusterNeverSuggestsLocalServiceCommands(t *testing.T) {
	got := checkServer(verdictEnv(remoteHost, unreachableErr(remoteHost), probeRefused, probeSilent))

	for _, a := range got.Problem.Actions {
		if strings.Contains(a.Command, "systemctl") || strings.Contains(a.Command, "journalctl") {
			t.Errorf("remote cluster suggested a local service command: %q", a.Command)
		}
	}
}

// Checks that can't run are skipped, not failed: a missing cluster is the
// configuration check's problem to report, and duplicating it as a server
// failure would double-count one issue.
func TestServerCheckSkipsWithoutACluster(t *testing.T) {
	got := checkServer(&doctorEnv{})

	if got.Status != checkSkip {
		t.Errorf("Status = %v, want skip", got.Status)
	}
	if got.Problem != nil {
		t.Error("a skipped check must not report a problem")
	}
}

// Every problem must name a fix. A diagnostic that reports something the reader
// can't act on is how a doctor command teaches people to ignore it.
//
// The list below covers every verdict path deliberately, including the
// unclassified fallbacks and the odd probe combinations. An earlier version
// only exercised the tidy cases and so missed three paths that produced an
// error with nothing to do about it.
func TestEveryProblemOffersAnAction(t *testing.T) {
	envs := map[string]*doctorEnv{
		"not running":       verdictEnv(remoteHost, unreachableErr(remoteHost), probeRefused, probeSilent),
		"api not answering": verdictEnv(remoteHost, unreachableErr(remoteHost), probeOpen, probeSilent),
		"unreachable":       verdictEnv(remoteHost, unreachableErr(remoteHost), probeSilent, probeSilent),
		"capability missing": verdictEnv(remoteHost,
			rpc.NewResolveLookupError("entities", remoteHost, "unknown"), probeOpen, probeOpen),
		"unauthorized": verdictEnv(remoteHost,
			rpc.NewResolveStatusError("entities", remoteHost, 401), probeOpen, probeOpen),

		// These call serverLogActions, which used to return nil for anything
		// that wasn't a local Linux host, leaving the verdict with no next step.
		"went silent": verdictEnv(remoteHost,
			rpc.NewResolveWentSilentError("entities", remoteHost, time.Second, errors.New("timeout")), probeOpen, probeOpen),
		"no answer": verdictEnv(remoteHost,
			rpc.NewResolveNoAnswerError("entities", remoteHost, time.Second, errors.New("deadline")), probeOpen, probeOpen),

		// Unclassified failures still have to offer something.
		"non-resolve error":   verdictEnv(remoteHost, errors.New("something odd"), probeOpen, probeOpen),
		"unexpected status":   verdictEnv(remoteHost, rpc.NewResolveStatusError("entities", remoteHost, 503), probeOpen, probeOpen),
		"decode failure":      verdictEnv(remoteHost, rpc.NewResolveDecodeError("entities", remoteHost, errors.New("bad")), probeOpen, probeOpen),
		"probe disagreement":  verdictEnv(remoteHost, unreachableErr(remoteHost), probeOpen, probeRefused),
		"probe failed to run": verdictEnv(remoteHost, unreachableErr(remoteHost), probeFailed, probeFailed),
	}

	for name, env := range envs {
		t.Run(name, func(t *testing.T) {
			got := checkServer(env)
			if got.Problem == nil {
				t.Fatal("expected a problem")
			}
			if len(got.Problem.Actions) == 0 && len(got.Problem.Causes) == 0 {
				t.Errorf("problem %q offers neither an action nor a cause", got.Problem.Summary)
			}
		})
	}
}

// Action.Command is meant to be pasted into a shell. Prose dressed up as a
// command is worse than a note, because it looks runnable and isn't.
func TestActionCommandsLookRunnable(t *testing.T) {
	envs := []*doctorEnv{
		verdictEnv("localhost:8443", unreachableErr("localhost:8443"), probeOpen, probeSilent),
		verdictEnv(remoteHost, unreachableErr(remoteHost), probeOpen, probeSilent),
		verdictEnv(remoteHost, unreachableErr(remoteHost), probeRefused, probeSilent),
	}

	for _, env := range envs {
		for _, a := range checkServer(env).Problem.Actions {
			if strings.HasPrefix(a.Command, "check ") || strings.Contains(a.Command, " that ") {
				t.Errorf("Action.Command reads as prose, not a command: %q", a.Command)
			}
		}
	}
}

func TestConfigurationCheck(t *testing.T) {
	t.Run("no clusters", func(t *testing.T) {
		got := checkConfiguration(&doctorEnv{cfg: &clientconfig.Config{}})
		if got.Status != checkFail {
			t.Errorf("Status = %v, want fail", got.Status)
		}
		if got.Problem == nil || len(got.Problem.Actions) == 0 {
			t.Error("expected actionable advice for an empty configuration")
		}
	})

	t.Run("configured", func(t *testing.T) {
		got := checkConfiguration(verdictEnv(remoteHost, nil, probeOpen, probeOpen))
		if got.Status != checkOK {
			t.Errorf("Status = %v, want ok", got.Status)
		}
		if !strings.Contains(got.Summary, "prod") {
			t.Errorf("Summary = %q, want it to name the active cluster", got.Summary)
		}
	})
}

// The footer has to agree with the exit code: a warning is not a problem.
func TestDoctorFooter(t *testing.T) {
	tests := []struct {
		fails, warns int
		want         string
	}{
		{0, 0, "Everything looks good."},
		{0, 1, "1 warning, nothing broken."},
		{0, 2, "2 warnings, nothing broken."},
		{1, 0, "1 problem found."},
		{2, 0, "2 problems found."},
		{1, 1, "1 problem found, 1 warning."},
	}

	for _, tt := range tests {
		if got := doctorFooter(tt.fails, tt.warns); got != tt.want {
			t.Errorf("doctorFooter(%d, %d) = %q, want %q", tt.fails, tt.warns, got, tt.want)
		}
	}
}
