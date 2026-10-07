package workloadid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The environment a sandbox is given for minting its own tokens.
const (
	// EnvTokenURL is the sandbox's token server endpoint.
	EnvTokenURL = "MIREN_IDENTITY_TOKEN_URL"
	// EnvTokenSecret is the bearer secret the token server expects from this
	// sandbox. Anything holding it can mint the sandbox's identity, so don't
	// pass it on to child processes that don't need it.
	EnvTokenSecret = "MIREN_IDENTITY_TOKEN_SECRET"
	// EnvTokenPath is a file holding a token for [APIAudience], kept fresh by
	// the cluster.
	EnvTokenPath = "MIREN_IDENTITY_TOKEN_PATH"
)

// MinterConfig says where a [Minter] gets tokens. A sandbox's environment
// carries all three values; see [MinterConfigFromEnv].
type MinterConfig struct {
	// TokenURL is the token server endpoint. Required for any audience
	// other than APIAudience.
	TokenURL string
	// TokenSecret is sent as a bearer token to TokenURL.
	TokenSecret string
	// TokenPath is the mounted APIAudience token, used when TokenURL is unset.
	TokenPath string
	// HTTPClient makes token server requests. The default has a 15 second
	// timeout.
	HTTPClient *http.Client
}

// MinterConfigFromEnv reads a MinterConfig from the sandbox environment.
func MinterConfigFromEnv() MinterConfig {
	return MinterConfig{
		TokenURL:    os.Getenv(EnvTokenURL),
		TokenSecret: os.Getenv(EnvTokenSecret),
		TokenPath:   os.Getenv(EnvTokenPath),
	}
}

// Minter gets workload identity tokens for the sandbox it runs in.
type Minter struct {
	tokenURL    string
	tokenSecret string
	tokenPath   string
	client      *http.Client
}

