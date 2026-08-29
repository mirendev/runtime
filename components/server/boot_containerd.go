//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

const containerdNamespace = "miren"

type containerdBootInputs struct {
	log         *slog.Logger
	config      serverconfig.ContainerdConfig
	releasePath string
	dataPath    string
}

type containerdBootOutput struct {
	client    *containerd.Client
	namespace string
}

type containerdBoot struct {
	component *boot.Component
	inputs    containerdBootInputs
	log       *slog.Logger
	daemon    *containerdcomp.ContainerdComponent
	result    containerdBootOutput
	output    boot.Output[containerdBootOutput]
}

func containerdInputs(options StartOptions) containerdBootInputs {
	config := options.Config.Containerd
	if config.GetSocketPath() == "" {
		config.SetSocketPath(filepath.Join(options.Config.Server.GetDataPath(), "containerd", "containerd.sock"))
	}
	return containerdBootInputs{
		log:         options.Log,
		config:      config,
		releasePath: options.Config.Server.GetReleasePath(),
		dataPath:    options.Config.Server.GetDataPath(),
	}
}

func newContainerdBoot(inputs containerdBootInputs) *containerdBoot {
	b := &containerdBoot{inputs: inputs}
	b.component, b.output = boot.Provide0("containerd", b.start,
		boot.WithStop(b.stop, componentStopTimeout))
	return b
}

func (b *containerdBoot) start(ctx context.Context) (containerdBootOutput, error) {
	socketPath := b.inputs.config.GetSocketPath()
	b.log = b.inputs.log
	log := b.log
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
				return containerdBootOutput{}, fmt.Errorf("containerd binary not found: %s", b.inputs.config.GetBinaryPath())
			}
		} else {
			binDir = b.inputs.releasePath
			containerdPath = filepath.Join(binDir, "containerd")
		}
		if _, err := os.Stat(containerdPath); err != nil {
			return containerdBootOutput{}, fmt.Errorf("containerd binary not found at %s: %w", containerdPath, err)
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
			return containerdBootOutput{}, err
		}
		socketPath = b.daemon.SocketPath()
		log.Info("embedded containerd started", "socket", socketPath)
	}

	client, err := containerd.New(socketPath, containerd.WithDefaultNamespace(containerdNamespace))
	if err != nil {
		return containerdBootOutput{}, fmt.Errorf("creating containerd client: %w", err)
	}
	readyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := client.Version(readyCtx); err != nil {
		_ = client.Close()
		return containerdBootOutput{}, fmt.Errorf("checking containerd readiness: %w", err)
	}
	b.result = containerdBootOutput{
		client:    client,
		namespace: containerdNamespace,
	}
	return b.result, nil
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
		b.log.Info("stopping embedded containerd")
		if err := b.daemon.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
