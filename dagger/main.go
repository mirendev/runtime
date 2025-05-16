// A module to build and test the Miren runtime

package main

import (
	"context"
	"dagger/runtime/internal/dagger"
	"runtime"
	"strconv"
	"strings"
)

type Runtime struct{}

var (
	arm_containerd = "https://github.com/containerd/containerd/releases/download/v2.0.4/containerd-2.0.4-linux-arm64.tar.gz"
	arm_buildkit   = "https://github.com/moby/buildkit/releases/download/v0.17.1/buildkit-v0.17.1.linux-arm64.tar.gz"
	arm_runc       = "https://github.com/opencontainers/runc/releases/download/v1.2.2/runc.arm64"
	arm_runsc      = "https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/runsc"
	arm_runscshim  = "https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/containerd-shim-runsc-v1"
	arm_nerdctl    = "https://github.com/containerd/nerdctl/releases/download/v2.0.5/nerdctl-2.0.5-linux-arm64.tar.gz"
)

var (
	amd_containerd = "https://github.com/containerd/containerd/releases/download/v2.0.4/containerd-2.0.4-linux-amd64.tar.gz"
	amd_buildkit   = "https://github.com/moby/buildkit/releases/download/v0.17.1/buildkit-v0.17.1.linux-amd64.tar.gz"
	amd_runc       = "https://github.com/opencontainers/runc/releases/download/v1.2.2/runc.amd64"
	amd_runsc      = "https://storage.googleapis.com/gvisor/releases/release/latest/x86_64/runsc"
	amd_runscshim  = "https://storage.googleapis.com/gvisor/releases/release/latest/x86_64/containerd-shim-runsc-v1"
	amd_nerdctl    = "https://github.com/containerd/nerdctl/releases/download/v2.0.5/nerdctl-2.0.5-linux-amd64.tar.gz"
)

var containerd, buildkit, runc, runsc, runscshim, nerdctl string

func init() {
	if runtime.GOARCH == "arm64" {
		containerd = arm_containerd
		buildkit = arm_buildkit
		runc = arm_runc
		runsc = arm_runsc
		runscshim = arm_runscshim
		nerdctl = arm_nerdctl
	} else {
		containerd = amd_containerd
		buildkit = amd_buildkit
		runc = amd_runc
		runsc = amd_runsc
		runscshim = amd_runscshim
		nerdctl = amd_nerdctl
	}
}

func (m *Runtime) WithServices(dir *dagger.Directory) *dagger.Container {
	ch := dag.Container().
		From("clickhouse/clickhouse-server:latest").
		WithEnvVariable("CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT", "1").
		WithEnvVariable("CLICKHOUSE_PASSWORD", "default").
		WithExposedPort(9000).
		AsService()

	pg := dag.Container().
		From("postgres:17").
		WithEnvVariable("POSTGRES_DB", "runtime_test").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_HOST_AUTH_METHOD", "trust").
		WithExposedPort(5432).
		AsService()

	etcd := dag.Container().
		From("bitnami/etcd:3.5.19").
		WithEnvVariable("ALLOW_NONE_AUTHENTICATION", "yes").
		WithEnvVariable("ETCD_ADVERTISE_CLIENT_URLS", "http://etcd:2379").
		WithExposedPort(2379).
		AsService()

	minio := dag.Container().
		From("quay.io/minio/minio:RELEASE.2025-04-03T14-56-28Z").
		WithEnvVariable("MINIO_ROOT_USER", "admin").
		WithEnvVariable("MINIO_ROOT_PASSWORD", "password").
		WithExposedPort(9000).
		WithExec([]string{"minio", "server", "/data"}).
		AsService()

	return m.BuildEnv(dir).
		WithServiceBinding("clickhouse", ch).
		WithServiceBinding("postgres", pg).
		WithServiceBinding("etcd", etcd).
		WithServiceBinding("minio", minio)
}

