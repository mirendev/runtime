package serverlifecycle

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Snapshot struct {
	InstanceID  string
	Version     string
	Commit      string
	Ready       bool
	InstallKind string
}

// Prober observes the running server: which process is there before acting,
// and when the new one is up.
type Prober interface {
	Probe(ctx context.Context) (Snapshot, error)
}

// HealthProber reads the "server" block of /.well-known/miren/health.
type HealthProber struct {
	URL string
	// ServerName is the SNI to send. Empty works with the default autocert
	// ingress, which answers SNI-less handshakes with its self-signed cert.
	ServerName string
	client     *http.Client
}

const DefaultHealthURL = "https://127.0.0.1:443/.well-known/miren/health"

// NewHealthProber accepts any certificate: the point is to reach the process
// on this host, not to authenticate it.
func NewHealthProber(url, serverName string) *HealthProber {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // local readiness probe, see above
		ServerName:         serverName,
	}
	return &HealthProber{
		URL:        url,
		ServerName: serverName,
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *HealthProber) Probe(ctx context.Context) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()

	// 503 still carries the body, so an unhealthy server is still identifiable.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return Snapshot{}, fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	var body struct {
		Server *struct {
			Version     string `json:"version"`
			Commit      string `json:"commit"`
			InstanceID  string `json:"runtime_instance_id"`
			Ready       bool   `json:"ready"`
			InstallKind string `json:"install_kind"`
		} `json:"server"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Snapshot{}, fmt.Errorf("decode health response: %w", err)
	}
	if body.Server == nil {
		return Snapshot{}, fmt.Errorf("health endpoint has no server block; server predates lifecycle support")
	}
	return Snapshot{
		InstanceID:  body.Server.InstanceID,
		Version:     body.Server.Version,
		Commit:      body.Server.Commit,
		Ready:       body.Server.Ready,
		InstallKind: body.Server.InstallKind,
	}, nil
}
