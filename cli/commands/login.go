package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
)

var (
	// ErrNoAutoConfigNeeded indicates that auto-configuration is not needed or not possible
	ErrNoAutoConfigNeeded = errors.New("no auto-configuration needed")
	// ErrAutoConfigFailed indicates that auto-configuration was attempted but failed
	ErrAutoConfigFailed = errors.New("auto-configuration failed")
)

// DeviceFlowInitResponse represents the response from /api/v1/device/code
type DeviceFlowInitResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_uri"`
	VerificationURLComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	PollingInterval         int    `json:"polling_interval"`
}

// DeviceFlowExchangeResponse represents the response from /api/v1/device/token
type DeviceFlowExchangeResponse struct {
	Status           string `json:"status"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	ExpiresIn        int    `json:"expires_in,omitempty"`
}

// BeginKeyRegistrationRequest represents the request to begin key registration
type BeginKeyRegistrationRequest struct {
	Name      string `json:"name"`
	KeyType   string `json:"key_type"`
	PublicKey string `json:"public_key"`
}

// BeginKeyRegistrationResponse represents the response from begin key registration
type BeginKeyRegistrationResponse struct {
	Envelope  string `json:"envelope"`
	Challenge string `json:"challenge"`
}

// CompleteKeyRegistrationRequest represents the request to complete key registration
type CompleteKeyRegistrationRequest struct {
	Envelope  string `json:"envelope"`
	Signature string `json:"signature"`
}

// KeyRegistrationResponse represents a successfully registered key
type KeyRegistrationResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
}

// Login authenticates with miren.cloud using device flow
func Login(ctx *Context, opts struct {
	CloudURL     string `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	IdentityName string `short:"i" long:"identity" description:"Name for this identity in config" default:"cloud"`
	NoSave       bool   `long:"no-save" description:"Don't save credentials to config file"`
	NoQR         bool   `long:"no-qr" description:"Don't display QR code"`
}) error {
	// Initialize device flow
	ctx.Info("Initiating device flow authentication...")
	initResp, err := initiateDeviceFlow(opts.CloudURL)
	if err != nil {
		return fmt.Errorf("failed to initiate device flow: %w", err)
	}

	// Display instructions to user
	if initResp.VerificationURLComplete != "" {
		// Display QR code if we have a complete URL and QR is not disabled
		if !opts.NoQR {
			ctx.Completed("Scan this QR code with your phone to authenticate:")
			ctx.Info("")
			// Generate ASCII QR code to stdout
			qrterminal.GenerateWithConfig(initResp.VerificationURLComplete, qrterminal.Config{
				Level:     qrterminal.L,
				Writer:    os.Stdout,
				BlackChar: qrterminal.BLACK,
				WhiteChar: qrterminal.WHITE,
				QuietZone: 1,
			})
			ctx.Info("")
			ctx.Info("Or use one of these methods:")
		} else {
			ctx.Completed("Please authenticate using one of these methods:")
		}
		ctx.Info("")
		ctx.Info("Option 1: Visit this URL (code included):")
		ctx.Info("  %s", initResp.VerificationURLComplete)
		ctx.Info("")
		ctx.Info("Option 2: Visit this URL and enter the code manually:")
		ctx.Info("  URL: %s", initResp.VerificationURL)
		ctx.Info("  Code: %s", initResp.UserCode)
		ctx.Info("")
	} else {
		// Show the traditional flow with separate URL and code
		ctx.Completed("Please visit the following URL to authenticate:")
		ctx.Info("  %s", initResp.VerificationURL)
		ctx.Info("")
		ctx.Info("Enter this code when prompted:")
		ctx.Info("  %s", initResp.UserCode)
		ctx.Info("")
	}

	// Start polling for authentication
	ctx.Info("Waiting for authentication...")

	// Calculate timeout (10 minutes or expires_in, whichever is shorter)
	timeout := 10 * time.Minute
	if initResp.ExpiresIn > 0 && time.Duration(initResp.ExpiresIn)*time.Second < timeout {
		timeout = time.Duration(initResp.ExpiresIn) * time.Second
	}

	pollInterval := 5 * time.Second
	if initResp.PollingInterval > 0 {
		pollInterval = time.Duration(initResp.PollingInterval) * time.Second
	}

	token, err := pollForToken(ctx, opts.CloudURL, initResp.DeviceCode, pollInterval, timeout, func(status string) {
		if status == "pending" {
			fmt.Print(".")
		}
	})
	if err != nil {
		fmt.Println() // New line after dots
		return fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Println() // New line after dots

	ctx.Completed("Authentication successful!")

	// Generate and register a keypair for future authentication
	ctx.Info("Generating keypair for future authentication...")
	keyPair, err := cloudauth.GenerateKeyPair()
	if err != nil {
		ctx.Warn("Failed to generate keypair: %v", err)
		ctx.Info("You can still use token authentication")
	} else {
		// Register the public key with the server
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "miren-cli"
		}
		keyName := fmt.Sprintf("miren-cli@%s", hostname)

		if err := registerPublicKey(opts.CloudURL, token, keyPair, keyName); err != nil {
			ctx.Warn("Failed to register public key: %v", err)
			ctx.Info("You can still use token authentication")
			keyPair = nil // Don't save the key if registration failed
		} else {
			ctx.Info("Public key registered successfully")
		}
	}

	// Save to config unless --no-save is specified
	if !opts.NoSave {
		if keyPair != nil {
			// Save identity with keypair for future authentication
			if err := saveKeyPairToConfig(opts.IdentityName, opts.CloudURL, keyPair); err != nil {
				ctx.Warn("Failed to save identity to config: %v", err)
			} else {
				ctx.Info("Identity '%s' saved to config", opts.IdentityName)
				ctx.Info("Future authentication will use the keypair (no login required)")
				ctx.Info("")

				// Check if we should auto-configure a cluster
				if err := autoConfigureCluster(ctx, opts.IdentityName, opts.CloudURL, keyPair); err != nil {
					// Don't fail the login, just log if there's a real error
					if !errors.Is(err, ErrNoAutoConfigNeeded) && !errors.Is(err, ErrAutoConfigFailed) {
						// Only log unexpected errors, not expected ones
						ctx.Info("Note: %v", err)
					}
				}
			}
		} else {
			// No keypair was registered
			ctx.Warn("Authentication successful but no persistent credentials were saved")
			ctx.Info("You can still use the token with --token flag:")
			ctx.Info("  export MIREN_TOKEN=%s", token)
		}
	} else {
		if keyPair != nil {
			privateKeyPEM, _ := keyPair.PrivateKeyPEM()
			ctx.Info("Private key (not saved):")
			ctx.Info("%s", privateKeyPEM)
			ctx.Info("")
		}
		ctx.Info("Token (not saved):")
		ctx.Info("  %s", token)
		ctx.Info("")
		ctx.Info("Export as environment variable:")
		ctx.Info("  export MIREN_TOKEN=%s", token)
	}

	return nil
}

