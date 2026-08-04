package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/workloadidentity"
)

const tokenRefreshInterval = 45 * time.Minute

type tokenEntry struct {
	filePath  string
	appName   string
	role      string
	sandboxID string
}

type tokenRefresher struct {
	mu      sync.Mutex
	entries map[string]tokenEntry // keyed by sandbox ID
}

func newTokenRefresher() *tokenRefresher {
	return &tokenRefresher{
		entries: make(map[string]tokenEntry),
	}
}

// register records a sandbox for periodic token refresh. The role is captured
// here and preserved across refreshes, so a role change takes effect the next
// time the sandbox is built (a redeploy or restart), not mid-lifetime.
func (tr *tokenRefresher) register(sandboxID, filePath, appName, role string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.entries[sandboxID] = tokenEntry{
		filePath:  filePath,
		appName:   appName,
		role:      role,
		sandboxID: sandboxID,
	}
}

func (tr *tokenRefresher) unregister(sandboxID string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.entries, sandboxID)
}

func (tr *tokenRefresher) snapshot() []tokenEntry {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	entries := make([]tokenEntry, 0, len(tr.entries))
	for _, e := range tr.entries {
		entries = append(entries, e)
	}
	return entries
}

// ReleaseTokenState drops every in-memory workload identity record for a sandbox.
// Called from StopSandbox so the state goes away with the sandbox itself rather
// than with the entity, which outlives it by up to the periodic cleanup horizon,
// and from the boot-failure cleanup paths, which never reach StopSandbox.
func (c *SandboxController) ReleaseTokenState(id entity.Id) {
	sandboxID := id.String()
	if c.tokenRefresher != nil {
		c.tokenRefresher.unregister(sandboxID)
	}
	if c.tokenSecrets != nil {
		c.tokenSecrets.unregister(sandboxID)
	}
}

func (c *SandboxController) runTokenRefresh(ctx context.Context) {
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshTokens()
		}
	}
}

func (c *SandboxController) refreshTokens() {
	if c.WorkloadIssuer == nil || c.tokenRefresher == nil {
		return
	}

	entries := c.tokenRefresher.snapshot()
	var dropped int
	for _, e := range entries {
		token, err := c.WorkloadIssuer.IssueTokenWithOptions(e.appName, e.sandboxID, workloadidentity.TokenOptions{Role: e.role})
		if err != nil {
			c.Log.Warn("failed to refresh workload identity token", "sandbox", e.sandboxID, "error", err)
			continue
		}
		// Write in-place (not atomic rename) because the file is bind-mounted
		// into containers. Rename would create a new inode and the container
		// would keep reading the stale old token. The brief window of partial
		// content is acceptable for a few-hundred-byte JWT.
		err = os.WriteFile(e.filePath, []byte(token), 0644)
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			// The sandbox directory is gone, so the sandbox is gone. Teardown
			// normally unregisters the entry (see ReleaseTokenState); this keeps a
			// path that skipped it from refreshing a dead sandbox forever. Release
			// through the same call teardown uses, so the backstop also clears the
			// token secret instead of leaving it authorized.
			c.Log.Debug("dropping token refresh entry for departed sandbox", "sandbox", e.sandboxID)
			c.ReleaseTokenState(entity.Id(e.sandboxID))
			dropped++
		default:
			c.Log.Warn("failed to write refreshed workload identity token", "sandbox", e.sandboxID, "error", err)
		}
	}

	if refreshed := len(entries) - dropped; refreshed > 0 {
		c.Log.Debug("refreshed workload identity tokens", "count", refreshed)
	}

	// A non-zero count means some teardown path skipped ReleaseTokenState — the
	// self-heal covered for it, but the gap is worth seeing without debug logging.
	if dropped > 0 {
		c.Log.Info("dropped stale token refresh entries", "count", dropped)
	}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
