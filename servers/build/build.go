package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	buildkitclient "github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/procfile"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/stackbuild"
	"miren.dev/runtime/pkg/tarx"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/pkg/workloadroles"
)

var buildTracer = otel.Tracer("miren.dev/runtime/build")

// buildLogWriter writes build log entries to VictoriaLogs with version metadata
type buildLogWriter struct {
	log      *slog.Logger
	writer   observability.LogWriter
	entityID string
	version  string
}

func (w *buildLogWriter) write(msg string) {
	if w.writer == nil || w.entityID == "" {
		return
	}
	err := w.writer.WriteEntry(w.entityID, observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      msg,
		Attributes: map[string]string{
			"source":  "build",
			"version": w.version,
		},
	})
	if err != nil {
		w.log.Warn("failed to write build log entry", "error", err)
	}
}

type buildSession struct {
	dir        string
	appName    string
	cancelFunc context.CancelFunc
}

// BuildKitProvider is the subset of *buildkit.Component the builder depends on.
// Abstracting it as an interface lets unit tests inject a fake daemon (e.g. one
// whose Client fails) instead of standing up a real containerd-managed buildkitd.
type BuildKitProvider interface {
	Client(ctx context.Context) (*buildkitclient.Client, error)
	SocketPath() string
	IsRunning() bool
}

type Builder struct {
	Log           *slog.Logger
	EAS           *entityserver_v1alpha.EntityAccessClient
	ec            *entityserver.Client
	appClient     *app.Client
	addonsClient  *app_v1alpha.AddonsClient
	ingressClient *ingress.Client
	TempDir       string
	Registry      string
	DNSHostname   string // Cloud-provisioned DNS hostname for default route display
	DataPath      string

	Resolver  netresolve.Resolver
	LogWriter observability.LogWriter

	// BuildKit is the persistent BuildKit component for container image builds.
	// When set, uses the shared daemon instead of launching ephemeral sandboxes.
	BuildKit BuildKitProvider

	WorkloadIssuer *workloadidentity.Issuer

	// deploy owns the server-side deployment record lifecycle for builds whose
	// client asked the server to manage it. It is an in-process wrapper over the
	// same entity access client the builder already holds, so tracking a
	// deployment costs no extra RPC hop.
	deploy *deploylifecycle.Tracker

	sessions   sync.Map // sessionID → *buildSession
	cacheLocks *appLocks
}

func NewBuilder(log *slog.Logger, eas *entityserver_v1alpha.EntityAccessClient, appClient *app.Client, addonsClient *app_v1alpha.AddonsClient, res netresolve.Resolver, tmpdir string, logWriter observability.LogWriter, dnsHostname string, bk *buildkit.Component, dataPath string) *Builder {
	b := &Builder{
		Log:           log.With("module", "builder"),
		EAS:           eas,
		appClient:     appClient,
		addonsClient:  addonsClient,
		ingressClient: ingress.NewClient(log, eas),
		Resolver:      res,
		TempDir:       tmpdir,
		ec:            entityserver.NewClient(log, eas),
		LogWriter:     logWriter,
		DNSHostname:   dnsHostname,
		DataPath:      dataPath,
		cacheLocks:    newAppLocks(),
		deploy:        deploylifecycle.NewTracker(log, eas),
	}
	// Only assign when non-nil: storing a typed-nil *buildkit.Component into the
	// BuildKitProvider interface would make the field non-nil and defeat the
	// `b.BuildKit == nil` guard used to detect an unconfigured daemon.
	if bk != nil {
		b.BuildKit = bk
	}
	return b
}

// mergeServiceEnvVars merges per-service environment variables from app.toml into existing service env vars.
// Uses the same source-tracking logic as global variables:
// - Manual vars (source="manual") always persist and shadow config vars with the same key
// - app.toml vars (source="config") override existing config vars but never manual vars
// - Removing a var from app.toml only deletes it if source="config"
func mergeServiceEnvVars(existingEnvs []core_v1alpha.ConfigSpecServicesEnv, newEnvs []core_v1alpha.ConfigSpecServicesEnv) []core_v1alpha.ConfigSpecServicesEnv {
	// If no new env vars from app.toml, preserve all existing
	if len(newEnvs) == 0 {
		return existingEnvs
	}

	// Build map of app.toml env vars
	newEnvMap := make(map[string]core_v1alpha.ConfigSpecServicesEnv)
	for _, e := range newEnvs {
		newEnvMap[e.Key] = e
	}

	// Build result by merging
	envMap := make(map[string]core_v1alpha.ConfigSpecServicesEnv)

	// Keep manual and addon vars - they shadow config vars with the same key
	for _, e := range existingEnvs {
		source := e.Source
		if source == "" {
			source = "manual" // backward compatibility: preserve unknown-source vars
		}

		if source == "manual" || source == "addon" {
			envMap[e.Key] = e
		}
		// config vars only kept if still in app.toml (checked below)
	}

	// Add app.toml vars, but never override manual vars.
	// When a manual var shadows a config var, carry metadata (Required, Description)
	// from the config var so app.toml declarations are always visible.
	for key, configVar := range newEnvMap {
		if existing, hasManual := envMap[key]; hasManual {
			existing.Required = configVar.Required
			existing.Description = configVar.Description
			envMap[key] = existing
		} else {
			envMap[key] = configVar
		}
	}

	// Convert back to slice
	result := make([]core_v1alpha.ConfigSpecServicesEnv, 0, len(envMap))
	for _, e := range envMap {
		result = append(result, e)
	}

	return result
}

// errNoServices is returned when a build produces no services
var errNoServices = errors.New("no services defined: please define at least one service in a Procfile or .miren/app.toml")

// validateServicesExist checks that at least one service is defined in the config.
// Returns an error if no services are found.
func validateServicesExist(spec core_v1alpha.ConfigSpec) error {
	if len(spec.Services) == 0 {
		return errNoServices
	}
	return nil
}

