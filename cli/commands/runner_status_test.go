package commands

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
	runnerserver "miren.dev/runtime/servers/runner"
)

func TestRunnerStatusSandboxCounts(t *testing.T) {
	tests := []struct {
		name     string
		statuses []compute_v1alpha.SandboxStatus
		want     string
	}{
		{
			name:     "no sandboxes",
			statuses: nil,
			want:     "0 running",
		},
		{
			name: "only running",
			statuses: []compute_v1alpha.SandboxStatus{
				compute_v1alpha.RUNNING,
				compute_v1alpha.RUNNING,
				compute_v1alpha.RUNNING,
			},
			want: "3 running",
		},
		{
			name: "dead and stopped are excluded",
			statuses: []compute_v1alpha.SandboxStatus{
				compute_v1alpha.RUNNING,
				compute_v1alpha.DEAD,
				compute_v1alpha.RUNNING,
				compute_v1alpha.STOPPED,
				compute_v1alpha.DEAD,
			},
			want: "2 running",
		},
		{
			name: "non-running sandboxes are broken out",
			statuses: []compute_v1alpha.SandboxStatus{
				compute_v1alpha.RUNNING,
				compute_v1alpha.PENDING,
				compute_v1alpha.NOT_READY,
				compute_v1alpha.PENDING,
			},
			want: "1 running (1 not_ready, 2 pending)",
		},
		{
			name:     "empty status is not silently counted as running",
			statuses: []compute_v1alpha.SandboxStatus{compute_v1alpha.RUNNING, ""},
			want:     "1 running (1 unknown)",
		},
		{
			name: "everything dead",
			statuses: []compute_v1alpha.SandboxStatus{
				compute_v1alpha.DEAD,
				compute_v1alpha.STOPPED,
			},
			want: "0 running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := runnerStatusCoordinator(t, tt.statuses, "full-runner-id")
			// A stale/local runner ID must not override certificate-derived scope.
			cfg.RunnerID = "another-node"
			path := filepath.Join(t.TempDir(), "runner.yaml")
			require.NoError(t, cfg.Save(path))
			var stdout bytes.Buffer
			ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: &stdout, Stderr: io.Discard}
			require.NoError(t, RunnerStatus(ctx, struct {
				ConfigPath string `long:"config" description:"Path to runner config" default:"/var/lib/miren/runner/config.yaml"`
				DataPath   string `long:"data-path" description:"Path to runner data" default:"/var/lib/miren/runner"`
			}{ConfigPath: path, DataPath: t.TempDir()}))
			require.Contains(t, stdout.String(), "Sandboxes:    "+tt.want+"\n")
		})
	}
}

func TestRunnerStatusSandboxCountsDenialIsUnknown(t *testing.T) {
	cfg := runnerStatusCoordinator(t, []compute_v1alpha.SandboxStatus{compute_v1alpha.RUNNING}, "full-run")
	// A valid certificate for an unregistered runner must not be rescued by
	// using the configured node ID or an EntityAccess fallback.
	ctx := &Context{Context: t.Context(), Log: slog.Default()}
	require.Contains(t, describeSandboxes(ctx, cfg), "unknown (coordinator query failed:")
}

func TestRunnerStatusSandboxCountsLegacy(t *testing.T) {
	for _, mode := range []string{"missing-service", "old-service"} {
		t.Run(mode, func(t *testing.T) {
			cfg := runnerStatusCoordinator(t, []compute_v1alpha.SandboxStatus{
				compute_v1alpha.RUNNING, compute_v1alpha.PENDING, compute_v1alpha.PENDING,
				compute_v1alpha.NOT_READY, compute_v1alpha.DEAD, compute_v1alpha.STOPPED,
			}, "full-runner-id", mode)
			ctx := &Context{Context: t.Context(), Log: slog.Default()}
			require.Equal(t, "1 running (1 not_ready, 2 pending)", describeSandboxes(ctx, cfg))
		})
	}
}

func runnerStatusCoordinator(t *testing.T, statuses []compute_v1alpha.SandboxStatus, certRunnerID string, mode ...string) *runnerconfig.Config {
	t.Helper()
	const runnerID = "full-runner-id"
	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", ValidFor: time.Hour})
	require.NoError(t, err)
	serverCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "coordinator", IPs: []net.IP{net.ParseIP("127.0.0.1")}, ValidFor: time.Hour,
	})
	require.NoError(t, err)
	clientCert, err := ca.IssueCertificate(caauth.Options{CommonName: "runner-" + certRunnerID, ValidFor: time.Hour})
	require.NoError(t, err)
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("127.0.0.1:0"),
		rpc.WithCertPEMs(serverCert.CertPEM, serverCert.KeyPEM),
		rpc.WithCertificateVerification(ca.GetCACertificate()), rpc.WithRequireClientCerts,
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	for _, id := range []string{runnerID, "another-node"} {
		_, err := es.EAC.Create(t.Context(), entity.New(
			entity.DBId, compute_v1alpha.NewNodeId(id).Id(),
			(&compute_v1alpha.Node{RunnerId: id}).Encode,
		).Attrs())
		require.NoError(t, err)
	}
	for i, status := range append(statuses, compute_v1alpha.RUNNING) {
		node := runnerID
		if i == len(statuses) {
			node = "another-node"
		}
		_, err := es.EAC.Create(t.Context(), entity.New(
			entity.DBId, entity.Id(fmt.Sprintf("sandbox/status-%d", i)),
			(&compute_v1alpha.Sandbox{Status: status}).Encode,
			(&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{
				Kind: compute_v1alpha.KindSandbox, Node: compute_v1alpha.NewNodeId(node).Id(),
			}}).Encode,
		).Attrs())
		require.NoError(t, err)
	}
	iface := runner_v1alpha.AdaptRunnerRegistration(
		runnerserver.NewRegistrationServer(runnerserver.RegistrationServerConfig{
			Log: slog.Default(), EAC: es.EAC, Authority: ca,
		}))
	if len(mode) > 0 {
		server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(es.Server))
		if mode[0] == "old-service" {
			var methods []rpc.Method
			for _, method := range iface.Methods() {
				if method.Name != "SandboxCounts" {
					methods = append(methods, method)
				}
			}
			server.Server().ExposeValue(rpc.ServiceRunner, rpc.NewInterface(methods, nil))
		}
	} else {
		// Modern servers need only the runner-scoped capability.
		server.Server().ExposeValue(rpc.ServiceRunner, iface)
	}
	return &runnerconfig.Config{RunnerID: runnerID, CoordinatorAddress: server.ListenAddr(),
		CACert: string(ca.GetCACertificate()), ClientCert: string(clientCert.CertPEM), ClientKey: string(clientCert.KeyPEM)}
}

