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

type whoamiTestJSON struct {
	Cluster    string               `json:"cluster"`
	Identities []whoamiIdentityJSON `json:"identities"`
	Pending    []whoamiPendingJSON  `json:"pending"`
}

func decodeJSON[T any](t *testing.T, out *CommandOutput) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(out.Stdout.Bytes(), &v), "stdout must hold only the JSON document:\n%s", out.Stdout.String())
	return v
}

// runWhoamiJSON runs whoami --json, which writes to the process's stdout.
func runWhoamiJSON(t *testing.T) (whoamiTestJSON, error) {
	t.Helper()
	var err error
	text := captureOutputJSON(t, func() { _, err = RunCommand(Whoami, "--json") })
	var v whoamiTestJSON
	require.NoError(t, json.Unmarshal([]byte(text), &v), "stdout must hold only the JSON document:\n%s", text)
	return v, err
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

// TestLoginReentrant drives login the way an agent does: without a terminal,
// login prints the URL and exits 75 while it awaits approval, a rerun resumes
// the same login, and once approved a rerun saves the identity.
func TestLoginReentrant(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	who, err := runWhoamiJSON(t)
	r.Equal(ErrExitCode(1), err, "whoami must fail when nobody is logged in")
	r.Empty(who.Identities)

	out, err := RunCommand(Login, "--url", srv.URL, "--json")
	r.Equal(ErrExitCode(exitLoginPending), err)
	started := decodeJSON[loginJSON](t, out)
	r.Equal("pending", started.Status)
	r.Equal("https://example.com/verify", started.VerificationURL)
	r.Equal("ABCD-1234", started.UserCode)
	r.Equal("miren login --wait 50s --url "+srv.URL+" --json", started.Next, "next must be runnable as-is")

	// Reruns, including with a trailing slash on the URL, resume the same
	// login rather than orphaning the URL the user may already have opened.
	_, err = RunCommand(Login, "--url", srv.URL+"/", "--wait", "1500ms", "--json")
	r.Equal(ErrExitCode(exitLoginPending), err)
	r.Equal(int32(1), mc.deviceCodeHits.Load())
	r.True(pendingExists(t, "cloud"), "an unapproved login must stay resumable")

	who, err = runWhoamiJSON(t)
	r.Equal(ErrExitCode(1), err)
	r.Len(who.Pending, 1)

	mc.held.Store(false)
	out, err = RunCommand(Login, "--url", srv.URL, "--json")
	r.NoError(err)
	done := decodeJSON[loginJSON](t, out)
	r.Equal("logged_in", done.Status)
	r.Equal("user@example.com", done.User)
	r.False(pendingExists(t, "cloud"), "a finished login must not linger")
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	who, err = runWhoamiJSON(t)
	r.NoError(err)
	r.Equal("logged_in", who.Identities[0].Status)
	r.Empty(who.Pending)

	// Logging in again while signed in is a no-op, not a new device flow.
	out, err = RunCommand(Login, "--url", srv.URL, "--json")
	r.NoError(err)
	r.Equal("logged_in", decodeJSON[loginJSON](t, out).Status)
	r.Equal(int32(1), mc.deviceCodeHits.Load())
}

// TestLoginInterrupted guards an agent harness interrupting login: it must not
// exit 0 (which reads as "approved") and must keep the login resumable.
func TestLoginInterrupted(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	cctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	ctx := newTestContext()
	ctx.Context = cctx
	err := login(ctx, loginRequest{CloudURL: srv.URL, Identity: "cloud", KeyName: defaultKeyName, Wait: 10 * time.Second, JSON: true})
	require.Equal(t, ErrExitCode(exitLoginInterrupted), err)
	require.True(t, pendingExists(t, "cloud"))
}

// TestLoginUnreachable makes sure an outage isn't reported as "not approved
// yet", which would keep an agent looping with no idea why.
func TestLoginUnreachable(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(Login, "--url", srv.URL)
	require.Equal(t, ErrExitCode(exitLoginPending), err)
	srv.Close()

	out, err := RunCommand(Login, "--url", srv.URL, "--wait", "1500ms", "--json")
	require.Error(t, err)
	require.NotEqual(t, ErrExitCode(exitLoginPending), err)
	require.Equal(t, "unreachable", decodeJSON[loginJSON](t, out).Status)
	require.True(t, pendingExists(t, "cloud"), "the login is still good once the cloud is back")
}

// TestLoginConcurrent: logins at once must share one device flow, or one of
// them could poll a code nobody approved.
func TestLoginConcurrent(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = RunCommand(Login, "--url", srv.URL) })
	}
	wg.Wait()
	for _, err := range errs {
		require.Equal(t, ErrExitCode(exitLoginPending), err)
	}
	require.Equal(t, int32(1), mc.deviceCodeHits.Load())
}