// validateRequiredVars checks that all required environment variables have non-empty values.
// Returns an error listing all missing required vars with actionable guidance.
func validateRequiredVars(spec core_v1alpha.ConfigSpec) error {
	type missingVar struct {
		key         string
		description string
		service     string
	}
	var missing []missingVar

	for _, v := range spec.Variables {
		if v.Required && strings.TrimSpace(v.Value) == "" {
			missing = append(missing, missingVar{key: v.Key, description: v.Description})
		}
	}
	for _, svc := range spec.Services {
		for _, e := range svc.Env {
			if e.Required && strings.TrimSpace(e.Value) == "" {
				missing = append(missing, missingVar{key: e.Key, description: e.Description, service: svc.Name})
			}
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("missing required environment variables:\n")
	for _, m := range missing {
		if m.service != "" {
			fmt.Fprintf(&b, "  - %s (service: %s)", m.key, m.service)
		} else {
			fmt.Fprintf(&b, "  - %s", m.key)
		}
		if m.description != "" {
			fmt.Fprintf(&b, ": %s", m.description)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nSet them with: miren env set -e KEY=VALUE (or -s KEY=VALUE for sensitive vars)")
	return fmt.Errorf("%s", b.String())
}

// nodePortKey uniquely identifies a node port allocation by port number and protocol.
type nodePortKey struct {
	port     int64
	protocol string
}

// validateNodePorts checks for node port conflicts both within the config being
// deployed and against existing sandbox pools in the cluster. This catches
// collisions at deploy time rather than letting them fail at runtime in nftables.
func validateNodePorts(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id, spec core_v1alpha.ConfigSpec) error {
	// Collect all (node_port, protocol) pairs from the new config
	newPorts := map[nodePortKey]string{} // key → service name
	for _, svc := range spec.Services {
		for _, p := range svc.Ports {
			if p.NodePort <= 0 {
				continue
			}
			proto := "tcp"
			if p.Protocol == core_v1alpha.ConfigSpecServicesPortsUDP {
				proto = "udp"
			}
			key := nodePortKey{port: p.NodePort, protocol: proto}
			if existing, ok := newPorts[key]; ok {
				return fmt.Errorf("services %q and %q both claim node_port %d/%s", existing, svc.Name, p.NodePort, proto)
			}
			newPorts[key] = svc.Name
		}
	}

	if len(newPorts) == 0 {
		return nil
	}

	// List all SandboxPool entities to check for cross-app conflicts
	resp, err := eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return fmt.Errorf("failed to list sandbox pools: %w", err)
	}

	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Skip pools belonging to the app being deployed
		if pool.App == appID {
			continue
		}

		// Skip scaled-down pools
		if pool.DesiredInstances <= 0 {
			continue
		}

		// Get the app name from sandbox labels for error messages
		appName, _ := pool.SandboxLabels.Get("app")
		if appName == "" {
			appName = pool.App.String()
		}

		for _, container := range pool.SandboxSpec.Container {
			for _, p := range container.Port {
				if p.NodePort <= 0 {
					continue
				}
				proto := "tcp"
				if p.Protocol == compute_v1alpha.SandboxSpecContainerPortUDP {
					proto = "udp"
				}
				key := nodePortKey{port: p.NodePort, protocol: proto}
				if svcName, ok := newPorts[key]; ok {
					return fmt.Errorf("node_port %d/%s (service %q) is already in use by app %q service %q", p.NodePort, proto, svcName, appName, pool.Service)
				}
			}
		}
	}

	return nil
}

// validateDiskConfigs checks that disks referenced with size_gb=0 already exist
// in the entity store. This catches missing disks at deploy time rather than
// letting sandboxes fail at runtime with retry loops.
func validateDiskConfigs(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, spec core_v1alpha.ConfigSpec) error {
	for _, svc := range spec.Services {
		for _, disk := range svc.Disks {
			if disk.SizeGb > 0 || disk.Name == "" || disk.Provider == core_v1alpha.ConfigSpecServicesDisksLOCAL {
				continue
			}
			// size_gb == 0 means we expect the disk to already exist
			listResp, err := eac.List(ctx, entity.String(storage.DiskNameId, disk.Name))
			if err != nil {
				return fmt.Errorf("failed to query disk %q: %w", disk.Name, err)
			}
			if len(listResp.Values()) == 0 {
				return fmt.Errorf("disk %q does not exist; set size_gb to auto-create it", disk.Name)
			}
		}
	}
	return nil
}

func mapDiskProvider(provider string) core_v1alpha.ConfigSpecServicesDisksProvider {
	switch provider {
	case "local":
		return core_v1alpha.ConfigSpecServicesDisksLOCAL
	default:
		return core_v1alpha.ConfigSpecServicesDisksMIREN
	}
}

// buildServicesConfig collects services from app config and procfile,
// resolves defaults, and returns the final service configurations.
// This is the core logic for determining which services exist in an app_version
// and what their concurrency settings should be.
// ensureWeb, when true, guarantees a web service exists: if neither app.toml
// nor the Procfile supplied a web command, webDefault becomes its command. That
// default may be empty, which is the image/Dockerfile case where the image's
// own ENTRYPOINT+CMD run in exec form at launch (MIR-1444).
func buildServicesConfig(appConfig *appconfig.AppConfig, procfileServices map[string]string, ensureWeb bool, webDefault string) []core_v1alpha.ConfigSpecServices {
	// Build command map from app config
	srvMap := map[string]string{}
	if appConfig != nil {
		for k, v := range appConfig.Services {
			if v != nil && v.Command != "" {
				srvMap[k] = v.Command
			}
		}
	}

	// Add procfile services (app config takes precedence)
	for k, v := range procfileServices {
		if _, ok := srvMap[k]; !ok {
			srvMap[k] = v
		}
	}

	// Default a web command when nothing above supplied one. srvMap["web"]
	// existing here is exactly "app.toml or Procfile defined a web command", so
	// this owns the whole "is there a web command yet?" decision in one place.
	if ensureWeb {
		if _, ok := srvMap["web"]; !ok {
			srvMap["web"] = webDefault
		}
	}

	// Collect all service names from both commands and app config
	// Services may have concurrency config without explicit commands
	allServiceNames := make([]string, 0, len(srvMap))
	for serviceName := range srvMap {
		allServiceNames = append(allServiceNames, serviceName)
	}

	// Also include services that have config in app.toml but no commands
	if appConfig != nil {
		for serviceName := range appConfig.Services {
			if !slices.Contains(allServiceNames, serviceName) {
				allServiceNames = append(allServiceNames, serviceName)
			}
		}
	}

	// Resolve defaults for all services
	ac := appConfig
	if ac != nil {
		ac.ResolveDefaults(allServiceNames)
	} else {
		// No app.toml - create minimal config with defaults
		ac = &appconfig.AppConfig{}
		ac.ResolveDefaults(allServiceNames)
	}

	// Build ConfigSpec.Services[] from fully-resolved appconfig
	// IMPORTANT: Iterate over allServiceNames, not srvMap, because services
	// may have concurrency config without commands
	var services []core_v1alpha.ConfigSpecServices
	for _, serviceName := range allServiceNames {
		svc := core_v1alpha.ConfigSpecServices{
			Name:    serviceName,
			Command: srvMap[serviceName],
		}

		// Map from appconfig to entity schema
		// After ResolveDefaults(), every service is guaranteed to have config
		if serviceConfig, ok := ac.Services[serviceName]; ok && serviceConfig != nil {
			// Copy image if specified
			if serviceConfig.Image != "" {
				svc.Image = serviceConfig.Image
			}

			// Copy port configuration: prefer ports[] array over scalar fields
			if len(serviceConfig.Ports) > 0 {
				svc.Ports = make([]core_v1alpha.ConfigSpecServicesPorts, 0, len(serviceConfig.Ports))
				for _, p := range serviceConfig.Ports {
					sp := core_v1alpha.ConfigSpecServicesPorts{
						Port:     int64(p.Port),
						Name:     p.Name,
						Type:     p.Type,
						NodePort: int64(p.NodePort),
					}
					if p.Type == "udp" {
						sp.Protocol = core_v1alpha.ConfigSpecServicesPortsUDP
					} else {
						sp.Protocol = core_v1alpha.ConfigSpecServicesPortsTCP
					}
					svc.Ports = append(svc.Ports, sp)
				}
			} else {
				if serviceConfig.Port > 0 {
					svc.Port = int64(serviceConfig.Port)
				}
				if serviceConfig.PortName != "" {
					svc.PortName = serviceConfig.PortName
				}
				if serviceConfig.PortType != "" {
					svc.PortType = serviceConfig.PortType
				}
			}

			if serviceConfig.PortTimeout != "" {
				svc.PortTimeout = serviceConfig.PortTimeout
			}

			if serviceConfig.Concurrency != nil {
				svc.Concurrency = core_v1alpha.ConfigSpecServicesConcurrency{
					Mode:                serviceConfig.Concurrency.Mode,
					NumInstances:        int64(serviceConfig.Concurrency.NumInstances),
					RequestsPerInstance: int64(serviceConfig.Concurrency.RequestsPerInstance),
					ScaleDownDelay:      serviceConfig.Concurrency.ScaleDownDelay,
					ShutdownTimeout:     serviceConfig.Concurrency.ShutdownTimeout,
				}
			}

			// Convert disk configurations
			if len(serviceConfig.Disks) > 0 {
				svc.Disks = make([]core_v1alpha.ConfigSpecServicesDisks, 0, len(serviceConfig.Disks))
				for _, disk := range serviceConfig.Disks {
					svc.Disks = append(svc.Disks, core_v1alpha.ConfigSpecServicesDisks{
						Name:         disk.Name,
						MountPath:    disk.MountPath,
						ReadOnly:     disk.ReadOnly,
						SizeGb:       int64(disk.SizeGB),
						Filesystem:   disk.Filesystem,
						LeaseTimeout: disk.LeaseTimeout,
						Owner:        disk.Owner,
						Provider:     mapDiskProvider(disk.Provider),
					})
				}
			}

			// Convert service-specific environment variables
			if len(serviceConfig.EnvVars) > 0 {
				svc.Env = make([]core_v1alpha.ConfigSpecServicesEnv, 0, len(serviceConfig.EnvVars))
				for _, envVar := range serviceConfig.EnvVars {
					svc.Env = append(svc.Env, core_v1alpha.ConfigSpecServicesEnv{
						Key:         envVar.Key,
						Value:       envVar.Value,
						Sensitive:   envVar.Sensitive,
						Required:    envVar.Required,
						Description: envVar.Description,
						Source:      "config",
					})
				}
			}
		}

		services = append(services, svc)
	}

	return services
}

// ConfigInputs holds all the inputs needed to build an app version config.
type ConfigInputs struct {
	// BuildResult contains entrypoint, working dir, and image entrypoint/cmd from the build
	BuildResult *BuildResult

	// AppConfig is the parsed app.toml configuration (may be nil)
	AppConfig *appconfig.AppConfig

	// ProcfileServices maps service names to commands from the Procfile (may be nil)
	ProcfileServices map[string]string

	// ExistingConfig is the current config to preserve manual env vars from
	ExistingConfig core_v1alpha.ConfigSpec

	// CliEnvVars are environment variables passed via CLI flags (e.g., miren deploy -e KEY=VALUE)
	// These are applied with source="manual" and take precedence over app.toml vars
	CliEnvVars []*build_v1alpha.EnvironmentVariable
}

// buildVersionConfig builds the app version config from all inputs.
// This is a pure function that can be easily tested.
func buildVersionConfig(inputs ConfigInputs) core_v1alpha.ConfigSpec {
	var spec core_v1alpha.ConfigSpec

	res := inputs.BuildResult
	ac := inputs.AppConfig
	procfileServices := inputs.ProcfileServices

	// Preserve existing variables for merging later
	spec.Variables = inputs.ExistingConfig.Variables

	// Set entrypoint from stack build result
	if res != nil && res.Entrypoint != "" {
		spec.Entrypoint = res.Entrypoint
	}

	// Set start directory from build result, defaulting to /app
	if res != nil && res.WorkingDir != "" {
		spec.StartDirectory = res.WorkingDir
	} else {
		spec.StartDirectory = "/app"
	}

	// A build produces a runnable artifact, so it needs a routable web service.
	// If nothing (app.toml or Procfile) defined a web command, fall back to the
	// build's default: a stack build supplies one, while an image/Dockerfile
	// build leaves it empty so the image's own ENTRYPOINT+CMD run in exec form
	// at launch (MIR-1444). buildServicesConfig owns that defaulting, so we
	// don't stage synthetic entries into the caller-owned Procfile map.
	webDefault := ""
	if res != nil {
		webDefault = res.Command
		if webDefault == "" {
			webDefault = res.Entrypoint
		}
	}
	spec.Services = buildServicesConfig(ac, procfileServices, res != nil, webDefault)

	// Merge env vars: preserve manual vars from existing services
	for i := range spec.Services {
		serviceName := spec.Services[i].Name

		// Find matching service in existing config
		for _, existingSvc := range inputs.ExistingConfig.Services {
			if existingSvc.Name == serviceName {
				// Merge env vars: app.toml vars override, but manual vars persist
				spec.Services[i].Env = mergeServiceEnvVars(existingSvc.Env, spec.Services[i].Env)
				break
			}
		}
	}

	// Merge environment variables from app config
	// Preserves existing variables when app.toml has no [[env]] section
	spec.Variables = mergeVariablesFromAppConfig(spec.Variables, ac)

	// Apply CLI-provided env vars last (highest precedence, always source="manual")
	spec.Variables = mergeCliEnvVars(spec.Variables, inputs.CliEnvVars)

	return spec
}

func buildVariablesFromAppConfig(appConfig *appconfig.AppConfig) []core_v1alpha.ConfigSpecVariables {
	if appConfig == nil || len(appConfig.EnvVars) == 0 {
		return nil
	}

	variables := make([]core_v1alpha.ConfigSpecVariables, 0, len(appConfig.EnvVars))
	for _, envVar := range appConfig.EnvVars {
		variables = append(variables, core_v1alpha.ConfigSpecVariables{
			Key:         envVar.Key,
			Value:       envVar.Value,
			Sensitive:   envVar.Sensitive,
			Required:    envVar.Required,
			Description: envVar.Description,
			Source:      "config",
		})
	}
	return variables
}

// mergeVariablesFromAppConfig merges environment variables from app.toml into existing variables.
// The merge strategy respects variable sources:
// - Manual vars (source="manual") always persist and shadow config vars with the same key
// - Variables from app.toml (source="config") override existing config vars but never manual vars
// - If a variable is removed from app.toml, it's only deleted if it was originally from config
// - If appConfig is nil or has no env vars, all existing variables are preserved.
func mergeVariablesFromAppConfig(existingVars []core_v1alpha.ConfigSpecVariables, appConfig *appconfig.AppConfig) []core_v1alpha.ConfigSpecVariables {
	appConfigVars := buildVariablesFromAppConfig(appConfig)

	// If no app.toml vars, preserve all existing vars
	if appConfigVars == nil {
		return existingVars
	}

	// Build a map of app.toml variables for quick lookup
	appConfigMap := make(map[string]core_v1alpha.ConfigSpecVariables)
	for _, v := range appConfigVars {
		appConfigMap[v.Key] = v
	}

	// Build result by merging
	varMap := make(map[string]core_v1alpha.ConfigSpecVariables)

	// First, add all existing manual and addon variables - these always persist
	for _, v := range existingVars {
		// Backward compatibility: preserve unknown-source vars as manual
		source := v.Source
		if source == "" {
			source = "manual"
		}

		// Keep manual and addon vars - they shadow config vars with the same key
		if source == "manual" || source == "addon" {
			varMap[v.Key] = v
		}
		// config vars are only kept if still in app.toml (checked below)
	}

	// Now add app.toml variables, but never override manual vars.
	// When a manual var shadows a config var, carry metadata (Required, Description)
	// from the config var so app.toml declarations are always visible.
	for key, configVar := range appConfigMap {
		if existing, hasManual := varMap[key]; hasManual {
			existing.Required = configVar.Required
			existing.Description = configVar.Description
			varMap[key] = existing
		} else {
			varMap[key] = configVar
		}
	}

	// Convert map back to slice
	result := make([]core_v1alpha.ConfigSpecVariables, 0, len(varMap))
	for _, v := range varMap {
		result = append(result, v)
	}

	return result
}

// mergeCliEnvVars merges CLI-provided environment variables into the existing config.
// CLI vars are always marked with source="manual" and override any existing var with the same key.
func mergeCliEnvVars(existingVars []core_v1alpha.ConfigSpecVariables, cliVars []*build_v1alpha.EnvironmentVariable) []core_v1alpha.ConfigSpecVariables {
	if len(cliVars) == 0 {
		return existingVars
	}

	// Build a map of existing vars
	varMap := make(map[string]core_v1alpha.ConfigSpecVariables)
	for _, v := range existingVars {
		varMap[v.Key] = v
	}

	// CLI vars always override (marked as user-provided).
	// Preserve Required/Description metadata from existing vars (e.g. from app.toml).
	for _, cv := range cliVars {
		newVar := core_v1alpha.ConfigSpecVariables{
			Key:       cv.Key(),
			Value:     cv.Value(),
			Sensitive: cv.Sensitive(),
			Source:    "manual",
		}
		if existing, ok := varMap[cv.Key()]; ok {
			newVar.Required = existing.Required
			newVar.Description = existing.Description
		}
		varMap[cv.Key()] = newVar
	}

	// Convert map back to slice
	result := make([]core_v1alpha.ConfigSpecVariables, 0, len(varMap))
	for _, v := range varMap {
		result = append(result, v)
	}

	return result
}

// validateWorkloadRole checks an app.toml workload_role without any I/O: it must
// be a known, app-scoped role. app.toml is owner-controlled and applied by the
// cert-trusted build server, so honoring a cluster-scoped value here would let
// an app owner escalate — those are the operator's to grant via SetWorkloadRole.
// A cluster-scoped or unknown value fails the deploy rather than being ignored.
// Returns nil when no role is declared.
//
// It is deliberately separate from persistence and run during config prep, so an
// invalid role fails the deploy *before* the new version is activated rather
// than after it is already live and serving.
func validateWorkloadRole(ac *appconfig.AppConfig) error {
	if ac == nil || ac.WorkloadRole == "" {
		return nil
	}
	role := ac.WorkloadRole

	if _, ok := workloadroles.Lookup(role); !ok {
		return fmt.Errorf("unknown workload_role %q in app.toml", role)
	}
	if !workloadroles.IsAppScoped(role) {
		return fmt.Errorf("workload_role %q is cluster-scoped and cannot be set from app.toml; "+
			"an operator must grant it with `miren app set-workload-role`", role)
	}
	return nil
}

// applyWorkloadRole validates a declared app.toml workload_role and persists it
// onto the app entity. It patches only workload_role, so a concurrent
// active_version write is not clobbered.
func (b *Builder) applyWorkloadRole(ctx context.Context, name string, ac *appconfig.AppConfig) error {
	if err := validateWorkloadRole(ac); err != nil {
		return err
	}
	if ac == nil || ac.WorkloadRole == "" {
		return nil
	}

	var appRec core_v1alpha.App
	if err := b.ec.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("loading app %s to set workload role: %w", name, err)
	}
	if appRec.WorkloadRole == ac.WorkloadRole {
		return nil
	}
	return b.ec.Patch(ctx, appRec.ID, 0, entity.String(core_v1alpha.AppWorkloadRoleId, ac.WorkloadRole))
}

func (b *Builder) nextVersion(ctx context.Context, name string) (
	*core_v1alpha.App,
	*core_v1alpha.AppVersion,
	core_v1alpha.ConfigSpec,
	string,
	error,
) {
	var appRec core_v1alpha.App

	err := b.ec.Get(ctx, name, &appRec)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil, core_v1alpha.ConfigSpec{}, "", err
		}

		appRec.Project = "project/default"

		id, err := b.ec.Create(ctx, name, &appRec)
		if err != nil {
			return nil, nil, core_v1alpha.ConfigSpec{}, "", err
		}
		appRec.ID = id
	}

	var currentCfg core_v1alpha.ConfigSpec

	switch {
	case appRec.ActiveVersion != "":
		var verRec core_v1alpha.AppVersion

		err := b.ec.GetById(ctx, appRec.ActiveVersion, &verRec)
		if err != nil {
			return nil, nil, core_v1alpha.ConfigSpec{}, "", err
		}

		// Resolve config from ConfigVersion if available, otherwise use inline
		resolvedCfg, err := coreutil.ResolveConfig(ctx, b.ec.EAC(), &verRec)
		if err != nil {
			return nil, nil, core_v1alpha.ConfigSpec{}, "", fmt.Errorf("failed to resolve config: %w", err)
		}
		currentCfg = *resolvedCfg

	case appRec.InitialConfig != "":
		// First deploy after `miren init` staged config server-side. Seed from
		// the initial ConfigVersion so generated secrets are picked up.
		var cv core_v1alpha.ConfigVersion
		if err := b.ec.GetById(ctx, appRec.InitialConfig, &cv); err != nil {
			return nil, nil, core_v1alpha.ConfigSpec{}, "", fmt.Errorf("failed to load initial config: %w", err)
		}
		currentCfg = cv.Spec
	}

	ver := name + "-" + idgen.Gen("v")
	art := name + "-" + idgen.Gen("a")

	b.Log.Info("creating new app version", "app", appRec.ID, "version", ver, "artifact", art)

	var av core_v1alpha.AppVersion
	av.App = appRec.ID
	av.Version = ver
	av.ImageUrl = "cluster.local:5000/" + name + ":" + art
	av.AdminToken = idgen.GenAdminToken()

	return &appRec, &av, currentCfg, art, nil
}

