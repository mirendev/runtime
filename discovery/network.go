package discovery

import (
	"net/http"
	"net/http/httputil"
	"strings"
)

type HTTPEndpoint struct {
	Host string
}

func (h *HTTPEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var rp httputil.ReverseProxy
	rp.Director = h.redirect
	rp.ServeHTTP(w, req)
}

func (h *HTTPEndpoint) redirect(req *http.Request) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(h.Host, "http://")
}
