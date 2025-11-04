package commands

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"miren.dev/runtime/clientconfig"
)

// skipAddresses contains addresses that should be skipped when trying to connect
var skipAddresses = map[string]bool{
	"0.0.0.0":               true,
	"127.0.0.1":             true,
	"::1":                   true,
	"localhost":             true,
	"localhost.localdomain": true,
}

func ConfigBind(ctx *Context, opts struct {
	Identity string `short:"i" long:"identity" description:"Name of the identity to use (optional - will use the only one if single)"`
	Cluster  string `short:"c" long:"cluster" description:"Name of the cluster to create (optional - will list available)"`
	Address  string `short:"a" long:"address" description:"Address/hostname of the cluster (optional - will use from selected cluster)"`
	Force    bool   `short:"f" long:"force" description:"Overwrite existing cluster configuration"`
}) error {
	// Load the main config to check if the identity exists
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if the identity exists
	if mainConfig == nil || !mainConfig.HasIdentities() {
		return fmt.Errorf("no identities configured. Please run 'miren login' first")
	}

	// If no identity specified, check if we can auto-select
	if opts.Identity == "" {
		availableIdentities := mainConfig.GetIdentityNames()
		if len(availableIdentities) == 1 {
			// Auto-select the only identity
			opts.Identity = availableIdentities[0]
			ctx.Info("Using identity '%s' (only one available)", opts.Identity)
		} else if len(availableIdentities) > 1 {
			// Multiple identities available, user must specify
			return fmt.Errorf("multiple identities available, please specify one with --identity: %s", strings.Join(availableIdentities, ", "))
		} else {
			return fmt.Errorf("no identities configured. Please run 'miren login' first")
		}
	}

	identity, err := mainConfig.GetIdentity(opts.Identity)
	if err != nil {
		// List available identities to help the user
		availableIdentities := mainConfig.GetIdentityNames()
		if len(availableIdentities) > 0 {
			return fmt.Errorf("identity %q not found. Available identities: %v", opts.Identity, availableIdentities)
		}
		return fmt.Errorf("identity %q not found in configuration", opts.Identity)
	}

	// If no cluster name or address provided, query the identity server for available clusters
	var caCert string
	var allAddresses []string

	if opts.Cluster == "" && opts.Address == "" {
		ctx.Info("Fetching available clusters from identity server...")

		clusters, err := fetchAvailableClusters(ctx, mainConfig, identity)
		if err != nil {
			return fmt.Errorf("failed to fetch available clusters: %w", err)
		}

		if len(clusters) == 0 {
			return fmt.Errorf("no clusters available for your account")
		}

		// Present cluster selection to user and get local name
		selectedCluster, localName, err := selectClusterFromList(ctx, clusters)
		if err != nil {
			return err
		}

		opts.Cluster = localName

		// Store all available addresses
		allAddresses = selectedCluster.APIAddresses

		// Try to connect to the cluster
		workingAddress, cert, err := tryConnectToCluster(ctx, selectedCluster, true)
		if err != nil {
			return err
		}

		caCert = cert
		opts.Address = workingAddress

		if localName != selectedCluster.Name {
			ctx.Info("Binding cluster '%s' as '%s' (connected to %s)", selectedCluster.Name, localName, workingAddress)
		} else {
			ctx.Info("Binding cluster '%s' (connected to %s)", selectedCluster.Name, workingAddress)
		}
	} else if opts.Cluster == "" || opts.Address == "" {
		return fmt.Errorf("both --cluster and --address must be specified, or neither (to list available clusters)")
	} else {
		// Manual mode - address was specified directly
		ctx.Info("Connecting to %s to extract TLS certificate...", opts.Address)

		// Extract the TLS certificate from the server
		cert, fingerprint, err := extractTLSCertificate(ctx, opts.Address)
		if err != nil {
			return fmt.Errorf("failed to extract TLS certificate: %w", err)
		}
		caCert = cert
		ctx.Completed("Successfully extracted TLS certificate (fingerprint: %s)", fingerprint)
	}

	// Create the cluster configuration
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname:     opts.Address,
		AllAddresses: allAddresses,
		Identity:     opts.Identity,
		CACert:       caCert,
	}

	// Load or create the main client config
	mainConfig, err = clientconfig.LoadConfig()
	if err != nil {
		// If no config exists, create a new one
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load client config: %w", err)
		}
	}

	// Check if the leaf config already exists (by trying to get the cluster)
	if mainConfig.HasCluster(opts.Cluster) && !opts.Force {
		return fmt.Errorf("cluster configuration %q already exists. Use --force to overwrite", opts.Cluster)
	}

	// Create the cluster config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			opts.Cluster: clusterConfig,
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/{cluster}.yaml)
	mainConfig.SetLeafConfig(opts.Cluster, leafConfigData)

	// Save the main config (which will also save the leaf config)
	if err := mainConfig.Save(); err != nil {
		return fmt.Errorf("failed to save cluster configuration: %w", err)
	}

	ctx.Completed("Successfully bound identity %q to cluster %q at %s", opts.Identity, opts.Cluster, opts.Address)
	ctx.Info("Configuration saved to clientconfig.d/%s.yaml", opts.Cluster)

	// If there's no active cluster set, suggest setting this one
	if mainConfig != nil && mainConfig.ActiveCluster() == "" {
		ctx.Info("")
		ctx.Info("Tip: Set this as your active cluster with:")
		ctx.Info("  miren config set-active %s", opts.Cluster)
	}

	return nil
}

