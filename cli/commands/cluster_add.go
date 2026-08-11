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
	"slices"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// skipAddresses contains addresses that should be skipped when trying to connect
var skipAddresses = map[string]bool{
	"0.0.0.0":               true,
	"127.0.0.1":             true,
	"::1":                   true,
	"localhost":             true,
	"localhost.localdomain": true,
}

func ClusterAdd(ctx *Context, opts struct {
	Identity string `short:"i" long:"identity" description:"Name of the identity to use (optional - will use the only one if single)"`
	Cluster  string `short:"c" long:"cluster" description:"Name of the cluster to create (optional - will list available)"`
	Address  string `short:"a" long:"address" description:"Address/hostname of the cluster (optional - will use from selected cluster)"`
	Force    bool   `short:"f" long:"force" description:"Overwrite existing cluster configuration"`
}) error {
	return addCluster(ctx, opts.Identity, opts.Cluster, opts.Address, opts.Force)
}

// AddClusterInteractive prompts the user to select and add a cluster interactively.
// It auto-selects the identity if only one is available.
// Returns nil if a cluster was successfully added.
func AddClusterInteractive(ctx *Context) error {
	return addCluster(ctx, "", "", "", false)
}

func addCluster(ctx *Context, identityName, clusterName, address string, force bool) error {
	// Load the main config to check if the identity exists
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Detect manual mode: both --cluster and --address provided
	manualMode := clusterName != "" && address != ""

	// In manual mode, identity is optional (a CI runner authenticates with an
	// OIDC token from its environment and has none configured).
	// In discovery mode, identity is required to fetch available clusters.
	if !manualMode {
		// Discovery mode — identity is required
		if mainConfig == nil || !mainConfig.HasIdentities() {
			return fmt.Errorf("no identities configured. Please run 'miren login' first, or use --cluster and --address to add a cluster directly")
		}

		if identityName == "" {
			availableIdentities := mainConfig.GetIdentityNames()
			if len(availableIdentities) == 1 {
				identityName = availableIdentities[0]
				ctx.Info("Using identity '%s' (only one available)", identityName)
			} else if len(availableIdentities) > 1 {
				return fmt.Errorf("multiple identities available, please specify one with --identity: %s", strings.Join(availableIdentities, ", "))
			} else {
				return fmt.Errorf("no identities configured. Please run 'miren login' first, or use --cluster and --address to add a cluster directly")
			}
		}
	} else if identityName != "" {
		// Manual mode with explicit --identity: validate it exists
		if mainConfig == nil || !mainConfig.HasIdentities() {
			return fmt.Errorf("identity %q not found: no identities configured", identityName)
		}
	} else if mainConfig != nil {
		// Manual mode with no --identity. Optional isn't the same as unwanted:
		// someone who has run `miren login` has an identity sitting right there,
		// and leaving it off produces a cluster entry with no way to authenticate
		// at all — which used to surface much later, and much less helpfully, as a
		// certificate parse error on the first command that needed the cluster.
		// Discovery mode already adopts a lone identity; match it.
		switch names := mainConfig.GetIdentityNames(); len(names) {
		case 0:
			// Nothing configured, so this really is an ambient-auth setup.
		case 1:
			identityName = names[0]
			ctx.Info("Using identity '%s' (only one available)", identityName)
		default:
			return fmt.Errorf("multiple identities available, please specify one with --identity: %s", strings.Join(names, ", "))
		}
	}

	// Look up identity if one was specified (skip if manual mode with no identity)
	var identity *clientconfig.IdentityConfig
	if identityName != "" {
		if mainConfig == nil {
			return fmt.Errorf("identity %q not found in configuration", identityName)
		}
		var err error
		identity, err = mainConfig.GetIdentity(identityName)
		if err != nil {
			availableIdentities := mainConfig.GetIdentityNames()
			if len(availableIdentities) > 0 {
				return fmt.Errorf("identity %q not found. Available identities: %v", identityName, availableIdentities)
			}
			return fmt.Errorf("identity %q not found in configuration", identityName)
		}
	}

	// If no cluster name or address provided, query the identity server for available clusters
	var clusterCert *clusterCertificate
	var allAddresses []string
	var clusterXID string

	if clusterName == "" && address == "" {
		ctx.Info("Fetching available clusters from identity server...")

		clusters, err := fetchAvailableClusters(ctx, mainConfig, identityName, identity)
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

		clusterName = localName
		clusterXID = selectedCluster.XID

		// Store all available addresses
		allAddresses = selectedCluster.APIAddresses

		// Try to connect to the cluster
		workingAddress, cert, err := tryConnectToCluster(ctx, selectedCluster, true)
		if err != nil {
			return err
		}

		clusterCert = cert
		address = workingAddress

		if localName != selectedCluster.Name {
			ctx.Info("Adding cluster '%s' as '%s' (connected to %s)", selectedCluster.Name, localName, workingAddress)
		} else {
			ctx.Info("Adding cluster '%s' (connected to %s)", selectedCluster.Name, workingAddress)
		}
	} else if clusterName == "" || address == "" {
		return fmt.Errorf("both --cluster and --address must be specified, or neither (to list available clusters)")
	} else {
		// Manual mode - address was specified directly
		ctx.Info("Connecting to %s to extract TLS certificate...", address)

		// Extract the TLS certificate from the server
		cert, err := extractTLSCertificate(ctx, address)
		if err != nil {
			return fmt.Errorf("failed to extract TLS certificate: %w", err)
		}
		clusterCert = cert
		ctx.Completed("Successfully extracted TLS certificate (fingerprint: %s)", cert.Fingerprint)
	}

	// Create the cluster configuration
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname:     address,
		AllAddresses: allAddresses,
		Identity:     identityName,
		XID:          clusterXID,
	}

	if clusterCert != nil {
		clusterConfig.CACert = clusterCert.CAPEM
	}

	applyVerificationName(ctx, clusterConfig, address, clusterCert)

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
	if mainConfig.HasCluster(clusterName) && !force {
		if ui.IsInteractive() {
			// Prompt user to choose: overwrite or cancel
			items := []ui.PickerItem{
				ui.SimplePickerItem{Text: "Overwrite existing configuration"},
				ui.SimplePickerItem{Text: "Cancel"},
			}

			title := fmt.Sprintf("Cluster %q already exists", clusterName)
			selected, err := ui.RunPicker(items, ui.WithTitle(title))
			if err != nil {
				return fmt.Errorf("failed to run picker: %w", err)
			}
			if selected == nil || selected.ID() == "Cancel" {
				return fmt.Errorf("cancelled")
			}
			// User chose to overwrite, continue
		} else {
			return fmt.Errorf("cluster configuration %q already exists. Use --force to overwrite", clusterName)
		}
	}

	// Create the cluster config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: clusterConfig,
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/{cluster}.yaml)
	mainConfig.SetLeafConfig(clusterName, leafConfigData)

	if mainConfig.GetClusterCount() == 1 {
		// If this is the first cluster, set it as active
		if err := mainConfig.SetActiveCluster(clusterName); err != nil {
			return fmt.Errorf("failed to set active cluster: %w", err)
		}
		ctx.Info("Setting %q as the active cluster", clusterName)
	}

	// Save the main config (which will also save the leaf config)
	if err := mainConfig.Save(); err != nil {
		return fmt.Errorf("failed to save cluster configuration: %w", err)
	}

	if identityName != "" {
		ctx.Completed("Successfully added cluster %q with identity %q at %s", clusterName, identityName, address)
	} else {
		ctx.Completed("Successfully added cluster %q at %s", clusterName, address)
		ctx.Info("No identity is attached, so this cluster authenticates with whatever the environment supplies.")
		ctx.Info("Outside CI that's usually not what you want. Run 'miren login' and add it again.")
	}
	ctx.Info("Configuration saved to clientconfig.d/%s.yaml", clusterName)

	// If there's no active cluster set, suggest setting this one
	if mainConfig != nil && mainConfig.ActiveCluster() == "" {
		ctx.Info("")
		ctx.Info("Tip: Set this as your active cluster with:")
		ctx.Info("  miren cluster switch %s", clusterName)
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
	if after, ok := strings.CutPrefix(addr, "https://"); ok {
		addr = after
	} else if after, ok := strings.CutPrefix(addr, "http://"); ok {
		addr = after
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

// clusterCertificate is what probing a cluster's API endpoint tells us about
// its TLS configuration.
type clusterCertificate struct {
	// CAPEM is the certificate to pin, PEM encoded: the root of the presented
	// chain, or the certificate itself when it is self-signed.
	CAPEM string

	// Fingerprint is the SHA1 of the pinned certificate's DER bytes, hex encoded.
	Fingerprint string

	// Leaf is the certificate the server presented for itself. It carries the
	// SANs, which is what decides whether a given dial address can be verified.
	Leaf *x509.Certificate
}

// verificationName decides what name the cluster's certificate should be
// verified against when dialing with sniHost, and reports whether verification
// can succeed at all.
//
// Dialing by IP normally needs no help, because the API certificate carries
// every address the server discovered as an IP SAN, and those are the addresses
// cluster discovery hands out. A hostname is a different story: the certificate
// carries `localhost`, `api.miren`, and whatever the operator listed in
// `additional_names`, so a MagicDNS name or any other DNS record pointed at the
// cluster matches nothing and the handshake fails on a name mismatch. The same
// goes for an IP the server doesn't know it has, like one in front of a static
// NAT.
//
// api.miren is the way out. Every API certificate carries it precisely so that
// clients which cannot dial a SAN still verify against the cluster CA rather
// than skipping verification — it is how sandboxes reach the API over a bridge
// address that was leased after the certificate was issued. Borrowing it keeps
// a hostname dial fully verified instead of downgrading it.
func verificationName(sniHost string, leaf *x509.Certificate) (serverName string, verifiable bool) {
	if leaf == nil {
		return "", true
	}

	// Handles IP literals as well as names, matching against IP SANs.
	if leaf.VerifyHostname(sniHost) == nil {
		return "", true
	}

	if slices.Contains(leaf.DNSNames, clientconfig.APIServerName) {
		return clientconfig.APIServerName, true
	}

	return "", false
}

// applyVerificationName sets the cluster's TLS server name when the dial
// address can't verify on its own, warning when nothing will.
//
// Both early returns are unreachable in practice and neither warns, which is
// deliberate: every caller reaches here only after extractTLSCertificate
// succeeded on this same address, so a nil certificate or an address that no
// longer parses would both mean something upstream broke its own contract.
// Warning about that would put an alarming and unactionable message in front of
// a user whose cluster is fine. The cost if it ever did happen is a config
// written without a TLSServerName, which fails later with a name mismatch.
func applyVerificationName(ctx *Context, cfg *clientconfig.ClusterConfig, address string, cert *clusterCertificate) {
	if cert == nil {
		return
	}

	_, sniHost, err := normalizeAddress(address)
	if err != nil {
		return
	}

	serverName, verifiable := verificationName(sniHost, cert.Leaf)
	switch {
	case !verifiable:
		ctx.Warn("The cluster's certificate doesn't cover %q, so connections to it will fail.", sniHost)
		ctx.Warn("Add it to additional_names in the server config and restart, or use an address the certificate already covers.")
	case serverName != "":
		cfg.TLSServerName = serverName
		ctx.Info("Verifying the cluster's certificate as %q, since it doesn't cover %q", serverName, sniHost)
	}
}

// extractTLSCertificate connects to the server via QUIC and inspects the
// certificate it presents.
func extractTLSCertificate(ctx context.Context, address string) (*clusterCertificate, error) {
	// Normalize the address with robust parsing
	normalizedAddr, sniHost, err := normalizeAddress(address)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize address: %w", err)
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
		InitialPacketSize:    rpc.InitialPacketSize,
		HandshakeIdleTimeout: 5 * time.Second,
		MaxIdleTimeout:       10 * time.Second,
	}

	// Try to establish a QUIC connection
	udpAddr, err := net.ResolveUDPAddr("udp", normalizedAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	// Try IPv6/dual-stack binding first, fallback to IPv4
	var udpConn *net.UDPConn
	udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err != nil {
		// Fallback to IPv4 if IPv6 fails
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return nil, fmt.Errorf("failed to create UDP socket: %w", err)
		}
	}
	defer udpConn.Close()

	transport := &quic.Transport{
		Conn: udpConn,
	}
	defer transport.Close()

	conn, err := transport.Dial(connCtx, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to establish QUIC connection: %w", err)
	}
	defer conn.CloseWithError(0, "done")

	// Get the TLS connection state
	connState := conn.ConnectionState().TLS

	// Extract the certificate chain
	if len(connState.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no certificates found in TLS handshake")
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

	return &clusterCertificate{
		CAPEM:       string(certPEM),
		Fingerprint: fingerprint,
		Leaf:        connState.PeerCertificates[0],
	}, nil
}
