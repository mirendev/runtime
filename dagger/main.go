// A module to build and test the Miren runtime

package main

import (
	"context"
	"dagger/runtime/internal/dagger"
	"fmt"
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
		From("oci.miren.cloud/clickhouse:v2").
		WithEnvVariable("CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT", "1").
		WithEnvVariable("CLICKHOUSE_PASSWORD", "default").
		WithExposedPort(9000).
		AsService()

	vl := dag.Container().
		From("docker.io/victoriametrics/victoria-logs:v1.0.0-victorialogs").
		WithExposedPort(9428).
		AsService()

	vm := dag.Container().
		From("docker.io/victoriametrics/victoria-metrics:v1.106.1").
		WithExposedPort(8428).
		AsService()

	etcd := dag.Container().
		From("oci.miren.cloud/etcd:v1").
		WithExposedPort(2379).
		AsService(dagger.ContainerAsServiceOpts{
			Args: []string{"etcd",
				"--listen-client-urls", "http://0.0.0.0:2379",
				"--advertise-client-urls", "http://etcd:2379",
				"--initial-advertise-peer-urls", "http://localhost:2380",
			},
		})

	return m.BuildEnv(dir).
		WithServiceBinding("clickhouse", ch).
		WithServiceBinding("etcd", etcd).
		WithServiceBinding("victorialogs", vl).
		WithServiceBinding("victoriametrics", vm)
}

func (m *Runtime) BuildEnv(dir *dagger.Directory) *dagger.Container {
	return dag.Container().
		From("golang:1.24").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y",
			"bash",
			"gotestsum",
			"inetutils-ping",
			"iproute2",
			"iptables",
			"tmux",
			"vim",
			"netcat-openbsd",
			"util-linux",
			"e2fsprogs",
		}).
		WithFile("/upstream/containerd.tar.gz", dag.HTTP(containerd)).
		WithFile("/upstream/buildkit.tar.gz", dag.HTTP(buildkit)).
		WithFile("/upstream/nerdctl.tar.gz", dag.HTTP(nerdctl)).
		WithFile("/upstream/runc", dag.HTTP(runc), dagger.ContainerWithFileOpts{
			Permissions: 0755,
		}).
		//WithFile("/upstream/runsc", dag.HTTP(runsc), dagger.ContainerWithFileOpts{
		//Permissions: 0755,
		//}).
		//WithFile("/upstream/containerd-shim-runsc-v1", dag.HTTP(runscshim), dagger.ContainerWithFileOpts{
		//Permissions: 0755,
		//}).
		//WithFile("/usr/local/bin/runsc-ignore", dir.File("hack/runsc-ignore"), dagger.ContainerWithFileOpts{
		//Permissions: 0755,
		//}).
		WithFile("/etc/containerd/config.toml", dir.File("hack/containerd-config.toml")).
		WithExec([]string{"tar", "-C", "/usr/local", "-xvf", "/upstream/containerd.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local", "-xvf", "/upstream/buildkit.tar.gz"}).
		WithExec([]string{"tar", "-C", "/usr/local/bin", "-xvf", "/upstream/nerdctl.tar.gz"}).
		WithExec([]string{"mv", "/upstream/runc", "/usr/local/bin/runc"}).
		//WithExec([]string{"mv", "/upstream/runsc", "/usr/local/bin/runsc"}).
		//WithExec([]string{"mv", "/upstream/containerd-shim-runsc-v1", "/usr/local/bin/containerd-shim-runsc-v1"}).
		//WithExec([]string{"/usr/local/bin/runsc", "install"}).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-124")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-124")).
		WithEnvVariable("GOCACHE", "/go/build-cache")
}

func (m *Runtime) Package(
	ctx context.Context,
	dir *dagger.Directory,
) *dagger.File {
	c := m.BuildEnv(dir).
		WithDirectory("/src", dir).
		WithExec([]string{"/bin/sh", "-c", `
		set -e
		cd /src
		make bin/miren
		mkdir -p /tmp/package
		cp bin/miren /tmp/package
		cp /usr/local/bin/runc /tmp/package
			# cp /usr/local/bin/containerd-shim-runsc-v1 /tmp/package
		cp /usr/local/bin/containerd-shim-runc-v2 /tmp/package
		cp /usr/local/bin/containerd /tmp/package
		cp /usr/local/bin/nerdctl /tmp/package
		cp /usr/local/bin/ctr /tmp/package
		tar -C /tmp/package -czf /tmp/package.tar.gz .
`})
	return c.File("/tmp/package.tar.gz")
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
	// NOTE: This flag cannot be called "verbose" - see https://github.com/dagger/dagger/issues/10428
	// +optional
	verboose bool,
	// +optional
	run string,
	// +optional
	fast bool,
	// +optional
	tags string,
) (string, error) {
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithEnvVariable("VICTORIALOGS_ADDR", "victorialogs:9428").
		WithEnvVariable("VICTORIAMETRICS_ADDR", "victoriametrics:8428").
		WithEnvVariable("DISABLE_NBD_TEST", "1").
		WithMountedCache("/data", dag.CacheVolume("containerd"))

	if tests == "" {
		tests = "./..."
	}

	if shell {
		w = w.WithEnvVariable("USESHELL", "1")
		w = w.Terminal(dagger.ContainerTerminalOpts{
			InsecureRootCapabilities: true,
			Cmd:                      []string{"/bin/bash", "/src/hack/test.sh"},
		})
	} else {
		args := []string{"/bin/bash", "/src/hack/test.sh"}

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

		if verboose {
			w = w.WithEnvVariable("VERBOSE", "1")
		}

		if fast {
			args = append(args, "-failfast")
		}

		if tags != "" {
			args = append(args, fmt.Sprintf("--tags=%s", tags))
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
	tmux bool,
) (string, error) {
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithMountedCache("/data", dag.CacheVolume("containerd")).
		WithMountedCache("/var/lib/miren", dag.CacheVolume("miren-data"))

	if tmux {
		w = w.WithEnvVariable("USE_TMUX", "1")
	}

	w = w.Terminal(dagger.ContainerTerminalOpts{
		InsecureRootCapabilities: true,
		Cmd:                      []string{"/bin/bash", "/src/hack/dev.sh"},
	})

	return w.Stdout(ctx)
}

// Debug returns a container with just the services (etcd, clickhouse) for local debugging
func (m *Runtime) Debug(
	ctx context.Context,
	dir *dagger.Directory,
) (string, error) {
	// Simple container with just the services - no containerd, buildkit, or runtime setup
	w := m.WithServices(dir).
		WithDirectory("/src", dir).
		WithWorkdir("/src")

	w = w.Terminal(dagger.ContainerTerminalOpts{
		Cmd: []string{"/bin/bash", "/src/hack/run-services.sh"},
	})

	return w.Stdout(ctx)
}
