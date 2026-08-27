//go:build linux

package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"miren.dev/runtime/pkg/readiness"
)

// pprofAddr is localhost-only. A second server on the same host runs without
// pprof and logs the bind failure instead of failing startup.
const pprofAddr = "127.0.0.1:6060"

type pprofBoot struct {
	component     *readiness.Component
	observability *observabilityBoot
}

func newPprofBoot(observability *observabilityBoot) *pprofBoot {
	b := &pprofBoot{observability: observability}
	b.component = readiness.NewComponent("pprof", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(observability.component)},
		Start:        b.start,
	})
	return b
}

func (b *pprofBoot) start(ctx context.Context, _ readiness.Reporter) error {
	startPprofServer(ctx, b.observability.output().log)
	return nil
}

func startPprofServer(ctx context.Context, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	listener, err := net.Listen("tcp", pprofAddr)
	if err != nil {
		log.Error("pprof debug server not started", "addr", pprofAddr, "err", err)
		return
	}

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("pprof debug server exited", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Warn("pprof debug server shutdown", "err", err)
		}
	}()
}