func (b *Builder) loadAppConfig(dfs fsutil.FS) (*appconfig.AppConfig, error) {
	dr, err := dfs.Open(appconfig.AppConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File not found is expected for apps without app.toml
			return nil, nil
		}
		// Return other errors (permission denied, IO errors, etc.)
		return nil, err
	}

	defer dr.Close()

	data, err := io.ReadAll(dr)
	if err != nil {
		return nil, err
	}

	ac, err := appconfig.Parse(data)
	if err != nil {
		return nil, err
	}

	return ac, nil
}

// sendErrorStatus sends an error status update if status is not nil, logging any send errors
func (b *Builder) sendErrorStatus(ctx context.Context, status *stream.SendStreamClient[*build_v1alpha.Status], format string, args ...interface{}) {
	if status != nil {
		so := new(build_v1alpha.Status)
		so.Update().SetError(fmt.Sprintf(format, args...))
		if _, err := status.Send(ctx, so); err != nil {
			b.Log.Warn("error sending error status", "error", err)
		}
	}
}

// sendLogStatus sends a structured log status update if status is not nil
func (b *Builder) sendLogStatus(ctx context.Context, status *stream.SendStreamClient[*build_v1alpha.Status], level, text string, fields ...*build_v1alpha.LogField) {
	if status != nil {
		so := new(build_v1alpha.Status)
		entry := &build_v1alpha.LogEntry{}
		entry.SetLevel(level)
		entry.SetText(text)
		if len(fields) > 0 {
			entry.SetFields(fields)
		}
		so.Update().SetLog(entry)
		if _, err := status.Send(ctx, so); err != nil {
			b.Log.Warn("error sending log status", "error", err)
		}
	}
}

