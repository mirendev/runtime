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
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/ui"
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
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	ExpiresIn        int    `json:"expires_in,omitempty"`
}

// deviceFlowTokens carries the credentials returned by a successful device-flow
// exchange. The refresh token is present only when the cloud supports ephemeral
// login; older clouds return an empty RefreshToken.
type deviceFlowTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
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

// getOrCreateKey checks if a key with the given name already exists in the config,
// and reuses it if found. Otherwise, it generates a new key.
// Returns the keypair and any error.
func getOrCreateKey(ctx *Context, keyName string) (*cloudauth.KeyPair, error) {
	// Try to load existing config
	config, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Check if key already exists
	if config != nil && config.HasKey(keyName) {
		keyConfig, err := config.GetKey(keyName)
		if err != nil {
			return nil, fmt.Errorf("failed to get key: %w", err)
		}

		// Load the keypair from the stored private key
		keyPair, err := cloudauth.LoadKeyPairFromPEM(keyConfig.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load keypair from config: %w", err)
		}

		ctx.Info("Found existing key: %s", keyName)
		return keyPair, nil
	}

	// No existing key found, generate a new one
	ctx.Info("Generating new keypair for future authentication...")
	keyPair, err := cloudauth.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	return keyPair, nil
}

// LoginWithDefaults runs the login flow with default settings
func LoginWithDefaults(ctx *Context) error {
	return login(ctx, "https://miren.cloud", "cloud", "miren-cli", false, false, false)
}

