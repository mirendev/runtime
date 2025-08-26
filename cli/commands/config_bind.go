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
	var expectedFingerprint string
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

		for _, addr := range selectedCluster.APIAddresses {
			ctx.Info("Trying to connect to %s...", addr)
			cert, fingerprint, err := extractTLSCertificate(ctx, addr)
			if err != nil {
				ctx.Warn("Failed to connect to %s: %v", addr, err)
				lastErr = err
				continue
			}
			// Successfully connected
			caCert = cert
			actualFingerprint = fingerprint
			workingAddress = addr
			opts.Address = addr
			break
		}

		if workingAddress == "" {
			if lastErr != nil {
				return fmt.Errorf("failed to connect to any cluster address: %w", lastErr)
			}
			return fmt.Errorf("no addresses available for cluster %s", selectedCluster.Name)
		}

		expectedFingerprint = selectedCluster.CACertFingerprint
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

	// If we have an expected fingerprint, verify it
	if expectedFingerprint != "" {
		// Compare fingerprints (case-insensitive)
		if !strings.EqualFold(expectedFingerprint, actualFingerprint) {
			ctx.Warn("Certificate fingerprint mismatch!")
			ctx.Warn("Expected: %s", expectedFingerprint)
			ctx.Warn("Actual:   %s", actualFingerprint)
			return fmt.Errorf("certificate fingerprint verification failed - possible security issue")
		}

		ctx.Completed("Certificate fingerprint verified")
	}

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

// extractTLSCertificate connects to the server via QUIC and extracts the TLS certificate
// Returns the PEM-encoded certificate and its SHA1 fingerprint (hex-encoded)
func extractTLSCertificate(ctx *Context, address string) (string, string, error) {
	// Normalize the address - add default port if not specified
	normalizedAddr := address
	if !strings.Contains(address, ":") {
		normalizedAddr = address + ":8443"
	}

	// Parse the address to get the hostname for SNI
	host, _, err := net.SplitHostPort(normalizedAddr)
	if err != nil {
		// If splitting fails, the address might not have a port
		host = address
	}

	// Create a context with timeout
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create TLS config that accepts any certificate for now (we're extracting it)
	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,           // We're extracting the cert, not verifying it yet
		NextProtos:         []string{"h3"}, // HTTP/3 ALPN
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

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return "", "", fmt.Errorf("failed to create UDP socket: %w", err)
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
