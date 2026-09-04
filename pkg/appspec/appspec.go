// Package appspec builds the sandbox spec that runs an app's code.
//
// It exists because there were two of these. The deployment launcher had the
// real one, and the exec proxy had a copy carrying the comment "adapted from
// controllers/deployment/launcher.go" -- which had since drifted: it dropped
// per-service env, ignored disks entirely, hardcoded the container command to
// /bin/sh, and labeled every sandbox `service: web` whatever the app declared.
// Anyone using `miren app run` got that second, worse spec.
//
// The fork happened because the original was a method that reached into the
// launcher's entity client, so there was no way to call it from elsewhere. So
// the one thing this package deliberately does not do is talk to the store:
// callers resolve what they need and pass it in.
package appspec

import (
	"fmt"
	"log/slog"
	"slices"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/secret"
)

// Options describes one sandbox to build.
//
// Every zero value reproduces what the deployment launcher did before this
// package existed, so a service pool passes only the first group of fields.
// The rest exist for task runs, which want the same image, env, and app
// identity but none of the machinery that assumes a long-running server.
type Options struct {
	// AppID is the app the sandbox belongs to; it becomes the log entity.
	AppID entity.Id
	// AppName is the app's metadata name, used for MIREN_APP. Callers resolve
	// it -- fetching it here is what made the original impossible to reuse.
	AppName string

	Version *core_v1alpha.AppVersion
	Config  *core_v1alpha.ConfigSpec

	// Service names the entry in Config.Services to draw ports, env, disks,
	// and the command from. Empty means none of that applies, which is the
	// case for a task run.
	Service string

	// Task names the entry in Config.Tasks to draw env from. A run has no
	// service, so without this the env a task declares is stored at build time
	// and read back nowhere -- which silently drops the credentials a task was
	// declared to carry.
	Task string

	Image string

	// Command overrides the service's command. The config entrypoint is still
	// prepended, so callers pass the command as written in app.toml.
	Command string

	// SkipPorts suppresses port configuration entirely.
	//
	// Runs must set this, and not only to avoid a stray listener: the sandbox
	// controller waits for declared ports to bind and kills the sandbox when
	// they don't. A migration would be executed and then reported as a failed
	// startup. Suppressing ports also drops PORT, which follows from there
	// being no port rather than needing its own switch.
	SkipPorts bool

	// SkipDisks suppresses disk volumes and mounts.
	//
	// Runs set this. Miren disks are single-writer, enforced by requiring the
	// attaching service to be fixed at one instance; a run alongside the
	// service that holds the lease would either block or race it. RFD-97 cut
	// per-task disks for exactly that reason.
	SkipDisks bool

	// Stdin makes the container attachable: containerd wires up a stdin FIFO
	// when the task is created and cannot add one afterwards.
	Stdin bool
	// Tty allocates a pty, which also merges stderr into stdout.
	Tty bool

	// LogAttrs replaces the default log labels. Whatever is set here lands
	// verbatim on the container's log entries, which is how a run's output
	// becomes findable without any change to the log pipeline.
	LogAttrs types.Labels

	// RestartPolicy set to never stops the sandbox controller rebooting the
	// container if it vanishes. Runs must set it: rebooting re-executes the
	// command.
	RestartPolicy compute_v1alpha.SandboxSpecRestartPolicy

	// ExtraEnv is appended after config env but before the system-managed
	// variables, so it cannot shadow PORT or ADMIN_TOKEN.
	ExtraEnv []string

	// ShutdownTimeout overrides the service's graceful-shutdown window.
	ShutdownTimeout string
}

