package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"miren.dev/runtime/clientconfig"
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

// Login authenticates with miren.cloud using device flow
func Login(ctx *Context, opts struct {
	CloudURL    string `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	ClusterName string `short:"n" long:"name" description:"Name for this cluster in config" default:"cloud"`
	NoSave      bool   `long:"no-save" description:"Don't save credentials to config file"`
	NoQR        bool   `long:"no-qr" description:"Don't display QR code"`
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

	token, err := pollForToken(context.Background(), opts.CloudURL, initResp.DeviceCode, pollInterval, timeout, func(status string) {
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

	// Save to config unless --no-save is specified
	if !opts.NoSave {
		if err := saveTokenToConfig(opts.ClusterName, opts.CloudURL, token); err != nil {
			ctx.Warn("Failed to save credentials to config: %v", err)
			ctx.Info("You can still use the token with --token flag:")
			ctx.Info("  export MIREN_TOKEN=%s", token)
		} else {
			ctx.Info("Credentials saved to config as cluster '%s'", opts.ClusterName)
			ctx.Info("Use 'miren config set-active %s' to make it the active cluster", opts.ClusterName)
		}
	} else {
		ctx.Info("Token (not saved):")
		ctx.Info("  %s", token)
		ctx.Info("")
		ctx.Info("Export as environment variable:")
		ctx.Info("  export MIREN_TOKEN=%s", token)
	}

	return nil
}

func initiateDeviceFlow(cloudURL string) (*DeviceFlowInitResponse, error) {
	url := strings.TrimSuffix(cloudURL, "/") + "/api/v1/device/code"

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
	url := strings.TrimSuffix(cloudURL, "/") + "/api/v1/device/token"

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

func saveTokenToConfig(clusterName, cloudURL, token string) error {
	// Load existing config or create new one
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if err == clientconfig.ErrNoConfig {
			config = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Create or update cluster config
	if config.Clusters == nil {
		config.Clusters = make(map[string]*clientconfig.ClusterConfig)
	}

	// Parse cloud URL to get hostname
	hostname := strings.TrimPrefix(cloudURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")
	hostname = strings.TrimSuffix(hostname, "/")

	config.Clusters[clusterName] = &clientconfig.ClusterConfig{
		Hostname:   hostname,
		ClientCert: "",    // Token auth doesn't use certs
		ClientKey:  token, // Store token in ClientKey field
		CACert:     "",
		Insecure:   false,
	}

	// Set as active cluster if it's the only one
	if len(config.Clusters) == 1 {
		config.ActiveCluster = clusterName
	}

	// Save config
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SaveTo(configPath)
}

func getConfigPath() (string, error) {
	// Check environment variable first
	if path := os.Getenv(clientconfig.EnvConfigPath); path != "" {
		return path, nil
	}

	// Use default path in home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, clientconfig.DefaultConfigPath), nil
}
