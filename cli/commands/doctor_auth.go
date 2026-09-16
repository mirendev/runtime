package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/ui"
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

// checkAuthentication reports who we're signed in as.
//
// It stays a one-line roll-up on purpose. `miren whoami` already prints the
// full identity, with --format json, and doctor's job is to answer whether
// anything is wrong rather than to be a second rendering of the same fields.
func checkAuthentication(env *doctorEnv) checkResult {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}
	}

	if env.cluster.Identity == "" {
		// Not a problem on its own: a local development cluster with no cloud
		// registration is a perfectly normal setup, and warning about it would
		// train people to ignore doctor.
		return checkResult{Status: checkSkip, Summary: "(no identity configured)"}
	}

	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "(server unreachable)"}
	}

	res := env.auth
	if res.Claims == nil && res.UserInfo == nil {
		return checkResult{
			Status:  checkWarn,
			Summary: fmt.Sprintf("identity %q isn't usable", env.cluster.Identity),
			Problem: &ui.Diagnostic{
				Summary: "couldn't authenticate with the configured identity",
				Detail: "The cluster is configured to use an identity, but no valid token " +
					"could be obtained for it. Commands that need authentication will " +
					"fail even though the server is reachable.",
				Actions: []ui.Action{
					{Command: "miren login", Note: "sign in again"},
					{Command: "miren whoami", Note: "inspect the current identity"},
				},
				// Shown by default: "your token expired" and "that identity
				// doesn't exist" need different fixes, and only the underlying
				// error distinguishes them.
				Cause:     res.Err,
				ShowCause: res.Err != nil,
			},
		}
	}

	return checkResult{Status: checkOK, Summary: authSummary(res)}
}

func authSummary(res authResult) string {
	if res.UserInfo != nil && res.UserInfo.User.Email != "" {
		return res.UserInfo.User.Email
	}
	if res.Claims != nil && res.Claims.Subject != "" {
		return res.Claims.Subject
	}
	return res.Method
}
