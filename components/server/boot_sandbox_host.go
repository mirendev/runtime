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
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type sandboxHostBootInputs struct {
	config                  serverconfig.ServerConfig
	resolver                netresolve.Resolver
	apiPort                 int
	group                   *errgroup.Group
	bridge                  string
	tempDir                 string
	servicePrefixes         []netip.Prefix
	stopSandboxesOnShutdown bool
}

type sandboxHostBoot struct {
	component     *boot.Component
	inputs        sandboxHostBootInputs
	containerd    containerdBootOutput
	log           *slog.Logger
	value         *runner.SandboxHost
	output        boot.Output[*runner.SandboxHost]
	started       bool
	cleanupOnStop bool
}

func sandboxHostInputs(options StartOptions, resolver netresolve.Resolver, apiPort int) sandboxHostBootInputs {
	return sandboxHostBootInputs{
		config:                  options.Config.Server,
		resolver:                resolver,
		apiPort:                 apiPort,
		group:                   options.Group,
		bridge:                  "rt0",
		tempDir:                 os.TempDir(),
		stopSandboxesOnShutdown: options.Config.Server.GetStopSandboxesOnShutdown(),
		servicePrefixes:         serviceNetworkPrefixes(),
	}
}

func newSandboxHostBoot(inputs sandboxHostBootInputs, access boot.Output[clusterAccessBootOutput], storage boot.Output[*runner.NodeStorage], containerdOutput boot.Output[containerdBootOutput], network boot.Output[networkBootOutput], observability boot.Output[observabilityBootOutput]) *sandboxHostBoot {
	b := &sandboxHostBoot{inputs: inputs}
	b.component, b.output = boot.Provide5(
		"sandbox-host", access, storage, containerdOutput, network, observability,
		b.start, boot.WithStop(b.stop, runnerComponentStopTimeout),
	)
	return b
}

func (b *sandboxHostBoot) start(ctx context.Context, access clusterAccessBootOutput, storage *runner.NodeStorage, containerdOutput containerdBootOutput, network networkBootOutput, observability observabilityBootOutput) (*runner.SandboxHost, error) {
	config := access.config

	dependencies := runner.RunnerDeps{
		CC:              containerdOutput.Client,
		Namespace:       containerdOutput.Namespace,
		Bridge:          b.inputs.bridge,
		Tempdir:         b.inputs.tempDir,
		Subnet:          network.subnet,
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
		CACert:          access.caCert,
	}

	var err error
	b.value, err = runner.NewSandboxHost(access.access, storage, dependencies, config)
	if err != nil {
		return nil, err
	}
	if err := b.value.Start(ctx, b.inputs.group); err != nil {
		return nil, err
	}
	b.containerd = containerdOutput
	b.log = observability.log
	b.started = true
	return b.value, nil
}

func (b *sandboxHostBoot) stop(ctx context.Context) error {
	if b.value == nil {
		return nil
	}
	var errs []error
	if err := b.value.Close(); err != nil {
		errs = append(errs, err)
	}
	if b.cleanupOnStop && b.started && b.containerd.Client != nil {
		if err := stopAllSandboxContainers(ctx, b.log, b.containerd.Client, b.containerd.Namespace); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *sandboxHostBoot) enableShutdownCleanup() {
	b.cleanupOnStop = b.inputs.stopSandboxesOnShutdown
}

func stopAllSandboxContainers(ctx context.Context, log *slog.Logger, client *containerd.Client, namespace string) error {
	log.Info("stopping all sandbox containers via containerd")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, namespace)

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
