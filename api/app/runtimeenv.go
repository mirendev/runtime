package app

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
