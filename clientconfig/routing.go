package clientconfig

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/log"
	"github.com/quic-go/quic-go"
	"miren.dev/runtime/pkg/rpc"
)

const cloudRouteCacheTTL = 30 * time.Second

// ClusterRouteError preserves both connection failures when neither route works.
type ClusterRouteError struct {
	Direct error
	Cloud  error
}

func (e *ClusterRouteError) Error() string {
	return fmt.Sprintf("couldn't connect directly (%v) or through Miren Cloud (%v)", e.Direct, e.Cloud)
}

func (e *ClusterRouteError) Unwrap() []error { return []error{e.Direct, e.Cloud} }

type clusterRoute struct {
	Address       string `json:"address,omitempty"`
	CACert        string `json:"ca_cert,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`
	Cloud         bool   `json:"cloud,omitempty"`
}

func (r clusterRoute) save(path string) {
	if path == "" {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cluster-route-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(r)
	closeErr := f.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(f.Name(), path)
	}
}

// Successful routes avoid discovery on the next connection. Failed connections
// invalidate the route before rediscovery; no application call is retried.
func (c *ClusterConfig) routedOptions(ctx context.Context, config *Config) ([]rpc.StateOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cloudURL, err := c.CloudEndpoint(config)
	if err != nil {
		return nil, err
	}
	identity, err := config.GetIdentity(c.Identity)
	if err != nil {
		return nil, err
	}
	// Authentication is independent of the route. Resolve it before discovery
	// so an expired or rate-limited login cannot invalidate a working route.
	token, err := config.TokenForIdentity(ctx, c.Identity, identity, cloudURL)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with cloud: %w", err)
	}
	// Scope the hint to the cloud and connection configuration, not a local
	// alias that may be reused for another cluster.
	data, _ := json.Marshal(struct {
		Cluster *ClusterConfig
		Cloud   string
	}{c, cloudURL})
	key := sha256.Sum256(data)
	cacheDir, _ := getCacheDir()
	var cachePath string
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, fmt.Sprintf("cluster_route_%x.json", key))
	}

	return []rpc.StateOption{
		rpc.WithBindAddr("[::]:0"),
		rpc.WithClientRouting(func(ctx context.Context, connect func(...rpc.StateOption) (*rpc.NetworkClient, error)) (*rpc.NetworkClient, error) {
			attempt := func(route clusterRoute) (*rpc.NetworkClient, error) {
				var opts []rpc.StateOption
				if route.Cloud {
					endpoint, err := cloudRPCEndpoint(cloudURL, c.XID, c.Insecure)
					if err != nil {
						return nil, err
					}
					opts = c.cloudDialOptions(endpoint, token)
				} else {
					direct := *c
					direct.Hostname, direct.AllAddresses = route.Address, nil
					direct.CACert, direct.TLSServerName = route.CACert, route.TLSServerName
					opts = append(direct.dialOptions(route.Address), rpc.WithBearerToken(token))
				}
				return connect(opts...)
			}
			var cached clusterRoute
			data, _ := os.ReadFile(cachePath)
			info, statErr := os.Stat(cachePath)
			valid := json.Unmarshal(data, &cached) == nil &&
				((cached.Cloud && statErr == nil && time.Since(info.ModTime()) < cloudRouteCacheTTL) || (!cached.Cloud && cached.Address != ""))
			// An address with saved trust can be dialed without a preliminary probe.
			if !valid && c.Hostname != "" && (c.CACert != "" || c.CACertFingerprint == "") {
				cached = clusterRoute{Address: c.Hostname, CACert: c.CACert, TLSServerName: c.TLSServerName}
				valid = true
			}
			var directErr error
			if valid {
				client, err := attempt(cached)
				if err == nil {
					// Do not slide a cloud route's expiry on every successful use.
					if !cached.Cloud {
						cached.save(cachePath)
					}
					return client, nil
				}
				if !routeConnectionFailed(err) {
					return nil, err
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				_ = os.Remove(cachePath)
				if !cached.Cloud {
					directErr = fmt.Errorf("%s: %w", cached.Address, err)
				}
				log.G(ctx).WithError(err).Debug("Remembered cluster route failed; rediscovering")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			direct, probeErr := c.discoverDirectRoute(ctx)
			directErr = errors.Join(directErr, probeErr)
			if direct != nil {
				route := clusterRoute{Address: direct.Hostname, CACert: direct.CACert, TLSServerName: direct.TLSServerName}
				client, err := attempt(route)
				if err == nil {
					route.save(cachePath)
					return client, nil
				}
				if !routeConnectionFailed(err) {
					return nil, err
				}
				directErr = errors.Join(directErr, fmt.Errorf("%s: %w", route.Address, err))
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			route := clusterRoute{Cloud: true}
			client, err := attempt(route)
			if err == nil {
				route.save(cachePath)
				return client, nil
			}
			return nil, &ClusterRouteError{Direct: directErr, Cloud: err}
		}),
	}, nil
}

// A server rejecting a capability or credential is not a broken network route.
func routeConnectionFailed(err error) bool {
	var re *rpc.ResolveError
	if errors.As(err, &re) {
		switch re.Kind {
		case rpc.ResolveLookupError, rpc.ResolveStatusError, rpc.ResolveDecodeError:
			return false
		case rpc.ResolveHTTPError, rpc.ResolveUnreachableError, rpc.ResolveWentSilentError, rpc.ResolveNoAnswerError:
			return true
		}
	}
	return true
}

func (c *ClusterConfig) discoverDirectRoute(ctx context.Context) (*ClusterConfig, error) {
	addresses := slices.Clone(c.AllAddresses)
	if c.Hostname != "" && !slices.Contains(addresses, c.Hostname) {
		addresses = append(addresses, c.Hostname)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no direct addresses advertised")
	}
	type result struct {
		cluster *ClusterConfig
		err     error
		address string
		elapsed time.Duration
	}
	results := make(chan result, len(addresses))
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	started := time.Now()
	for _, address := range addresses {
		go func() {
			cluster, err := c.probeDirectRoute(probeCtx, address)
			results <- result{cluster, err, address, time.Since(started)}
		}()
	}
	var failures []error
	for range addresses {
		select {
		case res := <-results:
			if res.err == nil {
				cancel()
				return res.cluster, nil
			}
			failure := fmt.Errorf("%s: %w", res.address, res.err)
			var timeout net.Error
			var dns *net.DNSError
			if errors.As(res.err, &timeout) && timeout.Timeout() && !errors.As(res.err, &dns) {
				failure = rpc.NewResolveUnreachableError("", res.address, res.elapsed, res.err)
			}
			failures = append(failures, failure)
			log.G(ctx).WithField("cluster", c.XID).WithField("direct_error", failure).Debug("Direct cluster route failed")
		case <-probeCtx.Done():
			failures = append(failures, fmt.Errorf("probing %v: %w", addresses, probeCtx.Err()))
			return nil, errors.Join(failures...)
		}
	}
	return nil, errors.Join(failures...)
}

// Probe without sending credentials. If add ran off-network, cloud's CA
// fingerprint lets us recover the CA securely when a direct route appears.
func (c *ClusterConfig) probeDirectRoute(ctx context.Context, address string) (*ClusterConfig, error) {
	if !strings.Contains(address, ":") {
		address += ":8443"
	}
	host, portName, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := net.LookupPort("udp", portName)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	ip := addresses[0]
	// Prefer IPv4 on dual-stack hosts, matching the RPC dialer's resolver.
	for _, candidate := range addresses {
		if candidate.IP.To4() != nil {
			ip = candidate
			break
		}
	}
	direct := *c
	direct.Hostname = address
	direct.AllAddresses = nil
	tlsConfig := &tls.Config{ServerName: host, NextProtos: []string{"h3"}}
	if c.TLSServerName != "" {
		tlsConfig.ServerName = c.TLSServerName
	}
	if c.CACert != "" {
		tlsConfig.RootCAs = x509.NewCertPool()
		if !tlsConfig.RootCAs.AppendCertsFromPEM([]byte(c.CACert)) {
			return nil, fmt.Errorf("invalid cluster CA")
		}
	} else if c.CACertFingerprint != "" {
		// Require pinned-root recovery even if the peer has a system-trusted CA.
		tlsConfig.RootCAs = x509.NewCertPool()
	}
	network := "udp6"
	if ip.IP.To4() != nil {
		network = "udp4"
	}
	socket, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, err
	}
	defer socket.Close()
	transport := &quic.Transport{Conn: socket}
	defer transport.Close()
	remoteAddr := &net.UDPAddr{IP: ip.IP, Port: port, Zone: ip.Zone}
	quicConfig := &quic.Config{
		InitialPacketSize: rpc.InitialPacketSize, HandshakeIdleTimeout: 2 * time.Second,
	}
	conn, err := transport.Dial(ctx, remoteAddr, tlsConfig, quicConfig)
	if err != nil && c.CACert == "" && c.CACertFingerprint != "" {
		var verification *tls.CertificateVerificationError
		if !errors.As(err, &verification) {
			return nil, err
		}
		// A failed handshake exposes the chain without disabling verification.
		// Trust its root only after checking the cloud-provided fingerprint.
		certs := verification.UnverifiedCertificates
		root := certs[len(certs)-1]
		sum := sha1.Sum(root.Raw)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), c.CACertFingerprint) {
			return nil, fmt.Errorf("cluster CA fingerprint mismatch")
		}
		tlsConfig = tlsConfig.Clone()
		tlsConfig.RootCAs = x509.NewCertPool()
		tlsConfig.RootCAs.AddCert(root)
		if c.TLSServerName == "" && certs[0].VerifyHostname(tlsConfig.ServerName) != nil && slices.Contains(certs[0].DNSNames, APIServerName) {
			tlsConfig.ServerName = APIServerName
		}
		conn, err = transport.Dial(ctx, remoteAddr, tlsConfig, quicConfig)
		if err == nil {
			direct.CACert = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}))
			direct.TLSServerName = tlsConfig.ServerName
		}
	}
	if err != nil {
		return nil, err
	}
	_ = conn.CloseWithError(0, "route probe complete")
	return &direct, nil
}