// NewMinter returns a Minter for cfg, which needs a TokenURL or a TokenPath.
func NewMinter(cfg MinterConfig) (*Minter, error) {
	if cfg.TokenURL == "" && cfg.TokenPath == "" {
		return nil, fmt.Errorf("workloadid: neither %s nor %s is set; is this running in a Miren sandbox?", EnvTokenURL, EnvTokenPath)
	}
	if cfg.TokenURL != "" {
		if _, err := url.Parse(cfg.TokenURL); err != nil {
			return nil, fmt.Errorf("workloadid: parsing token URL: %w", err)
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Minter{
		tokenURL:    cfg.TokenURL,
		tokenSecret: cfg.TokenSecret,
		tokenPath:   cfg.TokenPath,
		client:      client,
	}, nil
}

// NewMinterFromEnv is NewMinter(MinterConfigFromEnv()).
func NewMinterFromEnv() (*Minter, error) {
	return NewMinter(MinterConfigFromEnv())
}

// Mint returns a token for audience that lives for about ttl. The cluster
// clamps ttl to between a minute and a day; zero asks for its default of an
// hour. An empty audience means APIAudience.
//
// Without a token server, Mint can only return the mounted token, which is for
// APIAudience alone, so asking it for any other audience is an error rather
// than a token the receiver would refuse.
func (m *Minter) Mint(ctx context.Context, audience string, ttl time.Duration) (string, error) {
	if m.tokenURL != "" {
		return m.fetch(ctx, audience, ttl)
	}
	if audience != "" && audience != APIAudience {
		return "", fmt.Errorf("minting a token for %q needs %s; only the %q token is mounted", audience, EnvTokenURL, APIAudience)
	}
	data, err := os.ReadFile(m.tokenPath)
	if err != nil {
		return "", fmt.Errorf("reading identity token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// fetch asks the token server for one token: GET with the bearer secret,
// audience and ttl (seconds) as query parameters, {"value": "<jwt>"} back.
func (m *Minter) fetch(ctx context.Context, audience string, ttl time.Duration) (string, error) {
	u, err := url.Parse(m.tokenURL)
	if err != nil {
		return "", fmt.Errorf("parsing token URL: %w", err)
	}
	q := u.Query()
	if audience != "" {
		q.Set("audience", audience)
	}
	if ttl > 0 {
		q.Set("ttl", strconv.Itoa(int(ttl.Seconds())))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if m.tokenSecret != "" {
		req.Header.Set("Authorization", "Bearer "+m.tokenSecret)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(body))
		}
		return "", fmt.Errorf("token server returned %s: %s", resp.Status, e.Error)
	}
	var t struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if t.Value == "" {
		return "", errors.New("token server returned an empty token")
	}
	return t.Value, nil
}

// Keeper keeps a token for one audience fresh in the background, for code that
// presents the same token over and over (a proxy adding a header, a file a
// binary re-reads).
//
// Code that needs a token now and then, such as to exchange it for something
// else, is better served calling [Minter.Mint] each time.
type Keeper struct {
	minter   *Minter
	audience string
	ttl      time.Duration
	logger   *slog.Logger

	mu       sync.RWMutex
	token    string
	expiry   time.Time
	watchers []func(string)
}

// NewKeeper returns a Keeper that mints tokens for audience lasting about ttl.
// It holds no token until [Keeper.Refresh] or [Keeper.Run] gets one.
func NewKeeper(m *Minter, audience string, ttl time.Duration, logger *slog.Logger) *Keeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Keeper{minter: m, audience: audience, ttl: ttl, logger: logger}
}

// Audience is the audience this Keeper mints for.
func (k *Keeper) Audience() string { return k.audience }

// Token returns the current token, or "" if none has been minted yet.
func (k *Keeper) Token() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.token
}

// OnUpdate registers fn to be called with every token minted after this call.
func (k *Keeper) OnUpdate(fn func(string)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.watchers = append(k.watchers, fn)
}

// Refresh mints a new token now and installs it. Call it once before handing
// the Keeper's token to anything, so a sandbox that can't mint fails at
// startup rather than on first use.
func (k *Keeper) Refresh(ctx context.Context) error {
	tok, err := k.minter.Mint(ctx, k.audience, k.ttl)
	if err != nil {
		return err
	}

	// The cluster may have clamped the lifetime we asked for, so schedule
	// from the token's own expiry. It came from our own token server, so
	// reading it unverified is fine.
	expiry := time.Now().Add(k.ttl)
	var c jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(tok, &c); err == nil && c.ExpiresAt != nil {
		expiry = c.ExpiresAt.Time
	}

	k.mu.Lock()
	k.token = tok
	k.expiry = expiry
	watchers := append([]func(string){}, k.watchers...)
	k.mu.Unlock()

	for _, fn := range watchers {
		fn(tok)
	}
	return nil
}

// Run refreshes the token until ctx is done. It mints immediately if there is
// no token yet, then again at half of each token's remaining lifetime, so a
// briefly unreachable token server has the other half to come back. Failures
// back off and retry rather than giving up.
func (k *Keeper) Run(ctx context.Context) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second

	var wait time.Duration
	if k.Token() != "" {
		wait = k.halfLife()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		if err := k.Refresh(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			k.mu.RLock()
			remaining := time.Until(k.expiry)
			k.mu.RUnlock()
			level := slog.LevelWarn
			if remaining <= 0 {
				level = slog.LevelError
			}
			k.logger.Log(ctx, level, "workload identity token refresh failed; retrying",
				"audience", k.audience, "retry_in", backoff,
				"token_valid_for", max(remaining, 0).Round(time.Second), "error", err)

			wait = backoff
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		k.logger.Debug("workload identity token refreshed", "audience", k.audience)
		wait = k.halfLife()
		if wait < minKeeperWait {
			// The token is already expired, or nearly, by our clock: a stale
			// mounted file, or a clock ahead of the cluster's by more than the
			// lifetime. Refreshing at half-life would spin, so back off as if
			// it had failed, while still serving the token we got.
			k.logger.Warn("workload identity token expires too soon to refresh at half-life; is the clock skewed?",
				"audience", k.audience, "token_valid_for", max(2*wait, 0).Round(time.Second), "retry_in", backoff)
			wait = backoff
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
	}
}

// minKeeperWait is the shortest half-life Keeper.Run will refresh on. Tokens
// the cluster mints live at least a minute, so only a stale or skewed token
// comes in under it.
const minKeeperWait = time.Second

// halfLife is how long until the current token is halfway to expiring.
func (k *Keeper) halfLife() time.Duration {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return time.Until(k.expiry) / 2
}
