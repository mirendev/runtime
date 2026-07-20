package app

import "strings"

// Environment variables Miren injects into every sandbox, describing the
// workload to itself.
//
// These live under MIREN_RUNTIME_ so they cannot collide with the MIREN_* vars
// the client reads as input (MIREN_APP, MIREN_CLUSTER, ...). The two directions
// previously shared the name MIREN_APP, which meant the CLI run inside a sandbox
// picked up the sandbox's app instead of the one in .miren/app.toml.
const (
	EnvRuntimeApp         = "MIREN_RUNTIME_APP"
	EnvRuntimeVersion     = "MIREN_RUNTIME_VERSION"
	EnvRuntimeInstanceNum = "MIREN_RUNTIME_INSTANCE_NUM"
)

// RuntimeEnvNames lists every var Miren injects under MIREN_RUNTIME_. User
// config must not override these, and none of them may match a var the client
// reads from its own environment.
//
// EnvRuntimeApp and EnvRuntimeVersion are injected into every app sandbox.
// EnvRuntimeInstanceNum is injected only for instance-backed sandboxes — those
// carrying an "instance" metadata label; it is set at sandbox-boot time in
// controllers/sandbox rather than by the deployment launcher.
var RuntimeEnvNames = []string{
	EnvRuntimeApp,
	EnvRuntimeVersion,
	EnvRuntimeInstanceNum,
}

// The pre-rename names Miren used to inject before MIR-1406 moved these vars
// under MIREN_RUNTIME_. We inject them alongside the canonical names for a
// deprecation window so apps still reading the old names keep working.
const (
	legacyEnvApp         = "MIREN_APP"
	legacyEnvVersion     = "MIREN_VERSION"
	legacyEnvInstanceNum = "MIREN_INSTANCE_NUM"
)

// RuntimeEnvAliases maps each canonical MIREN_RUNTIME_ name to its deprecated
// pre-rename alias. Both are injected with the same value during the deprecation
// window; the aliases will be removed in a future release.
var RuntimeEnvAliases = map[string]string{
	EnvRuntimeApp:         legacyEnvApp,
	EnvRuntimeVersion:     legacyEnvVersion,
	EnvRuntimeInstanceNum: legacyEnvInstanceNum,
}

// LegacyRuntimeEnvNames lists the deprecated pre-rename names that are still
// injected as aliases. Kept as its own list — deliberately NOT folded into
// RuntimeEnvNames, which callers assume is entirely under the MIREN_RUNTIME_
// prefix — so it can seed the pool-reuse skip-list (see filterSystemEnvVars in
// controllers/deployment) without those names being treated as canonical.
var LegacyRuntimeEnvNames = []string{
	legacyEnvApp,
	legacyEnvVersion,
	legacyEnvInstanceNum,
}

// RuntimeEnvWithAlias returns the canonical MIREN_RUNTIME_ assignment for
// canonical=value, plus its deprecated pre-rename alias set to the same value.
// The alias exists only for a deprecation window so apps still reading the old
// name keep working; it will be removed in a future release. If canonical has no
// registered alias, only the canonical assignment is returned.
//
// Deprecation policy for injected/ambient interface surface: renames of vars
// Miren injects into sandboxes (or any other ambient interface an app observes)
// default to alias-and-deprecate, not a hard cut. Inject both the old and new
// names for a release window, document the deprecation, and only then drop the
// old name in a later release. MIR-1406 shipped as a hard cut and silently broke
// apps reading the old names on their next recycle; this helper is where the
// next such rename should hang its alias.
func RuntimeEnvWithAlias(canonical, value string) []string {
	out := []string{canonical + "=" + value}
	if alias, ok := RuntimeEnvAliases[canonical]; ok {
		out = append(out, alias+"="+value)
	}
	return out
}

// IsReservedEnvVar reports whether key belongs to the reserved MIREN_ namespace
// that user config must not set or override. Miren owns the whole prefix so it
// can inject MIREN_RUNTIME_* (and other MIREN_*) vars without a user's config
// shadowing them. This is the single source of truth for the prefix check that
// the env-var guards and system-var filters all apply.
func IsReservedEnvVar(key string) bool {
	return strings.HasPrefix(key, "MIREN_")
}
