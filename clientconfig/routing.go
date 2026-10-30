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

// Only failed direct attempts are cached. A working direct route is checked
// again on the next connection, so leaving its network never pins us to it.
func (c *ClusterConfig) routedOptions(ctx context.Context, config *Config) ([]rpc.StateOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cloudURL, err := c.CloudEndpoint(config)
	if err != nil {
		return nil, err
	}
	// Scope the hint to the cloud and connection configuration, not a local
	// alias that may be reused for another cluster.
	data, _ := json.Marshal(struct {
		Cluster *ClusterConfig
		Cloud   string
	}{c, cloudURL})
	key := sha256.Sum256(data)
	cacheDir, _ := getCacheDir()
	cachePath := filepath.Join(cacheDir, fmt.Sprintf("cloud_route_%x", key))
	if cacheDir != "" {
		if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < cloudRouteCacheTTL {
			reason, _ := os.ReadFile(cachePath)
			log.G(ctx).WithField("cluster", c.XID).WithField("direct_errors", string(reason)).Debug("Using cached direct-route failure; trying cloud")
			return c.viaCloudOptions(ctx, config)
		}
	}

	addresses := slices.Clone(c.AllAddresses)
	if c.Hostname != "" && !slices.Contains(addresses, c.Hostname) {
		addresses = append(addresses, c.Hostname)
	}
	type result struct {
		cluster *ClusterConfig
		err     error
		address string
	}
	results := make(chan result, len(addresses))
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, address := range addresses {
		go func() {
			cluster, err := c.probeDirectRoute(probeCtx, address)
			results <- result{cluster, err, address}
		}()
	}
	var failures []string
	for range addresses {
		select {
		case res := <-results:
			if res.err == nil {
				cancel()
				return res.cluster.directRPCOptions(ctx, config, "")
			}
			failure := fmt.Sprintf("%s: %v", res.address, res.err)
			failures = append(failures, failure)
			log.G(ctx).WithField("cluster", c.XID).WithField("direct_error", failure).Debug("Direct cluster route failed")
		case <-probeCtx.Done():
			failures = append(failures, fmt.Sprintf("probing %v: %v", addresses, probeCtx.Err()))
			goto relay
		}
	}

relay:
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log.G(ctx).WithField("cluster", c.XID).WithField("direct_errors", strings.Join(failures, "; ")).Debug("No verified direct route; trying cloud")
	opts, err := c.viaCloudOptions(ctx, config)
	if err == nil && cacheDir != "" {
		_ = os.WriteFile(cachePath, []byte(strings.Join(failures, "; ")), 0600)
	}
	return opts, err
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
