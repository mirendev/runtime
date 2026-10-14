package workload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config describes the sandbox-local metadata API, not the coordinator API.
type Config struct {
	URL    string
	Secret string
	// HTTPClient defaults to a client with a 25-second timeout, longer than
	// the metadata server's 20-second long poll. Redirects are never followed.
	HTTPClient *http.Client
}

// ConfigFromEnv reads the metadata credentials injected by Miren.
func ConfigFromEnv() Config {
	return Config{URL: os.Getenv("MIREN_METADATA_URL"), Secret: os.Getenv("MIREN_METADATA_SECRET")}
}

// Session is an assignment to this sandbox. Spec is the resolved app service
// specification as JSON, which may contain secrets; do not log it.
type Session struct {
	ID      string          `json:"-"`
	App     string          `json:"app"`
	Version string          `json:"version"`
	Service string          `json:"service"`
	Group   string          `json:"group,omitempty"`
	Spec    json.RawMessage `json:"spec"`
}

// Snapshot includes current assignments and deletions awaiting cleanup.
type Snapshot struct {
	Sessions []string             `json:"sessions"`
	Details  map[string]Session   `json:"session_details"`
	Deleted  []string             `json:"deleted"`
	Detached map[string]time.Time `json:"detached"`
	Version  string               `json:"version"`
}

// HTTPError intentionally omits the response body, which could contain secrets.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("workload metadata: HTTP %d", e.StatusCode) }

// Client accesses only the calling sandbox's metadata.
type Client struct {
	url, secret string
	http        *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("workload: invalid metadata URL")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("workload: metadata secret is required")
	}
	h := &http.Client{Timeout: 25 * time.Second}
	if cfg.HTTPClient != nil {
		*h = *cfg.HTTPClient
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{url: strings.TrimRight(cfg.URL, "/"), secret: cfg.Secret, http: h}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, c.url+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+c.secret)
	r.Header.Set("Content-Type", "application/json")
	return c.http.Do(r)
}

// Sessions long-polls when version is nonempty. A nil snapshot means unchanged.
func (c *Client) Sessions(ctx context.Context, version string) (*Snapshot, error) {
	r, err := c.request(ctx, "GET", "/sessions?wait="+url.QueryEscape(version), nil)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if r.StatusCode != http.StatusOK {
		return nil, &HTTPError{r.StatusCode}
	}
	var s Snapshot
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("workload: invalid Session snapshot")
	}
	for id, detail := range s.Details {
		detail.ID = id
		s.Details[id] = detail
	}
	return &s, nil
}

// AcknowledgeDeletion must be called only after the Session's resources close.
// A missing binding is already gone, including when a successful ack's response
// was lost and the coordinator removed the binding before the retry.
func (c *Client) AcknowledgeDeletion(ctx context.Context, id string) error {
	r, err := c.request(ctx, "POST", "/sessions/deletions/ack", map[string]string{"session": id})
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent && r.StatusCode != http.StatusNotFound {
		return &HTTPError{r.StatusCode}
	}
	return nil
}

// AcknowledgeDetachment releases a withdrawn assignment after cleanup. The
// notice timestamp prevents a delayed retry from acknowledging a newer detach.
func (c *Client) AcknowledgeDetachment(ctx context.Context, id string, detachedAt time.Time) error {
	r, err := c.request(ctx, "POST", "/sessions/detachments/ack", map[string]any{"session": id, "detached_at": detachedAt})
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent && r.StatusCode != http.StatusNotFound {
		return &HTTPError{r.StatusCode}
	}
	return nil
}

// ReportSessionActivity renews an individual assignment's activity. An active
// report must succeed before accepting work; idle may trigger automatic parking.
func (c *Client) ReportSessionActivity(ctx context.Context, id string, active bool) error {
	state := "idle"
	if active {
		state = "active"
	}
	r, err := c.request(ctx, "POST", "/sessions/activity", map[string]string{"session": id, "state": state})
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		return &HTTPError{r.StatusCode}
	}
	return nil
}

// ReportActivity renews activity and returns the advertised shutdown deadline,
// or zero if there is no notice. Idle is advisory, not a cleanup acknowledgment.
func (c *Client) ReportActivity(ctx context.Context, active bool) (time.Time, error) {
	state := "idle"
	if active {
		state = "active"
	}
	r, err := c.request(ctx, "POST", "/activity", map[string]string{"state": state})
	if err != nil {
		return time.Time{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		return time.Time{}, &HTTPError{r.StatusCode}
	}
	if deadline := r.Header.Get("Miren-Shutdown-At"); deadline != "" {
		t, err := time.Parse(time.RFC3339Nano, deadline)
		if err != nil {
			return time.Time{}, fmt.Errorf("workload: invalid shutdown deadline")
		}
		return t, nil
	}
	return time.Time{}, nil
}
