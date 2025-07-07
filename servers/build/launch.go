package build

import (
	"context"
	"log/slog"
	"maps"

	"github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/computex"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/imagerefs"
)

type LaunchBuildkit struct {
	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

type RunningBuildkit struct {
	*LaunchBuildkit

	addr string
	task client.Task
	id   string
}

type launchOptions struct {
	logEntity string
	cacheDir  string
	attrs     map[string]string
}

type LaunchOption func(*launchOptions)

func WithLogEntity(logEntity string) LaunchOption {
	return func(lo *launchOptions) {
		lo.logEntity = logEntity
	}
}

func WithLogAttrs(attrs map[string]string) LaunchOption {
	return func(lo *launchOptions) {
		lo.attrs = maps.Clone(attrs)
	}
}

func (l *LaunchBuildkit) generateConfig() string {
	str := `
# debug enables additional debug logging
debug = true
# trace enables additional trace logging (very verbose, with potential performance impacts)
trace = false
root = "/var/lib/buildkit"
insecure-entitlements = [ "network.host", "security.insecure" ]

[log]
  # log formatter: json or text
  format = "text"

[dns]
  nameservers=["1.1.1.1","8.8.8.8"]
  options=["edns0"]
  searchDomains=["example.com"]

[grpc]
  address = [ "tcp://0.0.0.0:3000" ]
  # debugAddress is address for attaching go profiles and debuggers.
		# debugAddress = "0.0.0.0:6060"
  uid = 0
  gid = 0

# config for build history API that stores information about completed build commands
[history]
  # maxAge is the maximum age of history entries to keep, in seconds.
  maxAge = 172800
  # maxEntries is the maximum number of history entries to keep.
  maxEntries = 50

# registry configures a new Docker register used for cache import or output.
[registry."docker.io"]
  http = true

# optionally mirror configuration can be done by defining it as a registry.
[registry."cluster.local:5000"]
	insecure = true
  http = true
`

	return str
}

func (l *LaunchBuildkit) Launch(ctx context.Context, addr string, lo ...LaunchOption) (*RunningBuildkit, error) {
	var opts launchOptions
	for _, o := range lo {
		o(&opts)
	}

	var sb compute_v1alpha.Sandbox
	sb.LogEntity = "build"
	if opts.logEntity != "" {
		sb.LogEntity = opts.logEntity
	}

	for k, v := range opts.attrs {
		sb.LogAttribute = append(sb.LogAttribute, types.Label{
			Key:   k,
			Value: v,
		})
	}

	sb.StaticHost = append(sb.StaticHost, compute_v1alpha.StaticHost{
		Host: "cluster.local",
		Ip:   addr,
	})
	sb.Container = append(sb.Container, compute_v1alpha.Container{
		Name:       "app",
		Image:      imagerefs.BuildKit,
		Privileged: true,
		Port: []compute_v1alpha.Port{
			{
				Port: 3000,
				Name: "http",
				Type: "http",
			},
		},
		ConfigFile: []compute_v1alpha.ConfigFile{
			{
				Path: "/etc/buildkit/buildkitd.toml",
				Mode: "0644",
				Data: l.generateConfig(),
			},
		},
	})

	ver := idgen.Gen("buildkit")

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: ver,
		}).Encode,
		sb.Encode,
	))

	pr, err := l.eac.Put(ctx, &rpcE)
	if err != nil {
		l.log.Error("error creating sandbox", "error", err)
		return nil, err
	}

	ep, _, err := computex.WaitForSandbox(ctx, pr.Id(), l.eac)
	if err != nil {
		l.log.Error("error waiting for sandbox", "error", err)
		return nil, err
	}

	l.log.Info("buildkit started", "address", addr)

	return &RunningBuildkit{
		LaunchBuildkit: l,
		id:             pr.Id(),
		addr:           ep + ":3000",
	}, nil
}

func (l *RunningBuildkit) Client(ctx context.Context) (*buildkit.Client, error) {
	bk, err := buildkit.New(ctx, "tcp://"+l.addr)
	return bk, err
}

func (l *RunningBuildkit) Close(ctx context.Context) error {
	_, err := l.eac.Delete(ctx, l.id)
	return err
}
