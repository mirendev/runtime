package build

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"time"

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
	id   string
}

type launchOptions struct {
	logEntity string
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
	l.log.Info("starting buildkit launch", "clusterAddr", addr)
	var opts launchOptions
	for _, o := range lo {
		o(&opts)
	}
	l.log.Debug("launch options processed", "logEntity", opts.logEntity, "attrs", opts.attrs)

	l.log.Debug("creating sandbox configuration")
	var sb compute_v1alpha.Sandbox
	sb.LogEntity = "build"
	if opts.logEntity != "" {
		sb.LogEntity = opts.logEntity
	}
	l.log.Debug("sandbox log entity set", "logEntity", sb.LogEntity)

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
	l.log.Debug("configuring buildkit container", "image", imagerefs.BuildKit)
	config := l.generateConfig()
	l.log.Debug("generated buildkit config", "configSize", len(config))
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
				Data: strings.TrimSpace(config),
			},
		},
	})

	ver := idgen.GenNS("sb")
	l.log.Info("creating buildkit sandbox entity", "name", ver)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: ver,
		}).Encode,
		entity.Ident, "sandbox/"+ver,
		sb.Encode,
	).Attrs())

	l.log.Debug("putting sandbox entity to entity store")
	pr, err := l.eac.Put(ctx, &rpcE)
	if err != nil {
		l.log.Error("error creating sandbox", "error", err)
		return nil, err
	}

	l.log.Info("sandbox entity created, waiting for it to be ready", "entityId", pr.Id())
	ep, _, err := computex.WaitForSandbox(ctx, pr.Id(), l.eac)
	if err != nil {
		l.log.Error("error waiting for sandbox", "error", err, "entityId", pr.Id())
		return nil, err
	}
	l.log.Debug("sandbox ready", "endpoint", ep)

	l.log.Info("buildkit started", "address", addr, "endpoint", ep+":3000")

	// Create the running buildkit instance
	rb := &RunningBuildkit{
		LaunchBuildkit: l,
		id:             pr.Id(),
		addr:           ep + ":3000",
	}

	// Verify connectivity before returning
	l.log.Debug("verifying buildkit connectivity")
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	testClient, err := rb.Client(testCtx)
	if err != nil {
		l.log.Error("buildkit started but cannot connect", "error", err, "addr", rb.addr)
		// Don't fail here, just log - the caller can retry
	} else {
		l.log.Info("buildkit connectivity verified", "addr", rb.addr)
		_ = testClient.Close()
	}

	return rb, nil
}

func (l *RunningBuildkit) Client(ctx context.Context) (*buildkit.Client, error) {
	l.log.Info("attempting to create buildkit client", "addr", l.addr)

	// Add retry logic with exponential backoff
	var bk *buildkit.Client
	var err error
	maxRetries := 5
	retryDelay := time.Second

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			l.log.Debug("retrying buildkit connection", "attempt", i+1, "delay", retryDelay)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			retryDelay *= 2 // exponential backoff
		}

		l.log.Debug("connecting to buildkit", "addr", l.addr, "attempt", i+1)
		bk, err = buildkit.New(ctx, "tcp://"+l.addr)
		if err == nil {
			l.log.Info("buildkit client created successfully", "addr", l.addr, "attempts", i+1)
			return bk, nil
		}

		l.log.Warn("failed to create buildkit client",
			"error", err,
			"addr", l.addr,
			"attempt", i+1,
			"willRetry", i < maxRetries-1)
	}

	l.log.Error("failed to create buildkit client after all retries",
		"error", err,
		"addr", l.addr,
		"maxRetries", maxRetries)
	return nil, err
}

func (l *RunningBuildkit) Close(ctx context.Context) error {
	_, err := l.eac.Delete(ctx, l.id)
	return err
}
