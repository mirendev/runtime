package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const metadataServerPort = 7123

type metadataErrorResponse struct {
	Error string `json:"error"`
}

// metadataHandler is the workload-facing API. Each endpoint owns its method
// and payload; all of them share the same sandbox-bound authentication.
func (c *SandboxController) metadataHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/token", c.handleTokenRequest)
	mux.HandleFunc("/v1/activity", c.handleActivityRequest)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeMetadataError(w, http.StatusNotFound, "not found")
	})
	return mux
}

func (c *SandboxController) startMetadataServer(ctx context.Context) {
	listenAddr := fmt.Sprintf("%s:%d", c.Subnet.Router().Addr(), metadataServerPort)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           c.metadataHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	c.Log.Info("starting workload metadata server", "addr", listenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		c.Log.Error("metadata server failed", "error", err)
	}
}

func (c *SandboxController) authenticateWorkload(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		writeMetadataError(w, http.StatusBadRequest, "invalid remote address")
		return "", "", false
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeMetadataError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
		return "", "", false
	}
	bearerToken := strings.TrimPrefix(authHeader, "Bearer ")
	sandboxID, appName, ok := c.NetServ.LookupSandboxByIP(remoteHost)
	if !ok {
		writeMetadataError(w, http.StatusForbidden, "unknown source address")
		return "", "", false
	}
	if !c.verifyTokenSecret(sandboxID, bearerToken) {
		c.repairTokenSecret(sandboxID)
	}
	if !c.verifyTokenSecret(sandboxID, bearerToken) {
		// A recycled address can name its previous sandbox. Repair the mapping
		// from the entity store, then authenticate against the corrected owner.
		corrected, correctedApp, refreshed := c.refreshSandboxByIP(remoteHost)
		if refreshed && corrected != sandboxID && !c.verifyTokenSecret(corrected, bearerToken) {
			c.repairTokenSecret(corrected)
		}
		if !refreshed || corrected == sandboxID || !c.verifyTokenSecret(corrected, bearerToken) {
			c.Log.Warn("workload metadata request failed verification",
				"source_ip", remoteHost, "resolved_sandbox", sandboxID, "resolved_app", appName)
			writeMetadataError(w, http.StatusForbidden, "invalid token")
			return "", "", false
		}

		c.Log.Warn("corrected stale sandbox address mapping during metadata request",
			"source_ip", remoteHost,
			"stale_sandbox", sandboxID, "stale_app", appName,
			"sandbox", corrected, "app", correctedApp)
		sandboxID, appName = corrected, correctedApp
	}
	return sandboxID, appName, true
}

func writeMetadataError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(metadataErrorResponse{Error: msg})
}