func initiateDeviceFlow(cloudURL string) (*DeviceFlowInitResponse, error) {
	url, err := url.JoinPath(cloudURL, "/api/v1/device/code")
	if err != nil {
		return nil, fmt.Errorf("invalid cloud URL: %w", err)
	}

	reqBody := map[string]string{
		"client_id": "miren-cli",
		"scope":     "full",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var initResp DeviceFlowInitResponse
	if err := json.Unmarshal(body, &initResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &initResp, nil
}

func pollForToken(ctx context.Context, cloudURL, deviceCode string, interval, maxDuration time.Duration, progress func(string)) (string, error) {
	url, err := url.JoinPath(cloudURL, "/api/v1/device/token")
	if err != nil {
		return "", fmt.Errorf("invalid cloud URL: %w", err)
	}

	reqBody := map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   "miren-cli",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Create a timeout context with the maximum duration
	timeoutCtx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return "", fmt.Errorf("authentication timed out after %v", maxDuration)
		case <-ticker.C:
			req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
			if err != nil {
				return "", err
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				// Network error, continue polling
				progress("pending")
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				progress("pending")
				continue
			}

			// Server always returns 200 with status in JSON
			var exchangeResp DeviceFlowExchangeResponse
			if err := json.Unmarshal(body, &exchangeResp); err != nil {
				return "", fmt.Errorf("failed to parse response: %w", err)
			}

			switch exchangeResp.Status {
			case "authorized":
				if exchangeResp.AccessToken == "" {
					return "", fmt.Errorf("server returned authorized status but no token")
				}
				return exchangeResp.AccessToken, nil

			case "denied":
				return "", fmt.Errorf("authorization denied by user")

			case "expired":
				return "", fmt.Errorf("device code expired")

			case "pending":
				progress("pending")
				// Continue polling

			case "error":
				switch exchangeResp.Error {
				case "slow_down":
					// Increase polling interval
					ticker.Reset(interval * 2)
					progress("pending")
				case "authorization_pending":
					progress("pending")
				default:
					return "", fmt.Errorf("server error: %s - %s", exchangeResp.Error, exchangeResp.ErrorDescription)
				}

			default:
				// Unknown status, treat as pending
				progress("pending")
			}
		}
	}
}