func logField(key, value string) *build_v1alpha.LogField {
	f := &build_v1alpha.LogField{}
	f.SetKey(key)
	f.SetValue(value)
	return f
}

// configDeclaresLocalStorage reports whether the config already declares a disk
// that satisfies the local-storage migration. That's true when any service
// declares an explicit local-provider disk (regardless of its container mount
// path — all local volumes share the same per-app host directory keyed on app
// ID), or a disk of any provider at the legacy /miren/data/local mount path.
func configDeclaresLocalStorage(configSpec core_v1alpha.ConfigSpec) bool {
	for _, svc := range configSpec.Services {
		for _, disk := range svc.Disks {
			if disk.Provider == core_v1alpha.ConfigSpecServicesDisksLOCAL || disk.MountPath == "/miren/data/local" {
				return true
			}
		}
	}
	return false
}

// shouldWarnLocalStorageMigration reports whether a deploy should warn that the
// app has existing data in local storage but no explicit disk config.
func shouldWarnLocalStorageMigration(dataPath string, appID entity.Id, configSpec core_v1alpha.ConfigSpec) bool {
	if dataPath == "" {
		return false
	}
	if configDeclaresLocalStorage(configSpec) {
		return false
	}

	localPath := filepath.Join(dataPath, "data", "local", appID.String())
	entries, err := os.ReadDir(localPath)
	return err == nil && len(entries) > 0
}

// checkLocalStorageMigration checks whether the app has existing data in
// local storage but no explicit disk config, and sends a deploy warning.
func (b *Builder) checkLocalStorageMigration(ctx context.Context, appID entity.Id, configSpec core_v1alpha.ConfigSpec, status *stream.SendStreamClient[*build_v1alpha.Status]) {
	if !shouldWarnLocalStorageMigration(b.DataPath, appID, configSpec) {
		return
	}

	b.sendLogStatus(ctx, status, "warn", "Local storage data was automatically mounted",
		logField("detail", "This app has data stored on disk, but your app.toml doesn't declare it yet. "+
			"Add a disk config so the data continues to be available — this safety net will be removed in a future release."),
		logField("link", "[Configuring Disks](https://miren.md/disks)"),
	)
}

