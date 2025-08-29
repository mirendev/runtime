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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"miren.dev/runtime/clientconfig"
)

// skipAddresses contains addresses that should be skipped when trying to connect
var skipAddresses = map[string]bool{
	"0.0.0.0":   true,
	"127.0.0.1": true,
	"::1":       true,
}

func ConfigBind(ctx *Context, opts struct {
	Identity string `short:"i" long:"identity" required:"true" description:"Name of the identity to use"`
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
	if mainConfig == nil || mainConfig.Identities == nil {
		return fmt.Errorf("no identities configured. Please run 'miren login' first")
	}

	identity, exists := mainConfig.Identities[opts.Identity]
	if !exists {
		// List available identities to help the user
		availableIdentities := make([]string, 0, len(mainConfig.Identities))
		for name := range mainConfig.Identities {
			availableIdentities = append(availableIdentities, name)
		}
		if len(availableIdentities) > 0 {
			return fmt.Errorf("identity %q not found. Available identities: %v", opts.Identity, availableIdentities)
		}
		return fmt.Errorf("identity %q not found in configuration", opts.Identity)
	}

	// If no cluster name or address provided, query the identity server for available clusters
	var caCert string
	var actualFingerprint string
	var allAddresses []string

	if opts.Cluster == "" && opts.Address == "" {
		ctx.Info("Fetching available clusters from identity server...")

		clusters, err := fetchAvailableClusters(ctx, identity)
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

		// Try each address until one works
		var lastErr error
		var workingAddress string

		// Try the cluster's advertised addresses first
		for _, addr := range selectedCluster.APIAddresses {
			// Check if this address should be skipped
			// Parse the host from the address (handle both with and without port)
			host, _, _ := net.SplitHostPort(addr)
			if host == "" {
				// No port in the address, use as-is
				host = addr
			}
			// Strip brackets from IPv6 addresses
			host = strings.Trim(host, "[]")

			if skipAddresses[host] {
				continue
			}

			ctx.Info("Trying to connect to %s...", addr)
			cert, fingerprint, err := extractTLSCertificate(ctx, addr)
			if err != nil {
				ctx.Warn("Failed to connect to %s: %v", addr, err)
				lastErr = err
				continue
			}

			// Check fingerprint if we have an expected one
			if selectedCluster.CACertFingerprint != "" {
				if !strings.EqualFold(selectedCluster.CACertFingerprint, fingerprint) {
					ctx.Warn("Certificate fingerprint mismatch for %s", addr)
					ctx.Warn("Expected: %s", selectedCluster.CACertFingerprint)
					ctx.Warn("Actual:   %s", fingerprint)
					lastErr = fmt.Errorf("certificate fingerprint verification failed for %s", addr)
					continue
				}
				ctx.Info("Certificate fingerprint verified for %s", addr)
			}

			// Successfully connected and verified
			caCert = cert
			actualFingerprint = fingerprint
			workingAddress = addr
			opts.Address = addr
			break
		}

		// If all normal addresses failed, try localhost as a fallback
		if workingAddress == "" {
			ctx.Info("All cluster addresses failed, trying localhost as fallback...")

			// Try common localhost addresses with default port
			localhostAddresses := []string{
				"127.0.0.1:8443",
				"[::1]:8443",
				"0.0.0.0:8443",
			}

			for _, addr := range localhostAddresses {
				ctx.Info("Trying localhost address %s...", addr)
				cert, fingerprint, err := extractTLSCertificate(ctx, addr)
				if err != nil {
					lastErr = err
					continue
				}

				// Check fingerprint if we have an expected one
				if selectedCluster.CACertFingerprint != "" {
					if !strings.EqualFold(selectedCluster.CACertFingerprint, fingerprint) {
						ctx.Warn("Certificate fingerprint mismatch for %s", addr)
						ctx.Warn("Expected: %s", selectedCluster.CACertFingerprint)
						ctx.Warn("Actual:   %s", fingerprint)
						lastErr = fmt.Errorf("certificate fingerprint verification failed for %s", addr)
						continue
					}
					ctx.Info("Certificate fingerprint verified for %s", addr)
				}

				// Successfully connected and verified
				caCert = cert
				actualFingerprint = fingerprint
				workingAddress = addr
				opts.Address = addr
				ctx.Completed("Successfully connected to localhost at %s", addr)
				break
			}
		}

		if workingAddress == "" {
			if lastErr != nil {
				return fmt.Errorf("failed to connect to any cluster address: %w", lastErr)
			}
			return fmt.Errorf("no addresses available for cluster %s", selectedCluster.Name)
		}

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
		caCert, actualFingerprint, err = extractTLSCertificate(ctx, opts.Address)
		if err != nil {
			return fmt.Errorf("failed to extract TLS certificate: %w", err)
		}
	}

	ctx.Completed("Successfully extracted TLS certificate (fingerprint: %s)", actualFingerprint)

	// Create the cluster configuration
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname:     opts.Address,
		AllAddresses: allAddresses,
		Identity:     opts.Identity,
		CACert:       caCert,
	}

	// Determine the config.d directory path
	configDirPath, err := getConfigDirPath()
	if err != nil {
		return fmt.Errorf("failed to get config directory path: %w", err)
	}

	if err := os.MkdirAll(configDirPath, 0755); err != nil {
		return fmt.Errorf("failed to create config.d directory: %w", err)
	}

	// Create the config file path
	configFilePath := filepath.Join(configDirPath, fmt.Sprintf("%s.yaml", opts.Cluster))

	// Check if the file already exists
	if _, err := os.Stat(configFilePath); err == nil && !opts.Force {
		return fmt.Errorf("cluster configuration %q already exists. Use --force to overwrite", opts.Cluster)
	}

	// Create the configuration with just this cluster
	config := &clientconfig.Config{
		Clusters: map[string]*clientconfig.ClusterConfig{
			opts.Cluster: clusterConfig,
		},
	}

	// Save the configuration to the config.d directory
	if err := config.SaveTo(configFilePath); err != nil {
		return fmt.Errorf("failed to save cluster configuration: %w", err)
	}

	ctx.Completed("Successfully bound identity %q to cluster %q at %s", opts.Identity, opts.Cluster, opts.Address)
	ctx.Info("Configuration saved to: %s", configFilePath)

	// If there's no active cluster set, suggest setting this one
	if mainConfig != nil && mainConfig.ActiveCluster == "" {
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
