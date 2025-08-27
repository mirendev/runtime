package commands

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/quic-go/quic-go/http3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
)

// DebugAuthResponse represents the response from the debug-auth endpoint
type DebugAuthResponse struct {
	Success       bool              `json:"success"`
	ServerVersion string            `json:"server_version,omitempty"`
	AuthMethod    string            `json:"auth_method,omitempty"`
	Identity      string            `json:"identity,omitempty"`
	UserInfo      map[string]string `json:"user_info,omitempty"`
	Message       string            `json:"message,omitempty"`
}

// DebugConnection tests connectivity and authentication with a server
func DebugConnection(ctx *Context, opts struct {
	Identity string `short:"i" long:"identity" description:"Identity name to use for authentication"`
	Cluster  string `short:"c" long:"cluster" description:"Cluster name from config to test"`
	Server   string `short:"s" long:"server" description:"Server hostname or IP address to test directly"`
	Insecure bool   `long:"insecure" description:"Skip TLS certificate verification"`
	Verbose  bool   `short:"v" long:"verbose" description:"Show detailed connection information"`
}) error {
	// Determine which server to test
	var testServer string

	// Load configuration first to determine the server
	config, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		ctx.Warn("Failed to load config: %v", err)
		return err
	}

	if opts.Server != "" {
		// Explicit server specified
		testServer = opts.Server
	} else if opts.Cluster != "" && config != nil {
		// Use cluster's hostname
		cluster, exists := config.Clusters[opts.Cluster]
		if !exists {
			ctx.Warn("Cluster '%s' not found in config", opts.Cluster)
			return fmt.Errorf("cluster not found")
		}
		if cluster.Hostname == "" {
			ctx.Warn("Cluster '%s' has no hostname configured", opts.Cluster)
			return fmt.Errorf("cluster has no hostname")
		}
		testServer = cluster.Hostname
	} else {
		ctx.Warn("Either --server or --cluster must be specified")
		return fmt.Errorf("no server specified")
	}

	ctx.Info("Testing connection to %s", testServer)

	// Step 1: Test basic connectivity
	ctx.Info("")
	ctx.Info("Step 1: Testing network connectivity...")
	if err := testNetworkConnectivity(ctx, testServer, opts.Verbose); err != nil {
		ctx.Warn("Network connectivity test failed: %v", err)
		return err
	}
	ctx.Completed("Network connectivity: OK")

	// Step 2: Prepare authentication
	ctx.Info("")
	ctx.Info("Step 2: Preparing authentication...")

	var identity *clientconfig.IdentityConfig

	// Determine which identity to use
	if opts.Identity != "" {
		// Use specified identity
		if config != nil {
			identity, err = config.GetIdentity(opts.Identity)
			if err != nil {
				ctx.Warn("Failed to get identity '%s': %v", opts.Identity, err)
				return err
			}
			ctx.Info("Using identity: %s", opts.Identity)
		} else {
			ctx.Warn("No configuration found. Please run 'miren login' first.")
			return fmt.Errorf("no configuration found")
		}
	} else if opts.Cluster != "" {
		// Use identity from cluster
		if config != nil {
			cluster, exists := config.Clusters[opts.Cluster]
			if !exists {
				ctx.Warn("Cluster '%s' not found in config", opts.Cluster)
				return fmt.Errorf("cluster not found")
			}

			if cluster.Identity != "" {
				identity, err = config.GetIdentity(cluster.Identity)
				if err != nil {
					ctx.Warn("Failed to get identity '%s' for cluster '%s': %v", cluster.Identity, opts.Cluster, err)
					return err
				}
				ctx.Info("Using identity '%s' from cluster '%s'", cluster.Identity, opts.Cluster)
			} else if cluster.CloudAuth && cluster.ClientKey != "" {
				// Backward compatibility: create identity from cluster
				identity = &clientconfig.IdentityConfig{
					Type:       "keypair",
					PrivateKey: cluster.ClientKey,
				}
				ctx.Info("Using legacy CloudAuth from cluster '%s'", opts.Cluster)
			} else if cluster.ClientCert != "" && cluster.ClientKey != "" {
				// Backward compatibility: certificate auth
				identity = &clientconfig.IdentityConfig{
					Type:       "certificate",
					ClientCert: cluster.ClientCert,
					ClientKey:  cluster.ClientKey,
				}
				ctx.Info("Using certificate auth from cluster '%s'", opts.Cluster)
			}
		} else {
			ctx.Warn("No configuration found. Please run 'miren login' first.")
			return fmt.Errorf("no configuration found")
		}
	} else {
		ctx.Warn("No identity or cluster specified. Testing without authentication.")
	}

	// Step 3: Test authentication
	ctx.Info("")
	ctx.Info("Step 3: Testing authentication...")

	authHeader := ""
	authMethod := "none"

	if identity != nil {
		switch identity.Type {
		case "keypair":
			// For keypair auth, we need to get a JWT token
			// The server we're testing should be the cluster, not the auth server
			keyPair, err := cloudauth.LoadKeyPairFromPEM(identity.PrivateKey)
			if err != nil {
				ctx.Warn("Failed to load private key: %v", err)
				return err
			}

			// Use the issuer from the identity
			authServerURL := identity.Issuer
			if authServerURL == "" {
				// Fall back to test server if no issuer is set (for backward compatibility)
				authServerURL = normalizeServerURL(testServer)
				ctx.Warn("Identity has no issuer configured, using test server as auth server")
			}

			ctx.Info("Requesting JWT token using keypair from %s...", authServerURL)
			// Note: This will use the cached token if available
			token, err := clientconfig.AuthenticateWithKey(ctx, authServerURL, keyPair)
			if err != nil {
				ctx.Warn("Failed to authenticate with keypair: %v", err)
				ctx.Info("Note: Ensure the auth server is reachable at %s", authServerURL)
				return err
			}

			authHeader = "Bearer " + token
			authMethod = "keypair/jwt"
			ctx.Completed("JWT token obtained")

			// Decode and print JWT claims for debugging
			parser := jwt.NewParser()
			parsedToken, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
			if err == nil {
				if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
					ctx.Info("JWT Claims:")
					claimsJSON, _ := json.MarshalIndent(claims, "  ", "  ")
					ctx.Info("  %s", string(claimsJSON))
				}
			}

		case "certificate":
			// Certificate auth is handled at TLS level
			authMethod = "certificate"
			ctx.Info("Using client certificate authentication")

		default:
			ctx.Warn("Unknown identity type: %s", identity.Type)
		}
	}

	// Step 4: Test server debug endpoint
	ctx.Info("")
	ctx.Info("Step 4: Testing server debug endpoint...")

	debugURL := normalizeServerURL(testServer) + "/api/v1/debug-auth"
	req, err := http.NewRequestWithContext(ctx, "GET", debugURL, nil)
	if err != nil {
		ctx.Warn("Failed to create request: %v", err)
		return err
	}

	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	// Create HTTP/3 client with appropriate TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: opts.Insecure,
	}

	if identity != nil && identity.Type == "certificate" {
		// Add client certificate
		cert, err := tls.X509KeyPair([]byte(identity.ClientCert), []byte(identity.ClientKey))
		if err != nil {
			ctx.Warn("Failed to load client certificate: %v", err)
			return err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http3.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		ctx.Warn("Failed to connect to debug endpoint: %v", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ctx.Warn("Failed to read response: %v", err)
		return err
	}

	switch resp.StatusCode {
	case http.StatusNotFound:
		ctx.Warn("Debug endpoint not available on server (404)")
		ctx.Info("Server may be an older version without debug support")
	case http.StatusUnauthorized:
		ctx.Warn("Authentication failed (401)")
		if opts.Verbose && len(body) > 0 {
			ctx.Info("Server response: %s", string(body))
		}
		return fmt.Errorf("authentication failed")
	case http.StatusOK:
		var debugResp DebugAuthResponse
		if err := json.Unmarshal(body, &debugResp); err != nil {
			ctx.Warn("Failed to parse debug response: %v", err)
			if opts.Verbose {
				ctx.Info("Raw response: %s", string(body))
			}
		} else {
			ctx.Completed("Server connection successful!")
			ctx.Info("")
			ctx.Info("Server Information:")
			if debugResp.ServerVersion != "" {
				ctx.Info("  Version: %s", debugResp.ServerVersion)
			}
			if debugResp.AuthMethod != "" {
				ctx.Info("  Auth Method: %s", debugResp.AuthMethod)
			}
			if debugResp.Identity != "" {
				ctx.Info("  Authenticated As: %s", debugResp.Identity)
			}
			if len(debugResp.UserInfo) > 0 {
				ctx.Info("  User Info:")
				for k, v := range debugResp.UserInfo {
					ctx.Info("    %s: %s", k, v)
				}
			}
			if debugResp.Message != "" {
				ctx.Info("  Message: %s", debugResp.Message)
			}
		}
	default:
		ctx.Warn("Unexpected status code: %d", resp.StatusCode)
		if opts.Verbose && len(body) > 0 {
			ctx.Info("Server response: %s", string(body))
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Summary
	ctx.Info("")
	ctx.Info("============================================================")
	ctx.Info("Debug Summary:")
	ctx.Info("  Server: %s", testServer)
	ctx.Info("  Network: ✓")
	ctx.Info("  Auth Method: %s", authMethod)
	if identity != nil {
		ctx.Info("  Authentication: ✓")
	} else {
		ctx.Info("  Authentication: N/A (no identity)")
	}

	return nil
}

func testNetworkConnectivity(ctx *Context, server string, verbose bool) error {
	// Normalize server URL for HTTP/3
	testURL := normalizeServerURL(server) + "/api/v1/debug-auth"

	if verbose {
		ctx.Info("Testing HTTP/3 connectivity to %s...", testURL)
	}

	// Create HTTP/3 client with minimal configuration
	h3Client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Just testing connectivity, not security
			},
		},
	}
	defer h3Client.CloseIdleConnections()

	// Make a simple GET request without authentication
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h3Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect via HTTP/3: %w", err)
	}
	defer resp.Body.Close()

	if verbose {
		ctx.Info("Received response: %d %s", resp.StatusCode, resp.Status)
	}

	// We expect 401 Unauthorized without auth, but that's OK for connectivity test
	// Anything other than a connection error means we reached the server
	return nil
}

func normalizeServerURL(server string) string {
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		return strings.TrimSuffix(server, "/")
	}
	return "https://" + server
}