func (m *Runtime) Etcd() *dagger.Container {
	etcd := dag.Container().
		From("bitnami/etcd:3.5.19").
		WithEnvVariable("ALLOW_NONE_AUTHENTICATION", "yes").
		WithEnvVariable("ETCD_ADVERTISE_CLIENT_URLS", "http://etcd:2379").
		WithExposedPort(2379).
		//WithExposedPort(2380).
		AsService()

	return dag.Container().
		WithServiceBinding("etcd", etcd).
		Terminal()
}

func (m *Runtime) BuildEnv(dir *dagger.Directory) *dagger.Container {
	return dag.Container().
		From("golang:1.24").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-124")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-124")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"go", "install", "gotest.tools/gotestsum@latest"}).
		WithExec([]string{"sh", "-c", "cd /usr/bin && curl https://clickhouse.com/ | sh"}, dagger.ContainerWithExecOpts{}).
		WithExec([]string{"apt-get", "install", "-y",
			"bash",
			"inetutils-ping",
			"iproute2",
			"iptables",
			"tmux",
		}).
		WithFile("/upstream/containerd.tar.gz", dag.HTTP(containerd)).
		WithFile("/upstream/buildkit.tar.gz", dag.HTTP(buildkit)).
		WithFile("/upstream/nerdctl.tar.gz", dag.HTTP(nerdctl)).
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
		WithExec([]string{"tar", "-C", "/usr/local/bin", "-xvf", "/upstream/nerdctl.tar.gz"}).
		WithExec([]string{"mv", "/upstream/runc", "/usr/local/bin/runc"}).
		WithExec([]string{"mv", "/upstream/runsc", "/usr/local/bin/runsc"}).
		WithExec([]string{"mv", "/upstream/containerd-shim-runsc-v1", "/usr/local/bin/containerd-shim-runsc-v1"}).
		WithExec([]string{"/usr/local/bin/runsc", "install"})
}

func (m *Runtime) Container(
	ctx context.Context,
	dir *dagger.Directory,
) *dagger.Container {
	return m.BuildEnv(dir)
}

func (m *Runtime) Test(
	ctx context.Context,
	dir *dagger.Directory,
	// +optional
	shell bool,
	// +optional
	tests string,
	// +optional
	count int,
	// +optional
	verbose bool,
	// +optional
	run string,
	// +optional
	fast bool,
) (string, error) {
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithEnvVariable("S3_URL", "http://minio:9000").
		WithMountedCache("/data", dag.CacheVolume("containerd"))

	if tests == "" {
		tests = "./..."
	}

	if shell {
		w = w.Terminal(dagger.ContainerTerminalOpts{
			InsecureRootCapabilities: true,
			Cmd:                      []string{"/bin/bash"},
		})
	} else {
		args := []string{"sh", "/src/hack/test.sh"}

		for _, t := range strings.Split(tests, " ") {
			args = append(args, t)
		}

		if count > 0 {
			//args = append(args, "--")
			args = append(args, "-count", strconv.Itoa(count))
		}

		if run != "" {
			args = append(args, "-run", run)
		}

		if verbose {
			w = w.WithEnvVariable("VERBOSE", "1")
		}

		if fast {
			args = append(args, "-failfast")
		}

		w = w.WithExec(args, dagger.ContainerWithExecOpts{
			InsecureRootCapabilities: true,
		})
	}

	return w.Stdout(ctx)
}

func (m *Runtime) Dev(
	ctx context.Context,
	dir *dagger.Directory,
	// +optional
	shell bool,
	// +optional
	tests string,
	// +optional
	count int,
	// +optional
	verbose bool,
	// +optional
	run string,
) (string, error) {
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithEnvVariable("S3_URL", "http://minio:9000").
		WithMountedCache("/data", dag.CacheVolume("containerd"))

	w = w.Terminal(dagger.ContainerTerminalOpts{
		InsecureRootCapabilities: true,
		Cmd:                      []string{"/bin/bash", "/src/hack/dev.sh"},
	})

	return w.Stdout(ctx)
}
