package commands

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"miren.dev/runtime/api/compute/compute_v1alpha"
)

func TestFormatSandboxCounts(t *testing.T) {
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
			// The bug this command shipped with reported 0 while sandboxes were
			// plainly running, so a non-zero count with dead entries mixed in is
			// the case worth pinning down.
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
			if got := formatSandboxCounts(tt.statuses); got != tt.want {
				t.Errorf("formatSandboxCounts() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSandboxCountsOrdersBreakdownDeterministically(t *testing.T) {
	statuses := []compute_v1alpha.SandboxStatus{
		compute_v1alpha.NOT_READY,
		compute_v1alpha.PENDING,
		compute_v1alpha.NOT_READY,
		compute_v1alpha.PENDING,
	}

	first := formatSandboxCounts(statuses)
	for range 20 {
		if got := formatSandboxCounts(statuses); got != first {
			t.Fatalf("formatSandboxCounts() is unstable: got %q then %q", first, got)
		}
	}
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