// isSystemEnvVar returns true if the given key is a system-managed env var
// that should not be injected as a build arg. The whole MIREN_ namespace is
// reserved (the injected MIREN_RUNTIME_* vars are enumerated in
// api/app/runtimeenv.go), so only the unprefixed names need explicit cases here.
func isSystemEnvVar(key string) bool {
	switch key {
	case "PORT", "ADMIN_TOKEN":
		return true
	}
	return app.IsReservedEnvVar(key)
}

// computeBuildEnvVars computes the merged set of environment variables to inject
// into the build process. It reuses the same merge logic as runtime config:
// existing config vars + app.toml vars + CLI vars, then filters out system-managed vars.
func computeBuildEnvVars(
	existingVars []core_v1alpha.ConfigSpecVariables,
	ac *appconfig.AppConfig,
	cliVars []*build_v1alpha.EnvironmentVariable,
) map[string]string {
	merged := mergeVariablesFromAppConfig(existingVars, ac)
	merged = mergeCliEnvVars(merged, cliVars)

	result := make(map[string]string)
	for _, v := range merged {
		if isSystemEnvVar(v.Key) {
			continue
		}
		result[v.Key] = v.Value
	}
	return result
}

func (b *Builder) BuildFromTar(ctx context.Context, state *build_v1alpha.BuilderBuildFromTar) (retErr error) {
	args := state.Args()

	name := args.Application()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	status := args.Status()

	eph := ephemeralOptsFromArgs(args.HasEphemeralLabel(), args.EphemeralLabel(),
		args.HasEphemeralTtl(), args.EphemeralTtl())

	// Take the deployment record and its lock before receiving the tar, so a
	// build that is blocked by another in-flight deploy fails fast instead of
	// after a full upload.
	var deployReq *build_v1alpha.DeployRequest
	if args.HasDeployment() {
		deployReq = args.Deployment()
	}
	deploy, err := b.beginDeploy(ctx, name, deployReq, eph, status)
	if err != nil {
		return err
	}
	defer func() { deploy.failOnError(ctx, retErr) }()

	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "buildkit-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(path)

	so := new(build_v1alpha.Status)

	if status != nil {
		so.Update().SetMessage("Reading application data")
		_, _ = status.Send(ctx, so)
	}

	b.Log.Debug("receiving tar data", "app", name, "tempdir", path)

	// -- build.receive_tar span
	ctx, recvSpan := buildTracer.Start(ctx, "build.receive_tar",
		trace.WithAttributes(attribute.String("miren.app.name", name)))
	r := stream.ToReader(ctx, td)

	_, err = tarx.TarFS(r, path)
	if err != nil {
		recvSpan.RecordError(err)
		recvSpan.SetStatus(codes.Error, err.Error())
		recvSpan.End()
		b.sendErrorStatus(ctx, status, "Error untaring data: %v", err)
		return fmt.Errorf("error untaring data: %w", err)
	}
	recvSpan.End()

	// Save source code cache for future delta uploads
	if b.DataPath != "" {
		cache := &sourceCache{dataPath: b.DataPath, log: b.Log, locks: b.cacheLocks}
		if err := cache.saveSourceImage(name, path); err != nil {
			b.Log.Warn("failed to save source code cache", "error", err, "app", name)
		}
	}

	if status != nil {
		so.Update().SetMessage("Launching builder")
		_, _ = status.Send(ctx, so)
	}

	result, err := b.buildFromDir(ctx, name, path, status, args.EnvVars(), eph, deploy)
	if err != nil {
		return err
	}

	state.Results().SetVersion(result.version)
	if result.versionShortId != "" {
		state.Results().SetVersionShortId(result.versionShortId)
	}
	state.Results().SetAccessInfo(&result.accessInfo)

	return nil
}

func (b *Builder) PrepareUpload(ctx context.Context, state *build_v1alpha.BuilderPrepareUpload) error {
	args := state.Args()

	name := args.Application()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	if b.DataPath == "" {
		return fmt.Errorf("data path not configured")
	}

	manifest := args.Manifest()

	tempDir, err := os.MkdirTemp(b.TempDir, "buildkit-")
	if err != nil {
		return err
	}

	cache := &sourceCache{dataPath: b.DataPath, log: b.Log, locks: b.cacheLocks}

	matched, needed, err := cache.stageMatchingFiles(name, tempDir, manifest)
	if err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("staging cached files: %w", err)
	}

	sessionID := idgen.Gen("s")

	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())

	b.sessions.Store(sessionID, &buildSession{
		dir:        tempDir,
		appName:    name,
		cancelFunc: cancelCleanup,
	})

	// Cleanup goroutine: remove session after 10 minutes if unused.
	// When BuildFromPrepared claims the session, it cancels this context
	// to stop the goroutine and prevent a race where cleanup runs mid-build.
	go func() {
		select {
		case <-time.After(10 * time.Minute):
			if val, loaded := b.sessions.LoadAndDelete(sessionID); loaded {
				sess := val.(*buildSession)
				os.RemoveAll(sess.dir)
				b.Log.Info("cleaned up expired upload session", "session", sessionID)
			}
		case <-cleanupCtx.Done():
		}
	}()

	b.Log.Info("prepared upload session",
		"session", sessionID,
		"app", name,
		"cached", matched,
		"needed", len(needed),
		"total", len(manifest),
	)

	result := &build_v1alpha.PrepareUploadResult{}
	result.SetSessionId(sessionID)
	result.SetNeededPaths(&needed)
	result.SetCachedCount(int32(matched))

	state.Results().SetResult(&result)

	return nil
}

func (b *Builder) BuildFromPrepared(ctx context.Context, state *build_v1alpha.BuilderBuildFromPrepared) (retErr error) {
	args := state.Args()
	sessionID := args.SessionId()

	val, ok := b.sessions.LoadAndDelete(sessionID)
	if !ok {
		return fmt.Errorf("unknown or expired upload session: %s", sessionID)
	}

	sess := val.(*buildSession)
	sess.cancelFunc()
	defer os.RemoveAll(sess.dir)

	name := sess.appName

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	status := args.Status()

	eph := ephemeralOptsFromArgs(args.HasEphemeralLabel(), args.EphemeralLabel(),
		args.HasEphemeralTtl(), args.EphemeralTtl())

	// Take the deployment record and its lock before extracting the delta, so a
	// blocked build fails fast (see BuildFromTar).
	var deployReq *build_v1alpha.DeployRequest
	if args.HasDeployment() {
		deployReq = args.Deployment()
	}
	deploy, err := b.beginDeploy(ctx, name, deployReq, eph, status)
	if err != nil {
		return err
	}
	defer func() { deploy.failOnError(ctx, retErr) }()

	// Extract partial tar into the session directory if provided
	td := args.Tardata()
	if td != nil {
		so := new(build_v1alpha.Status)
		if status != nil {
			so.Update().SetMessage("Receiving changed files")
			_, _ = status.Send(ctx, so)
		}

		r := stream.ToReader(ctx, td)
		_, err := tarx.TarFS(r, sess.dir)
		if err != nil {
			b.sendErrorStatus(ctx, status, "Error extracting changed files: %v", err)
			return fmt.Errorf("error extracting changed files: %w", err)
		}
	}

	// Save source code cache before building
	if b.DataPath != "" {
		cache := &sourceCache{dataPath: b.DataPath, log: b.Log, locks: b.cacheLocks}
		if err := cache.saveSourceImage(name, sess.dir); err != nil {
			b.Log.Warn("failed to save source code cache", "error", err, "app", name)
		}
	}

	if status != nil {
		so := new(build_v1alpha.Status)
		so.Update().SetMessage("Launching builder")
		_, _ = status.Send(ctx, so)
	}

	result, err := b.buildFromDir(ctx, name, sess.dir, status, args.EnvVars(), eph, deploy)
	if err != nil {
		return err
	}

	state.Results().SetVersion(result.version)
	if result.versionShortId != "" {
		state.Results().SetVersionShortId(result.versionShortId)
	}
	state.Results().SetAccessInfo(&result.accessInfo)

	return nil
}