// getConfigDirPath returns the path to the clientconfig.d directory
func getConfigDirPath() (string, error) {
	// Check environment variable first
	if path := os.Getenv(clientconfig.EnvConfigPath); path != "" {
		// If a custom config path is specified, use its directory
		return filepath.Join(filepath.Dir(path), "clientconfig.d"), nil
	}

	// Use default path in home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".config/miren/clientconfig.d"), nil
}

// registerPublicKey registers a public key with the cloud server
func registerPublicKey(cloudURL, token string, keyPair *cloudauth.KeyPair, keyName string) error {
	// Get public key in PEM format
	publicKeyPEM, err := keyPair.PublicKeyPEM()
	if err != nil {
		return fmt.Errorf("failed to encode public key: %w", err)
	}

	// Step 1: Begin key registration
	beginURL, err := url.JoinPath(cloudURL, "/api/v1/users/keys/begin")
	if err != nil {
		return fmt.Errorf("invalid cloud URL: %w", err)
	}
	beginReq := BeginKeyRegistrationRequest{
		Name:      keyName,
		KeyType:   "ed25519",
		PublicKey: publicKeyPEM,
	}

	beginData, err := json.Marshal(beginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal begin request: %w", err)
	}

	req, err := http.NewRequest("POST", beginURL, bytes.NewBuffer(beginData))
	if err != nil {
		return fmt.Errorf("failed to create begin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send begin request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if errMsg, ok := errResp["error"]; ok {
			return fmt.Errorf("server error: %s", errMsg)
		}
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var beginResp BeginKeyRegistrationResponse
	if err := json.Unmarshal(body, &beginResp); err != nil {
		return fmt.Errorf("failed to parse begin response: %w", err)
	}

	// Step 2: Sign the challenge
	data, err := base64.StdEncoding.DecodeString(beginResp.Challenge)
	if err != nil {
		return fmt.Errorf("failed to decode challenge: %w", err)
	}

	signature, err := keyPair.Sign(data)
	if err != nil {
		return fmt.Errorf("failed to sign challenge: %w", err)
	}

	// Step 3: Complete key registration
	completeURL, err := url.JoinPath(cloudURL, "/api/v1/users/keys/complete")
	if err != nil {
		return fmt.Errorf("invalid cloud URL: %w", err)
	}
	completeReq := CompleteKeyRegistrationRequest{
		Envelope:  beginResp.Envelope,
		Signature: signature,
	}

	completeData, err := json.Marshal(completeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal complete request: %w", err)
	}

	req, err = http.NewRequest("POST", completeURL, bytes.NewBuffer(completeData))
	if err != nil {
		return fmt.Errorf("failed to create complete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send complete request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if errMsg, ok := errResp["error"]; ok {
			return fmt.Errorf("server error: %s", errMsg)
		}
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// saveKeyPairToConfig saves a keypair as an identity in clientconfig.d
func saveKeyPairToConfig(identityName, cloudURL string, keyPair *cloudauth.KeyPair) error {
	// Get private key in PEM format
	privateKeyPEM, err := keyPair.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	// Parse cloud URL to get the issuer
	issuer := strings.TrimSuffix(cloudURL, "/")
	if !strings.HasPrefix(issuer, "http://") && !strings.HasPrefix(issuer, "https://") {
		issuer = "https://" + issuer
	}

	// Load or create the main client config
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil {
		// If no config exists, create a new one
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load client config: %w", err)
		}
	}

	// Create the identity config data
	leafConfigData := &clientconfig.ConfigData{
		Identities: map[string]*clientconfig.IdentityConfig{
			identityName: {
				Type:       "keypair",
				Issuer:     issuer,
				PrivateKey: privateKeyPEM,
			},
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/identity-{name}.yaml)
	mainConfig.SetLeafConfig("identity-"+identityName, leafConfigData)

	// Save the main config (which will also save the leaf config)
	return mainConfig.Save()
}

// autoConfigureCluster checks if there are any local clusters configured,
// and if not, fetches available clusters from the server and automatically
// configures the client if there's only one cluster available
func autoConfigureCluster(ctx *Context, identityName, cloudURL string, keyPair *cloudauth.KeyPair) error {
	// Load the main config to check if any clusters are configured
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if any clusters are already configured
	if mainConfig != nil && mainConfig.HasAnyClusters() {
		// Clusters already configured, no auto-configuration needed
		return ErrNoAutoConfigNeeded
	}

	ctx.Info("Checking for available clusters...")

	// Create identity config for fetching clusters
	issuer := strings.TrimSuffix(cloudURL, "/")
	if !strings.HasPrefix(issuer, "http://") && !strings.HasPrefix(issuer, "https://") {
		issuer = "https://" + issuer
	}

	privateKeyPEM, err := keyPair.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	identity := &clientconfig.IdentityConfig{
		Type:       "keypair",
		Issuer:     issuer,
		PrivateKey: privateKeyPEM,
	}

	// Fetch available clusters from the server
	clusters, err := fetchAvailableClusters(ctx, identity)
	if err != nil {
		return fmt.Errorf("failed to fetch available clusters: %w", err)
	}

	// Filter out clusters without API addresses
	var validClusters []ClusterResponse
	for _, cluster := range clusters {
		if len(cluster.APIAddresses) > 0 {
			validClusters = append(validClusters, cluster)
		}
	}

	if len(validClusters) == 0 {
		ctx.Info("No clusters available for your account")
		return ErrNoAutoConfigNeeded
	}

	if len(validClusters) > 1 {
		// Multiple clusters available, don't auto-configure
		ctx.Info("Multiple clusters available. Run 'miren cluster add' to select one:")
		for _, cluster := range validClusters {
			ctx.Info("  - %s (%s)", cluster.Name, cluster.OrganizationName)
		}
		return ErrNoAutoConfigNeeded
	}

	// Only one cluster available, auto-configure it
	cluster := validClusters[0]
	ctx.Info("Found one cluster: %s (%s)", cluster.Name, cluster.OrganizationName)
	ctx.Info("Automatically configuring cluster connection...")

	// Try to connect to the cluster and extract TLS certificate
	// Don't try localhost for auto-configuration - only try advertised addresses
	workingAddress, caCert, err := tryConnectToCluster(ctx, &cluster, false)
	if err != nil {
		ctx.Warn("Could not automatically connect to cluster: %v", err)
		ctx.Info("Run 'miren cluster add' manually to configure the cluster connection")
		return ErrAutoConfigFailed
	}

	// Create the cluster configuration
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname:     workingAddress,
		AllAddresses: cluster.APIAddresses,
		Identity:     identityName,
		CACert:       caCert,
	}

	// Reload config to get latest state
	mainConfig, err = clientconfig.LoadConfig()
	if err != nil {
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load client config: %w", err)
		}
	}

	// Use cluster name as the local name
	clusterName := cluster.Name

	// Create the cluster config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: clusterConfig,
		},
	}

	// Add as a leaf config
	mainConfig.SetLeafConfig(clusterName, leafConfigData)

	// Save the main config
	if err := mainConfig.Save(); err != nil {
		return fmt.Errorf("failed to save cluster configuration: %w", err)
	}

	ctx.Completed("Automatically configured cluster '%s' at %s", clusterName, workingAddress)

	// If there's no active cluster set, set this one
	if mainConfig.ActiveCluster() == "" {
		// Set as active cluster
		mainConfig.SetActiveCluster(clusterName)
		if err := mainConfig.Save(); err != nil {
			ctx.Warn("Failed to set as active cluster: %v", err)
		} else {
			ctx.Info("Set '%s' as the active cluster", clusterName)
		}
	}

	return nil
}