func TestDescribeContainerd(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing socket", func(t *testing.T) {
		got := describeContainerd(filepath.Join(dir, "absent.sock"))
		if got != "not running" {
			t.Errorf("got %q, want %q", got, "not running")
		}
	})

	t.Run("real socket", func(t *testing.T) {
		sock := filepath.Join(dir, "containerd.sock")
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("failed to create unix socket: %v", err)
		}
		defer l.Close()

		got := describeContainerd(sock)
		if !strings.HasPrefix(got, "running (socket ") {
			t.Errorf("got %q, want it to report running", got)
		}
	})

	t.Run("path exists but is not a socket", func(t *testing.T) {
		// A regular file here means something is wrong in a way that "not
		// running" would paper over.
		notASocket := filepath.Join(dir, "regular-file")
		if err := os.WriteFile(notASocket, []byte("hi"), 0600); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		got := describeContainerd(notASocket)
		if !strings.HasPrefix(got, "unknown (") {
			t.Errorf("got %q, want an unknown verdict", got)
		}
	})
}

func TestDescribeRunnerProcess(t *testing.T) {
	t.Run("no pidfile and no socket", func(t *testing.T) {
		dir := t.TempDir()
		got := describeRunnerProcess(dir, filepath.Join(dir, "absent.sock"))
		if got != "stopped" {
			t.Errorf("got %q, want %q", got, "stopped")
		}
	})

	t.Run("no pidfile but socket present", func(t *testing.T) {
		dir := t.TempDir()
		sock := filepath.Join(dir, "containerd.sock")
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("failed to create unix socket: %v", err)
		}
		defer l.Close()

		got := describeRunnerProcess(dir, sock)
		if got != "running (no pidfile, socket present)" {
			t.Errorf("got %q, want the socket-proxy verdict", got)
		}
	})

	t.Run("pidfile names this process", func(t *testing.T) {
		dir := t.TempDir()
		pid := os.Getpid()
		writePidFile(t, dir, strconv.Itoa(pid))

		want := "running (pid " + strconv.Itoa(pid) + ")"
		if got := describeRunnerProcess(dir, filepath.Join(dir, "absent.sock")); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("unparseable pidfile reports unknown rather than stopped", func(t *testing.T) {
		dir := t.TempDir()
		writePidFile(t, dir, "not-a-pid\n")

		got := describeRunnerProcess(dir, filepath.Join(dir, "absent.sock"))
		if !strings.HasPrefix(got, "unknown (") {
			t.Errorf("got %q, want an unknown verdict", got)
		}
		if strings.Contains(got, "stopped") || strings.Contains(got, "running") {
			t.Errorf("got %q, want it to avoid claiming a state it can't determine", got)
		}
	})

	t.Run("non-positive pids are rejected before signaling", func(t *testing.T) {
		// Signal(0) on a non-positive pid addresses a process group, so pid 0
		// would report our own group as a live runner.
		for _, contents := range []string{"0", "-1", "-4242"} {
			dir := t.TempDir()
			writePidFile(t, dir, contents)

			got := describeRunnerProcess(dir, filepath.Join(dir, "absent.sock"))
			if !strings.HasPrefix(got, "unknown (") {
				t.Errorf("pidfile %q: got %q, want an unknown verdict", contents, got)
			}
			if strings.Contains(got, "running") {
				t.Errorf("pidfile %q: got %q, want it to avoid claiming the runner is up", contents, got)
			}
		}
	})

	t.Run("pidfile names a dead process", func(t *testing.T) {
		dir := t.TempDir()

		cmd := exec.Command("true")
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to run throwaway process: %v", err)
		}
		writePidFile(t, dir, strconv.Itoa(cmd.Process.Pid))

		got := describeRunnerProcess(dir, filepath.Join(dir, "absent.sock"))
		if !strings.HasPrefix(got, "stale pid ") {
			t.Errorf("got %q, want a stale verdict", got)
		}
	})
}

func writePidFile(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "runner.pid"), []byte(contents), 0600); err != nil {
		t.Fatalf("failed to write pidfile: %v", err)
	}
}