type buildResult struct {
	version        string
	versionShortId string
	accessInfo     *build_v1alpha.AccessInfo
}

type ephemeralOpts struct {
	label string
	ttl   string
}

// ephemeralOptsFromArgs builds ephemeral options from the label/ttl parameters
// shared by both build entry points, returning nil for a non-ephemeral build.
func ephemeralOptsFromArgs(hasLabel bool, label string, hasTTL bool, ttl string) *ephemeralOpts {
	if !hasLabel || label == "" {
		return nil
	}
	if !hasTTL || ttl == "" {
		ttl = "24h"
	}
	return &ephemeralOpts{label: label, ttl: ttl}
}

func (b *Builder) buildFromDir(ctx context.Context, name string, path string,
	status *stream.SendStreamClient[*build_v1alpha.Status],
	envVars []*build_v1alpha.EnvironmentVariable,
	ephemeral *ephemeralOpts, deploy *deployTracking) (*buildResult, error) {

	// -- build.setup span: app config, stack detection, buildkit connect
	ctx, setupSpan := buildTracer.Start(ctx, "build.setup")

	tr, err := fsutil.NewFS(path)
	if err != nil {
		setupSpan.RecordError(err)
		setupSpan.SetStatus(codes.Error, err.Error())
		setupSpan.End()
		return nil, fmt.Errorf("error creating FS from build dir: %w", err)
	}

	ac, err := b.loadAppConfig(tr)
	if err != nil {
		setupSpan.RecordError(err)
		setupSpan.SetStatus(codes.Error, err.Error())
		setupSpan.End()
		b.sendErrorStatus(ctx, status, "Invalid app.toml: %v", err)
		return nil, fmt.Errorf("error loading app config: %w", err)
	}
	if ac != nil {
		b.Log.Info("loaded app config", "name", ac.Name, "envVarCount", len(ac.EnvVars), "serviceCount", len(ac.Services))
	}

	// Validate the app.toml workload_role up front, before any version is built
	// or activated, so a bad role fails the deploy without leaving a new version
	// live. applyWorkloadRole persists it after activation.
	if err := validateWorkloadRole(ac); err != nil {
		b.sendErrorStatus(ctx, status, "%v", err)
		return nil, err
	}

	buildStack, err := b.detectBuildStack(path, ac, name, tr)
	if err != nil {
		setupSpan.RecordError(err)
		setupSpan.SetStatus(codes.Error, err.Error())
		setupSpan.End()
		b.sendErrorStatus(ctx, status, "No supported stack detected for app %s: %v", name, err)
		return nil, err
	}

	setupSpan.SetAttributes(attribute.String("miren.build.stack", buildStack.Stack))
	setupSpan.End()

	appRec, mrv, existingCfg, _, err := b.nextVersion(ctx, name)
	if err != nil {
		b.Log.Error("error getting next version", "error", err)
		b.sendErrorStatus(ctx, status, "Error getting next version: %v", err)
		return nil, err
	}

	buildLog := &buildLogWriter{
		log:      b.Log,
		writer:   b.LogWriter,
		entityID: appRec.ID.String(),
		version:  mrv.Version,
	}

	deploy.setPhase(ctx, deploylifecycle.PhaseBuilding)

	// runBuildkitBuild is also the saga action's implementation, so the
	// span structure (buildkit + locate_artifact bundled together) is
	// slightly coarser than the pre-saga code's two separate spans.
	// Trace continuity matters more than the extra granularity.
	bkCtx, bkSpan := buildTracer.Start(ctx, "build.buildkit",
		trace.WithAttributes(attribute.String("miren.build.image", mrv.ImageUrl)))
	statusSender := NewRPCStatusSender(status, b.Log)
	res, artifactID, finalURL, err := b.runBuildkitBuild(bkCtx, runBuildkitBuildInputs{
		SourceDir:      path,
		AppName:        name,
		VersionName:    mrv.Version,
		ImageURL:       mrv.ImageUrl,
		BuildStack:     buildStack,
		AppConfig:      ac,
		ExistingConfig: existingCfg,
		CLIEnvVars:     envVars,
	}, statusSender, buildLog)
	if err != nil {
		bkSpan.RecordError(err)
		bkSpan.SetStatus(codes.Error, err.Error())
		bkSpan.End()
		return nil, err
	}
	bkSpan.End()

	deploy.setPhase(ctx, deploylifecycle.PhasePushing)

	mrv.Artifact = entity.Id(artifactID)
	mrv.ImageUrl = finalURL
	artifactName := strings.TrimPrefix(artifactID, "artifact/")
	b.Log.Debug("build complete", "image", mrv.ImageUrl)

	procfileServices, err := b.readProcFile(tr)
	if err != nil {
		return nil, fmt.Errorf("error reading procfile: %w", err)
	} else if procfileServices == nil {
		b.Log.Debug("no procfile found, using app config")
	} else {
		b.Log.Debug("using procfile", "services", maps.Keys(procfileServices))
	}

	// Build the version config from all inputs
	configSpec := buildVersionConfig(ConfigInputs{
		BuildResult:      res,
		AppConfig:        ac,
		ProcfileServices: procfileServices,
		ExistingConfig:   existingCfg,
		CliEnvVars:       envVars,
	})

	// Fail the deploy if no services are defined - this prevents deploying an app
	// that can't serve any traffic
	if err := validateServicesExist(configSpec); err != nil {
		b.sendErrorStatus(ctx, status, "%s. See https://miren.md/services", err)
		return nil, err
	}

	// Fail the deploy if required env vars are missing values.
	//
	// This runs after the image build rather than before it because the
	// final merged config depends on build outputs: the Procfile (which
	// determines per-service env vars) is read from the built image, and
	// the BuildResult provides entrypoint/command used to synthesize a
	// web service when none is defined in app.toml or Procfile. We can't
	// assemble the complete config — and therefore can't know which
	// required vars exist — until the build finishes.
	//
	// A future optimization could do a partial pre-flight check on global
	// vars from app.toml + existing config before building, but for now
	// we keep it simple with a single validation point that has the full
	// picture, matching how validateServicesExist works just above.
	if err := validateRequiredVars(configSpec); err != nil {
		b.sendErrorStatus(ctx, status, "%s", err)
		return nil, err
	}

	if err := validateNodePorts(ctx, b.ec.EAC(), appRec.ID, configSpec); err != nil {
		b.sendErrorStatus(ctx, status, "Deploy failed: %v", err)
		return nil, err
	}

	if err := validateDiskConfigs(ctx, b.ec.EAC(), configSpec); err != nil {
		b.sendErrorStatus(ctx, status, "Deploy failed: %v", err)
		return nil, err
	}

	if len(envVars) > 0 {
		b.Log.Info("applied CLI env vars", "count", len(envVars))
	}

	if ac != nil && len(ac.EnvVars) > 0 {
		b.Log.Info("merged env vars from app config", "count", len(ac.EnvVars))
	} else {
		b.Log.Debug("no new env vars from app config, preserving existing variables")
	}

	// Set ephemeral fields before creating the version entity
	isEphemeral := ephemeral != nil && ephemeral.label != ""
	if isEphemeral {
		if err := ephemeralx.ValidateLabel(ephemeral.label); err != nil {
			return nil, fmt.Errorf("invalid ephemeral label: %w", err)
		}
		ttlDuration, parseErr := time.ParseDuration(ephemeral.ttl)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid ephemeral TTL %q: %w", ephemeral.ttl, parseErr)
		}
		if ttlDuration <= 0 {
			return nil, fmt.Errorf("invalid ephemeral TTL %q: must be greater than 0", ephemeral.ttl)
		}
		mrv.EphemeralLabel = ephemeral.label
		mrv.EphemeralTtl = ephemeral.ttl
		mrv.EphemeralExpiresAt = time.Now().Add(ttlDuration)

		// Replace-on-same-label: delete existing ephemeral version with the same label
		if err := ephemeralx.ReplaceExisting(ctx, b.ec.EAC(), appRec.ID, ephemeral.label, b.Log); err != nil {
			return nil, fmt.Errorf("failed to replace existing ephemeral version %q: %w", ephemeral.label, err)
		}

		// Enforce per-app ephemeral version limit
		if err := ephemeralx.EnforceLimit(ctx, b.ec.EAC(), appRec.ID, ephemeralx.DefaultMaxEphemeral, b.Log); err != nil {
			return nil, fmt.Errorf("failed to enforce ephemeral limit: %w", err)
		}
	}

	// -- build.create_version span
	createVerCtx, createVerSpan := buildTracer.Start(ctx, "build.create_version",
		trace.WithAttributes(attribute.String("miren.app.version", mrv.Version)))

	// Create ConfigVersion as the sole config store (inline Config is no longer written)
	configVer := &core_v1alpha.ConfigVersion{
		App:  mrv.App,
		Spec: configSpec,
	}
	cvName := mrv.Version + "-cfg"
	cvid, err := b.ec.Create(createVerCtx, cvName, configVer)
	if err != nil {
		createVerSpan.RecordError(err)
		createVerSpan.SetStatus(codes.Error, err.Error())
		createVerSpan.End()
		return nil, fmt.Errorf("error creating config version: %w", err)
	}
	mrv.ConfigVersion = cvid
	mrv.Config = core_v1alpha.Config{}

	id, err := b.ec.Create(createVerCtx, mrv.Version, mrv)
	if err != nil {
		createVerSpan.RecordError(err)
		createVerSpan.SetStatus(codes.Error, err.Error())
		createVerSpan.End()
		return nil, fmt.Errorf("error creating app version: %w", err)
	}
	createVerSpan.End()

	// The record now names a real version, replacing the placeholder it
	// carried while the build ran.
	deploy.setAppVersion(ctx, string(id))

	if isEphemeral {
		b.Log.Info("created ephemeral app version",
			"app", name, "version", mrv.Version, "label", ephemeral.label,
			"ttl", ephemeral.ttl, "expires_at", mrv.EphemeralExpiresAt)
	} else {
		// -- build.activate span (only for non-ephemeral versions)
		activateCtx, activateSpan := buildTracer.Start(ctx, "build.activate")

		deploy.setPhase(ctx, deploylifecycle.PhaseActivating)

		// Provision addons before activating the version so that AddonAssociation
		// entities exist when the launcher runs. The addon controller will create
		// a new AppVersion with addon vars once provisioning completes.
		if ac != nil && b.addonsClient != nil {
			if err := b.provisionAddons(ctx, name, ac); err != nil {
				return nil, fmt.Errorf("addon provisioning failed: %w", err)
			}
		}

		b.Log.Info("updating app entity with new version", "app", name, "version", mrv.Version)
		err = b.appClient.SetActiveVersion(activateCtx, name, string(id))
		if err != nil {
			activateSpan.RecordError(err)
			activateSpan.SetStatus(codes.Error, err.Error())
			activateSpan.End()
			return nil, fmt.Errorf("error updating app entity: %w", err)
		}

		// Apply a workload_role declared in app.toml (app-scoped only).
		if err := b.applyWorkloadRole(activateCtx, name, ac); err != nil {
			activateSpan.RecordError(err)
			activateSpan.SetStatus(codes.Error, err.Error())
			activateSpan.End()
			return nil, err
		}
		activateSpan.End()

		// The new version is running: settle the record and release the lock.
		deploy.activate(ctx)

		b.Log.Info("app version updated", "app", name, "version", mrv.Version)

		b.checkLocalStorageMigration(ctx, appRec.ID, configSpec, status)
	}

	// Log the deployment to the app's logs
	b.logDeployment(ctx, name, mrv.Version, artifactName)

	var ephLabel string
	if ephemeral != nil {
		ephLabel = ephemeral.label
	}
	accessInfo := b.getAccessInfo(ctx, name, ephLabel)

	// Look up the short ID for the newly created version entity
	var versionShortId string
	if ent, err := b.ec.GetByIdWithEntity(ctx, id, mrv); err == nil {
		for _, attr := range ent.Attrs() {
			if entity.Id(attr.ID) == entity.DBShortId {
				versionShortId = attr.Value.String()
				break
			}
		}
	}

	return &buildResult{
		version:        mrv.Version,
		versionShortId: versionShortId,
		accessInfo:     accessInfo,
	}, nil
}

