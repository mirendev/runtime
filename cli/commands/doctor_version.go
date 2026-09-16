package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/pkg/ui"
)

// checkVersion compares the CLI build with the server build.
func checkVersion(env *doctorEnv) checkResult {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "no cluster selected"}
	}
	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "server not reachable"}
	}

	cli := env.cliVersion

	if env.serverVersionErr != nil {
		if errors.Is(env.serverVersionErr, errServerVersionUnsupported) {
			return checkResult{
				Status:  checkWarn,
				Summary: fmt.Sprintf("server does not report its version (CLI %s)", cli.Version),
				Problem: &ui.Diagnostic{
					Summary: "the server is too old to report its version",
					Detail: "The server answered but has no version endpoint, which means it " +
						"predates this CLI by several releases. Newer CLI commands may not " +
						"work against it until it is upgraded.",
					Actions: serverUpgradeActions(env),
					Cause:   env.serverVersionErr,
				},
			}
		}
		// No fix to name, so a fact rather than a diagnosis.
		return checkResult{
			Status:  checkSkip,
			Summary: fmt.Sprintf("could not read server version: %v", env.serverVersionErr),
		}
	}

	server := env.serverVersion
	summary := fmt.Sprintf("CLI %s, server %s", cli.Version, server.Version)
	if !server.Ready {
		summary += " (server still starting)"
	}

	switch compareVersions(cli.Version, server.Version) {
	case skewServerBehind:
		return checkResult{
			Status:  checkWarn,
			Summary: summary,
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("server %s is behind CLI %s", server.Version, cli.Version),
				Detail: "The server is running an older release than this CLI. Commands that " +
					"rely on newer server behavior may fail or misreport.",
				Actions: serverUpgradeActions(env),
			},
		}
	case skewCLIBehind:
		return checkResult{
			Status:  checkWarn,
			Summary: summary,
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("CLI %s is behind server %s", cli.Version, server.Version),
				Detail:  "This CLI is running an older release than the server it is talking to.",
				Actions: []ui.Action{
					{Command: "miren upgrade", Note: "update this CLI"},
				},
			},
		}
	case skewUnordered:
		return checkResult{Status: checkOK, Summary: summary + " (different builds)"}
	case skewNone:
	}
	return checkResult{Status: checkOK, Summary: summary}
}

// serverUpgradeActions names the one upgrade command. On a server host it
// upgrades the server and the CLI together, and an older binary'"'"'s CLI-only
// `miren upgrade` refuses when it finds a server running, so the same advice
// is right whichever version is on the host'"'"'s PATH.
func serverUpgradeActions(env *doctorEnv) []ui.Action {
	if env.local() {
		return []ui.Action{
			{Command: "sudo miren upgrade", Note: "upgrade the server on this machine"},
		}
	}
	return []ui.Action{
		{Command: "sudo miren upgrade", Note: "run on the server host to upgrade it"},
	}
}