// Build assembles the sandbox spec.
//
// log may be nil; it is only used to report config that had to be ignored.
func Build(log *slog.Logger, opts Options) (*compute_v1alpha.SandboxSpec, error) {
	if opts.Version == nil {
		return nil, fmt.Errorf("appspec: Version is required")
	}
	if opts.Config == nil {
		return nil, fmt.Errorf("appspec: Config is required")
	}

	ver := opts.Version
	cfgSpec := opts.Config
	serviceName := opts.Service

	logAttrs := opts.LogAttrs
	if logAttrs == nil {
		logAttrs = types.LabelSet("miren.stage", "app-run", "miren.service", serviceName)
	}

	// miren.app is appended outside the default above because it identifies the
	// sandbox rather than describing it: a task run supplies its own attributes
	// and would otherwise lose the app it belongs to.
	//
	// The app *name* is used, not its entity id. These attributes become metric
	// labels, and an app's dedicated addons already label themselves by name
	// (see the addon providers) -- an app and the database it depends on can
	// only be summed together if both spell the app the same way.
	if opts.AppName != "" {
		// Copied before appending: opts.LogAttrs belongs to the caller, and
		// appending in place can write through to a slice with spare capacity.
		attrs := make(types.Labels, 0, len(logAttrs)+1)
		for _, lbl := range logAttrs {
			// The app is the controller's to state, so a caller-supplied value
			// is replaced rather than kept. These become metric labels, and a
			// conflicting one would bill an app for another app's usage.
			if lbl.Key != "miren.app" {
				attrs = append(attrs, lbl)
			}
		}
		logAttrs = append(attrs, types.Label{Key: "miren.app", Value: opts.AppName})
	}

	sbSpec := &compute_v1alpha.SandboxSpec{
		Version:       ver.ID,
		LogEntity:     opts.AppID.String(),
		LogAttribute:  logAttrs,
		RestartPolicy: opts.RestartPolicy,
	}

	startDir := cfgSpec.StartDirectory
	if startDir == "" {
		startDir = "/app"
	}

	appEnv := appclient.RuntimeEnvWithAlias(appclient.EnvRuntimeApp, opts.AppName)
	appEnv = append(appEnv, appclient.RuntimeEnvWithAlias(appclient.EnvRuntimeVersion, ver.Version)...)

	appCont := compute_v1alpha.SandboxSpecContainer{
		Name:      "app",
		Image:     opts.Image,
		Env:       appEnv,
		Directory: startDir,
		Stdin:     opts.Stdin,
		Tty:       opts.Tty,
	}

	// Determine port configuration from service config
	var containerPorts []compute_v1alpha.SandboxSpecContainerPort
	portEnvValue := int64(0)
	shutdownTimeout := opts.ShutdownTimeout

	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName {
			if shutdownTimeout == "" && svc.Concurrency.ShutdownTimeout != "" {
				shutdownTimeout = svc.Concurrency.ShutdownTimeout
			}
			if !opts.SkipPorts && svc.PortTimeout != "" {
				sbSpec.PortWaitTimeout = svc.PortTimeout
			}

			if opts.SkipPorts {
				break
			}

			if len(svc.Ports) > 0 {
				// Multi-port path: map each port entry
				for _, p := range svc.Ports {
					portType := p.Type
					if portType == "" {
						portType = "http"
					}
					cp := compute_v1alpha.SandboxSpecContainerPort{
						Port:     p.Port,
						Name:     p.Name,
						Type:     portType,
						NodePort: p.NodePort,
					}
					switch p.Protocol {
					case core_v1alpha.ConfigSpecServicesPortsTCP:
						cp.Protocol = compute_v1alpha.SandboxSpecContainerPortTCP
					case core_v1alpha.ConfigSpecServicesPortsUDP:
						cp.Protocol = compute_v1alpha.SandboxSpecContainerPortUDP
					}
					containerPorts = append(containerPorts, cp)
				}

				// PORT env var: first HTTP-typed port, or first port if none is HTTP
				for _, cp := range containerPorts {
					if cp.Type == "http" {
						portEnvValue = cp.Port
						break
					}
				}
				if portEnvValue == 0 {
					portEnvValue = containerPorts[0].Port
				}

				if serviceName == "web" {
					hasHTTP := false
					for _, cp := range containerPorts {
						if cp.Type == "http" {
							hasHTTP = true
							break
						}
					}
					if !hasHTTP {
						containerPorts = append(containerPorts, compute_v1alpha.SandboxSpecContainerPort{
							Port: 3000, Name: "http", Type: "http",
						})
						portEnvValue = 3000
					}
				}
			} else {
				// Scalar port path (backward compat)
				port := svc.Port
				portName := svc.PortName
				portType := svc.PortType

				if port == 0 && serviceName == "web" {
					port = 3000
				}

				if port > 0 {
					if portName == "" {
						portName = "http"
					}
					if portType == "" {
						portType = "http"
					}
					containerPorts = []compute_v1alpha.SandboxSpecContainerPort{
						{Port: port, Name: portName, Type: portType},
					}
					portEnvValue = port
				}
			}
			break
		}
	}

	// Default to 3000 for web service if no service config matched at all
	if !opts.SkipPorts && len(containerPorts) == 0 && serviceName == "web" {
		containerPorts = []compute_v1alpha.SandboxSpecContainerPort{
			{Port: 3000, Name: "http", Type: "http"},
		}
		portEnvValue = 3000
	}

	if len(containerPorts) > 0 {
		appCont.Port = containerPorts
	}

	// Add user-supplied config env vars, stripping any system-managed keys.
	//
	// A backend-sourced variable contributes a reference, never a value. This
	// spec is persisted as part of the sandbox pool entity, so materializing
	// here would put the plaintext in etcd — the exact thing referencing a
	// secret instead of inlining it is meant to avoid. The value is substituted
	// in memory when the container is created.
	envMap := make(map[string]string)
	for _, x := range cfgSpec.Variables {
		if !IsSystemEnvVar(x.Key) {
			envMap[x.Key] = secret.EnvValue(x.Backend, x.Value)
		}
	}

	// Find and merge per-service env vars (these override global vars)
	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName {
			for _, x := range svc.Env {
				if !IsSystemEnvVar(x.Key) {
					envMap[x.Key] = secret.EnvValue(x.Backend, x.Value)
				}
			}
			break
		}
	}

	// Per-task env, on the same footing as a service's: it overrides the app's
	// globals and still loses to the system-managed vars appended below. A run
	// names a task instead of a service, so this is the branch that carries
	// what [tasks.<name>.env] declared.
	if opts.Task != "" {
		for _, t := range cfgSpec.Tasks {
			if t.Name == opts.Task {
				for _, x := range t.Env {
					if !IsSystemEnvVar(x.Key) {
						envMap[x.Key] = x.Value
					}
				}
				break
			}
		}
	}

	// Convert map to env var slice
	for k, v := range envMap {
		appCont.Env = append(appCont.Env, k+"="+v)
	}

	appCont.Env = append(appCont.Env, opts.ExtraEnv...)

	// Append system-managed env vars last so they cannot be overridden
	if portEnvValue > 0 {
		appCont.Env = append(appCont.Env, fmt.Sprintf("PORT=%d", portEnvValue))
	}
	if ver.AdminToken != "" {
		appCont.Env = append(appCont.Env, "ADMIN_TOKEN="+ver.AdminToken)
	}

	// Find the process override. An explicit command supplied by a caller wins;
	// otherwise args replace the image CMD in exec form, or command retains the
	// historical shell-form override. Config.Entrypoint is a stack-build shell
	// prefix and only applies to command. Args preserve the OCI image entrypoint
	// later through oci.WithImageConfigArgs.
	command := opts.Command
	if command == "" {
		for _, svc := range cfgSpec.Services {
			if svc.Name == serviceName {
				if len(svc.Args) > 0 {
					appCont.Args = slices.Clone(svc.Args)
				} else if svc.Command != "" {
					command = svc.Command
				}
				break
			}
		}
	}
	if command != "" {
		if cfgSpec.Entrypoint != "" {
			appCont.Command = cfgSpec.Entrypoint + " " + command
		} else {
			appCont.Command = command
		}
	}

	// Add disk volumes and mounts for this service
	if !opts.SkipDisks {
		for _, svc := range cfgSpec.Services {
			if svc.Name == serviceName {
				// Pre-compute concurrency mode for miren disk eligibility check
				var skipMirenDisks bool
				hasMirenDisks := false
				for _, disk := range svc.Disks {
					if disk.Provider == "" || disk.Provider == core_v1alpha.ConfigSpecServicesDisksMIREN {
						hasMirenDisks = true
						break
					}
				}
				if hasMirenDisks {
					svcConcurrency, err := coreutil.GetServiceConcurrency(cfgSpec, serviceName)
					if err != nil {
						return nil, fmt.Errorf("failed to get service concurrency: %w", err)
					}

					if svcConcurrency.Mode != "fixed" {
						if log != nil {
							log.Warn("skipping miren disk attachment for non-fixed service",
								"service", serviceName,
								"mode", svcConcurrency.Mode)
						}
						skipMirenDisks = true
					}
				}

				for _, disk := range svc.Disks {
					var provider string
					switch disk.Provider {
					case core_v1alpha.ConfigSpecServicesDisksLOCAL:
						provider = "local"
					case core_v1alpha.ConfigSpecServicesDisksSQLITE:
						provider = "sqlite"
					case core_v1alpha.ConfigSpecServicesDisksMIREN:
						provider = "miren"
					default:
						provider = "miren"
					}

					if skipMirenDisks && provider != "local" {
						continue
					}

					sbSpec.Volume = append(sbSpec.Volume, compute_v1alpha.SandboxSpecVolume{
						Name:         disk.Name,
						Provider:     provider,
						DiskName:     disk.Name,
						MountPath:    disk.MountPath,
						ReadOnly:     disk.ReadOnly,
						SizeGb:       disk.SizeGb,
						Filesystem:   disk.Filesystem,
						DbFile:       disk.DbFile,
						SqliteId:     disk.SqliteId,
						LeaseTimeout: disk.LeaseTimeout,
						Owner:        disk.Owner,
					})

					appCont.Mount = append(appCont.Mount, compute_v1alpha.SandboxSpecContainerMount{
						Source:      disk.Name,
						Destination: disk.MountPath,
					})
				}
				break
			}
		}
	}

	if shutdownTimeout != "" {
		appCont.ShutdownTimeout = shutdownTimeout
	}

	sbSpec.Container = []compute_v1alpha.SandboxSpecContainer{appCont}

	return sbSpec, nil
}

// IsSystemEnvVar reports whether a key is managed by the platform and must not
// be taken from user config.
func IsSystemEnvVar(key string) bool {
	switch key {
	case "PORT", "ADMIN_TOKEN":
		return true
	}
	return appclient.IsReservedEnvVar(key)
}
