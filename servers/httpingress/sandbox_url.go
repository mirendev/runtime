package httpingress

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// isMirenInternalPath reports whether a decoded request path falls under
// the endpoints the ingress serves itself rather than forwarding to apps.
func isMirenInternalPath(reqPath string) bool {
	clean := path.Clean("/" + reqPath)
	return clean == "/.well-known/miren" || strings.HasPrefix(clean, "/.well-known/miren/")
}

// joinSandboxURL joins a sandbox base URL ("http://10.0.0.5:8080") with an
// origin-form path and query. The host always comes from the sandbox URL:
// a target such as "@other-host/" is rejected rather than being allowed to
// change where the request goes.
func joinSandboxURL(sandboxURL, pathAndQuery string) (string, error) {
	base, err := url.Parse(sandboxURL)
	if err != nil {
		return "", fmt.Errorf("invalid sandbox URL: %w", err)
	}

	if base.Host == "" {
		return "", fmt.Errorf("sandbox URL %q has no host", sandboxURL)
	}

	if !strings.HasPrefix(pathAndQuery, "/") {
		return "", fmt.Errorf("request path must begin with /")
	}

	target, err := url.ParseRequestURI(pathAndQuery)
	if err != nil {
		return "", fmt.Errorf("invalid request path: %w", err)
	}

	if target.Scheme != "" || target.Host != "" || target.User != nil {
		return "", fmt.Errorf("request path must not name a host")
	}

	u := url.URL{
		Scheme:   base.Scheme,
		Host:     base.Host,
		Path:     target.Path,
		RawPath:  target.RawPath,
		RawQuery: target.RawQuery,
	}

	return u.String(), nil
}
