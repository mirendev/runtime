package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
)

func RunnerStatus(ctx *Context, opts struct {
	ConfigPath string `long:"config" description:"Path to runner config" default:"/var/lib/miren/runner/config.yaml"`
	DataPath   string `long:"data-path" description:"Path to runner data" default:"/var/lib/miren/runner"`
}) error {
	// Check if runner is configured
	cfg, err := runnerconfig.Load(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ctx.Printf("Runner:       not configured\n")
			ctx.Printf("  (no config at %s)\n", opts.ConfigPath)
			return fmt.Errorf("runner not configured: %w", err)
		}
		// A config we can't read is not the same as a config that isn't there,
		// and telling an operator "not configured" when the file exists but is
		// unreadable sends them off to re-run `runner join` for nothing.
		ctx.Printf("Runner:       unknown\n")
		ctx.Printf("  (config at %s could not be read)\n", opts.ConfigPath)
		return fmt.Errorf("reading runner config: %w", err)
	}

	ctx.Printf("Runner ID:    %s\n", cfg.RunnerID)
	ctx.Printf("Coordinator:  %s\n", cfg.CoordinatorAddress)

	// Check containerd
	socketPath := filepath.Join(opts.DataPath, "containerd", "containerd.sock")
	ctx.Printf("Containerd:   %s\n", describeContainerd(socketPath))

	// Check etcd connectivity
	if len(cfg.EtcdEndpoints) > 0 {
		ctx.Printf("Etcd:         %s\n", cfg.EtcdEndpoints)
	}

	// Check flannel subnet
	netdbPath := filepath.Join(opts.DataPath, "net.db")
	if _, err := os.Stat(netdbPath); err == nil {
		ctx.Printf("Network DB:   %s\n", netdbPath)
	}

	ctx.Printf("Status:       %s\n", describeRunnerProcess(opts.DataPath, socketPath))
	ctx.Printf("Sandboxes:    %s\n", describeSandboxes(ctx, cfg))

	return nil
}

// describeContainerd reports on the containerd socket. A missing socket means
// containerd isn't running; any other stat failure (permissions, most often)
// means we don't know, and saying "not running" would be a guess.
func describeContainerd(socketPath string) string {
	info, err := os.Stat(socketPath)
	switch {
	case err == nil && info.Mode()&os.ModeSocket != 0:
		return fmt.Sprintf("running (socket %s)", socketPath)
	case err == nil:
		return fmt.Sprintf("unknown (%s exists but is not a socket)", socketPath)
	case errors.Is(err, fs.ErrNotExist):
		return "not running"
	default:
		return fmt.Sprintf("unknown (%s)", err)
	}
}

// describeRunnerProcess reports whether the runner process is alive, preferring
// the pidfile and falling back to the containerd socket as a weak proxy.
func describeRunnerProcess(dataPath, socketPath string) string {
	pidFile := filepath.Join(dataPath, "runner.pid")

	data, err := os.ReadFile(pidFile)
	switch {
	case err == nil:
		return describePid(pidFile, string(data))
	case errors.Is(err, fs.ErrNotExist):
		_, serr := os.Stat(socketPath)
		switch {
		case serr == nil:
			return "running (no pidfile, socket present)"
		case errors.Is(serr, fs.ErrNotExist):
			return "stopped"
		default:
			return fmt.Sprintf("unknown (no pidfile; %s)", serr)
		}
	default:
		return fmt.Sprintf("unknown (%s: %s)", pidFile, err)
	}
}

func describePid(pidFile, contents string) string {
	var pid int
	if _, err := fmt.Sscanf(contents, "%d", &pid); err != nil {
		return fmt.Sprintf("unknown (%s does not contain a pid)", pidFile)
	}

	// Signal(0) is kill(2), where a non-positive pid addresses a process group
	// rather than a process: pid 0 asks about our own group, which would answer
	// yes and report a corrupt pidfile as a live runner.
	if pid <= 0 {
		return fmt.Sprintf("unknown (%s contains %d, which is not a pid)", pidFile, pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Sprintf("unknown (pid %d: %s)", pid, err)
	}

	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return fmt.Sprintf("running (pid %d)", pid)
	case errors.Is(err, syscall.EPERM):
		// The process is there, we just aren't allowed to signal it. Calling
		// this stale would declare a healthy runner dead whenever `runner
		// status` is run as anyone but the user who started it.
		return fmt.Sprintf("running (pid %d, owned by another user)", pid)
	default:
		return fmt.Sprintf("stale pid %d (%s)", pid, err)
	}
}

