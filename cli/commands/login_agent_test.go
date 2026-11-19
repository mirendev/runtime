package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

func decodeJSON[T any](t *testing.T, out *CommandOutput) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(out.Stdout.Bytes(), &v), "stdout must hold only the JSON document:\n%s", out.Stdout.String())
	return v
}

func pendingExists(t *testing.T, identity string) bool {
	t.Helper()
	_, err := os.Stat(pendingLoginPath(identity))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	return true
}

// TestLoginStartWait drives the two-step login an agent uses: start returns at
// once with a URL, wait reports "still pending" with a distinct exit status
// until the login is approved, then saves the identity.
func TestLoginStartWait(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	out, err := RunCommand(LoginStatus, "--json")
	r.Equal(ErrExitCode(1), err, "status must fail when nobody is logged in")
	r.False(decodeJSON[loginStatusJSON](t, out).LoggedIn)

	out, err = RunCommand(LoginStart, "--url", srv.URL, "--json")
	r.NoError(err)
	started := decodeJSON[loginStartJSON](t, out)
	r.Equal("pending", started.Status)
	r.Equal("https://example.com/verify", started.VerificationURL)
	r.Equal("ABCD-1234", started.UserCode)
	r.Equal("miren login wait --json", started.Next, "next must be runnable as-is, keeping JSON output")

	// A retried start hands back the same login rather than orphaning the URL
	// the user may already have opened.
	out, err = RunCommand(LoginStart, "--url", srv.URL, "--json")
	r.NoError(err)
	r.Equal("pending", decodeJSON[loginStartJSON](t, out).Status)
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	out, err = RunCommand(LoginWait, "--timeout", "1500ms", "--json")
	r.Equal(ErrExitCode(exitLoginPending), err)
	pending := decodeJSON[loginWaitJSON](t, out)
	r.Equal("pending", pending.Status)
	r.Equal("https://example.com/verify", pending.URL)
	r.True(pendingExists(t, "cloud"), "an unapproved login must stay resumable")

	mc.held.Store(false)
	out, err = RunCommand(LoginWait, "--timeout", "5s", "--json")
	r.NoError(err)
	done := decodeJSON[loginWaitJSON](t, out)
	r.Equal("logged_in", done.Status)
	r.Equal("user@example.com", done.User)
	r.False(pendingExists(t, "cloud"), "a finished login must not linger")

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	id, err := cfg.GetIdentity("cloud")
	r.NoError(err)
	r.Equal(clientconfig.IdentityToken, id.Type)

	out, err = RunCommand(LoginStatus, "--json")
	r.NoError(err)
	status := decodeJSON[loginStatusJSON](t, out)
	r.True(status.LoggedIn)
	r.Equal("logged_in", status.Identities[0].Status)
	r.Empty(status.Pending)

	// Starting again while signed in is a no-op, not a new device flow.
	out, err = RunCommand(LoginStart, "--url", srv.URL, "--json")
	r.NoError(err)
	r.Equal("logged_in", decodeJSON[loginStartJSON](t, out).Status)
	r.Equal(int32(1), mc.deviceCodeHits.Load())
}

// TestLoginWaitShortTimeout covers a --timeout below the cloud's polling
// interval (1s here): a login that is already approved must still complete
// instead of every call reporting "pending" until the code expires.
func TestLoginWaitShortTimeout(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL)
	require.NoError(t, err)
	_, err = RunCommand(LoginWait, "--timeout", "300ms")
	require.NoError(t, err)
}

// TestLoginWaitInterrupted guards the agent harness interrupting wait: it must
// not exit 0 (which reads as "approved") and must keep the login resumable.
func TestLoginWaitInterrupted(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL)
	require.NoError(t, err)

	cctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	ctx := newTestContext()
	ctx.Context = cctx
	err = loginWait(ctx, "cloud", 10*time.Second, true)
	require.Equal(t, ErrExitCode(exitLoginInterrupted), err)
	require.True(t, pendingExists(t, "cloud"))
}

// TestLoginWaitUnreachable makes sure an outage isn't reported as "not
// approved yet", which would keep an agent looping with no idea why.
func TestLoginWaitUnreachable(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL)
	require.NoError(t, err)
	srv.Close()

	out, err := RunCommand(LoginWait, "--timeout", "1500ms", "--json")
	require.Error(t, err)
	require.NotEqual(t, ErrExitCode(exitLoginPending), err)
	require.Equal(t, "unreachable", decodeJSON[loginWaitJSON](t, out).Status)
	require.True(t, pendingExists(t, "cloud"), "the login is still good once the cloud is back")
}

// TestLoginStartConcurrent: two starts at once must share one device flow, or
// wait could poll a code nobody approved.
func TestLoginStartConcurrent(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = RunCommand(LoginStart, "--url", srv.URL) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), mc.deviceCodeHits.Load())
}

// TestLoginStartChangedFlags: a pending login is reused only for the same
// kind of login; asking for a persistent key starts a new one.
func TestLoginStartChangedFlags(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL)
	require.NoError(t, err)
	_, err = RunCommand(LoginStart, "--url", srv.URL, "--persistent-key")
	require.NoError(t, err)
	require.Equal(t, int32(2), mc.deviceCodeHits.Load())

	p, err := loadPendingLogin("cloud")
	require.NoError(t, err)
	require.True(t, p.PersistentKey)
}