// TestLoginChangedFlags: a pending login is resumed only for the same kind of
// login; asking for a persistent key starts a new one.
func TestLoginChangedFlags(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	_, err := RunCommand(Login, "--url", srv.URL)
	require.Equal(t, ErrExitCode(exitLoginPending), err)
	_, err = RunCommand(Login, "--url", srv.URL, "--persistent-key")
	require.Equal(t, ErrExitCode(exitLoginPending), err)
	require.Equal(t, int32(2), mc.deviceCodeHits.Load())

	p, err := loadPendingLogin("cloud")
	require.NoError(t, err)
	require.True(t, p.PersistentKey)
}

// TestLoginPendingBesideConfigFile: with MIREN_CONFIG naming a file, pending
// logins live beside it rather than in the shared default config directory.
func TestLoginPendingBesideConfigFile(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	mc.held.Store(true)
	srv := mc.server(t)
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", filepath.Join(dir, "work.yaml"))
	t.Setenv("HOME", t.TempDir())

	_, err := RunCommand(Login, "--url", srv.URL)
	require.Equal(t, ErrExitCode(exitLoginPending), err)
	_, err = os.Stat(filepath.Join(dir, "login-pending", "cloud.json"))
	require.NoError(t, err)
}

func TestLoginRejectsBadInput(t *testing.T) {
	t.Setenv("MIREN_CONFIG", t.TempDir())
	for _, args := range [][]string{{"--identity", "../escape"}, {"--identity", "a/b"}, {"--wait", "-1s"}} {
		_, err := RunCommand(Login, args...)
		require.Error(t, err, args)
		require.NotEqual(t, ErrExitCode(exitLoginPending), err, args)
	}
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

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	r.Equal(int32(1), mc.deviceCodeHits.Load(), "a still-valid login must not start another device flow")

	// Revoke the saved token in the cloud while it's still fresh locally.
	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	id, err := cfg.GetIdentity("cloud")
	r.NoError(err)
	mc.revoked.Store(id.Token, true)

	who, err := runWhoamiJSON(t)
	r.Equal(ErrExitCode(1), err)
	r.Equal("expired", who.Identities[0].Status)

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	r.Equal(int32(2), mc.deviceCodeHits.Load(), "a revoked login must be re-authenticated")

	// An expired access token whose refresh the cloud rejects.
	r.NoError(saveTokenIdentityToConfig("cloud", &clientconfig.IdentityConfig{
		Type:         clientconfig.IdentityToken,
		Issuer:       srv.URL,
		Token:        "expired-token",
		RefreshToken: "revoked-refresh",
	}))
	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	r.Equal(int32(3), mc.deviceCodeHits.Load(), "an expired login must be re-authenticated")
}

// TestWhoamiUnchecked: when the cloud can't be reached, whoami must not claim
// nobody is signed in.
func TestWhoamiUnchecked(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	t.Setenv("MIREN_CONFIG", t.TempDir())

	require.NoError(t, testLogin(newTestContext(), srv.URL, false, false))
	srv.Close()

	who, err := runWhoamiJSON(t)
	require.Equal(t, ErrExitCode(exitWhoamiUnchecked), err)
	require.Equal(t, "error", who.Identities[0].Status)
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
// another, so login may neither report it as signed in there nor replace it
// without --force.
func TestLoginOtherCloud(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))

	other := "https://staging.example.com"
	r.ErrorContains(testLogin(newTestContext(), other, false, false), "already exists for "+srv.URL)
	out, err := RunCommand(Login, "--url", other, "--json")
	r.ErrorContains(err, "already exists for "+srv.URL)
	r.Equal("failed", decodeJSON[loginJSON](t, out).Status, "--json must always print its document")
	r.Equal(int32(1), mc.deviceCodeHits.Load())

	// A trailing slash on the same cloud still matches.
	out, err = RunCommand(Login, "--url", srv.URL+"/", "--json")
	r.NoError(err)
	r.Equal("logged_in", decodeJSON[loginJSON](t, out).Status)
}

