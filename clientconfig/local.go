package clientconfig

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/quic-go/quic-go"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/rpc"
)

func Local(cc *caauth.ClientCertificate, listenAddr string) *Config {
	addr := listenAddr
	if addr == "" {
		addr = "localhost:8443"
	} else {
		_, port, err := net.SplitHostPort(addr)
		if err == nil {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
	}

	cfg := NewConfig()
	cfg.SetCluster("local", &ClusterConfig{
		Hostname:   addr,
		CACert:     string(cc.CACert),
		ClientCert: string(cc.CertPEM),
		ClientKey:  string(cc.KeyPEM),
	})
	cfg.SetActiveCluster("local")

	return cfg
}

func (c *Config) RPCOptions(ctx context.Context) ([]rpc.StateOption, error) {
	if c.ActiveCluster() == "" {
		return nil, nil
	}

	active, err := c.GetCluster(c.ActiveCluster())
	if err != nil {
		return nil, nil
	}

	return active.RPCOptionsWithName(ctx, c, c.ActiveCluster())
}

func (c *Config) State(ctx context.Context, opts ...rpc.StateOption) (*rpc.State, error) {
	rpcOpts, err := c.RPCOptions(ctx)
	if err != nil {
		return nil, err
	}
	opts = append(rpcOpts, opts...)
	return rpc.NewState(ctx, opts...)
}

// RPCOptions returns RPC options without a cluster name (deprecated, use RPCOptionsWithName)
func (c *ClusterConfig) RPCOptions(ctx context.Context, config *Config) ([]rpc.StateOption, error) {
	return c.RPCOptionsWithName(ctx, config, "")
}

// RPCOptionsWithName returns RPC options with cluster name for caching
func (c *ClusterConfig) RPCOptionsWithName(ctx context.Context, config *Config, clusterName string) ([]rpc.StateOption, error) {
	// If AllAddresses is set, find a working address
	hostname := c.Hostname
	if len(c.AllAddresses) > 0 && clusterName != "" {
		// Check for cached address first
		if cachedAddr, err := getCachedAddress(clusterName); err == nil && cachedAddr != "" {
			// Verify the cached address is still in the list
			for _, addr := range c.AllAddresses {
				if addr == cachedAddr {
					hostname = cachedAddr
					goto foundAddress
				}
			}
		}

		// No valid cached address, probe all addresses
		workingAddr, err := findWorkingAddress(ctx, c.AllAddresses)
		if err == nil {
			hostname = workingAddr
			// Cache the working address (ignore errors)
			_ = saveAddressToCache(clusterName, workingAddr)
		}
		// If probing fails, fall back to configured hostname
	}

foundAddress:
	// Check if cluster references an identity
	if c.Identity != "" {
		identity, err := config.GetIdentity(c.Identity)
		if err != nil {
			return nil, fmt.Errorf("failed to get identity %q: %w", c.Identity, err)
		}

		// Handle different identity types
		switch identity.Type {
		case "keypair":
			// Load the private key from identity
			keyPair, err := cloudauth.LoadKeyPairFromPEM(identity.PrivateKey)
			if err != nil {
				return nil, fmt.Errorf("failed to load private key: %w", err)
			}

			// Use the issuer from the identity, or fall back to cluster hostname
			authServer := identity.Issuer
			if authServer == "" {
				authServer = hostname
			}

			// Get JWT token using the challenge-response flow
			token, err := AuthenticateWithKey(ctx, authServer, keyPair)
			if err != nil {
				return nil, fmt.Errorf("failed to authenticate with cloud: %w", err)
			}

			// Return options with bearer token
			base := []rpc.StateOption{
				rpc.WithEndpoint(hostname),
				rpc.WithBindAddr("[::]:0"),
				rpc.WithBearerToken(token),
			}

			if c.CACert != "" {
				base = append(base, rpc.WithCertificateVerification([]byte(c.CACert)))
			}

			return base, nil

		case "certificate":
			// Handle certificate-based authentication from identity
			return []rpc.StateOption{
				rpc.WithCertPEMs(
					[]byte(identity.ClientCert),
					[]byte(identity.ClientKey),
				),
				rpc.WithCertificateVerification([]byte(c.CACert)),
				rpc.WithEndpoint(hostname),
				rpc.WithBindAddr("[::]:0"),
			}, nil

		default:
			return nil, fmt.Errorf("unknown identity type: %s", identity.Type)
		}
	}

	// Backward compatibility: Handle cloud authentication (deprecated)
	if c.CloudAuth {
		// Load the private key from ClientKey field
		keyPair, err := cloudauth.LoadKeyPairFromPEM(c.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load private key: %w", err)
		}

		// Get JWT token using the challenge-response flow
		token, err := AuthenticateWithKey(ctx, hostname, keyPair)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with cloud: %w", err)
		}

		// Return options with bearer token
		return []rpc.StateOption{
			rpc.WithEndpoint(hostname),
			rpc.WithBindAddr("[::]:0"),
			rpc.WithBearerToken(token),
		}, nil
	}

	// Handle insecure connection
	if c.Insecure {
		return []rpc.StateOption{
			rpc.WithEndpoint(hostname),
			rpc.WithBindAddr("[::]:0"),
			rpc.WithSkipVerify,
		}, nil
	}

	// Backward compatibility: Handle certificate-based authentication (deprecated)
	return []rpc.StateOption{
		rpc.WithCertPEMs(
			[]byte(c.ClientCert),
			[]byte(c.ClientKey),
		),
		rpc.WithCertificateVerification([]byte(c.CACert)),
		rpc.WithEndpoint(hostname),
		rpc.WithBindAddr("[::]:0"),
	}, nil
}

