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
		Email:  "info@miren.dev",
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

	// Monitor for context cancellation and gracefully shutdown the server
	go func() {
		<-ctx.Done()
		log.Info("shutting down TLS server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("TLS server shutdown error", "error", err)
		}
		log.Info("TLS server shutdown complete")
	}()

	return nil
}