// describeSandboxes asks the coordinator what it has scheduled to this runner.
// The coordinator derives node scope from the runner certificate and excludes
// stopped/dead sandboxes; no sandbox records are sent to the CLI.
func describeSandboxes(ctx *Context, cfg *runnerconfig.Config) string {
	counts, err := fetchSandboxCounts(ctx, cfg)
	if err != nil {
		return fmt.Sprintf("unknown (%s)", err)
	}
	return counts
}

func formatRunnerSandboxCounts(counts *runner_v1alpha.RunnerRegistrationClientSandboxCountsResults) string {
	line := fmt.Sprintf("%d running", counts.Running())
	if len(counts.Others()) == 0 {
		return line
	}
	parts := make([]string, 0, len(counts.Others()))
	for _, count := range counts.Others() {
		parts = append(parts, fmt.Sprintf("%d %s", count.Count(), count.Status()))
	}
	return fmt.Sprintf("%s (%s)", line, strings.Join(parts, ", "))
}

// sandboxQueryTimeout bounds the whole coordinator round trip. This is a status
// command reached for when something already looks wrong, so a coordinator that
// accepts the connection and then goes quiet should cost a few seconds and an
// "unknown", not a hang.
const sandboxQueryTimeout = 15 * time.Second

func fetchSandboxCounts(ctx *Context, cfg *runnerconfig.Config) (string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, sandboxQueryTimeout)
	defer cancel()

	clientCfg := clientconfig.NewConfig()
	clientCfg.SetCluster("coordinator", &clientconfig.ClusterConfig{
		Hostname:   cfg.CoordinatorAddress,
		CACert:     cfg.CACert,
		ClientCert: cfg.ClientCert,
		ClientKey:  cfg.ClientKey,
	})
	if err := clientCfg.SetActiveCluster("coordinator"); err != nil {
		return "", err
	}

	state, err := clientCfg.State(queryCtx, rpc.WithLogger(ctx.Log))
	if err != nil {
		return "", fmt.Errorf("coordinator unreachable: %w", err)
	}

	client, err := state.Client(rpc.ServiceRunner)
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(queryCtx, client, "SandboxCounts")
	}
	if isMissingRPC(err) {
		client, err := state.Client("entities")
		if err != nil {
			return "", fmt.Errorf("coordinator unreachable: %w", err)
		}
		defer client.Close()
		eac := entityserver_v1alpha.NewEntityAccessClient(client)
		res, err := eac.List(queryCtx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, compute_v1alpha.NewNodeId(cfg.RunnerID).Id()))
		if err != nil {
			return "", fmt.Errorf("coordinator query failed: %w", err)
		}
		running := 0
		others := map[string]int{}
		for _, record := range res.Values() {
			var sb compute_v1alpha.Sandbox
			sb.Decode(record.Entity())
			if compute.SandboxDead(sb.Status) {
				continue
			}
			if sb.Status == compute_v1alpha.RUNNING {
				running++
			} else {
				status := strings.TrimPrefix(string(sb.Status), "status.")
				if status == "" {
					status = "unknown"
				}
				others[status]++
			}
		}
		line := fmt.Sprintf("%d running", running)
		var parts []string
		var names []string
		for name := range others {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%d %s", others[name], name))
		}
		if len(parts) > 0 {
			line += " (" + strings.Join(parts, ", ") + ")"
		}
		return line, nil
	}
	if err != nil {
		return "", fmt.Errorf("coordinator unreachable: %w", err)
	}

	res, err := runner_v1alpha.NewRunnerRegistrationClient(client).SandboxCounts(queryCtx)
	if err != nil {
		return "", fmt.Errorf("coordinator query failed: %w", err)
	}

	return formatRunnerSandboxCounts(res), nil
}
