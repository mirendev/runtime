//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/serverconfig"
)

type runnerBootInputs struct {
	config                  serverconfig.ServerConfig
	resolver                netresolve.Resolver
	secrets                 *secret.Registry
	apiPort                 int
	group                   *errgroup.Group
	bridge                  string
	tempDir                 string
	servicePrefixes         []netip.Prefix
	stopSandboxesOnShutdown bool
}

type runnerBootOutput struct {
	runner *runner.Runner
}

type runnerBoot struct {
	component     *boot.Component
	inputs        runnerBootInputs
	containerd    containerdBootOutput
	log           *slog.Logger
	result        runnerBootOutput
	output        boot.Output[runnerBootOutput]
	started       bool
	cleanupOnStop bool
}

func runnerInputs(options StartOptions, resolver netresolve.Resolver, secrets *secret.Registry, apiPort int) runnerBootInputs {
	return runnerBootInputs{
		config:                  options.Config.Server,
		resolver:                resolver,
		secrets:                 secrets,
		apiPort:                 apiPort,
		group:                   options.Group,
		bridge:                  "rt0",
		tempDir:                 os.TempDir(),
		stopSandboxesOnShutdown: options.Config.Server.GetStopSandboxesOnShutdown(),
		servicePrefixes:         serviceNetworkPrefixes(),
	}
}

func newRunnerBoot(inputs runnerBootInputs, registration boot.Output[registrationBootOutput], identity boot.Output[workloadIdentityBootOutput], containerdOutput boot.Output[containerdBootOutput], coordinator boot.Output[coordinatorBootOutput], entityAccess boot.Output[entityAccessBootOutput], network boot.Output[networkBootOutput], observability boot.Output[observabilityBootOutput]) *runnerBoot {
	b := &runnerBoot{inputs: inputs}
	b.component, b.output = boot.Provide7("runner", registration, identity, containerdOutput, coordinator, entityAccess, network, observability, b.start,
		boot.WithStop(b.stop, runnerComponentStopTimeout),
	)
	return b
}

func (b *runnerBoot) start(ctx context.Context, registration registrationBootOutput, identity workloadIdentityBootOutput, containerdOutput containerdBootOutput, coordinatorOutput coordinatorBootOutput, entityAccess entityAccessBootOutput, network networkBootOutput, observability observabilityBootOutput) (runnerBootOutput, error) {
	config := runner.RunnerConfig{
		Id:            b.inputs.config.GetRunnerID(),
		ListenAddress: b.inputs.config.GetRunnerAddress(),
		Workers:       runner.DefaulWorkers,
		DataPath:      b.inputs.config.GetDataPath(),
		DiskMode:      b.inputs.config.GetDiskMode(),
	}
	var err error
	coordinator := coordinatorOutput.coordinator
	config.Config, err = coordinator.RunnerConfig(config.ListenAddress)
	if err != nil {
		return runnerBootOutput{}, err
	}
	cloudAuth := registration.cloudAuth
	if cloudAuth.Enabled {
		config.CloudAuth = &cloudAuth
	} else {
		config.CloudAuth = &coordinate.CloudAuthConfig{}
	}

	dependencies := runner.RunnerDeps{
		Secrets:         b.inputs.secrets,
		CC:              containerdOutput.client,
		Namespace:       containerdOutput.namespace,
		Bridge:          b.inputs.bridge,
		Tempdir:         b.inputs.tempDir,
		Subnet:          network.subnet,
		NetServ:         entityAccess.netService,
		LogsMaintainer:  observability.logsMaintainer,
		LogWriter:       observability.logWriter,
		StatusMon:       observability.statusMonitor,
		IPv4Routable:    network.ipv4Routable,
		ServicePrefixes: b.inputs.servicePrefixes,
		DisableLocalNet: false,
		Resolver:        b.inputs.resolver,
		SandboxMetrics:  observability.sandboxMetrics,
		MetricsWriter:   observability.metricsWriter,
		IsCoordinator:   true,
		ApiAddress:      net.JoinHostPort(network.routerAddress.String(), strconv.Itoa(b.inputs.apiPort)),
		CACert:          coordinator.CACertificate(),
	}
	if issuer := identity.issuer; issuer != nil {
		dependencies.WorkloadIssuer = issuer
	}

	b.result.runner, err = runner.NewRunner(observability.log, dependencies, config)
	if err != nil {
		return runnerBootOutput{}, err
	}
	if err := b.result.runner.Start(ctx, b.inputs.group); err != nil {
		return runnerBootOutput{}, err
	}
	b.containerd = containerdOutput
	b.log = observability.log
	b.started = true
	return b.result, nil
}

func (b *runnerBoot) stop(ctx context.Context) error {
	if b.result.runner == nil {
		return nil
	}
	var errs []error
	if err := b.result.runner.Close(); err != nil {
		errs = append(errs, err)
	}
	if b.cleanupOnStop && b.started {
		if client := b.containerd.client; client != nil {
			if err := stopAllSandboxContainers(ctx, b.log, client); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (b *runnerBoot) enableShutdownCleanup() {
	b.cleanupOnStop = b.inputs.stopSandboxesOnShutdown
}

func stopAllSandboxContainers(ctx context.Context, log *slog.Logger, client *containerd.Client) error {
	log.Info("stopping all sandbox containers via containerd")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, containerdNamespace)

	containers, err := client.Containers(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	stopped := 0
	for _, container := range containers {
		if err := ctx.Err(); err != nil {
			log.Warn("sandbox shutdown interrupted", "stopped", stopped, "total", len(containers), "error", err)
			return err
		}
		task, err := container.Task(ctx, nil)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				log.Warn("sandbox shutdown interrupted", "stopped", stopped, "total", len(containers), "error", ctxErr)
				return ctxErr
			}
			continue
		}
		log.Info("stopping container", "container", container.ID())
		exitCh, waitErr := task.Wait(ctx)
		if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
			log.Debug("failed to send SIGTERM to task", "container", container.ID(), "error", err)
		} else {
			log.Debug("sent SIGTERM to task", "container", container.ID())
		}

		exited := false
		if waitErr == nil {
			grace := time.NewTimer(10 * time.Second)
			select {
			case <-ctx.Done():
				grace.Stop()
				log.Warn("sandbox shutdown interrupted", "stopped", stopped, "total", len(containers), "error", ctx.Err())
				return ctx.Err()
			case <-exitCh:
				grace.Stop()
				exited = true
			case <-grace.C:
				log.Debug("sandbox did not exit during grace period", "container", container.ID())
			}
		}
		var deleteOpts []containerd.ProcessDeleteOpts
		if !exited {
			deleteOpts = append(deleteOpts, containerd.WithProcessKill)
		}
		if _, err := task.Delete(ctx, deleteOpts...); err != nil {
			log.Debug("failed to delete task", "container", container.ID(), "error", err)
		} else {
			stopped++
		}
		if err := ctx.Err(); err != nil {
			log.Warn("sandbox shutdown interrupted", "stopped", stopped, "total", len(containers), "error", err)
			return err
		}
	}
	log.Info("stopped sandbox containers", "stopped", stopped, "total", len(containers))
	return nil
}
