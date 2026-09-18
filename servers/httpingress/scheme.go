package httpingress

import (
	"context"
	"net/http"
	"strings"
)

type originSchemeKey struct{}

// WithOriginScheme records the scheme a visitor used, as determined by a
// trusted in-process proxy that terminated their TLS (the Anywhere POP
// forwarder). requestScheme honors it ahead of headers and connection state,
// so it must only ever be set from code paths a client cannot reach directly.
func WithOriginScheme(ctx context.Context, scheme string) context.Context {
	return context.WithValue(ctx, originSchemeKey{}, scheme)
}

// requestScheme reports the scheme the client originally used, "http" or
// "https". The scheme picks the cached auth handler and decides whether the
// cookies it emits are Secure, so it has to come from something the client
// cannot forge, in order of preference: a scheme stamped by a trusted
// in-process proxy (WithOriginScheme), the front proxy's X-Forwarded-Proto or
// Forwarded header when the server is configured behind one, and finally the
// connection's own TLS state. Unrecognized header values are ignored rather
// than passed through.
func (s *Server) requestScheme(r *http.Request) string {
	if scheme, ok := r.Context().Value(originSchemeKey{}).(string); ok {
		return scheme
	}

	if s.config.TrustProxyHeaders {
		if proto, ok := ForwardedProto(r); ok {
			return proto
		}
	}

	if r.TLS != nil {
		return "https"
	}

	return "http"
}

// ForwardedProto extracts the scheme a front proxy reported, preferring
// X-Forwarded-Proto over the RFC 7239 Forwarded header. Chained proxies
// append rather than replace, so only the first element of either header
// (the hop nearest the client) is consulted. Callers decide whether the
// peer is trusted; this only parses.
func ForwardedProto(r *http.Request) (string, bool) {
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		first, _, _ := strings.Cut(xfp, ",")
		if proto, ok := normalizeScheme(first); ok {
			return proto, true
		}
	}

	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		for part := range strings.SplitSeq(first, ";") {
			part = strings.TrimSpace(part)
			if after, ok := strings.CutPrefix(strings.ToLower(part), "proto="); ok {
				return normalizeScheme(strings.Trim(after, `"`))
			}
		}
	}

	return "", false
}

func normalizeScheme(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "http":
		return "http", true
	case "https":
		return "https", true
	}
	return "", false
}
