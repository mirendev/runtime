//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	containerd "github.com/containerd/containerd/v2/client"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

const containerdNamespace = "miren"

type containerdBootInputs struct {
	config      serverconfig.ContainerdConfig
	releasePath string
	dataPath    string
}

type containerdBootOutput struct {
	client    *containerd.Client
	namespace string
}

type containerdBoot struct {
	component     *readiness.Component
	inputs        containerdBootInputs
	observability *observabilityBoot
	daemon        *containerdcomp.ContainerdComponent
	result        containerdBootOutput
}

func containerdInputs(options StartOptions) containerdBootInputs {
	config := options.Config.Containerd
	if config.GetSocketPath() == "" {
		config.SetSocketPath(filepath.Join(options.Config.Server.GetDataPath(), "containerd", "containerd.sock"))
	}
	return containerdBootInputs{
		config:      config,
		releasePath: options.Config.Server.GetReleasePath(),
		dataPath:    options.Config.Server.GetDataPath(),
	}
}

func newContainerdBoot(inputs containerdBootInputs, observability *observabilityBoot) *containerdBoot {
	b := &containerdBoot{inputs: inputs, observability: observability}
	b.component = readiness.NewComponent("containerd", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(observability.component)},
		Start:        b.start,
		Stop:         b.stop,
		StopTimeout:  componentStopTimeout,
	})
	return b
}

func (b *containerdBoot) output() containerdBootOutput {
	return b.result
}

func (b *containerdBoot) start(ctx context.Context, _ readiness.Reporter) error {
	socketPath := b.inputs.config.GetSocketPath()
	log := b.observability.output().log
	if b.inputs.config.GetStartEmbedded() {
		log.Info("starting embedded containerd", "binary", b.inputs.config.GetBinaryPath(), "release-path", b.inputs.releasePath, "socket", socketPath)

		var (
			containerdPath string
			binDir         string
			err            error
		)
		if b.inputs.releasePath == "" {
			containerdPath, err = exec.LookPath(b.inputs.config.GetBinaryPath())
			if err != nil {
				return fmt.Errorf("containerd binary not found: %s", b.inputs.config.GetBinaryPath())
			}
		} else {
			binDir = b.inputs.releasePath
			containerdPath = filepath.Join(binDir, "containerd")
		}
		if _, err := os.Stat(containerdPath); err != nil {
			return fmt.Errorf("containerd binary not found at %s: %w", containerdPath, err)
		}

		b.daemon = containerdcomp.NewContainerdComponent(log, b.inputs.dataPath)
		envPath := os.Getenv("PATH")
		if binDir != "" {
			envPath = binDir + ":" + envPath
		}
		if err := b.daemon.Start(ctx, &containerdcomp.Config{
			BinaryPath: containerdPath,
			BaseDir:    filepath.Join(b.inputs.dataPath, "containerd"),
			BinDir:     binDir,
			SocketPath: socketPath,
			Env:        []string{"PATH=" + envPath},
		}); err != nil {
			return err
		}
		socketPath = b.daemon.SocketPath()
		log.Info("embedded containerd started", "socket", socketPath)
	}

	client, err := containerd.New(socketPath, containerd.WithDefaultNamespace(containerdNamespace))
	if err != nil {
		return fmt.Errorf("creating containerd client: %w", err)
	}
	b.result = containerdBootOutput{
		client:    client,
		namespace: containerdNamespace,
	}
	return nil
}

func (b *containerdBoot) stop(ctx context.Context) error {
	var errs []error
	if b.result.client != nil {
		if err := b.result.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing containerd client: %w", err))
		}
		b.result.client = nil
	}
	if b.daemon != nil {
		b.observability.output().log.Info("stopping embedded containerd")
		if err := b.daemon.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
