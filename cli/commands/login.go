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
	return login(ctx, loginRequest{
		CloudURL: defaultCloudURL, Identity: "cloud", KeyName: defaultKeyName,
		Wait: interactiveLoginWait, Interactive: ui.IsInteractive(),
	})
}

const (
	defaultCloudURL = "https://miren.cloud"
	defaultKeyName  = "miren-cli"
	// interactiveLoginWait is how long a login at a terminal waits for approval.
	interactiveLoginWait = 10 * time.Minute
	// agentLoginWait is the --wait suggested to a caller without a terminal:
	// long enough to catch a prompt approval, short enough for one tool call.
	agentLoginWait = 50 * time.Second
)

// Login authenticates with miren.cloud using device flow
func Login(ctx *Context, opts struct {
	CloudURL      string         `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	IdentityName  string         `short:"i" long:"identity" description:"Name for this identity in config" default:"cloud"`
	KeyName       string         `short:"k" long:"key-name" description:"Name for the authentication key" default:"miren-cli"`
	NoSave        bool           `long:"no-save" description:"Don't save credentials to config file"`
	Force         bool           `short:"f" long:"force" description:"Overwrite existing identity without prompting"`
	PersistentKey bool           `long:"persistent-key" description:"Register a persistent key instead of the default renewable token login"`
	Wait          *time.Duration `long:"wait" description:"How long to wait for the login to be approved (default 10m at a terminal; without one, check once and exit 75 while it is pending)"`
	FormatOptions
}) error {
	// JSON output is for programs, so it never prompts, even at a terminal.
	interactive := ui.IsInteractive() && !opts.IsJSON()
	wait := time.Duration(0)
	switch {
	case opts.Wait != nil:
		wait = *opts.Wait
	case interactive:
		wait = interactiveLoginWait
	}
	return login(ctx, loginRequest{
		CloudURL:      opts.CloudURL,
		Identity:      opts.IdentityName,
		KeyName:       opts.KeyName,
		NoSave:        opts.NoSave,
		Force:         opts.Force,
		PersistentKey: opts.PersistentKey,
		Wait:          wait,
		JSON:          opts.IsJSON(),
		Interactive:   interactive,
	})
}

// loginRequest is one run of `miren login`.
type loginRequest struct {
	CloudURL      string
	Identity      string
	KeyName       string
	NoSave        bool
	Force         bool
	PersistentKey bool
	// Wait is how long to poll for approval; 0 checks once.
	Wait        time.Duration
	JSON        bool
	Interactive bool
}

// nextCommand is the command that resumes this login, keeping every flag the
// pending login must match.
func (r loginRequest) nextCommand() string {
	cmd := fmt.Sprintf("miren login --wait %s", agentLoginWait)
	if r.CloudURL != defaultCloudURL {
		cmd += " --url " + r.CloudURL
	}
	if r.Identity != "cloud" {
		cmd += " --identity " + r.Identity
	}
	if r.KeyName != defaultKeyName {
		cmd += " --key-name " + r.KeyName
	}
	if r.PersistentKey {
		cmd += " --persistent-key"
	}
	if r.NoSave {
		cmd += " --no-save"
	}
	if r.JSON {
		cmd += " --json"
	}
	return cmd
}

// loginJSON is the one document `miren login --json` prints.
type loginJSON struct {
	// Status is logged_in, pending, unreachable, interrupted, or failed.
	Status                  string    `json:"status"`
	Identity                string    `json:"identity"`
	User                    string    `json:"user,omitempty"`
	VerificationURL         string    `json:"verification_uri,omitempty"`
	VerificationURLComplete string    `json:"verification_uri_complete,omitempty"`
	UserCode                string    `json:"user_code,omitempty"`
	ExpiresAt               time.Time `json:"expires_at,omitzero"`
	Next                    string    `json:"next,omitempty"`
	Error                   string    `json:"error,omitempty"`
}

// login signs in with the device flow. It is re-entrant: a login that is
// already waiting for approval is resumed rather than replaced, so a caller
// that can't block for minutes, such as a coding agent, can run it again
// until the login is approved.
func login(ctx *Context, req loginRequest) error {
	if err := validateIdentityName(req.Identity); err != nil {
		return err
	}
	if req.Wait < 0 {
		return fmt.Errorf("--wait must not be negative")
	}
	out := ctx.Stdout
	if req.JSON {
		defer ctx.ProgressToStderr()()
	}
	report := func(r loginJSON) {
		if req.JSON {
			r.Identity = req.Identity
			_ = PrintJSONTo(out, r)
		}
	}

	// rotateKey: the cloud refused the identity's persistent key, so it can't
	// be registered again; this login makes a fresh one.
	rotateKey := false
	if !req.NoSave {
		config, err := clientconfig.LoadConfig()
		if err != nil && err != clientconfig.ErrNoConfig {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if config != nil && config.HasIdentity(req.Identity) {
			switch {
			case req.Force:
				rotateKey = keyRejected(ctx, config, req.Identity)
			case req.Interactive:
				// Try to get user info for the existing identity
				userInfo := getIdentityUserInfo(ctx, config, req.Identity)

				// Prompt user to choose: update existing or create new
				updateText := fmt.Sprintf("Update '%s' (re-authenticate)", req.Identity)
				if userInfo != "" {
					updateText = fmt.Sprintf("Update '%s' - %s (re-authenticate)", req.Identity, userInfo)
				}

				items := []ui.PickerItem{
					ui.SimplePickerItem{Text: updateText},
					ui.SimplePickerItem{Text: "Add new identity"},
				}

				title := fmt.Sprintf("Identity '%s' already exists", req.Identity)
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
					if err := validateIdentityName(newName); err != nil {
						return err
					}
					req.Identity = newName
				} else {
					rotateKey = keyRejected(ctx, config, req.Identity)
				}
			default:
				who, err := checkExistingLogin(ctx, config, req.Identity, req.CloudURL)
				if err != nil {
					return err
				}
				if who != "" {
					report(loginJSON{Status: "logged_in", User: who})
					ctx.Completed("Already logged in as %s (identity '%s'); use --force to re-authenticate", who, req.Identity)
					return nil
				}
				ctx.Info("Identity '%s' has expired; re-authenticating.", req.Identity)
				rotateKey = keyRejected(ctx, config, req.Identity)
			}
		}
	}

	pending, err := beginOrResumeLogin(ctx, req, rotateKey)
	if err != nil {
		return err
	}

	if !req.JSON {
		printVerificationInstructions(ctx, &DeviceFlowInitResponse{
			UserCode:                pending.UserCode,
			VerificationURL:         pending.VerificationURL,
			VerificationURLComplete: pending.VerificationURLComplete,
		})
		if req.Wait > 0 {
			ctx.Info("Waiting for authentication...")
		}
	}

	// Never wait past the device code's expiry, but always make the check:
	// the cloud reports the expiry itself.
	waitFor := min(req.Wait, max(time.Until(pending.ExpiresAt), time.Second))
	interval := 5 * time.Second
	if pending.PollingInterval > 0 {
		interval = time.Duration(pending.PollingInterval) * time.Second
	}
	dots := !req.JSON && req.Interactive
	tokens, err := pollForToken(ctx, pending.CloudURL, pending.DeviceCode, interval, waitFor, func(status string) {
		if dots && status == "pending" {
			fmt.Fprint(ctx.Stdout, ".")
		}
	})
	if dots {
		fmt.Fprintln(ctx.Stdout)
	}
	if err != nil {
		var timedOut *pollTimeoutError
		switch {
		case ctx.Err() != nil:
			// Interrupted: nothing was decided, so keep the login resumable.
			report(loginJSON{Status: "interrupted", VerificationURLComplete: pending.loginURL(), Next: req.nextCommand()})
			return ErrExitCode(exitLoginInterrupted)
		case errors.As(err, &timedOut) && !timedOut.reachedCloud && timedOut.lastErr != nil:
			// Not one poll got an answer, so whether it was approved is
			// unknown. The login stays resumable for when the cloud is back.
			report(loginJSON{Status: "unreachable", VerificationURLComplete: pending.loginURL(), ExpiresAt: pending.ExpiresAt, Next: req.nextCommand(), Error: timedOut.lastErr.Error()})
			return fmt.Errorf("couldn't reach %s to check the login: %w", pending.CloudURL, timedOut.lastErr)
		case errors.As(err, &timedOut) && time.Now().Before(pending.ExpiresAt):
			report(loginJSON{
				Status:                  "pending",
				VerificationURL:         pending.VerificationURL,
				VerificationURLComplete: pending.VerificationURLComplete,
				UserCode:                pending.UserCode,
				ExpiresAt:               pending.ExpiresAt,
				Next:                    req.nextCommand(),
			})
			if !req.JSON {
				ctx.Info("Not approved yet. This login expires at %s.", pending.ExpiresAt.Local().Format(time.Kitchen))
				ctx.Info("Once it's approved, run '%s' to finish.", req.nextCommand())
			}
			return ErrExitCode(exitLoginPending)
		}
		// Denied, expired, or a cloud error: this device code is finished.
		_ = os.Remove(pendingLoginPath(req.Identity))
		report(loginJSON{Status: "failed", Error: err.Error()})
		return fmt.Errorf("authentication failed: %w", err)
	}

	// The device code is spent once exchanged, so the pending login goes
	// whether or not saving succeeds; running login again then begins afresh.
	_ = os.Remove(pendingLoginPath(req.Identity))
	ctx.Completed("Authentication successful!")

	if err := finishLogin(ctx, pending.CloudURL, req.Identity, pending.KeyName, req.NoSave, pending.PersistentKey, pending.RotateKey, tokens); err != nil {
		report(loginJSON{Status: "failed", Error: err.Error()})
		return err
	}
	if req.JSON {
		result := loginJSON{Status: "logged_in"}
		if cfg, err := clientconfig.LoadConfig(); err == nil && !req.NoSave {
			result.User = getIdentityUserInfo(ctx, cfg, req.Identity)
		}
		report(result)
	}
	return nil
}

// keyRejected reports whether the cloud refuses an identity's persistent key.
// A revoked key keeps its fingerprint in the cloud, which then won't register
// it again, so re-authenticating such an identity needs a fresh key.
func keyRejected(ctx context.Context, config *clientconfig.Config, identityName string) bool {
	identity, err := config.GetIdentity(identityName)
	if err != nil || identity == nil || identity.Type != clientconfig.IdentityKeypair {
		return false
	}
	_, err = identityUser(ctx, config, identityName, true)
	return errors.Is(err, clientconfig.ErrLoginRequired)
}

// checkExistingLogin decides what a non-interactive login does about an
// identity that already exists. It returns who the identity is signed in as
// when its credentials still work, so the login can be skipped, and "" when
// the cloud definitively rejected them, so re-authenticating the same identity
// is safe. Any other failure is returned: re-login would then risk replacing a
// working identity because of a network blip.
//
// An identity signed in to a different cloud than cloudURL is neither: it
// says nothing about cloudURL, and re-authenticating would replace it.
func checkExistingLogin(ctx context.Context, config *clientconfig.Config, identityName, cloudURL string) (string, error) {
	if identity, err := config.GetIdentity(identityName); err == nil && identity != nil && identity.Issuer != "" {
		have, want := clientconfig.NormalizeIssuerURL(identity.Issuer), clientconfig.NormalizeIssuerURL(cloudURL)
		if have != want {
			return "", fmt.Errorf("identity '%s' already exists for %s, not %s; use --identity to add another or --force to replace it", identityName, have, want)
		}
	}
	who, err := identityUser(ctx, config, identityName, true)
	if err == nil {
		return who, nil
	}
	if errors.Is(err, clientconfig.ErrLoginRequired) {
		return "", nil
	}
	return "", fmt.Errorf("identity '%s' already exists and could not be verified (%v); use --force to re-authenticate or --identity to specify a different name", identityName, err)
}

// resolvePersistentKey decides whether to use the persistent-key flow.
// Ephemeral token login is the default; --persistent-key opts back in. An
// existing keypair identity keeps its key on re-login so we never silently
// orphan a registered server-side key.
func resolvePersistentKey(ctx *Context, identityName string, persistentKey bool) bool {
	if persistentKey {
		return true
	}
	if existing, err := clientconfig.LoadConfig(); err == nil && existing != nil {
		if id, err := existing.GetIdentity(identityName); err == nil && id != nil && id.Type == clientconfig.IdentityKeypair {
			ctx.Info("Identity '%s' already uses a persistent key; keeping it.", identityName)
			return true
		}
	}
	return false
}

func printVerificationInstructions(ctx *Context, initResp *DeviceFlowInitResponse) {
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
}

// finishLogin stores the credentials from a completed device flow, or prints
// them under --no-save.
//
// rotateKey replaces the identity's persistent key with a fresh one: the cloud
// keeps a revoked key's fingerprint and won't register it again.
func finishLogin(ctx *Context, cloudURL, identityName, keyName string, noSave, usePersistentKey, rotateKey bool, tokens *deviceFlowTokens) error {
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
		return persistKeypairIdentity(ctx, identityName, cloudURL, keyName, rotateKey, tokens.AccessToken)
	}
	return persistTokenIdentity(ctx, identityName, cloudURL, tokens)
}

// persistKeypairIdentity runs the persistent-key flow: register (or reuse) an
// ed25519 key with the cloud, store a keypair identity, and auto-configure a
// cluster. accessToken is the device-flow JWT used only to authorize key
// registration.
func persistKeypairIdentity(ctx *Context, identityName, cloudURL, keyName string, rotateKey bool, accessToken string) error {
	var keyPair *cloudauth.KeyPair
	var err error
	if rotateKey {
		ctx.Info("The cloud no longer accepts this identity's key; generating a new one...")
		keyPair, err = cloudauth.GenerateKeyPair()
	} else {
		keyPair, err = getOrCreateKey(ctx, keyName)
	}
	if err != nil {
		return fmt.Errorf("failed to get or create keypair: %w", err)
	}

	ctx.Info("Registering public key with server...")
	if err := registerPublicKey(cloudURL, accessToken, keyPair, keyName); err != nil {
		return fmt.Errorf("failed to register public key: %w", err)
	}
	ctx.Info("Public key registered successfully")

	if err := saveKeyPairToConfig(identityName, cloudURL, keyPair, keyName, rotateKey); err != nil {
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

// pollTimeoutError is pollForToken's deadline expiring before the login was
// approved, denied, or expired. It matches context.DeadlineExceeded.
type pollTimeoutError struct {
	after        time.Duration
	reachedCloud bool  // some poll got an answer from the cloud itself
	lastErr      error // why the latest poll didn't, if it didn't
}

func (e *pollTimeoutError) Error() string {
	msg := fmt.Sprintf("authentication timed out after %v", e.after)
	if e.lastErr != nil {
		msg += fmt.Sprintf(" (last poll failed: %v)", e.lastErr)
	}
	return msg
}

func (e *pollTimeoutError) Unwrap() error { return context.DeadlineExceeded }

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
	// The first poll goes out at once: a login approved before the call began
	// must not be missed by a deadline shorter than the polling interval.
	timer := time.NewTimer(interval)
	defer timer.Stop()
	wait := interval

	// Whether any poll got an answer from the cloud itself, and why the latest
	// one didn't, so a timeout can tell "not approved yet" from "unreachable".
	reachedCloud := false
	var lastErr error

	// Create a timeout context with the maximum duration
	timeoutCtx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	// The first check always runs, bounded by the client timeout rather than
	// maxDuration, so a maxDuration of 0 means "check once".
	first := true
	for {
		reqCtx := timeoutCtx
		if first {
			reqCtx = ctx
		} else {
			select {
			case <-timeoutCtx.Done():
				if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
					return nil, &pollTimeoutError{after: maxDuration, reachedCloud: reachedCloud, lastErr: lastErr}
				}
				return nil, timeoutCtx.Err()
			case <-timer.C:
			}
		}
		first = false
		{
			timer.Reset(wait)
			// Bind each poll request to the timeout context so a hung server
			// cannot outlive the overall deadline.
			req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewBuffer(jsonData))
			if err != nil {
				return nil, err
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				// Network error, continue polling
				lastErr = err
				progress("pending")
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				lastErr = err
				progress("pending")
				continue
			}

			// Intermediaries (CDN/WAF/reverse proxy/maintenance pages) return
			// non-200 with non-JSON bodies during transient outages; wait out
			// the polling window like a transport error. Bounded by timeoutCtx.
			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("cloud returned status %d", resp.StatusCode)
				progress("pending")
				continue
			}
			reachedCloud = true
			lastErr = nil

			// 200 + valid JSON is dispatched by the switch below. 200 + non-JSON
			// is a genuine origin contract violation -- surface it immediately
			// rather than silently retrying for the rest of the window.
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
					wait = interval * 2
					timer.Reset(wait)
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
// replaceKey overwrites a stored key of the same name.
func saveKeyPairToConfig(identityName, cloudURL string, keyPair *cloudauth.KeyPair, keyName string, replaceKey bool) error {
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
	if replaceKey || !mainConfig.HasKey(keyName) {
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
		if cluster.HasReachableAddress() {
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
	// Labels don't ask the cloud: a picker lists several identities one
	// after another, and an unreachable cloud shouldn't blank them all.
	who, _ := identityUser(ctx, config, identityName, false)
	if who == identityName {
		// identityUser's fallback; repeating the name adds nothing to a label.
		return ""
	}
	return who
}

// identityUser authenticates as a cloud identity and returns who it is signed
// in as, confirming with the cloud that it still accepts the credential. An
// expired or revoked token identity returns an error wrapping
// clientconfig.ErrLoginRequired. A token the cloud accepted always yields a
// non-empty name, falling back to the identity name when it carries no claims.
//
// With verify, it also asks the cloud whether it still accepts the token;
// without, it trusts a locally unexpired one.
func identityUser(ctx context.Context, config *clientconfig.Config, identityName string, verify bool) (string, error) {
	identity, err := config.GetIdentity(identityName)
	if err != nil {
		return "", err
	}
	if identity == nil {
		return "", fmt.Errorf("identity %q not found", identityName)
	}

	if identity.Type != clientconfig.IdentityKeypair && identity.Type != clientconfig.IdentityToken {
		return "", fmt.Errorf("identity %q is a %s identity, not a cloud login", identityName, identity.Type)
	}

	authServer := identity.Issuer
	if authServer == "" {
		return "", fmt.Errorf("identity %q has no issuer", identityName)
	}

	token, err := config.TokenForIdentity(ctx, identityName, identity, authServer)
	if err != nil {
		return "", err
	}

	// A token identity's access token comes from the local cache until it
	// expires, so ask the cloud whether it still accepts it: a session revoked
	// there must not read as signed in.
	var info *cloudUserInfo
	if verify {
		info, err = fetchCloudUserInfo(ctx, clientconfig.NormalizeIssuerURL(authServer), token)
	} else {
		err = errNoUserInfoEndpoint
	}
	switch {
	case errors.Is(err, errNoUserInfoEndpoint):
	case err != nil:
		return "", err
	case info.User.Email != "":
		return info.User.Email, nil
	case info.User.Name != "":
		return info.User.Name, nil
	}

	claims, err := auth.ParseUnverifiedClaims(token)
	switch {
	case err != nil || claims == nil:
		return identityName, nil
	case claims.Email != "":
		return claims.Email, nil
	case claims.Name != "":
		return claims.Name, nil
	case claims.Subject != "":
		return claims.Subject, nil
	default:
		return identityName, nil
	}
}