// Login authenticates with miren.cloud using device flow
func Login(ctx *Context, opts struct {
	CloudURL      string `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	IdentityName  string `short:"i" long:"identity" description:"Name for this identity in config" default:"cloud"`
	KeyName       string `short:"k" long:"key-name" description:"Name for the authentication key" default:"miren-cli"`
	NoSave        bool   `long:"no-save" description:"Don't save credentials to config file"`
	Force         bool   `short:"f" long:"force" description:"Overwrite existing identity without prompting"`
	PersistentKey bool   `long:"persistent-key" description:"Register a persistent key instead of the default renewable token login"`
}) error {
	return login(ctx, opts.CloudURL, opts.IdentityName, opts.KeyName, opts.NoSave, opts.Force, opts.PersistentKey)
}

func login(ctx *Context, cloudURL, identityName, keyName string, noSave, force, persistentKey bool) error {
	// Check for existing identity (unless forcing or not saving)
	if !force && !noSave {
		config, err := clientconfig.LoadConfig()
		if err != nil && err != clientconfig.ErrNoConfig {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if config != nil && config.HasIdentity(identityName) {
			if ui.IsInteractive() {
				// Try to get user info for the existing identity
				userInfo := getIdentityUserInfo(ctx, config, identityName)

				// Prompt user to choose: update existing or create new
				updateText := fmt.Sprintf("Update '%s' (re-authenticate)", identityName)
				if userInfo != "" {
					updateText = fmt.Sprintf("Update '%s' - %s (re-authenticate)", identityName, userInfo)
				}

				items := []ui.PickerItem{
					ui.SimplePickerItem{Text: updateText},
					ui.SimplePickerItem{Text: "Add new identity"},
				}

				title := fmt.Sprintf("Identity '%s' already exists", identityName)
				selected, err := ui.RunPicker(items,
					ui.WithTitle(title),
				)
				if err != nil {
					return fmt.Errorf("failed to run picker: %w", err)
				}
				if selected == nil {
					return fmt.Errorf("cancelled")
				}

				if selected.ID() == "Add new identity" {
					newName, err := ui.PromptForInput(
						ui.WithLabel("Enter name for new identity:"),
						ui.WithPlaceholder("personal"),
					)
					if err != nil {
						return err
					}
					if newName == "" {
						return fmt.Errorf("identity name cannot be empty")
					}
					if config.HasIdentity(newName) {
						return fmt.Errorf("identity %q already exists; choose a different name or select 'Update' to re-authenticate", newName)
					}
					identityName = newName
				}
			} else {
				// Non-interactive: require explicit --force or --identity
				return fmt.Errorf("identity '%s' already exists; use --force to overwrite or --identity to specify a different name", identityName)
			}
		}
	}

	// Decide whether to use the persistent-key flow. Ephemeral token login is the
	// default; --persistent-key opts back in. An existing keypair identity keeps
	// its key on re-login so we never silently orphan a registered server-side key.
	usePersistentKey := persistentKey
	if !usePersistentKey {
		if existing, err := clientconfig.LoadConfig(); err == nil && existing != nil {
			if id, err := existing.GetIdentity(identityName); err == nil && id != nil && id.Type == clientconfig.IdentityKeypair {
				usePersistentKey = true
				ctx.Info("Identity '%s' already uses a persistent key; keeping it.", identityName)
			}
		}
	}

	// Initialize device flow
	ctx.Info("Initiating device flow authentication...")
	initResp, err := initiateDeviceFlow(cloudURL)
	if err != nil {
		return fmt.Errorf("failed to initiate device flow: %w", err)
	}

	// Display instructions to user
	if initResp.VerificationURLComplete != "" {
		ctx.Completed("Please authenticate using one of these methods:")
		ctx.Info("")
		ctx.Info("Option 1: Visit this URL (code included):")
		ctx.Info("  %s", initResp.VerificationURLComplete)
		ctx.Info("")
		ctx.Info("Option 2: Visit this URL and enter the code manually:")
		ctx.Info("  URL: %s", initResp.VerificationURL)
		ctx.Info("  Code: %s", initResp.UserCode)
		ctx.Info("")
	} else {
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

	tokens, err := pollForToken(ctx, cloudURL, initResp.DeviceCode, pollInterval, timeout, func(status string) {
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

	// An ephemeral login needs a refresh token to renew without re-login. If the
	// cloud is too old to return one, fall back to the persistent-key flow so
	// login still produces durable credentials.
	if !usePersistentKey && tokens.RefreshToken == "" {
		ctx.Warn("Cloud did not return a refresh token; falling back to persistent key authentication.")
		usePersistentKey = true
	}

	if noSave {
		return printUnsavedCredentials(ctx, cloudURL, tokens, usePersistentKey, keyName)
	}

	if usePersistentKey {
		return persistKeypairIdentity(ctx, identityName, cloudURL, keyName, tokens.AccessToken)
	}
	return persistTokenIdentity(ctx, identityName, cloudURL, tokens)
}

// persistKeypairIdentity runs the persistent-key flow: register (or reuse) an
// ed25519 key with the cloud, store a keypair identity, and auto-configure a
// cluster. accessToken is the device-flow JWT used only to authorize key
// registration.
func persistKeypairIdentity(ctx *Context, identityName, cloudURL, keyName, accessToken string) error {
	keyPair, err := getOrCreateKey(ctx, keyName)
	if err != nil {
		return fmt.Errorf("failed to get or create keypair: %w", err)
	}

	ctx.Info("Registering public key with server...")
	if err := registerPublicKey(cloudURL, accessToken, keyPair, keyName); err != nil {
		return fmt.Errorf("failed to register public key: %w", err)
	}
	ctx.Info("Public key registered successfully")

	if err := saveKeyPairToConfig(identityName, cloudURL, keyPair, keyName); err != nil {
		return fmt.Errorf("failed to save identity to config: %w", err)
	}
	ctx.Info("Identity '%s' saved to config", identityName)
	ctx.Info("Future authentication will use the keypair (no login required)")
	ctx.Info("")

	privateKeyPEM, err := keyPair.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}
	identity := &clientconfig.IdentityConfig{
		Type:       clientconfig.IdentityKeypair,
		Issuer:     clientconfig.NormalizeIssuerURL(cloudURL),
		PrivateKey: privateKeyPEM,
	}
	maybeAutoConfigureCluster(ctx, identityName, cloudURL, identity)
	return nil
}

// persistTokenIdentity runs the ephemeral flow: store the device-flow access and
// refresh tokens as a "token" identity and auto-configure a cluster. No key is
// registered with the cloud, which is the whole point of ephemeral login.
func persistTokenIdentity(ctx *Context, identityName, cloudURL string, tokens *deviceFlowTokens) error {
	issuer := clientconfig.NormalizeIssuerURL(cloudURL)
	identity := &clientconfig.IdentityConfig{
		Type:         clientconfig.IdentityToken,
		Issuer:       issuer,
		Token:        tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}

	if err := saveTokenIdentityToConfig(identityName, identity); err != nil {
		return fmt.Errorf("failed to save identity to config: %w", err)
	}
	ctx.Info("Identity '%s' saved to config", identityName)
	ctx.Info("Future authentication reuses the saved token (renewed automatically)")
	ctx.Info("")

	maybeAutoConfigureCluster(ctx, identityName, cloudURL, identity)
	return nil
}

// maybeAutoConfigureCluster attempts cluster auto-configuration, logging only
// genuinely unexpected errors (not the expected "nothing to configure" cases).
func maybeAutoConfigureCluster(ctx *Context, identityName, cloudURL string, identity *clientconfig.IdentityConfig) {
	if err := autoConfigureCluster(ctx, identityName, cloudURL, identity); err != nil {
		if !errors.Is(err, ErrNoAutoConfigNeeded) && !errors.Is(err, ErrAutoConfigFailed) {
			ctx.Info("Note: %v", err)
		}
	}
}

// printUnsavedCredentials handles --no-save: it prints the obtained credentials
// instead of persisting them. For a persistent-key login it prints the freshly
// generated key; otherwise it prints the access token (and warns about the
// refresh token, a 7-day credential, landing in terminal scrollback).
func printUnsavedCredentials(ctx *Context, cloudURL string, tokens *deviceFlowTokens, usePersistentKey bool, keyName string) error {
	if usePersistentKey {
		keyPair, err := getOrCreateKey(ctx, keyName)
		if err != nil {
			return fmt.Errorf("failed to get or create keypair: %w", err)
		}

		// Register even though we aren't saving: a private key the cloud has
		// never seen cannot authenticate, so printing it unregistered would hand
		// the user a credential that silently fails later.
		ctx.Info("Registering public key with server...")
		if err := registerPublicKey(cloudURL, tokens.AccessToken, keyPair, keyName); err != nil {
			return fmt.Errorf("failed to register public key: %w", err)
		}

		privateKeyPEM, err := keyPair.PrivateKeyPEM()
		if err != nil {
			return fmt.Errorf("failed to encode private key: %w", err)
		}
		ctx.Info("Private key (not saved):")
		ctx.Info("%s", privateKeyPEM)
		ctx.Info("")
	}

	ctx.Info("Access token (not saved):")
	ctx.Info("  %s", tokens.AccessToken)
	ctx.Info("")
	if tokens.RefreshToken != "" {
		ctx.Warn("A refresh token was issued but not printed; it is a long-lived credential.")
		ctx.Info("Re-run 'miren login' without --no-save to store it securely.")
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

func pollForToken(ctx context.Context, cloudURL, deviceCode string, interval, maxDuration time.Duration, progress func(string)) (*deviceFlowTokens, error) {
	url, err := url.JoinPath(cloudURL, "/api/v1/device/token")
	if err != nil {
		return nil, fmt.Errorf("invalid cloud URL: %w", err)
	}

	reqBody := map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   "miren-cli",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
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
			if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("authentication timed out after %v: %w", maxDuration, timeoutCtx.Err())
			}
			return nil, timeoutCtx.Err()
		case <-ticker.C:
			// Bind each poll request to the timeout context so a hung server
			// cannot outlive the overall deadline.
			req, err := http.NewRequestWithContext(timeoutCtx, "POST", url, bytes.NewBuffer(jsonData))
			if err != nil {
				return nil, err
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
				return nil, fmt.Errorf("failed to parse response: %w", err)
			}

			switch exchangeResp.Status {
			case "authorized":
				if exchangeResp.AccessToken == "" {
					return nil, fmt.Errorf("server returned authorized status but no token")
				}
				return &deviceFlowTokens{
					AccessToken:  exchangeResp.AccessToken,
					RefreshToken: exchangeResp.RefreshToken,
					ExpiresIn:    exchangeResp.ExpiresIn,
				}, nil

			case "denied":
				return nil, fmt.Errorf("authorization denied by user")

			case "expired":
				return nil, fmt.Errorf("device code expired")

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
					return nil, fmt.Errorf("server error: %s - %s", exchangeResp.Error, exchangeResp.ErrorDescription)
				}

			default:
				// Unknown status, treat as pending
				progress("pending")
			}
		}
	}
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

	if resp.StatusCode == http.StatusConflict {
		// Key already registered
		return nil
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

// saveKeyPairToConfig saves a keypair as a key in clientconfig.d and creates
// an identity that references it
func saveKeyPairToConfig(identityName, cloudURL string, keyPair *cloudauth.KeyPair, keyName string) error {
	// Get private key in PEM format
	privateKeyPEM, err := keyPair.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	// Parse cloud URL to get the issuer. Share one normalization rule with the
	// token save path so a keypair and token identity for the same --url always
	// store an identical issuer (notably http:// for loopback dev clouds).
	issuer := clientconfig.NormalizeIssuerURL(cloudURL)

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

	// Save the key separately (if it doesn't already exist)
	if !mainConfig.HasKey(keyName) {
		// Get hostname for metadata
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}

		// Create metadata with hostname and creation timestamp
		metadata := map[string]string{
			"hostname":   hostname,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}

		keyConfigData := &clientconfig.ConfigData{
			Keys: map[string]*clientconfig.KeyConfig{
				keyName: {
					Name:        keyName,
					Type:        "ed25519",
					PrivateKey:  privateKeyPEM,
					Fingerprint: keyPair.Fingerprint(),
					Metadata:    metadata,
				},
			},
		}

		// Add as a leaf config (this will be saved to clientconfig.d/key-{name}.yaml)
		mainConfig.SetLeafConfig("key-"+keyName, keyConfigData)
	}

	// Create the identity config data that references the key
	leafConfigData := &clientconfig.ConfigData{
		Identities: map[string]*clientconfig.IdentityConfig{
			identityName: {
				Type:   clientconfig.IdentityKeypair,
				Issuer: issuer,
				KeyRef: keyName,
			},
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/identity-{name}.yaml)
	mainConfig.SetLeafConfig("identity-"+identityName, leafConfigData)

	// Save the main config (which will also save the leaf configs)
	return mainConfig.Save()
}

// saveTokenIdentityToConfig persists an ephemeral "token" identity (access +
// refresh token) to clientconfig.d/identity-{name}.yaml. Unlike the keypair
// flow there is no separate key file — the credentials live entirely in the
// identity leaf.
func saveTokenIdentityToConfig(identityName string, identity *clientconfig.IdentityConfig) error {
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil {
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	leafConfigData := &clientconfig.ConfigData{
		Identities: map[string]*clientconfig.IdentityConfig{
			identityName: identity,
		},
	}
	mainConfig.SetLeafConfig("identity-"+identityName, leafConfigData)

	return mainConfig.Save()
}

// autoConfigureCluster checks if there are any local clusters configured,
// and if not, fetches available clusters from the server and automatically
// configures the client if there's only one cluster available. The identity is
// an in-memory (unnamed) credential used to authenticate the cluster lookup; it
// carries either a direct private key (keypair) or device-flow tokens (token).
func autoConfigureCluster(ctx *Context, identityName, cloudURL string, identity *clientconfig.IdentityConfig) error {
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

	// Fetch available clusters from the server. The identity is anonymous
	// (name ""), so any token refresh it triggers is used for this call only and
	// not persisted — fine, since the tokens are fresh from the device flow.
	if mainConfig == nil {
		mainConfig = clientconfig.NewConfig()
	}
	clusters, err := fetchAvailableClusters(ctx, mainConfig, "", identity)
	if err != nil {
		return fmt.Errorf("failed to fetch available clusters: %w", err)
	}

	// Only clusters with a reachable address can be auto-configured.
	var validClusters []ClusterResponse
	for _, cluster := range clusters {
		if cluster.hasReachableAddress() {
			validClusters = append(validClusters, cluster)
		}
	}

	if len(validClusters) == 0 {
		if len(clusters) == 0 {
			ctx.Info("No clusters available for your account")
		} else {
			// Clusters exist but none advertise a reachable address — the common
			// firewalled-inbound-port case. Say so honestly instead of implying the
			// account has nothing, which sends users chasing an auth/org red
			// herring. See MIR-1316.
			printUnreachableClustersHelp(ctx, fmt.Sprintf("Found %d cluster(s), but none advertise a reachable address:", len(clusters)), clusters)
		}
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
	workingAddress, clusterCert, err := tryConnectToCluster(ctx, &cluster, false)
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
		XID:          cluster.XID,
		CACert:       clusterCert.CAPEM,
	}

	applyVerificationName(ctx, clusterConfig, workingAddress, clusterCert)

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

// getIdentityUserInfo tries to get user info (email) for an existing identity
// by authenticating with the identity's key and parsing the JWT claims.
// Returns empty string if unable to fetch.
func getIdentityUserInfo(ctx *Context, config *clientconfig.Config, identityName string) string {
	identity, err := config.GetIdentity(identityName)
	if err != nil || identity == nil {
		return ""
	}

	if identity.Type != clientconfig.IdentityKeypair && identity.Type != clientconfig.IdentityToken {
		return ""
	}

	// Get auth server URL
	authServer := identity.Issuer
	if authServer == "" {
		return ""
	}

	// Authenticate and get JWT token
	token, err := config.TokenForIdentity(ctx, identityName, identity, authServer)
	if err != nil {
		return ""
	}

	// Parse claims to get user info
	claims, err := auth.ParseUnverifiedClaims(token)
	if err != nil || claims == nil {
		return ""
	}

	// Prefer UserName (display name), fall back to UserID
	if claims.UserName != "" {
		return claims.UserName
	}
	return claims.UserID
}