func (c *ClusterConfig) State(ctx context.Context, config *Config, opts ...rpc.StateOption) (*rpc.State, error) {
	rpcOpts, err := c.RPCOptions(ctx, config)
	if err != nil {
		return nil, err
	}
	opts = append(opts, rpcOpts...)
	return rpc.NewState(ctx, opts...)
}

// BeginAuthRequest is the request to begin authentication
type BeginAuthRequest struct {
	Fingerprint string `json:"fingerprint"`
}

// BeginAuthResponse is the response from begin authentication
type BeginAuthResponse struct {
	Envelope  string `json:"envelope"`
	Challenge string `json:"challenge"`
}

// CompleteAuthRequest is the request to complete authentication
type CompleteAuthRequest struct {
	Envelope  string `json:"envelope"`
	Signature string `json:"signature"`
}

// CompleteAuthResponse is the response from complete authentication
type CompleteAuthResponse struct {
	User  interface{} `json:"user"`
	Token string      `json:"token"`
}

// getCacheDir returns the path to the miren cache directory
func getCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(home, ".cache", "miren")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return "", err
	}
	return cacheDir, nil
}

// getCachedToken retrieves and validates a cached JWT token
func getCachedToken(fingerprint string) (string, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return "", err
	}

	tokenPath := filepath.Join(cacheDir, fingerprint)
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No cached token
		}
		return "", err
	}

	tokenString := string(tokenData)

	// Parse the JWT without verification to check expiry
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", nil // Invalid token, need new one
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", nil // Invalid claims, need new one
	}

	// Check if token is expired
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() >= int64(exp) {
			return "", nil // Token expired
		}
		// Add a buffer of 5 minutes to avoid edge cases
		if time.Now().Unix() >= int64(exp)-300 {
			return "", nil // Token expiring soon
		}
	} else {
		return "", nil // No expiry claim, need new one
	}

	return tokenString, nil
}

// saveTokenToCache saves a JWT token to the cache
func saveTokenToCache(fingerprint, token string) error {
	cacheDir, err := getCacheDir()
	if err != nil {
		return err
	}

	tokenPath := filepath.Join(cacheDir, fingerprint)
	return os.WriteFile(tokenPath, []byte(token), 0600)
}

// getCachedAddress retrieves a cached working address for a cluster
func getCachedAddress(clusterName string) (string, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return "", err
	}

	addressPath := filepath.Join(cacheDir, "cluster_address_"+clusterName)
	addressData, err := os.ReadFile(addressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No cached address
		}
		return "", err
	}

	// Check if the cached address is still fresh (less than 1 hour old)
	fileInfo, err := os.Stat(addressPath)
	if err != nil {
		return "", err
	}

	if time.Since(fileInfo.ModTime()) > time.Hour {
		return "", nil // Cached address is too old
	}

	return string(addressData), nil
}

// saveAddressToCache saves a working address to the cache
func saveAddressToCache(clusterName, address string) error {
	cacheDir, err := getCacheDir()
	if err != nil {
		return err
	}

	addressPath := filepath.Join(cacheDir, "cluster_address_"+clusterName)
	return os.WriteFile(addressPath, []byte(address), 0600)
}

