package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

var (
	infoGreen  = lipgloss.NewStyle().Foreground(theme.Success)
	infoRed    = lipgloss.NewStyle().Foreground(theme.Error)
	infoYellow = lipgloss.NewStyle().Foreground(theme.Warning)
	infoGray   = lipgloss.NewStyle().Foreground(theme.Muted)
	infoLabel  = lipgloss.NewStyle().Foreground(theme.Info)
	infoBold   = lipgloss.NewStyle().Bold(true)
)

type cloudUserInfo struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
}

func fetchCloudUserInfo(ctx context.Context, cloudURL, token string) (*cloudUserInfo, error) {
	meURL, err := url.JoinPath(cloudURL, "/api/v1/me")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", meURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var info cloudUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func normalizeAuthServerURL(authServer string) string {
	if !strings.HasPrefix(authServer, "http://") && !strings.HasPrefix(authServer, "https://") {
		if strings.Contains(authServer, "localhost") || strings.Contains(authServer, "127.0.0.1") {
			return "http://" + authServer
		}
		return "https://" + authServer
	}
	return authServer
}

type authResult struct {
	Method       string
	IdentityName string
	Claims       *auth.ExtendedClaims
	UserInfo     *cloudUserInfo
	// Err is why authentication didn't work. Kept rather than discarded so the
	// auth check can say "your token expired" instead of the useless "couldn't
	// authenticate".
	Err error
}

// tryAuthenticate attempts to authenticate with the cluster using the configured identity.
// It returns auth details without printing anything - callers handle display.
func tryAuthenticate(ctx *Context, cfg *clientconfig.Config, cluster *clientconfig.ClusterConfig) authResult {
	result := authResult{Method: "none"}

	if cluster.Identity == "" || cfg == nil {
		return result
	}

	identity, err := cfg.GetIdentity(cluster.Identity)
	if err != nil {
		result.Err = err
		return result
	}
	if identity == nil {
		result.Err = fmt.Errorf("identity %q is not configured", cluster.Identity)
		return result
	}

	result.IdentityName = cluster.Identity

	switch identity.Type {
	case clientconfig.IdentityKeypair, clientconfig.IdentityToken:
		authServer := identity.Issuer
		if authServer == "" {
			authServer = cluster.Hostname
		}
		authServer = normalizeAuthServerURL(authServer)

		token, err := cfg.TokenForIdentity(ctx, cluster.Identity, identity, authServer)
		if err != nil {
			result.Err = err
			return result
		}

		result.Claims, _ = auth.ParseUnverifiedClaims(token)
		result.Method = string(identity.Type)

		result.UserInfo, _ = fetchCloudUserInfo(ctx, authServer, token)

	case clientconfig.IdentityCertificate:
		result.Method = "certificate"
	}

	return result
}

// Doctor runs the full diagnostic sweep.
//
// It is deliberately a single command. The previous version printed a
// three-line summary and then told you to go read three other commands, which
// made the summary a menu rather than a diagnosis. Everything runs here, and
// the subcommands are filters over the same registry rather than separate
// implementations.
func Doctor(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return runDoctor(ctx, opts.ConfigCentric, opts.FormatOptions, "")
}

// DoctorConfig, DoctorServer and DoctorAuth run one group of the sweep. They
// exist so existing muscle memory and docs keep working.
func DoctorConfig(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return runDoctor(ctx, opts.ConfigCentric, opts.FormatOptions, groupConfig)
}

func DoctorServer(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return runDoctor(ctx, opts.ConfigCentric, opts.FormatOptions, groupServer)
}

func DoctorAuth(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return runDoctor(ctx, opts.ConfigCentric, opts.FormatOptions, groupAuth)
}

