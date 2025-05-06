package coordinate

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	"miren.dev/runtime/servers/entityserver"
	execproxy "miren.dev/runtime/servers/exec_proxy"
)

type CoordinatorConfig struct {
	Address       string              `json:"address" yaml:"address"`
	EtcdEndpoints []string            `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix        string              `json:"prefix" yaml:"prefix"`
	Resolver      netresolve.Resolver `json:"resolver" yaml:"resolver"`
	TempDir       string              `json:"temp_dir" yaml:"temp_dir"`
	DataPath      string              `json:"data_path" yaml:"data_path"`

	Mem *metrics.MemoryUsage
	Cpu *metrics.CPUUsage
}

func NewCoordinator(log *slog.Logger, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{
		CoordinatorConfig: cfg,
		Log:               log,
	}
}

type Coordinator struct {
	CoordinatorConfig

	Log *slog.Logger

	state *rpc.State

	aa activator.AppActivator

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte
}

func (c *Coordinator) Activator() activator.AppActivator {
	return c.aa
}

const (
	day  = 24 * time.Hour
	year = 365 * day
)

func (c *Coordinator) LoadCA(ctx context.Context) error {
	cert := filepath.Join(c.DataPath, "server", "ca.crt")
	keyPath := filepath.Join(c.DataPath, "server", "ca.key")

	if data, err := os.ReadFile(cert); err == nil {
		c.Log.Info("loading existing CA", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for CA: %w", err)
		}

		ca, err := caauth.LoadFromPEM(data, key)
		if err != nil {
			return fmt.Errorf("failed to load CA: %w", err)
		}

		c.authority = ca
	} else {
		c.Log.Info("generating new CA", "path", cert)

		ca, err := caauth.New(caauth.Options{
			CommonName:   "runtime-server",
			Organization: "miren",
			ValidFor:     10 * year,
		})
		if err != nil {
			return fmt.Errorf("failed to generate CA: %w", err)
		}

		err = os.MkdirAll(filepath.Dir(cert), 0755)
		if err != nil {
			return fmt.Errorf("failed to create CA directory: %w", err)
		}

		cd, kd, err := ca.ExportPEM()
		if err != nil {
			return fmt.Errorf("failed to export CA: %w", err)
		}

		err = os.WriteFile(cert, cd, 0644)
		if err != nil {
			return fmt.Errorf("failed to write CA cert: %w", err)
		}

		err = os.WriteFile(keyPath, kd, 0600)
		if err != nil {
			return fmt.Errorf("failed to write CA key: %w", err)
		}

		c.authority = ca
	}

	return nil
}

func (c *Coordinator) LoadAPICert(ctx context.Context) error {
	cert := filepath.Join(c.DataPath, "server", "api.crt")
	keyPath := filepath.Join(c.DataPath, "server", "api.key")

	if data, err := os.ReadFile(cert); err == nil {
		c.Log.Info("loading existing API cert", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for API cert: %w", err)
		}

		c.apiCert = data
		c.apiKey = key
		return nil
	}

	c.Log.Info("generating new API cert", "path", cert)

	cc, err := c.authority.IssueCertificate(caauth.Options{
		CommonName:   "runtime-api",
		Organization: "miren",
		ValidFor:     1 * year,
		IPs: []net.IP{
			net.ParseIP("127.0.0.1"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to generate API cert: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(cert), 0755)
	if err != nil {
		return fmt.Errorf("failed to create API directory: %w", err)
	}

	err = os.WriteFile(cert, cc.CertPEM, 0644)
	if err != nil {
		return fmt.Errorf("failed to write API cert: %w", err)
	}

	err = os.WriteFile(keyPath, cc.KeyPEM, 0600)
	if err != nil {
		return fmt.Errorf("failed to write API key: %w", err)
	}

	c.apiCert = cc.CertPEM
	c.apiKey = cc.KeyPEM

	return nil
}

func (c *Coordinator) LocalConfig() (*clientconfig.Config, error) {
	cc, err := c.authority.IssueCertificate(caauth.Options{
		CommonName:   "runtime-user",
		Organization: "miren",
		ValidFor:     1 * year,
	})

	if err != nil {
		return nil, err
	}

	return clientconfig.Local(cc, c.authority.GetCACertificate()), nil
}

func (c *Coordinator) Start(ctx context.Context) error {
	c.Log.Info("starting coordinator", "address", c.Address, "etcd_endpoints", c.EtcdEndpoints, "prefix", c.Prefix)

	err := c.LoadCA(ctx)
	if err != nil {
		c.Log.Error("failed to load CA", "error", err)
		return err
	}

	err = c.LoadAPICert(ctx)
	if err != nil {
		c.Log.Error("failed to load CA", "error", err)
		return err
	}

	rs, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		//rpc.WithCertPEMs(c.apiCert, c.apiKey),
		//rpc.WithCertificateVerification(c.authority.GetCACertificate()),
		rpc.WithBindAddr(c.Address),
		rpc.WithLogger(c.Log),
	)
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}
	c.state = rs

	server := rs.Server()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:        c.EtcdEndpoints,
		AutoSyncInterval: time.Minute,
	})
	if err != nil {
		c.Log.Error("failed to create etcd client", "error", err)
		return err
	}

	etcdStore, err := entity.NewEtcdStore(ctx, c.Log, client, c.Prefix)
	if err != nil {
		c.Log.Error("failed to create etcd store", "error", err)
		return err
	}

	err = schema.Apply(ctx, etcdStore)
	if err != nil {
		c.Log.Error("failed to apply schema", "error", err)
		return err
	}

	ess, err := entityserver.NewEntityServer(c.Log, etcdStore)
	if err != nil {
		c.Log.Error("failed to create entity server", "error", err)
		return err
	}

	server.ExposeValue("entities", esv1.AdaptEntityAccess(ess))

	loopback, err := rs.Connect(c.Address, "entities")
	if err != nil {
		c.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(loopback)

	var (
		defPro core_v1alpha.Project
		defNet entityserver_v1alpha.Entity
	)

	defNet.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: "default",
		}).Encode,
		defPro.Encode,
		entity.Ident, "project/default",
	))

	_, err = eac.Put(ctx, &defNet)
	if err != nil {
		c.Log.Error("failed to create default project", "error", err)
		return err
	}

	aa := activator.NewLocalActivator(ctx, c.Log, eac)
	c.aa = aa

	eps := execproxy.NewServer(c.Log, eac, rs, aa)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(eps))

	bs := build.NewBuilder(c.Log, eac, c.Resolver, c.TempDir)
	server.ExposeValue("dev.miren.runtime/build", build_v1alpha.AdaptBuilder(bs))

	ec := aes.NewClient(c.Log, eac)

	ai := app.NewAppInfo(c.Log, ec, c.Cpu, c.Mem)
	server.ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(ai))
	server.ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(ai))

	c.Log.Info("started RPC server")
	return nil
}

func (c *Coordinator) Server() *rpc.Server {
	return c.state.Server()
}
