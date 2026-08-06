package clientconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// singleUseRefreshServer emulates the cloud's rotating, single-use /auth/refresh:
// the first presentation of a given refresh token mints a fresh pair; any reuse
// of a spent token returns 401. It counts successful (minting) refreshes.
type singleUseRefreshServer struct {
	mu       sync.Mutex
	spent    map[string]bool
	minted   int
	rotation int
}

func newSingleUseRefreshServer() *singleUseRefreshServer {
	return &singleUseRefreshServer{spent: map[string]bool{}}
}

func freshAccessJWT(t *testing.T) string {
	t.Helper()
	return makeToken(t, jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())})
}

func (s *singleUseRefreshServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		s.mu.Lock()
		defer s.mu.Unlock()

		if s.spent[req.RefreshToken] {
			http.Error(w, "Refresh token has been revoked", http.StatusUnauthorized)
			return
		}
		s.spent[req.RefreshToken] = true
		s.minted++
		s.rotation++
		newRefresh := "refresh-rot-" + itoa(s.rotation)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  freshAccessJWT(t),
			"refresh_token": newRefresh,
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}
}

// writeTokenIdentityConfig writes a MIREN_CONFIG dir containing a "token"
// identity whose access token is already expired, forcing a refresh.
func writeTokenIdentityConfig(t *testing.T, dir, issuer string) {
	t.Helper()
	expired := makeToken(t, jwt.MapClaims{"exp": float64(time.Now().Add(-time.Hour).Unix())})
	cd := ConfigData{
		Identities: map[string]*IdentityConfig{
			"cloud": {
				Type:         "token",
				Issuer:       issuer,
				Token:        expired,
				RefreshToken: "refresh-original",
			},
		},
	}
	data, err := yaml.Marshal(&cd)
	require.NoError(t, err)
	leafDir := filepath.Join(dir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(leafDir, "identity-cloud.yaml"), data, 0600))
}

// TestTokenForIdentityConcurrentRefresh is the load-bearing test for MIR-1385:
// many concurrent callers (goroutines AND subprocesses) sharing one expired
// access token must trigger exactly ONE successful /auth/refresh, because the
// refresh token is single-use. Every caller must still receive a valid token.
func TestTokenForIdentityConcurrentRefresh(t *testing.T) {
	srv := httptest.NewServer(nil)
	server := newSingleUseRefreshServer()
	srv.Config.Handler = server.handler(t)
	defer srv.Close()

	dir := t.TempDir()
	writeTokenIdentityConfig(t, dir, srv.URL)
	// Set once, before any goroutine starts: t.Setenv mutates process-global
	// state and must not be called concurrently.
	t.Setenv(EnvConfigPath, dir)

	const goroutines = 12
	const subprocesses = 6

	var wg sync.WaitGroup
	var goroutineFailures atomic.Int32
	tokens := make(chan string, goroutines+subprocesses)

	// In-process goroutines, each with its own freshly-loaded Config (mirrors
	// separate `m` invocations that share the on-disk config).
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg, err := LoadConfig()
			if err != nil {
				goroutineFailures.Add(1)
				return
			}
			id, err := cfg.GetIdentity("cloud")
			if err != nil {
				goroutineFailures.Add(1)
				return
			}
			tok, err := cfg.TokenForIdentity(context.Background(), "cloud", id, srv.URL)
			if err != nil {
				goroutineFailures.Add(1)
				return
			}
			tokens <- tok
		}()
	}

	// Subprocesses: prove the lock is cross-process, not just cross-goroutine.
	for range subprocesses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=TestTokenForIdentityChildProcess")
			cmd.Env = append(os.Environ(),
				"MIREN_TEST_REFRESH_CHILD=1",
				EnvConfigPath+"="+dir,
				"MIREN_TEST_ISSUER="+srv.URL,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				goroutineFailures.Add(1)
				t.Logf("child failed: %v\n%s", err, out)
				return
			}
			// Child prints the token on a line prefixed with TOKEN=.
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "TOKEN="); ok {
					tokens <- after
				}
			}
		}()
	}

	wg.Wait()
	close(tokens)

	require.Zero(t, goroutineFailures.Load(), "no caller should fail to obtain a token")

	server.mu.Lock()
	minted := server.minted
	server.mu.Unlock()
	require.Equal(t, 1, minted, "the single-use refresh token must be exchanged exactly once")

	// Every caller must have received a usable (fresh) access token.
	count := 0
	for tok := range tokens {
		require.True(t, tokenFresh(tok, tokenExpiryBuffer), "returned token must be fresh")
		count++
	}
	require.Equal(t, goroutines+subprocesses, count, "every caller must return a token")

	// On-disk pair must be the rotated one and internally consistent.
	final, err := readIdentityFromFile(filepath.Join(dir, "clientconfig.d", "identity-cloud.yaml"), "cloud")
	require.NoError(t, err)
	require.True(t, tokenFresh(final.Token, tokenExpiryBuffer))
	require.True(t, strings.HasPrefix(final.RefreshToken, "refresh-rot-"), "refresh token must be rotated")
}

// TestTokenForIdentityChildProcess is not a real test: it is the subprocess body
// re-executed by TestTokenForIdentityConcurrentRefresh. It is inert unless the
// MIREN_TEST_REFRESH_CHILD env var is set.
func TestTokenForIdentityChildProcess(t *testing.T) {
	if os.Getenv("MIREN_TEST_REFRESH_CHILD") != "1" {
		return
	}
	issuer := os.Getenv("MIREN_TEST_ISSUER")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	tok, err := cfg.TokenForIdentity(context.Background(), "cloud", id, issuer)
	require.NoError(t, err)
	// Stdout is captured by the parent.
	os.Stdout.WriteString("TOKEN=" + tok + "\n")
}
