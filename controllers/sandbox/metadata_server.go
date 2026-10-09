package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"miren.dev/runtime/network"
	"miren.dev/runtime/servers/metricspush"
)

const metadataServerPort = network.TokenServerPort

type metadataErrorResponse struct {
	Error string `json:"error"`
}

// metadataHandler is the workload-facing API. Each endpoint owns its method
// and payload; all of them share the same sandbox-bound authentication.
func (c *SandboxController) metadataHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/token", c.handleTokenRequest)
	mux.HandleFunc("/v1/activity", c.handleActivityRequest)
	if c.metricsPushEnabled() {
		metricspush.NewRelay(c.Log, c.relayAuthenticator, c.WorkloadIssuer, c.MetricsPusher).Register(mux)
	}
	mux.HandleFunc("/v1/sessions", c.handleSessionsRequest)
	mux.HandleFunc("/v1/sessions/activity", c.handleSessionActivity)
	mux.HandleFunc("/v1/sessions/deletions/ack", c.handleSessionAcknowledgment)
	mux.HandleFunc("/v1/sessions/detachments/ack", c.handleSessionAcknowledgment)
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
	sandboxID, appName, err := c.authenticateSandbox(remoteHost, bearerToken)
	if err != nil {
		message := "invalid token"
		if errors.Is(err, errUnknownSource) {
			message = "unknown source address"
		}
		writeMetadataError(w, http.StatusForbidden, message)
		return "", "", false
	}
	return sandboxID, appName, true
}

var (
	errUnknownSource = errors.New("unknown source address")
	errBadSecret     = errors.New("secret does not match the calling sandbox")
)

func (c *SandboxController) authenticateSandbox(remoteHost, bearerToken string) (string, string, error) {
	sandboxID, appName, ok := c.NetServ.LookupSandboxByIP(remoteHost)
	if !ok {
		return "", "", errUnknownSource
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
			return "", "", errBadSecret
		}

		c.Log.Warn("corrected stale sandbox address mapping during metadata request",
			"source_ip", remoteHost,
			"stale_sandbox", sandboxID, "stale_app", appName,
			"sandbox", corrected, "app", correctedApp)
		sandboxID, appName = corrected, correctedApp
	}
	return sandboxID, appName, nil
}

func writeMetadataError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(metadataErrorResponse{Error: msg})
}
