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
	"miren.dev/runtime/pkg/readiness"
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
	component     *readiness.Component
	inputs        runnerBootInputs
	registration  *registrationBoot
	identity      *workloadIdentityBoot
	containerd    *containerdBoot
	coordinator   *coordinatorBoot
	entityAccess  *entityAccessBoot
	network       *networkBoot
	observability *observabilityBoot
	result        runnerBootOutput
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
		servicePrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.10.0.0/16"),
			netip.MustParsePrefix("fd47:cafe:d00d::/64"),
		},
	}
}

func newRunnerBoot(inputs runnerBootInputs, registration *registrationBoot, identity *workloadIdentityBoot, containerd *containerdBoot, coordinator *coordinatorBoot, entityAccess *entityAccessBoot, network *networkBoot, observability *observabilityBoot) *runnerBoot {
	b := &runnerBoot{
		inputs:        inputs,
		registration:  registration,
		identity:      identity,
		containerd:    containerd,
		coordinator:   coordinator,
		entityAccess:  entityAccess,
		network:       network,
		observability: observability,
	}
	b.component = readiness.NewComponent("runner", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(registration.component),
			readiness.ReadyDep(identity.component),
			readiness.ReadyDep(containerd.component),
			readiness.ReadyDep(coordinator.component),
			readiness.ReadyDep(entityAccess.component),
			readiness.ReadyDep(network.component),
			readiness.ReadyDep(observability.component),
		},
		Start:       b.start,
		Stop:        b.stop,
		StopTimeout: runnerComponentStopTimeout,
	})
	return b
}

func (b *runnerBoot) output() runnerBootOutput {
	return b.result
}

func (b *runnerBoot) start(ctx context.Context, _ readiness.Reporter) error {
	config := runner.RunnerConfig{
		Id:            b.inputs.config.GetRunnerID(),
		ListenAddress: b.inputs.config.GetRunnerAddress(),
		Workers:       runner.DefaulWorkers,
		DataPath:      b.inputs.config.GetDataPath(),
		DiskMode:      b.inputs.config.GetDiskMode(),
	}
	var err error
	coordinator := b.coordinator.output().coordinator
	config.Config, err = coordinator.RunnerConfig(config.ListenAddress)
	if err != nil {
		return err
	}
	cloudAuth := b.registration.output().cloudAuth
	if cloudAuth.Enabled {
		config.CloudAuth = &cloudAuth
	} else {
		config.CloudAuth = &coordinate.CloudAuthConfig{}
	}

	containerd := b.containerd.output()
	entityAccess := b.entityAccess.output()
	network := b.network.output()
	observability := b.observability.output()
	dependencies := runner.RunnerDeps{
		Secrets:         b.inputs.secrets,
		CC:              containerd.client,
		Namespace:       containerd.namespace,
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
		IsCoordinator:   true,
		ApiAddress:      net.JoinHostPort(network.routerAddress.String(), strconv.Itoa(b.inputs.apiPort)),
		CACert:          coordinator.CACertificate(),
	}
	if issuer := b.identity.output().issuer; issuer != nil {
		dependencies.WorkloadIssuer = issuer
	}

	b.result.runner, err = runner.NewRunner(observability.log, dependencies, config)
	if err != nil {
		return err
	}
	if err := b.result.runner.Start(ctx, b.inputs.group); err != nil {
		return err
	}
	b.started = true
	return nil
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
		if client := b.containerd.output().client; client != nil {
			if err := stopAllSandboxContainers(ctx, b.observability.output().log, client); err != nil {
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
		task, err := container.Task(ctx, nil)
		if err != nil {
			continue
		}
		log.Info("stopping container", "container", container.ID())
		if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
			log.Debug("failed to send SIGTERM to task", "container", container.ID(), "error", err)
		} else {
			log.Debug("sent SIGTERM to task", "container", container.ID())
		}
		time.Sleep(100 * time.Millisecond)
		if _, err := task.Delete(ctx, containerd.WithProcessKill); err != nil {
			log.Debug("failed to delete task", "container", container.ID(), "error", err)
		} else {
			stopped++
		}
	}
	log.Info("stopped sandbox containers", "count", stopped)
	return nil
}
