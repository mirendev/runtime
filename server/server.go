package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/addons"
	"miren.dev/runtime/api"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/dataset"
	"miren.dev/runtime/health"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/lease"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/run"
	"miren.dev/runtime/shell"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg server -input rpc.yml -output rpc.gen.go

type Server struct {
	Log *slog.Logger
	Reg *asm.Registry

	Port int `asm:"server_port"`

	HTTPAddress string `asm:"http-address"`

	DataPath string `asm:"data-path"`
	TempDir  string `asm:"tempdir"`

	LocalPath string `asm:"local-path,optional"`

	RequireClientCerts bool `asm:"require-client-certs,optional"`

	Build   *build.RPCBuilder
	Shell   *shell.RPCShell
	AppCrud *app.RPCCrud

	AppInfo *RPCAppInfo
	LogsRPC *RPCLogs
	Disks   *RPCDisks
	Addons  *RPCAddons

	AddonReg *addons.Registry

	Lease *lease.LaunchContainer

	Health *health.ContainerMonitor

	Ingress *ingress.LeaseHTTP

	RunSCMon *observability.RunSCMonitor

	ConStatTracker *lease.ContainerStatsTracker

	Logs *observability.LogsMaintainer

	CR *run.ContainerRunner

	DataSets *dataset.Manager

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte
}

func (s *Server) periodicIdleShutdown(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(60 * time.Second):
			s.Lease.ShutdownIdle(ctx)
		}
	}
}

const shutdownTimeout = 60 * time.Second

func (s *Server) Shutdown() {
	s.Log.Info("performing shutdown tasks", "timeout", shutdownTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	cnt, err := s.Lease.Shutdown(ctx)
	if err != nil {
		s.Log.Error("error shutting down idle containers", "error", err)
	}

	s.Log.Info("shutdown complete", "idle-containers", cnt)
}

const (
	day  = 24 * time.Hour
	year = 365 * day
)

func (s *Server) LoadCA(ctx context.Context) error {
	cert := filepath.Join(s.DataPath, "server", "ca.crt")
	keyPath := filepath.Join(s.DataPath, "server", "ca.key")

	if data, err := os.ReadFile(cert); err == nil {
		s.Log.Info("loading existing CA", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for CA: %w", err)
		}

		ca, err := caauth.LoadFromPEM(data, key)
		if err != nil {
			return fmt.Errorf("failed to load CA: %w", err)
		}

		s.authority = ca
	} else {
		s.Log.Info("generating new CA", "path", cert)

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

		s.authority = ca
	}

	return nil
}

func (s *Server) LoadAPICert(ctx context.Context) error {
	cert := filepath.Join(s.DataPath, "server", "api.crt")
	keyPath := filepath.Join(s.DataPath, "server", "api.key")

	if data, err := os.ReadFile(cert); err == nil {
		s.Log.Info("loading existing API cert", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for API cert: %w", err)
		}

		s.apiCert = data
		s.apiKey = key
		return nil
	}

	s.Log.Info("generating new API cert", "path", cert)

	cc, err := s.authority.IssueCertificate(caauth.Options{
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

	s.apiCert = cc.CertPEM
	s.apiKey = cc.KeyPEM

	return nil
}

func (s *Server) Setup(ctx context.Context) error {
	err := os.MkdirAll(s.DataPath, 0700)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Join(s.DataPath, "server"), 0700)
	if err != nil {
		return err
	}

	err = s.LoadCA(ctx)
	if err != nil {
		return err
	}

	err = s.LoadAPICert(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) LocalConfig() (*clientconfig.Config, error) {
	cc, err := s.authority.IssueCertificate(caauth.Options{
		CommonName:   "runtime-user",
		Organization: "miren",
		ValidFor:     1 * year,
	})

	if err != nil {
		return nil, err
	}

	return clientconfig.Local(cc, s.authority.GetCACertificate()), nil
}

func (s *Server) Run(ctx context.Context) error {
	defer s.Shutdown()

	opts := []rpc.StateOption{
		rpc.WithCertPEMs(s.apiCert, s.apiKey),
		rpc.WithCertificateVerification(s.authority.GetCACertificate()),
		rpc.WithBindAddr(fmt.Sprintf("0.0.0.0:%d", s.Port)),
		rpc.WithLogger(s.Log),
	}

	if s.LocalPath != "" {
		opts = append(opts, rpc.WithLocalServer(s.LocalPath))
	}

	if s.RequireClientCerts {
		opts = append(opts, rpc.WithRequireClientCerts)
	}

	ss, err := rpc.NewState(ctx, opts...)
	if err != nil {
		return err
	}

	defer ss.Close()

	s.Logs.Setup(ctx)

	s.Log.Info("starting runsc monitoring")

	err = s.RunSCMon.WritePodInit("/run/runsc-init.json")
	if err != nil {
		return err
	}

	go s.RunSCMon.Monitor(ctx)
	defer s.RunSCMon.Close()

	go s.Health.MonitorEvents(ctx)
	go s.periodicIdleShutdown(ctx)

	go s.ConStatTracker.Monitor(ctx)

	s.Lease.RecoverContainers(ctx)

	err = s.CR.RestoreContainers(ctx)
	if err != nil {
		return err
	}

	go s.CR.ReconcileLoop(ctx)

	serv := ss.Server()

	err = s.SetupBuiltinAddons()
	if err != nil {
		return err
	}

	s.Log.Info("exposing build service")

	serv.ExposeValue("build", build.AdaptBuilder(s.Build))
	serv.ExposeValue("app", app.AdaptCrud(s.AppCrud))
	serv.ExposeValue("app-info", api.AdaptAppInfo(s.AppInfo))
	serv.ExposeValue("logs", api.AdaptLogs(s.LogsRPC))
	serv.ExposeValue("shell", shell.AdaptShellAccess(s.Shell))
	serv.ExposeValue("user", api.AdaptUserQuery(s))
	serv.ExposeValue("disks", api.AdaptDisks(s.Disks))
	serv.ExposeValue("addons", api.AdaptAddons(s.Addons))
	serv.ExposeValue("dataset", dataset.AdaptDataSets(s.DataSets))

	go http.ListenAndServe(s.HTTPAddress, s.Ingress)

	s.Log.Info("server started", "rpc-port", s.Port, "http-port", s.HTTPAddress, "https-port", ":443")

	err = s.ServeTLS()
	if err != nil {
		return err
	}

	<-ctx.Done()

	return nil
}