// getAccessInfo queries routes to determine how the app can be accessed.
// When ephemeralLabel is non-empty, wildcard routes are resolved to concrete
// hostnames using the label (e.g., *.app.example.com → feat-x.app.example.com).
func (b *Builder) getAccessInfo(ctx context.Context, appName string, ephemeralLabel string) *build_v1alpha.AccessInfo {
	info := &build_v1alpha.AccessInfo{}

	// Get the app entity to find its ID
	appEntity, err := b.appClient.GetByName(ctx, appName)
	if err != nil {
		b.Log.Debug("could not get app for access info", "app", appName, "error", err)
		return info
	}

	// Get all routes
	routes, err := b.ingressClient.List(ctx)
	if err != nil {
		b.Log.Debug("could not list routes for access info", "error", err)
		return info
	}

	// Filter routes for this app
	var hostnames []string
	var hasDefaultRoute bool

	for _, r := range routes {
		if r.Route.App != appEntity.ID {
			continue
		}
		if r.Route.Default {
			hasDefaultRoute = true
		}
		if r.Route.Host == "" {
			continue
		}
		host := r.Route.Host
		if ephemeralLabel != "" {
			if strings.HasPrefix(host, "*.") {
				// Resolve wildcard: *.app.example.com → feat-x.app.example.com
				host = ephemeralLabel + "." + host[2:]
			} else {
				// Prepend label to normal route: app.example.com → feat-x.app.example.com
				host = ephemeralLabel + "." + host
			}
		}
		hostnames = append(hostnames, host)
	}

	info.SetHostnames(&hostnames)
	info.SetDefaultRoute(hasDefaultRoute)

	// Include the cloud DNS hostname if available
	if b.DNSHostname != "" {
		info.SetClusterHostname(b.DNSHostname)
	}

	return info
}

func (b *Builder) provisionAddons(ctx context.Context, appName string, ac *appconfig.AppConfig) error {
	for addonName, cfg := range ac.Addons {
		variant := ""
		version := ""
		if cfg != nil {
			variant = cfg.Variant
			version = cfg.Version
		}

		_, err := b.addonsClient.CreateInstance(ctx, "", addonName, variant, appName, version)
		if err != nil {
			// "already attached" is expected on redeploys
			if strings.Contains(err.Error(), "already attached") {
				b.Log.Debug("addon already attached", "addon", addonName, "app", appName)
				continue
			}
			return fmt.Errorf("provisioning addon %q for app %q: %w", addonName, appName, err)
		}
		b.Log.Info("addon provisioned from app.toml", "addon", addonName, "variant", variant, "version", version, "app", appName)
	}

	// Reconcile removals: if the addons section is explicitly present (even if
	// empty), remove any existing associations not declared in app.toml. A nil
	// Addons map means the section was absent, so we skip removal to avoid
	// accidentally deleting all addons.
	if ac.Addons != nil {
		existing, err := b.addonsClient.ListInstances(ctx, appName)
		if err != nil {
			return fmt.Errorf("listing addon instances for app %q: %w", appName, err)
		}

		declared := make(map[string]bool, len(ac.Addons))
		for name := range ac.Addons {
			declared[name] = true
		}

		for _, inst := range existing.Addons() {
			if !declared[inst.Name()] {
				if _, err := b.addonsClient.DeleteInstance(ctx, appName, inst.Name()); err != nil {
					return fmt.Errorf("removing addon %q from app %q: %w", inst.Name(), appName, err)
				}
				b.Log.Info("addon removed per app.toml", "addon", inst.Name(), "app", appName)
			}
		}
	}

	return nil
}

