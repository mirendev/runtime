package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"miren.dev/runtime/clientconfig"
)

const (
	// exitLoginPending is the exit status of `miren login` when nobody has
	// approved the login yet. It is EX_TEMPFAIL: run the same command again.
	exitLoginPending = 75
	// exitLoginInterrupted is `miren login` being stopped by a signal. The
	// pending login is kept, so running login again resumes it.
	exitLoginInterrupted = 130
)

// pendingLogin is a device-flow login waiting for approval. It outlives the
// `miren login` that started it, so running login again resumes it instead of
// minting a new URL. It holds the device code, which can be exchanged for
// credentials once approved, so it is stored at mode 0600.
type pendingLogin struct {
	CloudURL      string `json:"cloud_url"`
	Identity      string `json:"identity"`
	KeyName       string `json:"key_name"`
	PersistentKey bool   `json:"persistent_key"`
	// RotateKey: the cloud refused the identity's stored key, so finishing
	// this login generates a new one.
	RotateKey               bool      `json:"rotate_key,omitempty"`
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURL         string    `json:"verification_uri"`
	VerificationURLComplete string    `json:"verification_uri_complete,omitempty"`
	PollingInterval         int       `json:"polling_interval"`
	ExpiresAt               time.Time `json:"expires_at"`
}

// loginURL is the URL to hand to the person approving the login.
func (p *pendingLogin) loginURL() string {
	if p.VerificationURLComplete != "" {
		return p.VerificationURLComplete
	}
	return p.VerificationURL
}

// pendingLoginDir sits beside the client config file, so separate configs
// (MIREN_CONFIG pointing at different files or directories) keep separate
// pending logins.
func pendingLoginDir() string {
	return filepath.Join(filepath.Dir(clientconfig.GetActiveConfigPath()), "login-pending")
}

func pendingLoginPath(identity string) string {
	return filepath.Join(pendingLoginDir(), identity+".json")
}

// validateIdentityName rejects names that can't be used as a file name, since
// the pending login for an identity is stored under its name.
func validateIdentityName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid identity name %q", name)
	}
	return nil
}

// loadPendingLogin returns the unexpired pending login for an identity, or nil.
// An expired one is removed.
func loadPendingLogin(identity string) (*pendingLogin, error) {
	data, err := os.ReadFile(pendingLoginPath(identity))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p pendingLogin
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse pending login: %w", err)
	}
	if !time.Now().Before(p.ExpiresAt) {
		_ = os.Remove(pendingLoginPath(identity))
		return nil, nil
	}
	return &p, nil
}

// listPendingLogins returns every unexpired pending login.
func listPendingLogins() []*pendingLogin {
	entries, err := os.ReadDir(pendingLoginDir())
	if err != nil {
		return nil
	}
	var out []*pendingLogin
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if p, err := loadPendingLogin(name); err == nil && p != nil {
			out = append(out, p)
		}
	}
	return out
}

// savePendingLogin writes the file atomically, so a concurrent login or
// whoami never reads it half-written.
func savePendingLogin(p *pendingLogin) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(pendingLoginDir(), p.Identity+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), pendingLoginPath(p.Identity))
}

// beginOrResumeLogin returns the pending login for req, starting a device flow
// only when there is none to resume. --force always starts afresh.
func beginOrResumeLogin(ctx *Context, req loginRequest, rotateKey bool) (*pendingLogin, error) {
	persistentKey := resolvePersistentKey(ctx, req.Identity, req.PersistentKey)
	cloudURL := clientconfig.NormalizeIssuerURL(req.CloudURL)

	// Hold a lock from checking for a pending login to saving a new one, so
	// two logins at once share one device code rather than each saving its
	// own and leaving the other polling a code nobody approved.
	if err := os.MkdirAll(pendingLoginDir(), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(pendingLoginDir(), req.Identity+".lock"))
	if _, err := lock.TryLockContext(ctx, 50*time.Millisecond); err != nil {
		return nil, fmt.Errorf("failed to lock pending login: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	if !req.Force {
		// Reuse an outstanding login so a retry doesn't orphan a URL the user
		// may already have opened.
		pending, err := loadPendingLogin(req.Identity)
		if err != nil {
			return nil, err
		}
		if pending != nil && clientconfig.NormalizeIssuerURL(pending.CloudURL) == cloudURL &&
			pending.KeyName == req.KeyName && pending.PersistentKey == persistentKey {
			pending.RotateKey = pending.RotateKey || rotateKey
			return pending, nil
		}
	}

	ctx.Info("Initiating device flow authentication...")
	initResp, err := initiateDeviceFlow(cloudURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate device flow: %w", err)
	}

	expiresIn := 10 * time.Minute
	if initResp.ExpiresIn > 0 {
		expiresIn = time.Duration(initResp.ExpiresIn) * time.Second
	}
	pending := &pendingLogin{
		CloudURL:                cloudURL,
		Identity:                req.Identity,
		KeyName:                 req.KeyName,
		PersistentKey:           persistentKey,
		RotateKey:               rotateKey,
		DeviceCode:              initResp.DeviceCode,
		UserCode:                initResp.UserCode,
		VerificationURL:         initResp.VerificationURL,
		VerificationURLComplete: initResp.VerificationURLComplete,
		PollingInterval:         initResp.PollingInterval,
		ExpiresAt:               time.Now().Add(expiresIn).UTC().Truncate(time.Second),
	}
	if err := savePendingLogin(pending); err != nil {
		return nil, fmt.Errorf("failed to save pending login: %w", err)
	}
	return pending, nil
}