func runDoctor(ctx *Context, cc ConfigCentric, format FormatOptions, group string) error {
	env := gatherDoctorEnv(ctx, cc)

	checks := checksForGroup(group)
	results := make([]checkResult, len(checks))
	for i, c := range checks {
		results[i] = c.Run(env)
	}

	// Set before rendering, so it applies to every output format. JSON is the
	// form a script is most likely to consume, and scripted health gates are
	// the whole reason the exit code exists — having it apply only to the
	// human-readable output would defeat the point.
	//
	// A non-zero exit counts outright failures only: warnings are advisory by
	// definition, and exiting non-zero for them would make the signal useless.
	for _, r := range results {
		if r.Status == checkFail {
			ctx.SetExitCode(1)
			break
		}
	}

	if format.IsJSON() {
		return printDoctorJSON(checks, results)
	}

	renderDoctor(ctx, checks, results)

	return nil
}

func renderDoctor(ctx *Context, checks []check, results []checkResult) {
	ctx.Printf("%s\n\n", infoBold.Render("Miren Doctor"))

	width := 0
	for _, c := range checks {
		width = max(width, len(c.Name))
	}

	for i, c := range checks {
		ctx.Printf("  %s %s  %s\n",
			statusMark(results[i].Status),
			fmt.Sprintf("%-*s", width, c.Name),
			statusText(results[i]))
	}

	fails, warns := 0, 0
	for _, r := range results {
		switch r.Status {
		case checkFail:
			fails++
		case checkWarn:
			warns++
		case checkOK, checkSkip:
		}
	}

	if fails == 0 && warns == 0 {
		ctx.Printf("\n%s\n", "Everything looks good.")
		return
	}

	// Only failing checks explain themselves. Everything that's fine already
	// said so in one line above.
	for _, r := range results {
		if r.Problem == nil {
			continue
		}
		ctx.Printf("\n")
		severity := ui.SeverityError
		if r.Status == checkWarn {
			severity = ui.SeverityWarning
		}
		r.Problem.ShowCause = ctx.Verbose()
		r.Problem.WriteWithSeverity(ctx.Stdout, severity)
	}

	ctx.Printf("\n%s\n", doctorFooter(fails, warns))
}

// doctorFooter keeps the wording honest about severity. Counting a warning as a
// "problem" while exiting zero tells the reader two different things at once.
func doctorFooter(fails, warns int) string {
	switch {
	case fails == 0 && warns == 0:
		return "Everything looks good."
	case fails == 0:
		return fmt.Sprintf("%s, nothing broken.", countOf(warns, "warning"))
	case warns == 0:
		return fmt.Sprintf("%s found.", countOf(fails, "problem"))
	default:
		return fmt.Sprintf("%s found, %s.", countOf(fails, "problem"), countOf(warns, "warning"))
	}
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func statusMark(s checkStatus) string {
	switch s {
	case checkOK:
		return infoGreen.Render("[" + ui.Checkmark + "]")
	case checkWarn:
		return infoYellow.Render("[!]")
	case checkFail:
		return infoRed.Render("[✗]")
	case checkSkip:
		return infoGray.Render("[-]")
	default:
		return infoGray.Render("[-]")
	}
}

func statusText(r checkResult) string {
	if r.Status == checkSkip {
		return infoGray.Render(r.Summary)
	}
	return r.Summary
}

func printDoctorJSON(checks []check, results []checkResult) error {
	type checkJSON struct {
		Name    string   `json:"name"`
		Group   string   `json:"group"`
		Status  string   `json:"status"`
		Summary string   `json:"summary"`
		Problem string   `json:"problem,omitempty"`
		Actions []string `json:"actions,omitempty"`
	}

	items := make([]checkJSON, len(checks))
	for i, c := range checks {
		item := checkJSON{
			Name:    c.Name,
			Group:   c.Group,
			Status:  results[i].Status.String(),
			Summary: results[i].Summary,
		}
		if p := results[i].Problem; p != nil {
			item.Problem = p.Summary
			for _, a := range p.Actions {
				item.Actions = append(item.Actions, a.Command)
			}
		}
		items[i] = item
	}

	return PrintJSON(items)
}
