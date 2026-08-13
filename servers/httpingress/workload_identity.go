package httpingress

import (
	"net"
	"net/http"
)

func (h *Server) isIssuerHost(reqHost string) bool {
	if h.workloadIssuer == nil {
		return false
	}
	host, _, err := net.SplitHostPort(reqHost)
	if err != nil {
		host = reqHost
	}

	// Matches a superseded anchor too, so a verifier pinned to the previous
	// issuer can still fetch keys for tokens minted before the anchor moved.
	for _, issuerHost := range h.workloadIssuer.Hostnames() {
		issuerHostOnly, _, err := net.SplitHostPort(issuerHost)
		if err != nil {
			issuerHostOnly = issuerHost
		}
		if host == issuerHostOnly {
			return true
		}
	}
	return false
}

func (h *Server) handleOIDCDiscovery(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.workloadIssuer == nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(h.workloadIssuer.DiscoveryDocument())
}

func (h *Server) handleJWKS(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.workloadIssuer == nil {
		http.NotFound(w, req)
		return
	}
	data, err := h.workloadIssuer.JWKSDocument()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}
