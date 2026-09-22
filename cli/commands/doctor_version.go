package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/ui"
)

// checkVersion states three things in one line: what this CLI is, what the
// server it is talking to is, and what the latest release is. It reads the
// same from every vantage point, because each part has exactly one meaning:
// the CLI is the binary you invoked, the server is the selected cluster, and
// latest is latest.
//
// When latest is known, that is what both are judged against, with the
// comparison `miren upgrade` makes when deciding whether it has anything to
// do; a warning here means that command would act. Only stable releases move
// the "latest" channel, which makes it the supported floor for now; if cloud
// ever publishes a floor of its own, this is where it would be consulted.
// Skew between CLI and server on its own is a fact, not a fault, unless latest
// couldn't be fetched, in which case it is the best evidence there is.
func checkVersion(env *doctorEnv) checkResult {
	cli := release.VersionInfo{
		Version:   env.cliVersion.Version,
		Commit:    env.cliVersion.Commit,
		BuildDate: env.cliVersion.BuildDate,
	}
	parts := []string{"CLI " + cli.Version}

	var latest release.VersionInfo
	latestKnown := env.latestErr == nil && env.latest != nil
	if latestKnown {
		latest = metadataVersionInfo(env.latest)
	}

	server, serverKnown := serverVersionInfo(env)
	tooOld := env.configured() && env.connErr == nil &&
		errors.Is(env.serverVersionErr, errServerVersionUnsupported)

	// Without latest to judge against, CLI-versus-server skew is all there is.
	skew := skewNone
	if !latestKnown && serverKnown {
		skew = compareVersions(cli.Version, server.Version)
	}

	switch {
	case !env.configured():
	case env.connErr != nil:
		parts = append(parts, "server unreachable")
	case tooOld:
		parts = append(parts, "server does not report its version")
	case env.serverVersionErr != nil:
		parts = append(parts, fmt.Sprintf("server version unknown (%v)", rootCause(env.serverVersionErr)))
	case !env.serverVersion.Ready:
		parts = append(parts, "server "+server.Version+" (still starting)")
	case skew == skewUnordered:
		parts = append(parts, "server "+server.Version+" (different build)")
	default:
		parts = append(parts, "server "+server.Version)
	}

	if latestKnown {
		parts = append(parts, "latest "+latest.Version)
	} else {
		parts = append(parts, "latest unknown ("+fetchFailure(env.latestErr)+")")
	}
	summary := strings.Join(parts, ", ")

	// A binary built without version information can't be placed relative to
	// anything, and the comparison would call it stale.
	cliBehind := latestKnown && versionKnown(cli) && latest.IsNewer(cli)
	serverBehind := latestKnown && serverKnown && latest.IsNewer(server)

	var actions []ui.Action
	if cliBehind {
		actions = append(actions, ui.Action{Command: "miren upgrade", Note: "update this CLI"})
	}
	if serverBehind || tooOld {
		actions = append(actions, serverUpgradeActions(env)...)
	}

	// A server too old to say what it is predates this CLI by several
	// releases, which is the one skew that is always a problem.
	if tooOld {
		return checkResult{
			Status:  checkWarn,
			Summary: summary,
			Problem: &ui.Diagnostic{
				Summary: "the server is too old to report its version",
				Detail: "The server answered but has no version endpoint, which means it " +
					"predates this CLI by several releases. Newer CLI commands may not " +
					"work against it until it is upgraded.",
				Actions: actions,
				Cause:   env.serverVersionErr,
			},
		}
	}

	if cliBehind || serverBehind {
		var behind []string
		if cliBehind {
			behind = append(behind, "CLI "+cli.Version)
		}
		if serverBehind {
			behind = append(behind, "server "+server.Version)
		}
		verb := "is"
		if len(behind) > 1 {
			verb = "are"
		}
		return checkResult{
			Status:  checkWarn,
			Summary: summary,
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("%s %s behind the latest release %s", strings.Join(behind, " and "), verb, latest.Version),
				Detail: "Releases older than the latest stop receiving fixes, and newer " +
					"clients may rely on server behavior an older release doesn't have.",
				Actions: actions,
			},
		}
	}

	// Being behind a prerelease is a fact with no fix: `miren upgrade` targets
	// stable, so it would do nothing about that gap. Being behind a stable
	// release is still worth the advice, whichever side is the prerelease.
	prereleaseAhead := (skew == skewServerBehind && isPrerelease(cli)) ||
		(skew == skewCLIBehind && isPrerelease(server))
	if prereleaseAhead {
		return checkResult{Status: checkOK, Summary: summary + " (prerelease skew)"}
	}

	switch skew {
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
	case skewUnordered, skewNone:
	}
	return checkResult{Status: checkOK, Summary: summary}
}

// serverVersionInfo is the selected cluster's build, and whether there is one
// to compare against.
func serverVersionInfo(env *doctorEnv) (release.VersionInfo, bool) {
	if !env.configured() || env.connErr != nil || env.serverVersionErr != nil || env.serverVersion == nil {
		return release.VersionInfo{}, false
	}
	v := release.VersionInfo{
		Version:   env.serverVersion.Version,
		Commit:    env.serverVersion.Commit,
		BuildDate: env.serverVersion.BuildDate,
	}
	return v, versionKnown(v)
}

func metadataVersionInfo(m *release.Metadata) release.VersionInfo {
	return release.VersionInfo{Version: m.Version, Commit: m.Commit, BuildDate: m.BuildDate}
}

func isPrerelease(v release.VersionInfo) bool {
	sem, err := release.ParseSemVer(v.Version)
	return err == nil && sem.IsPrerelease()
}

func versionKnown(v release.VersionInfo) bool {
	return v.Version != "" && v.Version != "unknown"
}

// fetchFailure is the one-phrase reason a fetch of latest failed. Nothing
// about the network can be fixed from here, so it is a fact in the summary
// rather than a diagnosis.
func fetchFailure(err error) string {
	switch {
	case err == nil:
		return "no metadata"
	case errors.Is(err, context.DeadlineExceeded):
		return "asset service did not answer"
	default:
		return rootCause(err).Error()
	}
}

// rootCause strips the wrapping off an error. The layers of a failed HTTP
// fetch describe the request, and a one-line fact only has room for the
// reason it failed.
func rootCause(err error) error {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err
		}
		err = inner
	}
}

// serverUpgradeActions names the one upgrade command. On a server host it
// upgrades the server and the CLI together, and an older binary's CLI-only
// `miren upgrade` refuses when it finds a server running, so the same advice
// is right whichever version is on the host's PATH.
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
