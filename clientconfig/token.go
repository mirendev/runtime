package clientconfig

import (
	"bytes"
	"context"
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

	"github.com/gofrs/flock"
	"github.com/golang-jwt/jwt/v5"
	"gopkg.in/yaml.v3"

	"miren.dev/runtime/pkg/cloudauth"
)

// ErrLoginRequired signals that a credential has expired or been revoked and
// the user must run `miren login` again. It is returned only for definitive
// server rejections (HTTP 401), never for transient failures (5xx, network
// errors) — treating a cloud blip as a logout would stampede every CLI user
// into re-authenticating at once.
var ErrLoginRequired = errors.New("login required: session expired or revoked")

// ErrNoBearerToken signals that an identity does not authenticate via a bearer
// token (e.g. certificate identities). Callers should fall back to their own
// credential handling rather than treating this as a failure.
var ErrNoBearerToken = errors.New("identity does not use bearer-token authentication")

// tokenPair is the access/refresh credential returned by the cloud's token
// endpoints (device flow and /auth/refresh).
type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// NormalizeIssuerURL ensures an auth-server URL carries a scheme, defaulting to
// https:// but using http:// for loopback hosts so local-dev clouds work. A
// URL that already has a scheme is returned with any trailing slash trimmed.
func NormalizeIssuerURL(server string) string {
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		return strings.TrimSuffix(server, "/")
	}
	if strings.Contains(server, "localhost") || strings.Contains(server, "127.0.0.1") {
		return "http://" + server
	}
	return "https://" + server
}

// refreshTokenPair exchanges a refresh token for a fresh access/refresh pair via
// POST {issuer}/auth/refresh. The endpoint rotates: it consumes the presented
// refresh token and returns a new one, so the caller MUST persist the returned
// pair atomically before using it.
//
// It returns ErrLoginRequired for a 401 (expired/revoked/already-spent token),
// and a plain wrapped error for transient failures (network, 5xx, malformed
// response) so callers can distinguish "must re-login" from "try again later".
func refreshTokenPair(ctx context.Context, issuer, refreshToken string) (*tokenPair, error) {
	if refreshToken == "" {
		return nil, ErrLoginRequired
	}

	refreshURL, err := url.JoinPath(NormalizeIssuerURL(issuer), "/auth/refresh")
	if err != nil {
		return nil, fmt.Errorf("invalid issuer URL: %w", err)
	}

	reqBody, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to parsing below
	case http.StatusUnauthorized:
		// Definitive rejection: token expired, revoked, or already spent by a
		// concurrent refresh. The refresh endpoint returns plain-text bodies.
		return nil, ErrLoginRequired
	default:
		// Transient (5xx, rate-limit, etc.): keep the existing tokens.
		return nil, fmt.Errorf("token refresh failed: server returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var pair tokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		return nil, fmt.Errorf("token refresh response missing access or refresh token")
	}

	return &pair, nil
}

// tokenExpiryBuffer is how long before a JWT's actual expiry we treat it as
// stale. Refreshing slightly early avoids handing a token to an RPC that then
// expires mid-flight due to clock skew or request latency.
const tokenExpiryBuffer = 5 * time.Minute

// tokenFresh reports whether a JWT access token is safe to use for at least
// buffer longer. It parses the token WITHOUT verifying the signature — this is
// only a local staleness check, not an authorization decision; the server
// validates the signature. A token is considered stale (not fresh) when it is
// unparseable, carries no exp claim, or expires within buffer.
func tokenFresh(tokenString string, buffer time.Duration) bool {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return false // No expiry claim, treat as stale so we fetch a fresh one.
	}

	return time.Now().Unix() < int64(exp)-int64(buffer.Seconds())
}

// TokenForIdentity returns a bearer token to authenticate as the given identity.
//
//   - keypair: re-mints a JWT via the challenge-response flow (AuthenticateWithKey).
//   - token:   returns the cached access token if still fresh; otherwise refreshes
//     it via /auth/refresh under a cross-process lock and persists the rotated pair.
//   - certificate: returns ErrNoBearerToken (the caller supplies a client cert).
//
// name identifies which identity leaf to write rotated tokens back to. Pass ""
// for an anonymous/in-memory identity (e.g. during login before it is named):
// a refresh is then used for this call only and not persisted.
//
// fallbackHost is used as the issuer when the identity carries no Issuer.
func (c *Config) TokenForIdentity(ctx context.Context, name string, identity *IdentityConfig, fallbackHost string) (string, error) {
	switch identity.Type {
	case "keypair":
		privateKeyPEM, err := c.GetPrivateKeyPEM(identity)
		if err != nil {
			return "", fmt.Errorf("failed to get private key: %w", err)
		}
		keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
		if err != nil {
			return "", fmt.Errorf("failed to load private key: %w", err)
		}
		authServer := identity.Issuer
		if authServer == "" {
			authServer = fallbackHost
		}
		return AuthenticateWithKey(ctx, authServer, keyPair)

	case "token":
		return c.tokenForTokenIdentity(ctx, name, identity, fallbackHost)

	case "certificate":
		return "", ErrNoBearerToken

	default:
		return "", fmt.Errorf("unknown identity type: %s", identity.Type)
	}
}

