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

type Server struct {
	Log  *slog.Logger
	Port int `asm:"server_port"`

	Build   *build.RPCBuilder
	Shell   *shell.RPCShell
	AppCrud *app.RPCCrud

	Lease *lease.LaunchContainer

	Health *health.ContainerMonitor

	Ingress *ingress.LeaseHTTP

	RunSCMon *observability.RunSCMonitor
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

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify,
		rpc.WithBindAddr(fmt.Sprintf("127.0.0.1:%d", s.Port)),
		rpc.WithLogger(s.Log),
	)
	if err != nil {
		return err
	}

	defer ss.Close()

	s.Log.Info("starting runsc monitoring")

	err = s.RunSCMon.WritePodInit("/run/runsc-init.json")
	if err != nil {
		return err
	}

	go s.RunSCMon.Monitor(ctx)
	defer s.RunSCMon.Close()

	go s.Health.MonitorEvents(ctx)
	go s.periodicIdleShutdown(ctx)

	s.Lease.RecoverContainers(ctx)

	serv := ss.Server()

	s.Log.Info("exposing build service")

	serv.ExposeValue("build", build.AdaptBuilder(s.Build))
	serv.ExposeValue("app", app.AdaptCrud(s.AppCrud))
	serv.ExposeValue("shell", shell.AdaptShellAccess(s.Shell))

	go http.ListenAndServe(":8080", s.Ingress)

	s.Log.Info("server started", "rpc-port", s.Port, "http-port", ":8080")

	<-ctx.Done()

	return ctx.Err()
}
