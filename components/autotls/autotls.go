package autotls

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/crypto/acme"
)

// CertificateProvider provides certificates via GetCertificate callback
type CertificateProvider interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// HTTPChallengeProvider is an optional interface that CertificateProviders
// can implement to handle HTTP-01 ACME challenge requests on port 80.
type HTTPChallengeProvider interface {
	HTTPHandler(fallback http.Handler) http.Handler
}

// ServeTLSWithController serves HTTPS using certificates provided by a controller.
// If the certProvider also implements HTTPChallengeProvider, the port-80 handler
// wraps the redirect handler to serve ACME HTTP-01 challenges.
func ServeTLSWithController(ctx context.Context, log *slog.Logger, certProvider CertificateProvider, h http.Handler) error {
	log = log.With("module", "autotls", "mode", "controller")
	log.Info("serving TLS with certificate controller")

	tlsConfig := &tls.Config{
		GetCertificate: certProvider.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
	}

	server := &http.Server{
		Addr:              ":443",
		Handler:           h,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting HTTPS server", "addr", ":443")
		err := server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTPS", "error", err)
		}
	}()

	// Build the port-80 handler: HTTPS redirect, optionally wrapped with ACME challenges
	redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hostWithoutPort, _, err := net.SplitHostPort(host); err == nil {
			host = hostWithoutPort
		}

		isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"
		isIPAddress := net.ParseIP(host) != nil

		if isLocalhost || isIPAddress {
			h.ServeHTTP(w, r)
			return
		}

		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Use HTTPS", http.StatusBadRequest)
			return
		}

		target := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusFound)
	})

	var port80Handler http.Handler = redirectHandler
	if challenger, ok := certProvider.(HTTPChallengeProvider); ok {
		port80Handler = challenger.HTTPHandler(redirectHandler)
		log.Info("ACME HTTP-01 challenge handler enabled on port 80")
	}

	httpServer := &http.Server{
		Addr:              ":80",
		Handler:           port80Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Info("starting HTTP server for HTTPS redirect", "addr", ":80")
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTP", "error", err)
		}
	}()

	// Monitor for context cancellation
	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTPS and HTTP servers")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTPS server shutdown error", "error", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP server shutdown error", "error", err)
		}
		log.Info("HTTPS and HTTP servers shutdown complete")
	}()

	return nil
}

// ServeTLSWithControllerOnAddr serves HTTPS on addr without binding port 80.
// Because port 80 is not bound, ACME HTTP-01 and TLS-ALPN-01 challenges cannot
// succeed in this mode; certificates must be issued via DNS-01.
func ServeTLSWithControllerOnAddr(ctx context.Context, log *slog.Logger, certProvider CertificateProvider, h http.Handler, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	return serveTLSOnListener(ctx, log, certProvider, h, ln)
}

func serveTLSOnListener(ctx context.Context, log *slog.Logger, certProvider CertificateProvider, h http.Handler, ln net.Listener) error {
	log = log.With("module", "autotls", "mode", "controller", "addr", ln.Addr().String())
	log.Info("serving TLS with certificate controller")

	tlsConfig := &tls.Config{
		GetCertificate: certProvider.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1"},
	}

	server := &http.Server{
		Handler:           h,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		err := server.ServeTLS(ln, "", "")
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTPS", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTPS server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTPS server shutdown error", "error", err)
		}
	}()

	return nil
}
