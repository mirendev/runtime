// Package cloudapi talks to Miren Cloud's HTTP API on behalf of a logged-in
// user. It covers the endpoints the CLI needs to answer "what clusters do I
// have, and can cloud reach this one", and it owns the base URL, the bearer
// token, and the request and response decoding that go with them.
//
// Deciding which identity to use, what to render, and how to reach a cluster
// stays with the caller. This package only makes the calls.
package cloudapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// listTimeout bounds the cluster listing, which a person is usually
	// waiting on.
	listTimeout = 30 * time.Second

	// onlineTimeout is shorter because the answer is worth no more than no
	// answer: a caller that cannot find out treats the cluster as unreachable
	// either way, so waiting longer buys nothing.
	onlineTimeout = 10 * time.Second
)

// Cluster is a cluster as Miren Cloud describes it.
type Cluster struct {
	XID               string         `json:"xid"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Tags              map[string]any `json:"tags"`
	APIAddresses      []string       `json:"api_addresses,omitempty"`
	CACertFingerprint string         `json:"ca_cert_fingerprint,omitempty"`
	OrganizationXID   string         `json:"organization_xid"`
	OrganizationName  string         `json:"organization_name"`
}

// HasReachableAddress reports whether cloud advertised at least one API address
// for this cluster. A cluster with none can't be dialed by any client, so it
// would otherwise be silently hidden from `miren cluster add`. The most common
// cause is a firewalled inbound port: miren connects over QUIC (UDP 8443), and
// when cloud's netcheck can't reach that port it drops the discovered public
// IP, leaving the cluster advertising nothing. See MIR-1316.
func (c Cluster) HasReachableAddress() bool {
	return len(c.APIAddresses) > 0
}

// TokenFunc returns a bearer token for the request about to be made. It is a
// function rather than a string because tokens expire, and the caller holds the
// machinery for refreshing and persisting them.
type TokenFunc func(ctx context.Context) (string, error)

// Client calls one Miren Cloud instance as one identity.
type Client struct {
	baseURL string
	token   TokenFunc
}

// New returns a client for the cloud at baseURL, authenticating with token.
func New(baseURL string, token TokenFunc) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("no cloud URL configured")
	}
	if token == nil {
		return nil, fmt.Errorf("no way to authenticate with %s", baseURL)
	}

	return &Client{baseURL: baseURL, token: token}, nil
}

// BaseURL is the cloud this client talks to. Callers use it to say which cloud
// they are talking about, and to notice an unencrypted one.
func (c *Client) BaseURL() string { return c.baseURL }

// ListClusters returns every cluster the account can see.
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var response struct {
		Clusters []Cluster `json:"clusters"`
	}

	if err := c.get(ctx, listTimeout, &response, "/api/v1/users/clusters"); err != nil {
		return nil, err
	}

	return response.Clusters, nil
}

// ClusterOnline reports whether cloud currently holds a link to the cluster,
// which is the precondition for relaying calls to it.
//
// A cluster with no id cannot be asked about, and reports as not online rather
// than as an error: the caller reaches the same decision either way.
func (c *Client) ClusterOnline(ctx context.Context, clusterXID string) (bool, error) {
	if clusterXID == "" {
		return false, nil
	}

	var response struct {
		Online bool `json:"online"`
	}

	if err := c.get(ctx, onlineTimeout, &response, "/api/v1/clusters/", clusterXID, "/online"); err != nil {
		return false, err
	}

	return response.Online, nil
}

func (c *Client) get(ctx context.Context, timeout time.Duration, out any, pathParts ...string) error {
	endpoint, err := url.JoinPath(c.baseURL, pathParts...)
	if err != nil {
		return err
	}

	token, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Bounded: the body here only ever goes into an error message, and a
		// proxy having a bad day can answer with something enormous. Reading
		// all of it would turn a failed request into a failed process.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("cloud returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	return nil
}
