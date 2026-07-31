package commands

import (
	"fmt"

	"miren.dev/runtime/pkg/ui"
)

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

	res := tryAuthenticate(env.ctx, env.cfg, env.cluster)

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