// tokenForTokenIdentity implements the "token" arm of TokenForIdentity: a
// lock-free fast path for a still-fresh access token, and a locked slow path
// that refreshes and persists the rotated pair.
func (c *Config) tokenForTokenIdentity(ctx context.Context, name string, identity *IdentityConfig, fallbackHost string) (string, error) {
	// Fast path: a fresh cached token needs no lock and no network.
	if tokenFresh(identity.Token, tokenExpiryBuffer) {
		return identity.Token, nil
	}

	issuer := identity.Issuer
	if issuer == "" {
		issuer = fallbackHost
	}

	// Anonymous identity: nothing to lock or persist, just refresh in memory.
	if name == "" {
		pair, err := refreshTokenPair(ctx, issuer, identity.RefreshToken)
		if err != nil {
			return "", err
		}
		return pair.AccessToken, nil
	}

	// Refreshing rotates the pair, so we need a file to write the new one back to.
	// An identity that was never loaded from disk (an in-memory config) has none.
	source := c.GetIdentitySource(name)
	if source == "" {
		return "", fmt.Errorf("identity %q has no config file on disk to store refreshed tokens in; run 'miren login' to save it", name)
	}

	// Serialize refreshes across processes. The refresh endpoint consumes the
	// presented refresh token (single-use rotation), so two racers would leave
	// one holding a spent token; the lock plus the in-lock re-read collapses the
	// loser into a cheap no-op.
	//
	// The lock file is a hidden dotfile beside the config so it doesn't clutter
	// the directory a user inspects. It is intentionally left in place rather than
	// removed on unlock: unlinking a flock target lets a concurrent waiter and a
	// new process end up locking two different inodes, defeating the mutex.
	lock := flock.New(lockPathFor(source))
	// TryLockContext rather than Lock so a cancelled command or an expiring
	// deadline stops waiting promptly instead of blocking on a peer's refresh.
	locked, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return "", fmt.Errorf("failed to acquire token refresh lock: %w", err)
	}
	if !locked {
		return "", fmt.Errorf("failed to acquire token refresh lock for identity %q", name)
	}
	defer func() { _ = lock.Unlock() }()

	// Re-read from disk inside the lock: a peer may have already rotated while we
	// waited, in which case its fresh access token is right there.
	current, err := readIdentityFromFile(source, name)
	if err != nil {
		return "", fmt.Errorf("failed to re-read identity %q: %w", name, err)
	}
	if tokenFresh(current.Token, tokenExpiryBuffer) {
		return current.Token, nil
	}

	pair, err := refreshTokenPair(ctx, issuer, current.RefreshToken)
	if err != nil {
		return "", err
	}

	// Persist the rotated pair immediately — the old refresh token is now spent
	// server-side, so losing the new one would force a re-login.
	if err := persistIdentityTokens(source, name, pair.AccessToken, pair.RefreshToken); err != nil {
		return "", fmt.Errorf("failed to persist refreshed tokens: %w", err)
	}

	return pair.AccessToken, nil
}

// lockPathFor returns the refresh-lock path for a config file: a hidden dotfile
// in the same directory (e.g. clientconfig.d/.identity-cloud.yaml.lock). Keeping
// it beside the file means every process derives the same path; the dot prefix
// keeps it out of the way of a user inspecting the directory. Being a non-.yaml
// name, loadConfigDir ignores it regardless.
func lockPathFor(source string) string {
	return filepath.Join(filepath.Dir(source), "."+filepath.Base(source)+".lock")
}

// readIdentityFromFile reads a single identity out of a config file (leaf or
// main). It reads only the named identity so it never depends on the rest of
// the file being complete.
func readIdentityFromFile(path, name string) (*IdentityConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cd ConfigData
	if err := yaml.Unmarshal(data, &cd); err != nil {
		return nil, err
	}
	id, ok := cd.Identities[name]
	if !ok || id == nil {
		return nil, fmt.Errorf("identity %q not found in %s", name, path)
	}
	return id, nil
}

// persistIdentityTokens writes a rotated access/refresh pair into the identity's
// source file, preserving every other field, via an atomic replace. The caller
// must hold the identity's lock.
func persistIdentityTokens(path, name, accessToken, refreshToken string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cd ConfigData
	if err := yaml.Unmarshal(data, &cd); err != nil {
		return err
	}
	id, ok := cd.Identities[name]
	if !ok || id == nil {
		return fmt.Errorf("identity %q not found in %s", name, path)
	}
	id.Token = accessToken
	id.RefreshToken = refreshToken

	out, err := yaml.Marshal(&cd)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, out, 0600)
}

// RevokeRefreshToken best-effort revokes a refresh token via
// POST {issuer}/auth/revoke/refresh. The endpoint requires the access token as a
// bearer credential. Any error is returned to the caller, which should treat
// revocation as advisory — a failed revoke must never block logout.
func RevokeRefreshToken(ctx context.Context, issuer, accessToken, refreshToken string) error {
	if refreshToken == "" {
		return nil // Nothing to revoke.
	}
	if accessToken == "" {
		// The endpoint authenticates with the access token, so without one we
		// cannot revoke. Report it rather than returning nil, which would let the
		// caller claim a revocation that never happened.
		return errors.New("no access token available to authorize revocation")
	}

	revokeURL, err := url.JoinPath(NormalizeIssuerURL(issuer), "/auth/revoke/refresh")
	if err != nil {
		return fmt.Errorf("invalid issuer URL: %w", err)
	}

	reqBody, err := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
		"reason":        "logout",
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("revoke returned status %d", resp.StatusCode)
	}
	return nil
}
