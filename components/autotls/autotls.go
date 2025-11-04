package autotls

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

func ServeTLS(ctx context.Context, log *slog.Logger, dataPath string, h http.Handler) error {
	mgr := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(filepath.Join(dataPath, "certs")),
		// TODO set HostPolicy to only allow certain domains
	}

	log.Info("serving TLS with autocert")

	server := &http.Server{
		Handler: h,
	}

	go func() {
		err := server.Serve(mgr.Listener())
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving TLS", "error", err)
		}
	}()

	// Start HTTP server on port 80 for ACME challenges and HTTP to HTTPS redirect
	httpServer := &http.Server{
		Addr:              ":80",
		Handler:           mgr.HTTPHandler(nil),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Info("starting HTTP server for ACME challenges and HTTPS redirect", "addr", ":80")
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTP", "error", err)
		}
	}()

	// Monitor for context cancellation and gracefully shutdown both servers
	go func() {
		<-ctx.Done()
		log.Info("shutting down TLS and HTTP servers")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown both servers
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("TLS server shutdown error", "error", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP server shutdown error", "error", err)
		}
		log.Info("TLS and HTTP servers shutdown complete")
	}()

	return nil
}