func (b *Builder) logDeployment(ctx context.Context, appName, version, artifact string) {
	if b.LogWriter == nil {
		return
	}

	// Get app entity ID
	var appRec core_v1alpha.App
	err := b.ec.Get(ctx, appName, &appRec)
	if err != nil {
		b.Log.Warn("failed to get app for deployment logging", "app", appName, "error", err)
		return
	}

	// Format in Heroku logfmt style
	logMsg := fmt.Sprintf("version=%s artifact=%s status=deployed", version, artifact)

	err = b.LogWriter.WriteEntry(appRec.ID.String(), observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      logMsg,
		Attributes: map[string]string{
			"source":   "build",
			"version":  version,
			"artifact": artifact,
		},
	})
	if err != nil {
		b.Log.Error("failed to write deployment log entry", "error", err, "app", appName)
	}
}

func (b *Builder) readProcFile(dfs fsutil.FS) (map[string]string, error) {
	r, err := dfs.Open("Procfile")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return procfile.Parser(data)
}

// AnalyzeApp analyzes an app without building it, returning detected stack, services, and configuration.
func (b *Builder) AnalyzeApp(ctx context.Context, state *build_v1alpha.BuilderAnalyzeApp) error {
	args := state.Args()
	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "analyze-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(path)

	b.Log.Debug("receiving tar data for analysis", "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := tarx.TarFS(r, path)
	if err != nil {
		return fmt.Errorf("error untaring data: %w", err)
	}

	result := &build_v1alpha.AnalysisResult{}

	// Collect detection events from multiple sources
	var events []build_v1alpha.DetectionEvent

	// Load app config
	ac, err := b.loadAppConfig(tr)
	if err != nil {
		return fmt.Errorf("error loading app config: %w", err)
	}
	if ac != nil {
		var event build_v1alpha.DetectionEvent
		event.SetKind("config")
		event.SetName("app.toml")
		event.SetMessage("Found app.toml configuration file")
		events = append(events, event)

		if ac.Name != "" {
			result.SetAppName(ac.Name)
		}

		// Extract env var keys (not values for security)
		var envKeys []string
		for _, ev := range ac.EnvVars {
			envKeys = append(envKeys, ev.Key)
		}
		if len(envKeys) > 0 {
			result.SetEnvVars(&envKeys)
		}

		// Check for explicit dockerfile in build config
		if ac.Build != nil && ac.Build.Dockerfile != "" {
			result.SetBuildDockerfile(ac.Build.Dockerfile)
		}
	}

	// Detect stack and build a BuildResult to use with buildVersionConfig
	var stackName string
	var buildResult BuildResult
	var detectedStack stackbuild.Stack
	var hasDockerfile bool

	// Check for Dockerfile.miren first
	if f, err := tr.Open("Dockerfile.miren"); err == nil {
		f.Close()
		hasDockerfile = true
		stackName = "dockerfile"
		result.SetBuildDockerfile("Dockerfile.miren")
	} else if ac != nil && ac.Build != nil && ac.Build.Dockerfile != "" {
		hasDockerfile = true
		stackName = "dockerfile"
	}

	// Always try to detect stack for analysis (env vars, events, etc.)
	// even when dockerfile is present
	var detectOpts stackbuild.BuildOptions
	detectOpts.Log = b.Log
	if ac != nil {
		detectOpts.Name = ac.Name
	}
	stack, err := stackbuild.DetectStack(path, detectOpts)
	if err != nil {
		b.Log.Debug("no stack detected", "error", err)
		if !hasDockerfile {
			stackName = "unknown"
		}
	} else {
		detectedStack = stack
		// Only use detected stack name/config if no dockerfile
		if !hasDockerfile {
			stackName = stack.Name()
			buildResult.Entrypoint = stack.Entrypoint()
			buildResult.Command = stack.WebCommand()
			buildResult.WorkingDir = stack.Image().Config.WorkingDir
		}
	}

	result.SetStack(stackName)
	if buildResult.Entrypoint != "" {
		result.SetEntrypoint(buildResult.Entrypoint)
	}

	// Add detection events from the stack
	if detectedStack != nil {
		stackEvents := detectedStack.Events()
		for _, e := range stackEvents {
			var event build_v1alpha.DetectionEvent
			event.SetKind(e.Kind)
			event.SetName(e.Name)
			event.SetMessage(e.Message)
			events = append(events, event)
		}

		// Add detected env vars from stack to the result
		// This supplements any env vars already found in app.toml
		stackEnvVars := detectedStack.RequiredEnvVars()
		if len(stackEnvVars) > 0 {
			// Get existing env keys if any
			var existingKeys []string
			if result.EnvVars() != nil {
				existingKeys = *result.EnvVars()
			}
			envKeySet := make(map[string]bool)
			for _, k := range existingKeys {
				envKeySet[k] = true
			}

			// Add stack-detected env var keys that aren't already present
			for _, ev := range stackEnvVars {
				if !envKeySet[ev.Name] {
					envKeySet[ev.Name] = true
					existingKeys = append(existingKeys, ev.Name)
				}
			}
			result.SetEnvVars(&existingKeys)
		}
	}

	// Read Procfile
	procfileServices, err := b.readProcFile(tr)
	if err != nil {
		return fmt.Errorf("error reading procfile: %w", err)
	}
	if len(procfileServices) > 0 {
		var event build_v1alpha.DetectionEvent
		event.SetKind("config")
		event.SetName("Procfile")
		event.SetMessage(fmt.Sprintf("Found Procfile with %d service(s)", len(procfileServices)))
		events = append(events, event)
	}

	// Use buildVersionConfig to compute services - same logic as BuildFromTar
	spec := buildVersionConfig(ConfigInputs{
		BuildResult:      &buildResult,
		AppConfig:        ac,
		ProcfileServices: procfileServices,
	})

	if spec.StartDirectory != "" {
		result.SetWorkingDir(spec.StartDirectory)
	}

	// Convert spec.Services to ServiceInfo with source tracking
	// This includes ALL services, even those without explicit commands (they use image default)
	var services []build_v1alpha.ServiceInfo
	for _, svc := range spec.Services {
		var svcInfo build_v1alpha.ServiceInfo
		svcInfo.SetName(svc.Name)

		if svc.Command != "" {
			svcInfo.SetCommand(svc.Command)
			// Determine source for this service
			source := determineServiceSource(svc.Name, svc.Command, ac, procfileServices, &buildResult)
			svcInfo.SetSource(source)

			// Add event when we inject a synthetic web service from stack detection
			if svc.Name == "web" && source == "stack" {
				var event build_v1alpha.DetectionEvent
				event.SetKind("service")
				event.SetName("web")
				event.SetMessage("Injected web service from stack detection")
				events = append(events, event)
			}
		} else {
			// Service has no explicit command - uses Dockerfile CMD (image default)
			svcInfo.SetSource("image")
		}

		services = append(services, svcInfo)
	}

	if len(services) > 0 {
		result.SetServices(&services)
	}

	// Set all collected events
	if len(events) > 0 {
		result.SetEvents(&events)
	}

	state.Results().SetResult(&result)
	return nil
}

// determineServiceSource identifies where a service command came from
func determineServiceSource(serviceName, command string, ac *appconfig.AppConfig, procfileServices map[string]string, buildResult *BuildResult) string {
	// Check app config first
	if ac != nil {
		if svcConfig, ok := ac.Services[serviceName]; ok && svcConfig != nil && svcConfig.Command != "" {
			if svcConfig.Command == command {
				return "app_config"
			}
		}
	}

	// Check Procfile
	if procfileServices != nil {
		if procCmd, ok := procfileServices[serviceName]; ok && procCmd == command {
			return "procfile"
		}
	}

	// Must be from stack detection
	if buildResult != nil {
		webCmd := buildResult.Command
		if serviceName == "web" && command == webCmd {
			return "stack"
		}
	}

	return "unknown"
}
