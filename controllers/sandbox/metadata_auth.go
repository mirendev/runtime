package sandbox

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/pkg/entity"
)

// The existing host-side filename is retained so running sandboxes can
// re-register their metadata credential after a runner restart.
const tokenSecretFilename = "token-secret"

// tokenSecretRegistry maps a sandbox's identity to its metadata credential. Keying by
// sandbox identity (rather than raw source IP) means a recycled pod IP can never match a
// stale secret left behind by a previous sandbox: the caller's identity is resolved from
// its source address, and the secret is checked against that sandbox.
//
// The address lookup is therefore load-bearing for identity, not just for DNS. It is what
// decides which app the issued token claims to be. That lookup used to be able to name a
// sandbox that no longer held the address, which this registry was the only thing
// standing between and an identity-confusion bug (MIR-1511) — so treat a verification
// failure as evidence the mapping may be wrong, not only that the caller may be.
type tokenSecretRegistry struct {
	mu        sync.RWMutex
	bySandbox map[string]string // sandboxID → secret
	retired   map[string]struct{}
	reload    refreshLimiter
}

func newTokenSecretRegistry() *tokenSecretRegistry {
	return &tokenSecretRegistry{
		bySandbox: make(map[string]string),
		retired:   make(map[string]struct{}),
	}
}

func (r *tokenSecretRegistry) register(sandboxID, secret string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.retired, sandboxID)
	r.bySandbox[sandboxID] = secret
}

func (r *tokenSecretRegistry) verify(sandboxID, secret string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	expected, ok := r.bySandbox[sandboxID]
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(secret)) == 1
}

// repair reloads the authoritative host-side value while holding the registry lock.
// Concurrent verifiers wait for the reload and then observe the repaired entry. The
// reload is independent of the request's bearer: publishing trusted host state cannot
// authorize a bad bearer, which is verified separately after repair returns.
func (r *tokenSecretRegistry) repair(sandboxID string, load func() (string, bool, error)) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, retired := r.retired[sandboxID]; retired || !r.reload.allow(sandboxID) {
		return false, nil
	}

	secret, ok, err := load()
	if err != nil || !ok || secret == "" {
		r.reload.reset(sandboxID)
		return false, err
	}
	r.bySandbox[sandboxID] = secret
	return true, nil
}

// retire revokes a sandbox while serialized with repair. A failed removal leaves a
// tombstone so a retained secret file cannot resurrect the credential; successful
// cleanup needs no tombstone because future repairs have nothing to load.
func (r *tokenSecretRegistry) retire(sandboxID string, remove func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.bySandbox, sandboxID)
	err := remove()
	if err != nil {
		r.retired[sandboxID] = struct{}{}
	} else {
		delete(r.retired, sandboxID)
	}
	return err
}

// refreshCooldown bounds how often one key can force the metadata server to redo recovery
// work such as an entity-store scan or persisted-secret read.
const refreshCooldown = 10 * time.Second

// refreshLimiterSweepAt is the size past which allow drops expired entries. The keys are
// sandbox IDs or addresses from a bridge subnet, so the maps stay far below this in
// practice; the sweep keeps a long-lived runner's churn from growing them without bound.
const refreshLimiterSweepAt = 256

// refreshLimiter rate-limits recovery work per key. Its zero value is ready to use and
// allows the first attempt for any key.
type refreshLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *refreshLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if at, seen := l.last[key]; seen && now.Sub(at) < refreshCooldown {
		return false
	}
	if l.last == nil {
		l.last = make(map[string]time.Time)
	}
	if len(l.last) >= refreshLimiterSweepAt {
		for addr, at := range l.last {
			if now.Sub(at) >= refreshCooldown {
				delete(l.last, addr)
			}
		}
	}
	l.last[key] = now
	return true
}

func (l *refreshLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.last, key)
}

func generateTokenSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// writeTokenSecret persists a sandbox's metadata credential host-side at 0600. It is
// never bind-mounted into the container (the container receives the secret via the
// MIREN_METADATA_SECRET env var); persisting it lets the controller re-register
// the same secret after a restart so the still-running sandbox keeps authenticating.
func writeTokenSecret(path, secret string) error {
	return atomicWriteFile(path, []byte(secret), 0600)
}

// loadTokenSecret reads a persisted metadata credential. It returns ok=false (with a nil
// error) when no secret file exists — e.g. a sandbox started before secret persistence was
// added — so callers can skip re-registration without treating absence as a failure.
func loadTokenSecret(path string) (secret string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	// Tolerate a trailing newline so a secret written by a text editor or
	// fmt.Fprintln still matches the in-process env value.
	return strings.TrimRight(string(data), "\r\n"), true, nil
}

func (c *SandboxController) verifyTokenSecret(sandboxID, secret string) bool {
	return c.tokenSecrets != nil && c.tokenSecrets.verify(sandboxID, secret)
}

// repairTokenSecret restores a missing or diverged in-memory entry from the
// sandbox-specific persisted secret. Authorization is decided only after this returns,
// by comparing the request's bearer with the restored value.
func (c *SandboxController) repairTokenSecret(sandboxID string) bool {
	if c.tokenSecrets == nil {
		return false
	}

	path := filepath.Join(c.Tempdir, "containerd", entity.Id(sandboxID).PathSafe(), tokenSecretFilename)
	repaired, err := c.tokenSecrets.repair(sandboxID, func() (string, bool, error) {
		return loadTokenSecret(path)
	})
	if err != nil {
		c.Log.Warn("failed to reload persisted token secret after verification failure",
			"sandbox", sandboxID, "error", err)
		return false
	}
	if repaired {
		c.Log.Info("re-registered workload token secret after verification failure", "sandbox", sandboxID)
	}
	return repaired
}

// refreshSandboxByIP re-derives an address's owner from the entity store, subject to the
// per-address cooldown: a container retrying a bad metadata request in a loop must not turn
// every failure into an entity-store scan.
func (c *SandboxController) refreshSandboxByIP(ip string) (sandboxID, appName string, ok bool) {
	if c.NetServ == nil || !c.lookupRefresh.allow(ip) {
		return "", "", false
	}
	return c.NetServ.RefreshSandboxByIP(ip)
}
