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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	}
	results := make(chan result, len(addresses))
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, address := range addresses {
		go func() {
			cluster, err := c.probeDirectRoute(probeCtx, address)
			results <- result{cluster, err}
		}()
	}
	for range addresses {
		select {
		case res := <-results:
			if res.err == nil {
				cancel()
				return res.cluster.directRPCOptions(ctx, config, "")
			}
		case <-probeCtx.Done():
			goto relay
		}
	}

relay:
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts, err := c.viaCloudOptions(ctx, config)
	if err == nil && cacheDir != "" {
		_ = os.WriteFile(cachePath, nil, 0600)
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
		// Replace system-root validation with a pinned-root chain validation,
		// not an unauthenticated connectivity check.
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			root := state.PeerCertificates[len(state.PeerCertificates)-1]
			sum := sha1.Sum(root.Raw)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), c.CACertFingerprint) {
				return fmt.Errorf("cluster CA fingerprint mismatch")
			}
			roots := x509.NewCertPool()
			roots.AddCert(root)
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			name := tlsConfig.ServerName
			leaf := state.PeerCertificates[0]
			if c.TLSServerName == "" && leaf.VerifyHostname(name) != nil && slices.Contains(leaf.DNSNames, APIServerName) {
				name = APIServerName
			}
			if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: name}); err != nil {
				return err
			}
			direct.CACert = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}))
			direct.TLSServerName = name
			return nil
		}
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
	conn, err := transport.Dial(ctx, &net.UDPAddr{IP: ip.IP, Port: port, Zone: ip.Zone}, tlsConfig, &quic.Config{
		InitialPacketSize: rpc.InitialPacketSize, HandshakeIdleTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	_ = conn.CloseWithError(0, "route probe complete")
	return &direct, nil
}