// normalizeAddress handles robust address normalization for various formats:
// - Strips optional scheme prefixes (https:// or http://)
// - Handles IPv6 literals correctly (bracketed and unbracketed)
// - Adds default port 8443 when no port is present
// Returns normalized address and host for SNI (with brackets stripped for IPv6)
func normalizeAddress(address string) (normalizedAddr, sniHost string, err error) {
	// Strip scheme if present
	addr := address
	if strings.HasPrefix(addr, "https://") {
		addr = strings.TrimPrefix(addr, "https://")
	} else if strings.HasPrefix(addr, "http://") {
		addr = strings.TrimPrefix(addr, "http://")
	}

	// Handle IPv6 literals and port logic
	if strings.Contains(addr, "]") {
		// Bracketed IPv6 format [::1]:8443 or [::1]
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// No port specified, assume it's just [::1]
			if strings.HasSuffix(addr, "]") {
				normalizedAddr = addr + ":8443"
				sniHost = strings.Trim(addr, "[]")
				return normalizedAddr, sniHost, nil
			}
			return "", "", fmt.Errorf("invalid IPv6 address format: %w", err)
		}
		normalizedAddr = addr
		sniHost = strings.Trim(host, "[]")
		return normalizedAddr, sniHost, nil
	} else if strings.Count(addr, ":") > 1 && !strings.Contains(addr, "[") {
		// Unbracketed IPv6 like ::1 or 2001:db8::1
		// Need to wrap in brackets and add port
		normalizedAddr = "[" + addr + "]:8443"
		sniHost = addr
		return normalizedAddr, sniHost, nil
	} else {
		// IPv4 or hostname
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// No port specified
			normalizedAddr = addr + ":8443"
			sniHost = addr
			return normalizedAddr, sniHost, nil
		}
		normalizedAddr = addr
		sniHost = host
		return normalizedAddr, sniHost, nil
	}
}

// extractTLSCertificate connects to the server via QUIC and extracts the TLS certificate
// Returns the PEM-encoded certificate and its SHA1 fingerprint (hex-encoded)
func extractTLSCertificate(ctx *Context, address string) (string, string, error) {
	// Normalize the address with robust parsing
	normalizedAddr, sniHost, err := normalizeAddress(address)
	if err != nil {
		return "", "", fmt.Errorf("failed to normalize address: %w", err)
	}

	// Create a context with timeout
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create TLS config that accepts any certificate for now (we're extracting it)
	tlsConfig := &tls.Config{
		ServerName:         sniHost,                                   // Use properly stripped SNI host
		InsecureSkipVerify: true,                                      // We're extracting the cert, not verifying it yet
		NextProtos:         []string{"h3", "h3-29", "h3-28", "h3-27"}, // HTTP/3 ALPN with common variants
	}

	// Create QUIC config
	quicConfig := &quic.Config{
		HandshakeIdleTimeout: 5 * time.Second,
		MaxIdleTimeout:       10 * time.Second,
	}

	// Try to establish a QUIC connection
	udpAddr, err := net.ResolveUDPAddr("udp", normalizedAddr)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve address: %w", err)
	}

	// Try IPv6/dual-stack binding first, fallback to IPv4
	var udpConn *net.UDPConn
	udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err != nil {
		// Fallback to IPv4 if IPv6 fails
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return "", "", fmt.Errorf("failed to create UDP socket: %w", err)
		}
	}
	defer udpConn.Close()

	transport := &quic.Transport{
		Conn: udpConn,
	}
	defer transport.Close()

	conn, err := transport.Dial(connCtx, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		return "", "", fmt.Errorf("failed to establish QUIC connection: %w", err)
	}
	defer conn.CloseWithError(0, "done")

	// Get the TLS connection state
	connState := conn.ConnectionState().TLS

	// Extract the certificate chain
	if len(connState.PeerCertificates) == 0 {
		return "", "", fmt.Errorf("no certificates found in TLS handshake")
	}

	// Get the root CA certificate (usually the last in the chain)
	// But for self-signed certs, there might be only one
	var rootCert *x509.Certificate
	if len(connState.PeerCertificates) == 1 {
		// Self-signed certificate
		rootCert = connState.PeerCertificates[0]
	} else {
		// Take the last certificate in the chain as the root CA
		rootCert = connState.PeerCertificates[len(connState.PeerCertificates)-1]
	}

	// Calculate SHA1 fingerprint of the raw DER bytes
	sum := sha1.Sum(rootCert.Raw)
	fingerprint := hex.EncodeToString(sum[:])

	// Encode the certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: rootCert.Raw,
	})

	return string(certPEM), fingerprint, nil
}
