// Package runnerlifecycle adapts pkg/serverlifecycle to the runner daemon:
// how an executor on the runner's host observes the runner process.
package runnerlifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// ServerInfoService is where a runner reports its process identity. Same
// name as the server's, since it describes whichever process answers.
// Mirrors components/distributedrunner.ServerInfoService; that package is
// linux-only.
const ServerInfoService = "dev.miren.runtime/server-info"

// Service is the runner's lifecycle ledger over RPC. Mirrors
// components/distributedrunner.RunnerLifecycleService.
const Service = "dev.miren.runtime/runner-lifecycle"

// ErrNoListenAddress: the runner config does not say where the runner
// listens, so there is nothing to probe. A runner records it on start; one
// that has not predates lifecycle support.
var ErrNoListenAddress = errors.New("runner config has no listen address; the runner must start once on a build that records it")

// Prober asks the runner over its own RPC listener, with the runner's own
// certificate, which process is answering. It is how the executor tells a
// restart took and the new build is ready. The config is re-read on every
// probe: the runner rewrites it on start, and the listen address is the
// part that can change across the restart being verified.
type Prober struct {
	ConfigPath string
	Log        *slog.Logger
	// Timeout bounds one probe. Zero means DefaultProbeTimeout.
	Timeout time.Duration
}

const DefaultProbeTimeout = 5 * time.Second

var _ serverlifecycle.Prober = (*Prober)(nil)

func (p *Prober) Probe(ctx context.Context) (serverlifecycle.Snapshot, error) {
	cfg, err := runnerconfig.Load(p.ConfigPath)
	if err != nil {
		return serverlifecycle.Snapshot{}, err
	}
	if cfg.ListenAddress == "" {
		return serverlifecycle.Snapshot{}, ErrNoListenAddress
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	state, err := ClientState(ctx, cfg, LocalAddress(cfg.ListenAddress), p.Log)
	if err != nil {
		return serverlifecycle.Snapshot{}, err
	}
	defer state.Close()
	client, err := state.Client(ServerInfoService)
	if err != nil {
		if re, ok := errors.AsType[*rpc.ResolveError](err); ok && re.Kind == rpc.ResolveLookupError {
			return serverlifecycle.Snapshot{}, fmt.Errorf("runner does not report its version; it predates lifecycle support: %w", err)
		}
		return serverlifecycle.Snapshot{}, err
	}
	defer client.Close()
	results, err := server_v1alpha.NewServerInfoClient(client).Version(ctx)
	if err != nil {
		return serverlifecycle.Snapshot{}, fmt.Errorf("query runner version: %w", err)
	}
	return serverlifecycle.Snapshot{
		InstanceID:  results.RuntimeInstanceId(),
		Version:     results.Version(),
		Commit:      results.Commit(),
		Ready:       results.Ready(),
		InstallKind: results.InstallKind(),
	}, nil
}

// LocalAddress makes a listen address dialable from the runner's own host:
// a wildcard host ("0.0.0.0", "::", or none) becomes loopback, which the
// runner's certificate covers. Anything else is already a real address.
func LocalAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	switch host {
	case "", "0.0.0.0":
		return net.JoinHostPort("127.0.0.1", port)
	case "::":
		return net.JoinHostPort("::1", port)
	}
	return listen
}

// ClientState dials address with the runner's cluster credentials. The
// runner's certificate is signed by the cluster CA, which is what its own
// listener and the coordinator both verify against.
func ClientState(ctx context.Context, cfg *runnerconfig.Config, address string, log *slog.Logger) (*rpc.State, error) {
	clientCfg := clientconfig.NewConfig()
	clientCfg.SetCluster("runner", &clientconfig.ClusterConfig{
		Hostname:   address,
		CACert:     cfg.CACert,
		ClientCert: cfg.ClientCert,
		ClientKey:  cfg.ClientKey,
	})
	if err := clientCfg.SetActiveCluster("runner"); err != nil {
		return nil, err
	}
	var opts []rpc.StateOption
	if log != nil {
		opts = append(opts, rpc.WithLogger(log))
	}
	return clientCfg.State(ctx, opts...)
}

// CoordinatorVersion asks the runner's coordinator which build it runs, so a
// runner upgraded from its own host lands on the same one.
func CoordinatorVersion(ctx context.Context, cfg *runnerconfig.Config, log *slog.Logger) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := ClientState(ctx, cfg, cfg.CoordinatorAddress, log)
	if err != nil {
		return "", err
	}
	defer state.Close()
	client, err := state.Client(ServerInfoService)
	if err != nil {
		if re, ok := errors.AsType[*rpc.ResolveError](err); ok && re.Kind == rpc.ResolveLookupError {
			return "", fmt.Errorf("coordinator does not report its version; it predates managed upgrades: %w", err)
		}
		return "", err
	}
	defer client.Close()
	results, err := server_v1alpha.NewServerInfoClient(client).Version(ctx)
	if err != nil {
		return "", fmt.Errorf("query coordinator version: %w", err)
	}
	return results.Version(), nil
}
