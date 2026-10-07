package containerd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	containerdclient "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/pkg/boot"
)

const Namespace = "miren"

// Capability is the ready containerd service consumed by boot graph nodes.
// Embedded and external daemons publish the same value.
type Capability struct {
	Client    *containerdclient.Client
	Namespace string
}

// BootConfig describes either an external containerd socket or an embedded
// daemon. Embedded is nil when the daemon is managed outside this process.
type BootConfig struct {
	Log              *slog.Logger
	DataPath         string
	SocketPath       string
	Embedded         *Config
	ReadinessTimeout time.Duration
	StopTimeout      time.Duration
	// ReportVersion, when set, receives the version of each runtime piece
	// once it is confirmed running: the containerd daemon, and for an
	// embedded daemon the runc beside it. An upgrade that replaced them on
	// disk reads this back to see that the swap took.
	ReportVersion func(component, version string)
}

// ExternalBootConfig connects to a daemon managed outside this process.
func ExternalBootConfig(log *slog.Logger, dataPath, socketPath string) BootConfig {
	return BootConfig{
		Log:        log,
		DataPath:   dataPath,
		SocketPath: socketPath,
	}
}

// EmbeddedBootConfig starts a daemon and connects to it. binaryPath may be an
// executable name when binDir is empty, in which case startup resolves PATH.
func EmbeddedBootConfig(log *slog.Logger, dataPath, binaryPath, binDir, socketPath string) BootConfig {
	if socketPath == "" {
		socketPath = filepath.Join(dataPath, "containerd", "containerd.sock")
	}
	envPath := os.Getenv("PATH")
	if binDir != "" {
		envPath = binDir + ":" + envPath
		binaryPath = filepath.Join(binDir, "containerd")
	}
	config := ExternalBootConfig(log, dataPath, socketPath)
	config.Embedded = &Config{
		BinaryPath: binaryPath,
		BaseDir:    filepath.Join(dataPath, "containerd"),
		BinDir:     binDir,
		SocketPath: socketPath,
		Env:        []string{"PATH=" + envPath},
	}
	return config
}

// Boot owns a containerd client and, when configured, its embedded daemon.
type Boot struct {
	Component *boot.Component
	Output    boot.Output[Capability]

	config BootConfig
	daemon *ContainerdComponent
	result Capability
}

// NewBoot declares a containerd boot component.
func NewBoot(name string, config BootConfig) *Boot {
	b := &Boot{config: config}
	b.Component, b.Output = boot.Provide0(name, b.start,
		boot.WithStop(b.stop, config.StopTimeout))
	return b
}

func (b *Boot) start(ctx context.Context) (Capability, error) {
	socketPath := b.config.SocketPath
	if b.config.Embedded == nil {
		b.config.Log.Info("connecting to external containerd", "socket", socketPath)
	} else {
		embedded := *b.config.Embedded
		if filepath.Base(embedded.BinaryPath) == embedded.BinaryPath {
			binaryPath, err := exec.LookPath(embedded.BinaryPath)
			if err != nil {
				return Capability{}, fmt.Errorf("containerd binary not found: %s", embedded.BinaryPath)
			}
			embedded.BinaryPath = binaryPath
		}
		b.config.Log.Info("starting embedded containerd",
			"binary", embedded.BinaryPath, "socket", embedded.SocketPath)
		b.daemon = NewContainerdComponent(b.config.Log, b.config.DataPath)
		if err := b.daemon.Start(ctx, &embedded); err != nil {
			return Capability{}, fmt.Errorf("starting embedded containerd: %w", err)
		}
		socketPath = b.daemon.SocketPath()
		b.config.Log.Info("embedded containerd started", "socket", socketPath)
	}

	client, err := containerdclient.New(socketPath,
		containerdclient.WithDefaultNamespace(Namespace))
	if err != nil {
		return Capability{}, fmt.Errorf("connecting to containerd: %w", err)
	}
	b.result = Capability{Client: client, Namespace: Namespace}

	timeout := b.config.ReadinessTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	daemon, err := client.Version(readyCtx)
	if err != nil {
		return Capability{}, fmt.Errorf("checking containerd readiness: %w", err)
	}
	b.report("containerd", daemon.Version)
	if b.config.Embedded != nil {
		b.report("runc", runcVersion(readyCtx, b.config.Embedded.BinDir))
	}
	return b.result, nil
}

func (b *Boot) report(component, version string) {
	if b.config.ReportVersion == nil || version == "" {
		return
	}
	b.config.ReportVersion(component, version)
}

// runcVersion asks the runc an embedded daemon's shims will exec. Empty when
// there is no answer; the version is a report, not a requirement.
func runcVersion(ctx context.Context, binDir string) string {
	path := "runc"
	if binDir != "" {
		path = filepath.Join(binDir, "runc")
	}
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	// First line is "runc version 1.2.2"; the rest is commit and spec.
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(strings.TrimPrefix(line, "runc version "))
}

func (b *Boot) stop(ctx context.Context) error {
	var errs []error
	if b.result.Client != nil {
		if err := b.result.Client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing containerd client: %w", err))
		}
		b.result.Client = nil
	}
	if b.daemon != nil {
		b.config.Log.Info("stopping embedded containerd")
		if err := b.daemon.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
