package metricspush

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// ClientConfig describes how a runner's relay reaches the coordinator.
type ClientConfig struct {
	// CoordinatorAddress is the coordinator's API host:port.
	CoordinatorAddress string

	// ClientCertPEM and ClientKeyPEM are the runner's certificate from Join.
	// The listener demands a client identity before it routes anything, and
	// the runner's is the one it has. It opens the door; the sandbox token
	// inside the request is what the ingest actually trusts.
	ClientCertPEM []byte
	ClientKeyPEM  []byte

	// CACertPEM verifies the coordinator.
	CACertPEM []byte

	// Timeout bounds a single push.
	Timeout time.Duration
}

// Client forwards pushes from a runner's relay to the coordinator's ingest.
//
// It is its own client rather than runnertelemetry's, even though both ride
// the runner's certificate: that one stamps the runner's system token on every
// request under the same header this one fills with a sandbox's token.
type Client struct {
	http      *http.Client
	url       string
	transport *http3.Transport

	mu        sync.Mutex
	available bool
	checkedAt time.Time
	ttl       time.Duration
	now       func() time.Time
}

// availabilityTTL is how long the coordinator's answer to Available is reused.
// It changes only when the coordinator restarts with a different config, and
// it is asked once per sandbox start, so a minute is plenty fresh.
const availabilityTTL = time.Minute

// unreachableTTL is how long a check that got no answer at all is reused.
// A sandbox started in that window goes without push for its whole life, so
// a coordinator blip should cost seconds of that, not a minute.
const unreachableTTL = 10 * time.Second

// availabilityTimeout bounds the check, which sits in the path of starting a
// sandbox. A coordinator that cannot answer quickly is treated as unavailable
// for that sandbox rather than holding it up.
const availabilityTimeout = 3 * time.Second

func NewClient(cfg ClientConfig) (*Client, error) {
	cert, err := tls.X509KeyPair(cfg.ClientCertPEM, cfg.ClientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading runner certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.CACertPEM) {
		return nil, errors.New("parsing cluster CA certificate")
	}

	// HTTP/3 because the coordinator's authenticated listener is QUIC, the
	// same reason runnertelemetry gives.
	transport := &http3.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			NextProtos:   []string{http3.NextProtoH3},
		},
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = forwardTimeout
	}
	return &Client{
		http:      &http.Client{Transport: transport, Timeout: timeout},
		url:       "https://" + cfg.CoordinatorAddress + IngestPath,
		transport: transport,
		now:       time.Now,
	}, nil
}

// Available asks the coordinator whether the cluster accepts pushes, reusing
// the answer for a minute.
func (c *Client) Available(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.checkedAt.IsZero() && c.now().Sub(c.checkedAt) < c.ttl {
		return c.available
	}

	ctx, cancel := context.WithTimeout(ctx, availabilityTimeout)
	defer cancel()
	available, answered := false, false
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err == nil {
		if resp, err := c.http.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			available, answered = resp.StatusCode == http.StatusNoContent, true
		}
	}
	c.available, c.checkedAt, c.ttl = available, c.now(), availabilityTTL
	if !answered {
		c.ttl = unreachableTTL
	}
	return available
}

func (c *Client) Push(ctx context.Context, p Push) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(p.Body))
	if err != nil {
		return fmt.Errorf("building push request: %w", err)
	}
	req.Header.Set(TokenHeader, p.Token)
	req.Header.Set(ScopeHeader, string(p.Scope))
	req.Header.Set(FormatHeader, string(p.Format))
	if len(p.Grouping) > 0 {
		grouping := url.Values{}
		for name, value := range p.Grouping {
			grouping.Set(name, value)
		}
		req.Header.Set(GroupingHeader, grouping.Encode())
	}
	if p.ContentType != "" {
		req.Header.Set("Content-Type", p.ContentType)
	}
	if p.ContentEncoding != "" {
		req.Header.Set("Content-Encoding", p.ContentEncoding)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return errorf(http.StatusBadGateway, "coordinator unreachable")
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 == 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	// The coordinator's refusal is the workload's answer, so its status and
	// message pass through untouched.
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, resp.Body)
	return &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(detail))}
}

// Close tears down the underlying QUIC connections.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}
