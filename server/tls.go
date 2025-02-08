package server

import (
	"net/http"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

func (s *Server) ServeTLS() error {
	mgr := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(filepath.Join(s.DataPath, "certs")),
		Email:  "info@miren.dev",
	}

	s.Log.Info("serving TLS with autocert")

	go func() {
		err := http.Serve(mgr.Listener(), s.Ingress)
		if err != nil {
			s.Log.Error("error serving TLS", "error", err)
		}
	}()

	return nil
}
