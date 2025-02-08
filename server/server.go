package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"miren.dev/runtime/app"
	"miren.dev/runtime/build"
	"miren.dev/runtime/health"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/lease"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/shell"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg server -input rpc.yml -output rpc.gen.go

type Server struct {
	Log  *slog.Logger
	Port int `asm:"server_port"`

	DataPath string `asm:"data-path"`

	LocalPath string `asm:"local-path,optional"`

	Build   *build.RPCBuilder
	Shell   *shell.RPCShell
	AppCrud *app.RPCCrud

	AppInfo *RPCAppInfo
	LogsRPC *RPCLogs

	Lease *lease.LaunchContainer

	Health *health.ContainerMonitor

	Ingress *ingress.LeaseHTTP

	RunSCMon *observability.RunSCMonitor

	ConStatTracker *lease.ContainerStatsTracker

	Logs *observability.LogsMaintainer
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

func (s *Server) Run(ctx context.Context) error {
	defer s.Shutdown()

	opts := []rpc.StateOption{
		rpc.WithSkipVerify,
		rpc.WithBindAddr(fmt.Sprintf("127.0.0.1:%d", s.Port)),
		rpc.WithLogger(s.Log),
	}

	if s.LocalPath != "" {
		opts = append(opts, rpc.WithLocalServer(s.LocalPath))
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

	serv := ss.Server()

	s.Log.Info("exposing build service")

	serv.ExposeValue("build", build.AdaptBuilder(s.Build))
	serv.ExposeValue("app", app.AdaptCrud(s.AppCrud))
	serv.ExposeValue("app-info", AdaptAppInfo(s.AppInfo))
	serv.ExposeValue("logs", AdaptLogs(s.LogsRPC))
	serv.ExposeValue("shell", shell.AdaptShellAccess(s.Shell))

	go http.ListenAndServe(":8080", s.Ingress)

	s.Log.Info("server started", "rpc-port", s.Port, "http-port", ":8080")

	err = s.ServeTLS()
	if err != nil {
		return err
	}

	<-ctx.Done()

	return nil
}
