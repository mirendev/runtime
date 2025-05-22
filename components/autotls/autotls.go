package autotls

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

func ServeTLS(log *slog.Logger, dataPath string, h http.Handler) error {
	mgr := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(filepath.Join(dataPath, "certs")),
		Email:  "info@miren.dev",
		// TODO set HostPolicy to only allow certain domains
	}

	log.Info("serving TLS with autocert")

	go func() {
		err := http.Serve(mgr.Listener(), h)
		if err != nil {
			log.Error("error serving TLS", "error", err)
		}
	}()

	return nil
}
