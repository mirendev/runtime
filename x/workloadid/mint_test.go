package workloadid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "sandbox-secret"

// fakeTokenServer stands in for the sandbox's token server.
type fakeTokenServer struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// failNext makes the next n requests fail with a 500.
	failNext int
	// lifetime is how long minted tokens live, ignoring the requested ttl, so
	// tests can stand in for the cluster's clamping.
	lifetime time.Duration
	requests []*http.Request
}

func newFakeTokenServer(t *testing.T) *fakeTokenServer {
	f := &fakeTokenServer{t: t, lifetime: time.Hour}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTokenServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	fail := f.failNext > 0
	if fail {
		f.failNext--
	}
	lifetime := f.lifetime
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+testSecret {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
		return
	}
	if fail {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to issue token"})
		return
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"aud": r.URL.Query()["audience"],
		"exp": time.Now().Add(lifetime).Unix(),
	})
	signed, err := tok.SignedString(generateKey(f.t))
	if err != nil {
		f.t.Errorf("sign: %v", err)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"value": signed})
}

func (f *fakeTokenServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTokenServer) minter(t *testing.T) *Minter {
	t.Helper()
	m, err := NewMinter(MinterConfig{TokenURL: f.srv.URL + "/v1/token", TokenSecret: testSecret})
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	return m
}

func TestMintSendsAudienceTTLAndSecret(t *testing.T) {
	f := newFakeTokenServer(t)

	tok, err := f.minter(t).Mint(context.Background(), "https://gatehouse.example", 5*time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok == "" {
		t.Fatal("Mint returned an empty token")
	}

	r := f.request(0)
	if r.Method != http.MethodGet || r.URL.Path != "/v1/token" {
		t.Errorf("request = %s %s", r.Method, r.URL.Path)
	}
	if got := r.URL.Query().Get("audience"); got != "https://gatehouse.example" {
		t.Errorf("audience = %q", got)
	}
	if got := r.URL.Query().Get("ttl"); got != "300" {
		t.Errorf("ttl = %q, want 300", got)
	}
}

// Zero values leave the choice to the cluster rather than sending empty or
// zero parameters it would refuse.
func TestMintOmitsUnsetParameters(t *testing.T) {
	f := newFakeTokenServer(t)

	if _, err := f.minter(t).Mint(context.Background(), "", 0); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	q := f.request(0).URL.Query()
	if q.Has("audience") || q.Has("ttl") {
		t.Errorf("query = %v, want neither audience nor ttl", q)
	}
}

func TestMintReportsTheServersError(t *testing.T) {
	f := newFakeTokenServer(t)
	m, err := NewMinter(MinterConfig{TokenURL: f.srv.URL, TokenSecret: "wrong"})
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	_, err = m.Mint(context.Background(), "aud", 0)
	if err == nil || !strings.Contains(err.Error(), "invalid token") || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want the status and the server's message", err)
	}
}

func TestMintFallsBackToTheMountedTokenForTheAPIAudience(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("mounted-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewMinter(MinterConfig{TokenPath: path})
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	for _, aud := range []string{"", APIAudience} {
		tok, err := m.Mint(context.Background(), aud, 0)
		if err != nil || tok != "mounted-token" {
			t.Errorf("Mint(%q) = %q, %v; want the mounted token", aud, tok, err)
		}
	}

	// The mounted token is only good for the API, so handing it out for
	// another audience would just move the failure to the receiver.
	if _, err := m.Mint(context.Background(), "https://elsewhere.example", 0); err == nil {
		t.Error("Mint returned the mounted token for a different audience")
	}
}

func TestNewMinterNeedsASource(t *testing.T) {
	if _, err := NewMinter(MinterConfig{}); err == nil {
		t.Error("NewMinter accepted a config with no token URL or path")
	}
}

func TestMinterConfigFromEnv(t *testing.T) {
	t.Setenv(EnvTokenURL, "http://10.0.0.1:7123/v1/token")
	t.Setenv(EnvTokenSecret, "s")
	t.Setenv(EnvTokenPath, "/var/run/miren/token")

	want := MinterConfig{TokenURL: "http://10.0.0.1:7123/v1/token", TokenSecret: "s", TokenPath: "/var/run/miren/token"}
	if got := MinterConfigFromEnv(); got != want {
		t.Errorf("MinterConfigFromEnv() = %+v, want %+v", got, want)
	}
}

func TestKeeperRefreshInstallsAndNotifies(t *testing.T) {
	f := newFakeTokenServer(t)
	k := NewKeeper(f.minter(t), "https://gatehouse.example", time.Hour, nil)

	if k.Token() != "" {
		t.Fatal("a new Keeper already holds a token")
	}

	var seen []string
	k.OnUpdate(func(tok string) { seen = append(seen, tok) })

	if err := k.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if k.Token() == "" || len(seen) != 1 || seen[0] != k.Token() {
		t.Errorf("token = %q, notified with %v", k.Token(), seen)
	}
}

// The cluster clamps lifetimes, so the Keeper schedules from the token it got
// rather than the lifetime it asked for.
func TestKeeperSchedulesFromTheTokensExpiry(t *testing.T) {
	f := newFakeTokenServer(t)
	f.lifetime = 2 * time.Minute
	k := NewKeeper(f.minter(t), "aud", 24*time.Hour, nil)

	if err := k.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if hl := k.halfLife(); hl > time.Minute || hl < 50*time.Second {
		t.Errorf("half life = %v, want about a minute", hl)
	}
}

// A failed refresh retries on a short backoff, not at the old token's next
// half-life, and keeps going until one succeeds.
func TestKeeperRunRetriesAndRefreshes(t *testing.T) {
	f := newFakeTokenServer(t)
	// Tokens with a two second life are refreshed every second, which keeps
	// this test short.
	f.lifetime = 2 * time.Second
	f.failNext = 1

	k := NewKeeper(f.minter(t), "aud", time.Minute, nil)
	updates := make(chan string, 10)
	k.OnUpdate(func(tok string) { updates <- tok })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { k.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.After(10 * time.Second)
	for got := range 2 {
		select {
		case <-updates:
		case <-deadline:
			t.Fatalf("saw %d tokens in 10s, want 2 (one after the retry, one at half-life); %d requests", got, f.count())
		}
	}
}

func (f *fakeTokenServer) request(i int) *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[i]
}

// A token that is already expired by our clock (a stale mounted file, or a
// clock ahead of the cluster's) has no positive half-life. Run must back off
// rather than refresh in a tight loop.
func TestKeeperRunBacksOffOnAnExpiredToken(t *testing.T) {
	f := newFakeTokenServer(t)
	f.mu.Lock()
	f.lifetime = -time.Minute
	f.mu.Unlock()

	k := NewKeeper(f.minter(t), "aud", time.Minute, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel()
	k.Run(ctx)

	// Mints at roughly 0s, 1s and 3s under backoff; a spin would be thousands.
	if n := f.count(); n > 5 {
		t.Errorf("%d mints in 3.5s for an already-expired token, want a handful", n)
	}
	if k.Token() == "" {
		t.Error("the expired token was not served while backing off")
	}
}
