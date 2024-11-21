// A module to build and test the Miren runtime

package main

import (
	"context"
	"dagger/runtime/internal/dagger"
)

type Runtime struct{}

var (
	containerd = "https://github.com/containerd/containerd/releases/download/v2.0.0/containerd-2.0.0-linux-arm64.tar.gz"
	buildkit   = "https://github.com/moby/buildkit/releases/download/v0.17.1/buildkit-v0.17.1.linux-arm64.tar.gz"
	runc       = "https://github.com/opencontainers/runc/releases/download/v1.2.1/runc.arm64"
	runsc      = "https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/runsc"
	runscshim  = "https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/containerd-shim-runsc-v1"
)

func (m *Runtime) WithServices(dir *dagger.Directory) *dagger.Container {
	ch := dag.Container().
		From("clickhouse/clickhouse-server:latest").
		WithExposedPort(9000).
		AsService()

	pg := dag.Container().
		From("postgres:17").
		WithEnvVariable("POSTGRES_DB", "miren_test").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_HOST_AUTH_METHOD", "trust").
		WithExposedPort(5432).
		AsService()

	return m.BuildEnv(dir).
		WithServiceBinding("clickhouse", ch).
		WithServiceBinding("postgres", pg)
}

func (m *Runtime) BuildEnv(dir *dagger.Directory) *dagger.Container {
	return dag.Container().
		From("golang:1.23").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-123")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-123")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "iptables", "bash"}).
		WithExec([]string{"go", "install", "gotest.tools/gotestsum@latest"}).
		WithFile("/upstream/containerd.tar.gz", dag.HTTP(containerd)).
		WithFile("/upstream/buildkit.tar.gz", dag.HTTP(buildkit)).
		WithFile("/upstream/runc", dag.HTTP(runc), dagger.ContainerWithFileOpts{
			Permissions: 0755,
		}).
		WithFile("/upstream/runsc", dag.HTTP(runsc), dagger.ContainerWithFileOpts{
			Permissions: 0755,
		}).
		WithFile("/upstream/containerd-shim-runsc-v1", dag.HTTP(runscshim), dagger.ContainerWithFileOpts{
			Permissions: 0755,
		}).
		WithFile("/usr/local/bin/runsc-ignore", dir.File("hack/runsc-ignore"), dagger.ContainerWithFileOpts{
			Permissions: 0755,
		}).
		WithFile("/etc/containerd/config.toml", dir.File("hack/containerd-config.toml")).
		WithExec([]string{"tar", "-C", "/usr/local", "-xvf", "/upstream/containerd.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local", "-xvf", "/upstream/buildkit.tar.gz"}).
		WithExec([]string{"mv", "/upstream/runc", "/usr/local/bin/runc"}).
		WithExec([]string{"mv", "/upstream/runsc", "/usr/local/bin/runsc"}).
		WithExec([]string{"mv", "/upstream/containerd-shim-runsc-v1", "/usr/local/bin/containerd-shim-runsc-v1"}).
		WithExec([]string{"/usr/local/bin/runsc", "install"})
}

func (m *Runtime) Test(
	ctx context.Context,
	dir *dagger.Directory,
	// +optional
	shell bool,
	// +optional
	tests string,
) (string, error) {
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithMountedCache("/data", dag.CacheVolume("containerd"))

	if tests == "" {
		tests = "./..."
	}

	if shell {
		w = w.Terminal(dagger.ContainerTerminalOpts{
			InsecureRootCapabilities: true,
		})
	} else {
		w = w.WithExec([]string{"sh", "/src/hack/test.sh", tests}, dagger.ContainerWithExecOpts{
			InsecureRootCapabilities:      true,
			ExperimentalPrivilegedNesting: true,
		})
	}

	return w.Stdout(ctx)
}
