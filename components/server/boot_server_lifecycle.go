//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverinfo"
	"miren.dev/runtime/pkg/serverlifecycle"
)

type serverLifecycleBootOutput struct {
	lifecycle *coordinate.ServerLifecycle
}

type serverLifecycleInputs struct {
	dir string
	// etcd and dataPath let an in-process executor snapshot embedded etcd
	// before an upgrade, the way the systemd executor reads them from the
	// config file.
	etcd     serverconfig.EtcdConfig
	dataPath string
}

func serverLifecycleInputsFrom(options StartOptions) serverLifecycleInputs {
	return serverLifecycleInputs{
		dir:      serverlifecycle.DefaultDir,
		etcd:     options.Config.Etcd,
		dataPath: options.Config.Server.GetDataPath(),
	}
}

type serverLifecycleBoot struct {
	component *boot.Component
	output    boot.Output[serverLifecycleBootOutput]
	inputs    serverLifecycleInputs
	value     *coordinate.ServerLifecycle
	instance  *serverinfo.Source

	// container is set for a container install, where the executor runs in
	// this process and has to be waited for on the way down.
	container       *serverlifecycle.ContainerLauncher
	cancelContainer context.CancelFunc
}

// The ledger is on disk and the RPC only reads it, so this needs nothing but
// the foundation. The cloud uplink takes its output so the capability is
// registered before the link connects.
func newServerLifecycleBoot(inputs serverLifecycleInputs, instance *serverinfo.Source, foundation boot.Output[foundationBootOutput]) *serverLifecycleBoot {
	b := &serverLifecycleBoot{inputs: inputs, instance: instance}
	b.component, b.output = boot.Provide1(
		"server-lifecycle", foundation, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *serverLifecycleBoot) start(ctx context.Context, foundation foundationBootOutput) (serverLifecycleBootOutput, error) {
	log := foundation.foundation.Log.With("component", "server-lifecycle")
	launcher, err := b.launcher(log)
	if err != nil {
		return serverLifecycleBootOutput{}, err
	}
	lifecycle := coordinate.NewServerLifecycle(foundation.foundation, b.instance, b.inputs.dir, launcher)
	if err := lifecycle.Start(ctx); err != nil {
		return serverLifecycleBootOutput{}, err
	}
	b.value = lifecycle
	return serverLifecycleBootOutput{lifecycle: lifecycle}, nil
}

// launcher picks where the executor runs from how this process is
// supervised. Under systemd it is a transient unit that outlives the
// restart. In a container nothing does, so it runs in-process and the next
// instance resumes it.
func (b *serverLifecycleBoot) launcher(log *slog.Logger) (serverlifecycle.Launcher, error) {
	if b.instance.Info().InstallKind != serverinfo.InstallKindContainer {
		exe, err := serverlifecycle.ExecutorBinary()
		if err != nil {
			return nil, err
		}
		return serverlifecycle.SystemdLauncher{Binary: exe}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancelContainer = cancel
	b.container = serverlifecycle.NewContainerLauncher(ctx, log, func() (*serverlifecycle.Executor, error) {
		return b.newContainerExecutor(log)
	})
	return b.container, nil
}

func (b *serverLifecycleBoot) newContainerExecutor(log *slog.Logger) (*serverlifecycle.Executor, error) {
	store, err := serverlifecycle.NewStore(b.inputs.dir)
	if err != nil {
		return nil, err
	}
	opts := serverlifecycle.DefaultOptions()
	// The image's /usr/local/bin/miren is container-boot's known-good
	// fallback. Pointing it into the volume would make a bad upgrade
	// unrecoverable.
	opts.PathSymlink = ""
	ex := serverlifecycle.NewExecutor(store, opts, log).
		WithRestarter(serverlifecycle.ContainerRestarter{}).
		WithProber(serverlifecycle.InstanceProber{Source: b.instance})
	if b.inputs.etcd.GetStartEmbedded() {
		ex.WithDataBackup(etcd.Backup{
			Dir:      filepath.Join(store.Dir(), "backups"),
			Endpoint: fmt.Sprintf("https://localhost:%d", b.inputs.etcd.GetClientPort()),
			TLS:      &etcd.TLSConfig{CertsDir: coordinate.EtcdCertsDir(b.inputs.dataPath)},
			Log:      log,
		})
	} else {
		log.Info("etcd is not embedded; upgrades will not snapshot it and rollback restores only the binary")
	}
	return ex, nil
}

func (b *serverLifecycleBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	if b.container != nil {
		b.cancelContainer()
		b.container.Wait()
	}
	return nil
}