// probeAddress checks if an address is reachable via QUIC
func probeAddress(ctx context.Context, address string) error {
	// Normalize the address - add default port if not specified
	normalizedAddr := address
	if !strings.Contains(address, ":") {
		normalizedAddr = address + ":8443"
	}

	// Create a context with short timeout for probing
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Try to establish a QUIC connection
	udpAddr, err := net.ResolveUDPAddr("udp", normalizedAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("failed to create UDP socket: %w", err)
	}
	defer udpConn.Close()

	transport := &quic.Transport{
		Conn: udpConn,
	}
	defer transport.Close()

	// Parse the address to get the hostname for SNI
	host, _, err := net.SplitHostPort(normalizedAddr)
	if err != nil {
		host = address
	}

	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,           // We're just checking connectivity
		NextProtos:         []string{"h3"}, // HTTP/3 ALPN
	}

	quicConfig := &quic.Config{
		HandshakeIdleTimeout: 2 * time.Second,
		MaxIdleTimeout:       3 * time.Second,
	}

	conn, err := transport.Dial(probeCtx, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		return err
	}
	conn.CloseWithError(0, "probe complete")

	return nil
}

// findWorkingAddress probes all addresses in parallel and returns the first working one
func findWorkingAddress(ctx context.Context, addresses []string) (string, error) {
	if len(addresses) == 0 {
		return "", fmt.Errorf("no addresses to probe")
	}

	// If there's only one address, just return it
	if len(addresses) == 1 {
		return addresses[0], nil
	}

	type result struct {
		address string
		err     error
	}

	// Create a channel to receive results
	resultChan := make(chan result, len(addresses))

	// Create a context that cancels when we find a working address
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Launch a goroutine for each address
	for _, addr := range addresses {
		go func(address string) {
			err := probeAddress(probeCtx, address)
			resultChan <- result{address: address, err: err}
		}(addr)
	}

	// Wait for the first successful probe or all failures
	var errors []string
	for i := 0; i < len(addresses); i++ {
		select {
		case res := <-resultChan:
			if res.err == nil {
				// Found a working address, cancel other probes
				cancel()
				return res.address, nil
			}
			errors = append(errors, fmt.Sprintf("%s: %v", res.address, res.err))
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// All addresses failed
	return "", fmt.Errorf("all addresses failed: %s", strings.Join(errors, "; "))
}

// AuthenticateWithKey performs the challenge-response authentication flow
func AuthenticateWithKey(ctx context.Context, hostname string, keyPair *cloudauth.KeyPair) (string, error) {
	fingerprint := keyPair.Fingerprint()

	// Check for cached token first
	if cachedToken, err := getCachedToken(fingerprint); err == nil && cachedToken != "" {
		return cachedToken, nil
	}

	// Build the cloud URL
	cloudURL := hostname
	if !strings.HasPrefix(cloudURL, "http://") && !strings.HasPrefix(cloudURL, "https://") {
		cloudURL = "https://" + cloudURL
	}

	// Step 1: Begin authentication
	beginURL, err := url.JoinPath(cloudURL, "/auth/user-key/begin")
	if err != nil {
		return "", fmt.Errorf("invalid cloud URL: %w", err)
	}
	beginReq := BeginAuthRequest{
		Fingerprint: keyPair.Fingerprint(),
	}

	beginData, err := json.Marshal(beginReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal begin request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", beginURL, bytes.NewBuffer(beginData))
	if err != nil {
		return "", fmt.Errorf("failed to create begin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send begin request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if errMsg, ok := errResp["error"]; ok {
			return "", fmt.Errorf("authentication failed: %s", errMsg)
		}
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var beginResp BeginAuthResponse
	if err := json.Unmarshal(body, &beginResp); err != nil {
		return "", fmt.Errorf("failed to parse begin response: %w", err)
	}

	challenge, err := base64.URLEncoding.DecodeString(beginResp.Challenge)
	if err != nil {
		return "", fmt.Errorf("failed to decode challenge: %w", err)
	}

	// Step 2: Sign the challenge
	signature, err := keyPair.Sign(challenge)
	if err != nil {
		return "", fmt.Errorf("failed to sign challenge: %w", err)
	}

	// Step 3: Complete authentication
	completeURL, err := url.JoinPath(cloudURL, "/auth/user-key/complete")
	if err != nil {
		return "", fmt.Errorf("invalid cloud URL: %w", err)
	}
	completeReq := CompleteAuthRequest{
		Envelope:  beginResp.Envelope,
		Signature: signature,
	}

	completeData, err := json.Marshal(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal complete request: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, "POST", completeURL, bytes.NewBuffer(completeData))
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send complete request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if errMsg, ok := errResp["error"]; ok {
			return "", fmt.Errorf("authentication failed: %s", errMsg)
		}
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var completeResp CompleteAuthResponse
	if err := json.Unmarshal(body, &completeResp); err != nil {
		return "", fmt.Errorf("failed to parse complete response: %w", err)
	}

	if completeResp.Token == "" {
		return "", fmt.Errorf("server did not return a token")
	}

	// Cache the token for future use
	_ = saveTokenToCache(fingerprint, completeResp.Token) // Ignore error - caching is optional

	return completeResp.Token, nil
}