// TestLoginRevokedPersistentKey: the cloud keeps a revoked key's fingerprint
// and won't register it again, so re-login must generate a fresh key, and the
// saved credentials must then actually authenticate.
func TestLoginRevokedPersistentKey(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the key token cache

	r.NoError(testLogin(newTestContext(), srv.URL, false, true))
	mc.revokeKeys()

	who, err := runWhoamiJSON(t)
	r.Equal(ErrExitCode(1), err)
	r.Equal("expired", who.Identities[0].Status)

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	r.Equal(int32(2), mc.deviceCodeHits.Load())

	who, err = runWhoamiJSON(t)
	r.NoError(err, "the re-login's key must authenticate")
	id := who.Identities[0]
	r.Equal("logged_in", id.Status)
	r.Equal("keypair", id.Type)
}

// TestLoginSaveFails: once the device code is exchanged it's spent, so a
// failure saving the login drops the pending login, letting a rerun begin a
// fresh one, and still reports a JSON outcome.
func TestLoginSaveFails(t *testing.T) {
	mc := &mockCloud{withRefresh: true, failKeyRegister: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	out, err := RunCommand(Login, "--url", srv.URL, "--persistent-key", "--json")
	require.Error(t, err)
	require.Equal(t, "failed", decodeJSON[loginJSON](t, out).Status)
	require.False(t, pendingExists(t, "cloud"))
}

// TestLoginForceUnattended: a forced login run without a terminal must be
// resumable, or every rerun would mint a new URL and an agent could never
// finish one unless the user approved within a single check.
func TestLoginForceUnattended(t *testing.T) {
	r := require.New(t)
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	r.NoError(testLogin(newTestContext(), srv.URL, false, false))
	mc.held.Store(true)

	out, err := RunCommand(Login, "--url", srv.URL, "--force", "--json")
	r.Equal(ErrExitCode(exitLoginPending), err)
	r.Contains(decodeJSON[loginJSON](t, out).Next, "--force", "next must continue the forced login")

	_, err = RunCommand(Login, "--url", srv.URL, "--force", "--wait", "1500ms")
	r.Equal(ErrExitCode(exitLoginPending), err)
	r.Equal(int32(2), mc.deviceCodeHits.Load(), "a forced rerun must resume, not mint a new code")

	// Once approved, the next forced rerun finishes the same login.
	mc.held.Store(false)
	out, err = RunCommand(Login, "--url", srv.URL, "--force", "--json")
	r.NoError(err)
	r.Equal("logged_in", decodeJSON[loginJSON](t, out).Status)
	r.Equal(int32(2), mc.deviceCodeHits.Load())
	r.False(pendingExists(t, "cloud"))
}

// TestLoginRotationKeepsSharedKey: two identities on different clouds share
// one key by name. When only one cloud revokes it, rotating that identity's
// key must leave the other identity's key alone.
func TestLoginRotationKeepsSharedKey(t *testing.T) {
	r := require.New(t)
	mcA, mcB := &mockCloud{withRefresh: true}, &mockCloud{withRefresh: true}
	srvA, srvB := mcA.server(t), mcB.server(t)
	defer srvA.Close()
	defer srvB.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	loginTo := func(url, identity string) error {
		return login(newTestContext(), loginRequest{
			CloudURL: url, Identity: identity, KeyName: defaultKeyName,
			PersistentKey: true, Wait: 10 * time.Second,
		})
	}
	r.NoError(loginTo(srvA.URL, "a"))
	r.NoError(loginTo(srvB.URL, "b"))
	mcA.revokeKeys()

	r.NoError(loginTo(srvA.URL, "a"))

	status := func() map[string]string {
		who, err := runWhoamiJSON(t)
		r.NoError(err)
		m := map[string]string{}
		for _, id := range who.Identities {
			m[id.Name] = id.Status
		}
		return m
	}
	r.Equal(map[string]string{"a": "logged_in", "b": "logged_in"}, status())

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	a, err := cfg.GetIdentity("a")
	r.NoError(err)
	r.NotEqual(defaultKeyName, a.KeyRef, "the rotated key gets its own name")

	// A later forced re-login of a keeps its rotated key rather than going
	// back to the revoked shared one.
	r.NoError(login(newTestContext(), loginRequest{
		CloudURL: srvA.URL, Identity: "a", KeyName: defaultKeyName, Force: true, Wait: 10 * time.Second,
	}))
	r.Equal(map[string]string{"a": "logged_in", "b": "logged_in"}, status())
}

// TestLoginJSONCloudDown: a first login against a cloud that can't be reached
// still prints a JSON outcome.
func TestLoginJSONCloudDown(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	srv.Close()
	t.Setenv("MIREN_CONFIG", t.TempDir())

	out, err := RunCommand(Login, "--url", srv.URL, "--json")
	require.Error(t, err)
	require.Equal(t, "failed", decodeJSON[loginJSON](t, out).Status)
}
