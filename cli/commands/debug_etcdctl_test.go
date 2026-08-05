package commands

import (
	"errors"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestBuildEtcdctlExecArgs(t *testing.T) {
	tests := []struct {
		name        string
		processArgs []string
		mounts      []specs.Mount
		userArgs    []string
		want        []string
	}{
		{
			name: "plaintext endpoint",
			processArgs: []string{
				"/usr/local/bin/etcd",
				"--advertise-client-urls", "http://localhost:12379",
			},
			userArgs: []string{"get", "/", "--prefix"},
			want: []string{
				"tasks", "exec", "--exec-id", "test-exec",
				"miren-etcd", "/usr/local/bin/etcdctl",
				"--endpoints=http://localhost:12379",
				"get", "/", "--prefix",
			},
		},
		{
			name: "TLS endpoint",
			processArgs: []string{
				"/usr/local/bin/etcd",
				"--advertise-client-urls=https://localhost:22379",
			},
			mounts:   []specs.Mount{{Destination: "/certs"}},
			userArgs: []string{"endpoint", "status", "--write-out=table"},
			want: []string{
				"tasks", "exec", "--exec-id", "test-exec",
				"miren-etcd", "/usr/local/bin/etcdctl",
				"--endpoints=https://localhost:22379",
				"--cacert=/certs/ca.crt",
				"--cert=/certs/client.crt",
				"--key=/certs/client.key",
				"endpoint", "status", "--write-out=table",
			},
		},
		{
			name: "listen URL fallback",
			processArgs: []string{
				"/usr/local/bin/etcd",
				"--listen-client-urls", "http://127.0.0.1:32379",
			},
			want: []string{
				"tasks", "exec", "--exec-id", "test-exec",
				"miren-etcd", "/usr/local/bin/etcdctl",
				"--endpoints=http://127.0.0.1:32379",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &specs.Spec{
				Process: &specs.Process{Args: tt.processArgs},
				Mounts:  tt.mounts,
			}
			got, err := buildEtcdctlExecArgs(spec, tt.userArgs, "test-exec")
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildEtcdctlExecArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		spec *specs.Spec
		want string
	}{
		{
			name: "missing process",
			spec: &specs.Spec{},
			want: "no process configuration",
		},
		{
			name: "missing endpoint",
			spec: &specs.Spec{Process: &specs.Process{Args: []string{"etcd"}}},
			want: "no client endpoint",
		},
		{
			name: "invalid endpoint",
			spec: &specs.Spec{Process: &specs.Process{Args: []string{
				"etcd", "--advertise-client-urls", "localhost:12379",
			}}},
			want: "invalid client endpoint",
		},
		{
			name: "TLS certificates not mounted",
			spec: &specs.Spec{Process: &specs.Process{Args: []string{
				"etcd", "--advertise-client-urls", "https://localhost:12379",
			}}},
			want: "client certificates are not mounted",
		},
		{
			name: "mixed endpoint schemes",
			spec: &specs.Spec{Process: &specs.Process{Args: []string{
				"etcd", "--advertise-client-urls", "http://etcd-1:2379,https://etcd-2:2379",
			}}},
			want: "mixes HTTP and HTTPS client endpoints",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildEtcdctlExecArgs(tt.spec, nil, "test-exec")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestEtcdClientEndpointSupportsMultipleURLs(t *testing.T) {
	got, tlsEnabled, err := etcdClientEndpoint([]string{
		"etcd",
		"--advertise-client-urls",
		"https://etcd-1:2379,https://etcd-2:2379",
	})
	require.NoError(t, err)
	require.Equal(t, "https://etcd-1:2379,https://etcd-2:2379", got)
	require.True(t, tlsEnabled)
}

func TestDebugEtcdctlOptionsPreservePassthroughOrder(t *testing.T) {
	cmd := Infer("debug etcdctl", "test", DebugEtcdctl)
	require.NoError(t, cmd.fs.Parse([]string{
		"get", "--prefix", "/", "--keys-only",
	}))

	opts := cmd.opts.Elem().Interface().(debugEtcdctlOptions)
	require.Equal(t, []string{
		"get", "--prefix", "/", "--keys-only",
	}, opts.etcdctlArgs())
}

func TestDebugEtcdctlMirenFlagsMustComeFirst(t *testing.T) {
	cmd := Infer("debug etcdctl", "test", DebugEtcdctl)
	require.NoError(t, cmd.fs.Parse([]string{
		"--socket", "/custom.sock", "get", "/", "--prefix", "--namespace", "custom",
	}))

	opts := cmd.opts.Elem().Interface().(debugEtcdctlOptions)
	require.Equal(t, "/custom.sock", opts.Socket)
	require.Equal(t, "miren", opts.Namespace)
	require.Equal(t, []string{
		"get", "/", "--prefix", "--namespace", "custom",
	}, opts.etcdctlArgs())
}

func TestLocalEtcdUnavailableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "container not found",
			err:  errdefs.ErrNotFound,
			want: `embedded etcd container "miren-etcd" was not found in containerd namespace "miren"`,
		},
		{
			name: "permission denied",
			err:  errors.New("dial unix /run/containerd.sock: connect: permission denied"),
			want: "permission denied accessing containerd at /run/containerd.sock",
		},
		{
			name: "connection failure",
			err:  errors.New("connection refused"),
			want: "this command must be run on a local Miren coordinator with embedded etcd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := localEtcdUnavailableError("/run/containerd.sock", "miren", tt.err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestLocalEtcdTaskUnavailableError(t *testing.T) {
	err := localEtcdTaskUnavailableError(errdefs.ErrNotFound)
	require.ErrorContains(t, err, `embedded etcd container "miren-etcd" exists but is not running`)
	require.ErrorContains(t, err, "check the Miren server and coordinator logs")
}