// TestLoginPendingBesideConfigFile: with MIREN_CONFIG naming a file, pending
// logins live beside it rather than in the shared default config directory.
func TestLoginPendingBesideConfigFile(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", filepath.Join(dir, "work.yaml"))
	t.Setenv("HOME", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "login-pending", "cloud.json"))
	require.NoError(t, err)
}

func TestLoginRejectsBadInput(t *testing.T) {
	t.Setenv("MIREN_CONFIG", t.TempDir())
	for _, args := range [][]string{{"--identity", "../escape"}, {"--identity", "a/b"}, {"--timeout", "-1s"}} {
		_, err := RunCommand(LoginWait, args...)
		require.Error(t, err, args)
	}
	_, err := RunCommand(LoginStart, "--identity", "../escape")
	require.ErrorContains(t, err, "invalid identity name")

	_, err = RunCommand(LoginWait)
	require.ErrorContains(t, err, "miren login start")
}

// TestLoginNonInteractiveRelogin covers re-running login without a terminal: a
// working identity is left alone, and one the cloud rejects — whether its
// refresh fails or its unexpired token was revoked — is re-authenticated in
// place instead of demanding --force.
func TestLoginNonInteractiveRelogin(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	r.Equal(int32(1), mc.deviceCodeHits.Load(), "a still-valid login must not start another device flow")

	// Revoke the saved token in the cloud while it's still fresh locally.
	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	id, err := cfg.GetIdentity("cloud")
	r.NoError(err)
	mc.revoked.Store(id.Token, true)

	out, err := RunCommand(LoginStatus, "--json")
	r.Equal(ErrExitCode(1), err)
	r.Equal("expired", decodeJSON[loginStatusJSON](t, out).Identities[0].Status)

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	r.Equal(int32(2), mc.deviceCodeHits.Load(), "a revoked login must be re-authenticated")

	// An expired access token whose refresh the cloud rejects.
	r.NoError(saveTokenIdentityToConfig("cloud", &clientconfig.IdentityConfig{
		Type:         clientconfig.IdentityToken,
		Issuer:       srv.URL,
		Token:        "expired-token",
		RefreshToken: "revoked-refresh",
	}))
	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	r.Equal(int32(3), mc.deviceCodeHits.Load(), "an expired login must be re-authenticated")
}

// TestLoginStatusUnchecked: when the cloud can't be reached, status must not
// claim nobody is signed in.
func TestLoginStatusUnchecked(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	t.Setenv("MIREN_CONFIG", t.TempDir())

	require.NoError(t, login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	srv.Close()

	out, err := RunCommand(LoginStatus, "--json")
	require.Equal(t, ErrExitCode(exitLoginUnchecked), err)
	require.Equal(t, "error", decodeJSON[loginStatusJSON](t, out).Identities[0].Status)
}

// TestIdentityLabelWithoutUser: a token with no user claims, on a cloud that
// predates /api/v1/me, still counts as signed in, but pickers show no user
// rather than repeating the identity name.
func TestIdentityLabelWithoutUser(t *testing.T) {
	mc := &mockCloud{noMe: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}).SignedString([]byte("secret"))
	require.NoError(t, err)
	require.NoError(t, saveTokenIdentityToConfig("cloud", &clientconfig.IdentityConfig{
		Type: clientconfig.IdentityToken, Issuer: srv.URL, Token: token, RefreshToken: "r",
	}))
	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)

	who, err := identityUser(context.Background(), cfg, "cloud", true)
	require.NoError(t, err)
	require.Equal(t, "cloud", who)
	require.Empty(t, getIdentityUserInfo(newTestContext(), cfg, "cloud"))
}

// TestLoginOtherCloud: an identity signed in to one cloud says nothing about
// another, so neither start nor a non-interactive login may report it as
// signed in there, or replace it without --force.
func TestLoginOtherCloud(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))

	other := "https://staging.example.com"
	err := login(newTestContext(), other, "cloud", "miren-cli", false, false, false)
	r.ErrorContains(err, "already exists for "+srv.URL)
	_, err = RunCommand(LoginStart, "--url", other, "--json")
	r.ErrorContains(err, "already exists for "+srv.URL)
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	// A trailing slash on the same cloud still matches.
	_, err = RunCommand(LoginStart, "--url", srv.URL+"/", "--json")
	r.NoError(err)
}

// TestLoginRevokedPersistentKey: a persistent key the cloud no longer accepts
// reads as expired and is signed in again, like a revoked token, instead of
// "couldn't check; use --force".
func TestLoginRevokedPersistentKey(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true, keyRevoked: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // no cached key token

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, true))

	out, err := RunCommand(LoginStatus, "--json")
	r.Equal(ErrExitCode(1), err)
	r.Equal("expired", decodeJSON[loginStatusJSON](t, out).Identities[0].Status)

	r.NoError(login(newTestContext(), srv.URL, "cloud", "miren-cli", false, false, false))
	r.Equal(int32(2), mc.deviceCodeHits.Load())
}

// TestLoginWaitSaveFails: once the device code is exchanged it's spent, so a
// failure saving the login drops the pending login, letting a retried start
// begin a fresh one, and still reports a JSON outcome.
func TestLoginWaitSaveFails(t *testing.T) {
	mc := &mockCloud{withRefresh: true, failKeyRegister: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	_, err := RunCommand(LoginStart, "--url", srv.URL, "--persistent-key")
	require.NoError(t, err)
	out, err := RunCommand(LoginWait, "--json")
	require.Error(t, err)
	require.Equal(t, "failed", decodeJSON[loginWaitJSON](t, out).Status)
	require.False(t, pendingExists(t, "cloud"))
}
