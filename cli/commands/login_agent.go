package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

const (
	// exitLoginPending is the exit status of `login wait` when nobody has
	// approved the login yet. It is EX_TEMPFAIL: run the same command again.
	exitLoginPending = 75
	// exitLoginInterrupted is `login wait` being stopped by a signal. The
	// pending login is kept, so waiting again resumes it.
	exitLoginInterrupted = 130
	// exitLoginUnchecked is `login status` finding no signed-in identity
	// because it couldn't check them, rather than because there are none.
	exitLoginUnchecked = 2
)

// pendingLogin is a device-flow login started by `login start` and finished by
// `login wait`, possibly from another process. It holds the device code, which
// can be exchanged for credentials once approved, so it is stored at mode 0600.
type pendingLogin struct {
	CloudURL                string    `json:"cloud_url"`
	Identity                string    `json:"identity"`
	KeyName                 string    `json:"key_name"`
	PersistentKey           bool      `json:"persistent_key"`
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

// savePendingLogin writes the file atomically, so a concurrent wait or status
// never reads it half-written.
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

type loginStartJSON struct {
	Status                  string    `json:"status"`
	Identity                string    `json:"identity"`
	User                    string    `json:"user,omitempty"`
	VerificationURL         string    `json:"verification_uri,omitempty"`
	VerificationURLComplete string    `json:"verification_uri_complete,omitempty"`
	UserCode                string    `json:"user_code,omitempty"`
	ExpiresAt               time.Time `json:"expires_at,omitzero"`
	Next                    string    `json:"next,omitempty"`
}

// LoginStart begins a device-flow login and returns immediately, so a caller
// that can't block for minutes — a script or a coding agent — can show the URL
// and later finish with `login wait`.
func LoginStart(ctx *Context, opts struct {
	CloudURL      string `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	IdentityName  string `short:"i" long:"identity" description:"Name for this identity in config" default:"cloud"`
	KeyName       string `short:"k" long:"key-name" description:"Name for the authentication key" default:"miren-cli"`
	Force         bool   `short:"f" long:"force" description:"Start a new login even if the identity is still signed in or a login is already pending"`
	PersistentKey bool   `long:"persistent-key" description:"Register a persistent key instead of the default renewable token login"`
	FormatOptions
}) error {
	if err := validateIdentityName(opts.IdentityName); err != nil {
		return err
	}
	out := ctx.Stdout
	if opts.IsJSON() {
		defer ctx.ProgressToStderr()()
	}
	waitCmd := "miren login wait"
	if opts.IdentityName != "cloud" {
		waitCmd += " --identity " + opts.IdentityName
	}
	if opts.IsJSON() {
		waitCmd += " --json"
	}

	if !opts.Force {
		config, err := clientconfig.LoadConfig()
		if err != nil && err != clientconfig.ErrNoConfig {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if config != nil && config.HasIdentity(opts.IdentityName) {
			who, err := checkExistingLogin(ctx, config, opts.IdentityName)
			if err != nil {
				return err
			}
			if who != "" {
				if opts.IsJSON() {
					return PrintJSONTo(out, loginStartJSON{Status: "logged_in", Identity: opts.IdentityName, User: who})
				}
				ctx.Completed("Already logged in as %s (identity '%s'); use --force to re-authenticate", who, opts.IdentityName)
				return nil
			}
		}
	}

	persistentKey := resolvePersistentKey(ctx, opts.IdentityName, opts.PersistentKey)

	// Hold a lock from checking for a pending login to saving a new one, so
	// two starts at once share one login rather than each saving its own and
	// leaving `wait` polling a code nobody approved.
	if err := os.MkdirAll(pendingLoginDir(), 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(pendingLoginDir(), opts.IdentityName+".lock"))
	if _, err := lock.TryLockContext(ctx, 50*time.Millisecond); err != nil {
		return fmt.Errorf("failed to lock pending login: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	if !opts.Force {
		// Reuse an outstanding login so a retried start doesn't orphan a URL
		// the user may already have opened.
		pending, err := loadPendingLogin(opts.IdentityName)
		if err != nil {
			return err
		}
		if pending != nil && pending.CloudURL == opts.CloudURL &&
			pending.KeyName == opts.KeyName && pending.PersistentKey == persistentKey {
			return printLoginStarted(ctx, out, opts.IsJSON(), pending, waitCmd)
		}
	}

	initResp, err := initiateDeviceFlow(opts.CloudURL)
	if err != nil {
		return fmt.Errorf("failed to initiate device flow: %w", err)
	}

	expiresIn := 10 * time.Minute
	if initResp.ExpiresIn > 0 {
		expiresIn = time.Duration(initResp.ExpiresIn) * time.Second
	}
	pending := &pendingLogin{
		CloudURL:                opts.CloudURL,
		Identity:                opts.IdentityName,
		KeyName:                 opts.KeyName,
		PersistentKey:           persistentKey,
		DeviceCode:              initResp.DeviceCode,
		UserCode:                initResp.UserCode,
		VerificationURL:         initResp.VerificationURL,
		VerificationURLComplete: initResp.VerificationURLComplete,
		PollingInterval:         initResp.PollingInterval,
		ExpiresAt:               time.Now().Add(expiresIn).UTC().Truncate(time.Second),
	}
	if err := savePendingLogin(pending); err != nil {
		return fmt.Errorf("failed to save pending login: %w", err)
	}

	return printLoginStarted(ctx, out, opts.IsJSON(), pending, waitCmd)
}

func printLoginStarted(ctx *Context, out io.Writer, asJSON bool, p *pendingLogin, waitCmd string) error {
	if asJSON {
		return PrintJSONTo(out, loginStartJSON{
			Status:                  "pending",
			Identity:                p.Identity,
			VerificationURL:         p.VerificationURL,
			VerificationURLComplete: p.VerificationURLComplete,
			UserCode:                p.UserCode,
			ExpiresAt:               p.ExpiresAt,
			Next:                    waitCmd,
		})
	}
	printVerificationInstructions(ctx, &DeviceFlowInitResponse{
		UserCode:                p.UserCode,
		VerificationURL:         p.VerificationURL,
		VerificationURLComplete: p.VerificationURLComplete,
	})
	ctx.Info("This login expires at %s.", p.ExpiresAt.Local().Format(time.Kitchen))
	ctx.Info("Run '%s' to finish; it returns as soon as the login is approved.", waitCmd)
	return nil
}

type loginWaitJSON struct {
	Status    string    `json:"status"`
	Identity  string    `json:"identity"`
	User      string    `json:"user,omitempty"`
	URL       string    `json:"url,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	Error     string    `json:"error,omitempty"`
}

// LoginWait finishes a login begun by `login start`. It polls for up to
// --timeout, a bound short enough for an agent's tool call, and exits with
// status 75 while the login is still awaiting approval so the caller can run
// it again.
func LoginWait(ctx *Context, opts struct {
	IdentityName string        `short:"i" long:"identity" description:"Identity whose pending login to finish" default:"cloud"`
	Timeout      time.Duration `long:"timeout" description:"How long to wait for approval before exiting with status 75 (0 waits until the login expires)" default:"50s"`
	FormatOptions
}) error {
	return loginWait(ctx, opts.IdentityName, opts.Timeout, opts.IsJSON())
}

func loginWait(ctx *Context, identity string, waitFor time.Duration, asJSON bool) error {
	if err := validateIdentityName(identity); err != nil {
		return err
	}
	if waitFor < 0 {
		return fmt.Errorf("--timeout must not be negative")
	}
	out := ctx.Stdout
	if asJSON {
		defer ctx.ProgressToStderr()()
	}
	report := func(r loginWaitJSON) {
		if asJSON {
			_ = PrintJSONTo(out, r)
		}
	}

	pending, err := loadPendingLogin(identity)
	if err != nil {
		return err
	}
	if pending == nil {
		report(loginWaitJSON{Status: "none", Identity: identity})
		return fmt.Errorf("no login in progress for identity '%s'; run 'miren login start' first", identity)
	}

	// At least a moment, so a login expiring right after it was loaded still
	// gets a final poll; pollForToken reports the expiry from the cloud.
	timeout := max(time.Until(pending.ExpiresAt), time.Second)
	if waitFor > 0 && waitFor < timeout {
		timeout = waitFor
	}
	interval := 5 * time.Second
	if pending.PollingInterval > 0 {
		interval = time.Duration(pending.PollingInterval) * time.Second
	}

	if !asJSON {
		ctx.Info("Waiting for the login to be approved at %s", pending.loginURL())
	}
	tokens, err := pollForToken(ctx, pending.CloudURL, pending.DeviceCode, interval, timeout, func(string) {})
	if err != nil {
		var timedOut *pollTimeoutError
		switch {
		case ctx.Err() != nil:
			// Interrupted: nothing was decided, so keep the login resumable.
			report(loginWaitJSON{Status: "interrupted", Identity: pending.Identity, URL: pending.loginURL()})
			return ErrExitCode(exitLoginInterrupted)
		case errors.As(err, &timedOut) && !timedOut.reachedCloud && timedOut.lastErr != nil:
			// Not one poll got an answer, so whether it was approved is
			// unknown. The login stays resumable for when the cloud is back.
			report(loginWaitJSON{Status: "unreachable", Identity: pending.Identity, URL: pending.loginURL(), ExpiresAt: pending.ExpiresAt, Error: timedOut.lastErr.Error()})
			return fmt.Errorf("couldn't reach %s to check the login: %w", pending.CloudURL, timedOut.lastErr)
		case errors.As(err, &timedOut) && time.Now().Before(pending.ExpiresAt):
			report(loginWaitJSON{Status: "pending", Identity: pending.Identity, URL: pending.loginURL(), ExpiresAt: pending.ExpiresAt})
			if !asJSON {
				fmt.Fprintf(ctx.Stderr, "Login not approved yet; run this command again to keep waiting.\n")
			}
			return ErrExitCode(exitLoginPending)
		}
		// Denied, expired, or a cloud error: this device code is finished.
		_ = os.Remove(pendingLoginPath(pending.Identity))
		report(loginWaitJSON{Status: "failed", Identity: pending.Identity, Error: err.Error()})
		return fmt.Errorf("authentication failed: %w", err)
	}

	ctx.Completed("Authentication successful!")
	if err := finishLogin(ctx, pending.CloudURL, pending.Identity, pending.KeyName, false, pending.PersistentKey, tokens); err != nil {
		return err
	}
	_ = os.Remove(pendingLoginPath(pending.Identity))

	if asJSON {
		result := loginWaitJSON{Status: "logged_in", Identity: pending.Identity}
		if cfg, err := clientconfig.LoadConfig(); err == nil {
			result.User = getIdentityUserInfo(ctx, cfg, pending.Identity)
		}
		return PrintJSONTo(out, result)
	}
	return nil
}

type loginStatusIdentityJSON struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Issuer string `json:"issuer,omitempty"`
	Status string `json:"status"`
	User   string `json:"user,omitempty"`
	Error  string `json:"error,omitempty"`
}

type loginStatusPendingJSON struct {
	Identity  string    `json:"identity"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type loginStatusJSON struct {
	LoggedIn   bool                      `json:"logged_in"`
	Identities []loginStatusIdentityJSON `json:"identities"`
	Pending    []loginStatusPendingJSON  `json:"pending"`
}

// unchecked reports whether no identity is signed in only because checking
// failed, e.g. the cloud was unreachable.
func (r *loginStatusJSON) unchecked() bool {
	return !r.LoggedIn && slices.ContainsFunc(r.Identities, func(id loginStatusIdentityJSON) bool { return id.Status == "error" })
}

// LoginStatus reports whether each configured cloud identity can still
// authenticate. Unlike whoami it needs no cluster, so it answers "am I logged
// in?" before anything else is set up. It exits 1 when no identity is signed
// in, and 2 when that's because none could be checked.
func LoginStatus(ctx *Context, opts struct {
	FormatOptions
}) error {
	config, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return fmt.Errorf("failed to load config: %w", err)
	}

	result := loginStatusJSON{Identities: []loginStatusIdentityJSON{}, Pending: []loginStatusPendingJSON{}}
	if config != nil {
		for _, name := range config.GetIdentityNames() {
			identity, err := config.GetIdentity(name)
			if err != nil || identity == nil {
				continue
			}
			entry := loginStatusIdentityJSON{Name: name, Type: string(identity.Type), Issuer: identity.Issuer}
			if identity.Type == clientconfig.IdentityCertificate {
				// Certificates authenticate to one cluster directly; there is
				// no cloud session to check.
				entry.Status = "certificate"
			} else {
				who, err := identityUser(ctx, config, name)
				switch {
				case err == nil:
					entry.Status = "logged_in"
					entry.User = who
					result.LoggedIn = true
				case errors.Is(err, clientconfig.ErrLoginRequired):
					entry.Status = "expired"
				default:
					entry.Status = "error"
					entry.Error = err.Error()
				}
			}
			result.Identities = append(result.Identities, entry)
		}
	}

	if entries, err := os.ReadDir(pendingLoginDir()); err == nil {
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok {
				continue
			}
			if p, err := loadPendingLogin(name); err == nil && p != nil {
				result.Pending = append(result.Pending, loginStatusPendingJSON{
					Identity:  p.Identity,
					URL:       p.loginURL(),
					ExpiresAt: p.ExpiresAt,
				})
			}
		}
	}

	if opts.IsJSON() {
		if err := PrintJSONTo(ctx.Stdout, result); err != nil {
			return err
		}
	} else {
		printLoginStatus(ctx, &result)
	}
	switch {
	case result.LoggedIn:
		return nil
	case result.unchecked():
		return ErrExitCode(exitLoginUnchecked)
	default:
		return ErrExitCode(1)
	}
}

func printLoginStatus(ctx *Context, result *loginStatusJSON) {
	if len(result.Identities) == 0 {
		ctx.Info("Not logged in. Run '%s' to sign in.", loginCommand())
	} else {
		headers := []string{"IDENTITY", "STATUS", "USER", "ISSUER"}
		showErrors := slices.ContainsFunc(result.Identities, func(id loginStatusIdentityJSON) bool { return id.Error != "" })
		if showErrors {
			headers = append(headers, "ERROR")
		}
		var rows []ui.Row
		for _, id := range result.Identities {
			row := ui.Row{id.Name, id.Status, id.User, id.Issuer}
			if showErrors {
				row = append(row, id.Error)
			}
			rows = append(rows, row)
		}
		table := ui.NewTable(
			ui.WithColumns(ui.AutoSizeColumns(headers, rows, ui.Columns())),
			ui.WithRows(rows),
		)
		ctx.Printf("%s\n", table.Render())
		switch {
		case result.unchecked():
			ctx.Info("")
			ctx.Info("Couldn't check whether you're signed in; see ERROR above.")
		case !result.LoggedIn:
			ctx.Info("")
			ctx.Info("No identity is signed in. Run '%s' to sign in.", loginCommand())
		}
	}
	for _, p := range result.Pending {
		ctx.Info("")
		ctx.Info("Login pending for '%s' until %s:", p.Identity, p.ExpiresAt.Local().Format(time.Kitchen))
		ctx.Info("  %s", p.URL)
	}
}

// loginCommand is the sign-in command to suggest in an error or hint. Without a
// terminal, plain `miren login` blocks for minutes waiting for approval, which
// a script or coding agent can't do, so they're pointed at the two-step flow.
func loginCommand() string {
	if ui.IsInteractive() {
		return "miren login"
	}
	return "miren login start"
}
