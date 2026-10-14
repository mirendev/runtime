// Package sessions manages app-scoped Sessions through the Miren coordinator
// REST API. It is not the sandbox-local workload metadata client.
package sessions

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config selects one app on a coordinator. URL must be an HTTPS origin.
type Config struct {
	URL string
	App string
	// TokenPath is re-read on every request to pick up workload token rotation.
	// Set exactly one of TokenPath or Token. Token is for callers outside Miren.
	TokenPath string
	Token     string
	// CACertPath adds the cluster CA to the system roots when HTTPClient is nil.
	CACertPath string
	// HTTPClient overrides transport and timeout, but redirects are still refused.
	// The default verifies TLS and has a 15-second timeout.
	HTTPClient *http.Client
}

// ConfigFromEnv uses the coordinator address, mounted identity token, and CA
// injected into a Miren workload. The caller supplies the app's short name.
func ConfigFromEnv(app string) Config {
	address := os.Getenv("MIREN_API_ADDRESS")
	if address != "" {
		address = "https://" + address
	}
	return Config{URL: address, App: app, TokenPath: os.Getenv("MIREN_IDENTITY_TOKEN_PATH"), CACertPath: os.Getenv("MIREN_CA_CERT_PATH")}
}

type DesiredState string

const (
	Running   DesiredState = "running"
	Suspended DesiredState = "suspended"
)

// Session is the coordinator's lifecycle summary, not an execution spec.
// ID is the full session/app/name ID; App and Version are also entity IDs.
type Session struct {
	ID                    string       `json:"id"`
	App                   string       `json:"app"`
	Version               string       `json:"version"`
	Service               string       `json:"service"`
	Group                 string       `json:"group"`
	MaxSessionsPerSandbox int64        `json:"max_sessions_per_sandbox"`
	DesiredState          DesiredState `json:"desired_state"`
	Phase                 string       `json:"phase"`
	Sandbox               string       `json:"sandbox"`
	Failure               string       `json:"failure"`
	IdleTimeoutSeconds    int64        `json:"idle_timeout_seconds"`
	Activity              string       `json:"activity"`
}

// CreateOptions leaves defaults to the coordinator: a generated name, service
// web, capacity one, and a five-minute idle timeout. Explicit zero idle timeout
// disables parking; nil uses the default. Group is an opaque sharing key within
// the app and service. Image, environment, and command come from the app model.
type CreateOptions struct {
	Name                  string `json:"name,omitempty"`
	Service               string `json:"service,omitempty"`
	Group                 string `json:"group,omitempty"`
	MaxSessionsPerSandbox int64  `json:"max_sessions_per_sandbox,omitempty"`
	IdleTimeoutSeconds    *int64 `json:"idle_timeout_seconds,omitempty"`
}

var (
	ErrNotFound = errors.New("sessions: not found")
	ErrConflict = errors.New("sessions: conflict")
)

// HTTPError exposes the coordinator error envelope. Message can contain server
// data; Error deliberately omits it so ordinary error logs do not leak it.
type HTTPError struct {
	StatusCode int    `json:"-"`
	Message    string `json:"error"`
	Code       string `json:"code"`
	Category   string `json:"category"`
}

func (e *HTTPError) Error() string { return fmt.Sprintf("sessions: HTTP %d", e.StatusCode) }

func (e *HTTPError) Is(target error) bool {
	return (target == ErrNotFound && e.StatusCode == http.StatusNotFound) ||
		(target == ErrConflict && e.StatusCode == http.StatusConflict)
}

// Client is safe for concurrent use. Methods do not automatically retry.
type Client struct {
	base, app, tokenPath, token string
	http                        *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("sessions: coordinator URL must be an HTTPS origin")
	}
	if !validName(cfg.App) {
		return nil, fmt.Errorf("sessions: invalid app name")
	}
	if (cfg.TokenPath == "") == (cfg.Token == "") {
		return nil, fmt.Errorf("sessions: set exactly one of TokenPath or Token")
	}
	h := &http.Client{Timeout: 15 * time.Second}
	if cfg.HTTPClient != nil {
		*h = *cfg.HTTPClient
	} else if cfg.CACertPath != "" {
		data, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("sessions: cannot read cluster CA")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("sessions: cannot load system CAs")
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("sessions: invalid cluster CA")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		h.Transport = transport
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: u.Scheme + "://" + u.Host + "/api/v1/apps/" + url.PathEscape(cfg.App) + "/sessions", app: cfg.App, tokenPath: cfg.TokenPath, token: cfg.Token, http: h}, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/:")
}

// Accept short names or full IDs belonging to this client's app, never a path
// to another app. This lets callers pass the ID returned by Create or List.
func (c *Client) path(id string) (string, error) {
	name := strings.TrimPrefix(id, "session/"+c.app+"/")
	if !validName(name) {
		return "", fmt.Errorf("sessions: expected a Session name or an ID in app %q", c.app)
	}
	return "/" + url.PathEscape(name), nil
}

func (c *Client) request(ctx context.Context, method, path string, body, result any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	token := c.token
	if c.tokenPath != "" {
		data, err := os.ReadFile(c.tokenPath)
		if err != nil {
			return fmt.Errorf("sessions: cannot read workload identity")
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return fmt.Errorf("sessions: empty identity token")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sessions: coordinator request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &HTTPError{StatusCode: resp.StatusCode}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(apiErr)
		return apiErr
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("sessions: invalid coordinator response")
		}
	}
	return nil
}

func (c *Client) Create(ctx context.Context, opts CreateOptions) (*Session, error) {
	var result struct {
		Session Session `json:"session"`
	}
	if err := c.request(ctx, http.MethodPost, "", opts, &result); err != nil {
		return nil, err
	}
	return &result.Session, nil
}

// List returns all Sessions for the app; the API is currently unpaginated.
func (c *Client) List(ctx context.Context) ([]Session, error) {
	var result struct {
		Sessions []Session `json:"sessions"`
	}
	if err := c.request(ctx, http.MethodGet, "", nil, &result); err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

func (c *Client) Get(ctx context.Context, id string) (*Session, error) {
	path, err := c.path(id)
	if err != nil {
		return nil, err
	}
	var result struct {
		Session Session `json:"session"`
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result.Session, nil
}

// SetDesiredState requests an asynchronous transition. The returned phase may
// not have reached the requested state yet; inspect it with Get when necessary.
func (c *Client) SetDesiredState(ctx context.Context, id string, state DesiredState) (*Session, error) {
	if state != Running && state != Suspended {
		return nil, fmt.Errorf("sessions: desired state must be running or suspended")
	}
	path, err := c.path(id)
	if err != nil {
		return nil, err
	}
	var result struct {
		Session Session `json:"session"`
	}
	if err := c.request(ctx, http.MethodPut, path+"/desired-state", struct {
		DesiredState DesiredState `json:"desired_state"`
	}{state}, &result); err != nil {
		return nil, err
	}
	return &result.Session, nil
}

func (c *Client) Resume(ctx context.Context, id string) (*Session, error) {
	return c.SetDesiredState(ctx, id, Running)
}

func (c *Client) Suspend(ctx context.Context, id string) (*Session, error) {
	return c.SetDesiredState(ctx, id, Suspended)
}

// Delete removes the Session; workload cleanup can continue asynchronously.
func (c *Client) Delete(ctx context.Context, id string) error {
	path, err := c.path(id)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, path, nil, nil)
}
