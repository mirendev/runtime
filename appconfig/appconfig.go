package appconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	tomlast "github.com/pelletier/go-toml/v2/unstable"

	"miren.dev/runtime/pkg/oncalendar"
)

var aliasWordRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type AppEnvVar struct {
	Key         string `json:"key" toml:"key"`
	Value       string `json:"value,omitempty" toml:"value,omitempty"`
	Required    bool   `json:"required,omitempty" toml:"required,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty" toml:"sensitive,omitempty"`
	Description string `json:"description,omitempty" toml:"description,omitempty"`

	// Backend names the secret backend the value comes from, and Ref addresses
	// the secret within it. Together they replace Value: the credential itself
	// never appears in app.toml, only a pointer to it, so the file stays safe to
	// commit.
	//
	// A reference authored here floats: each new ConfigVersion resolves it to
	// whatever is current, which is how a rotation reaches the app. Note that
	// this is about minting a config, not running one — deploying or rolling
	// back to an existing version keeps the reference that version recorded, so
	// a rotation reaches an app only once a new config is created for it.
	// Appending @version to Ref holds it at that exact version regardless.
	Backend string `json:"backend,omitempty" toml:"backend,omitempty"`
	Ref     string `json:"ref,omitempty" toml:"ref,omitempty"`
}

type BuildConfig struct {
	Dockerfile string   `toml:"dockerfile"`
	OnBuild    []string `toml:"onbuild"`
	Version    string   `toml:"version"`

	AlpineImage string `toml:"alpine_image"`

	// Secrets names secret backends to expose to the build. Each entry is mounted
	// into the build via BuildKit's secret session, so a `RUN --mount=type=secret,id=<id>`
	// step can read the decrypted value without it ever landing in an image layer
	// or build log. This is distinct from a runtime [[env]] reference: a build
	// secret reaches only the build and never becomes an environment variable in
	// the running container.
	Secrets []BuildSecret `toml:"secrets,omitempty"`
}

// BuildSecret exposes a secret to the build. ID is the mount identifier a
// Dockerfile references with `--mount=type=secret,id=<id>`; Backend and Ref
// address the secret the same way a runtime [[env]] reference does. Backend is
// optional and defaults to the built-in "cluster" store when omitted, matching
// the `--backend` CLI flag.
type BuildSecret struct {
	ID      string `json:"id" toml:"id"`
	Backend string `json:"backend,omitempty" toml:"backend,omitempty"`
	Ref     string `json:"ref" toml:"ref"`
}

// buildSecretIDRegexp constrains a build secret's mount id to characters
// BuildKit accepts in `--mount=type=secret,id=<id>`. An id with a slash or space
// would pass a bare non-empty check but fail cryptically mid-build, so it is
// rejected up front with a clear message.
var buildSecretIDRegexp = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// ServiceConcurrencyConfig represents per-service concurrency configuration
type ServiceConcurrencyConfig struct {
	Mode                string `toml:"mode"` // "auto" or "fixed"
	RequestsPerInstance int    `toml:"requests_per_instance"`
	ScaleDownDelay      string `toml:"scale_down_delay"` // e.g. "2m", "15m"
	NumInstances        int    `toml:"num_instances"`
	ShutdownTimeout     string `toml:"shutdown_timeout"` // e.g. "10s", "30s" - time to wait for graceful shutdown
}

// DiskConfig represents a disk attachment for a service.
// Provider defaults to "miren" (network disk) when empty.
// Use provider = "local" for node-local persistent storage.
//
// A SQLite database is not declared here. It comes from the miren-sqlite
// addon, which attaches its own storage; see docs/docs/addons.md.
type DiskConfig struct {
	Name         string `toml:"name"`
	Provider     string `toml:"provider"`
	MountPath    string `toml:"mount_path"`
	ReadOnly     bool   `toml:"read_only"`
	SizeGB       int    `toml:"size_gb"`
	Filesystem   string `toml:"filesystem"`
	LeaseTimeout string `toml:"lease_timeout"`
	Owner        string `toml:"owner"`
}

// Disk providers accepted in app.toml.
const (
	DiskProviderMiren = "miren"
	DiskProviderLocal = "local"
)

// DefaultSqliteDbFile is the database name used when a sqlite disk does not
// set db_file.
const DefaultSqliteDbFile = "data.db"

// DefaultSqliteId is the database identity used when a sqlite disk does not
// set id.
const DefaultSqliteId = "default"

// singleWriterAddons are addons whose storage only one process may write, so a
// service receiving it must run exactly one instance.
var singleWriterAddons = map[string]bool{
	"miren-sqlite": true,
}

// services reports whether an addon's storage reaches a named service. An empty
// list means every service, matching how addon variables reach every service.
func (c *AddonConfig) services() func(string) bool {
	if c == nil || len(c.Services) == 0 {
		return func(string) bool { return true }
	}
	return func(name string) bool {
		for _, s := range c.Services {
			if s == name {
				return true
			}
		}
		return false
	}
}

// PortConfig represents a network port for a service
type PortConfig struct {
	Port     int    `toml:"port"`
	Name     string `toml:"name"`
	Type     string `toml:"type"`
	NodePort int    `toml:"node_port"`
}

const (
	DefaultMetricsPath     = "/metrics"
	DefaultMetricsInterval = "30s"
	MinimumMetricsInterval = 30 * time.Second
)

// ServiceMetricsConfig describes an application's Prometheus-compatible scrape endpoint.
// Metrics are opt-in. When enabled, the runtime resolves Path, Port, and
// Interval before persisting the service configuration in a ConfigVersion.
type ServiceMetricsConfig struct {
	Enabled  bool   `toml:"enabled"`
	Path     string `toml:"path"`
	Port     int    `toml:"port"`
	Interval string `toml:"interval"`
	Public   bool   `toml:"public"`
}

// ServiceConfig represents configuration for a specific service
type ServiceConfig struct {
	Command     string                    `toml:"command"`
	Args        []string                  `toml:"args"`
	Port        int                       `toml:"port"`
	PortName    string                    `toml:"port_name"`
	PortType    string                    `toml:"port_type"`
	Ports       []PortConfig              `toml:"ports"`
	Image       string                    `toml:"image"`
	EnvVars     []AppEnvVar               `toml:"env"`
	Concurrency *ServiceConcurrencyConfig `toml:"concurrency"`
	Disks       []DiskConfig              `toml:"disks"`
	Metrics     *ServiceMetricsConfig     `toml:"metrics,omitempty"`
	// PortTimeout overrides the default 15s wait for the service to bind
	// its port during startup. Accepts a Go duration string (e.g. "60s", "2m").
	// Empty falls back to the default; invalid duration strings are rejected
	// at parse time by Validate.
	PortTimeout string `toml:"port_timeout,omitempty"`
}

// AddonConfig represents configuration for an addon in app.toml.
type AddonConfig struct {
	Variant string `toml:"variant"`
	Version string `toml:"version"`

	// Services names the services an addon's storage attaches to. Empty means
	// every service, matching how addon variables reach every service.
	//
	// It exists because storage an addon supplies can carry constraints that
	// the rest of the app should not have to inherit. A SQLite database allows
	// one writer, so a service holding it must run a single fixed instance;
	// without this an app could not also run a worker at three.
	Services []string `toml:"services"`
}

// Task trigger values. A task's trigger says what starts it; the default is
// TriggerManual, meaning it runs only when someone asks.
const (
	TriggerManual   = "manual"
	TriggerDeploy   = "deploy"
	TriggerSchedule = "schedule"
)

// ConsoleName is the task `miren app run` resolves when none is named. The
// convention predates tasks -- the exec server already looked for a service
// with this name -- so it is shared rather than spelled out in both places.
const ConsoleName = "console"

// DefaultTaskMaxConcurrent caps simultaneous runs of a task. Runs consume
// cluster capacity on request, so the default is the conservative one.
const DefaultTaskMaxConcurrent = 1

// ConsoleMaxConcurrent caps simultaneous console runs when the app has not
// declared [tasks.console].
//
// The conservative default is wrong here. `miren app run` had no limit at all
// before tasks absorbed it, and most apps will never declare the task, so
// falling back to 1 would silently make the second person to open a console
// wait behind the first -- a new restriction on an existing command rather than
// a bound on new functionality. Set well past what anyone reaches by hand; an
// app that wants a different number can declare the task and say so.
const ConsoleMaxConcurrent = 10

// TaskConfig represents a command the app knows how to run: what to run, what
// starts it, and how it ends.
//
// A task deliberately has no ports, concurrency, image, or disks. Those are
// absent from the schema rather than present-and-rejected, so the grammar never
// admits a configuration the validator has to refuse. Per-task image and disks
// are v1 cuts tracked as open questions in RFD-97, not oversights: a per-task
// image raises version-pinning questions RFD-91 is still settling, and a
// per-task disk collides with the single-writer lease model.
type TaskConfig struct {
	// Command is the default command. An invoke can override it, which is what
	// makes a manually-triggered task useful for ad-hoc work.
	Command string `json:"command" toml:"command"`

	// Trigger is one of "manual", "deploy", or "schedule". Empty means manual.
	Trigger string `json:"trigger,omitempty" toml:"trigger,omitempty"`

	// Every is a Go duration and is pure sugar over Schedule: it is desugared
	// to a day-aligned calendar expression at parse time, so only the calendar
	// form is ever stored. Mutually exclusive with Schedule.
	Every string `json:"every,omitempty" toml:"every,omitempty"`

	// Schedule is a systemd OnCalendar expression. Mutually exclusive with Every.
	Schedule string `json:"schedule,omitempty" toml:"schedule,omitempty"`

	// Timeout bounds the run, after which the sandbox is killed and the run is
	// marked TIMED_OUT. Empty means the platform default.
	Timeout string `json:"timeout,omitempty" toml:"timeout,omitempty"`

	// Retries applies to the deploy and schedule triggers, where nobody is
	// watching to retry by hand. A manually-triggered run that fails just
	// fails, and the caller decides.
	Retries int `json:"retries,omitempty" toml:"retries,omitempty"`

	// MaxConcurrent caps simultaneous runs. Zero means DefaultTaskMaxConcurrent.
	MaxConcurrent int `json:"max_concurrent,omitempty" toml:"max_concurrent,omitempty"`

	EnvVars []AppEnvVar `json:"env,omitempty" toml:"env,omitempty"`
}

// ResolvedSchedule returns the calendar expression this task fires on, with
// Every already desugared. It returns "" for a task that isn't scheduled.
//
// Callers can rely on Validate having rejected anything unparseable, so the
// only error here is a programming one.
func (tc *TaskConfig) ResolvedSchedule() (string, error) {
	if tc.Schedule != "" {
		return tc.Schedule, nil
	}
	if tc.Every == "" {
		return "", nil
	}
	d, err := time.ParseDuration(tc.Every)
	if err != nil {
		return "", fmt.Errorf("invalid every %q: %w", tc.Every, err)
	}
	return oncalendar.DesugarEvery(d)
}

// ResolvedTrigger returns the task's trigger, defaulting to manual.
func (tc *TaskConfig) ResolvedTrigger() string {
	if tc.Trigger == "" {
		return TriggerManual
	}
	return tc.Trigger
}

// ResolvedMaxConcurrent returns the task's concurrency cap, defaulting to 1.
func (tc *TaskConfig) ResolvedMaxConcurrent() int {
	if tc.MaxConcurrent <= 0 {
		return DefaultTaskMaxConcurrent
	}
	return tc.MaxConcurrent
}

type AppConfig struct {
	Name         string                    `toml:"name"`
	EnvVars      []AppEnvVar               `toml:"env,omitempty"`
	Concurrency  *int                      `toml:"concurrency,omitempty"`
	Services     map[string]*ServiceConfig `toml:"services,omitempty"`
	Tasks        map[string]*TaskConfig    `toml:"tasks,omitempty"`
	Build        *BuildConfig              `toml:"build,omitempty"`
	Include      []string                  `toml:"include,omitempty"`
	Addons       map[string]*AddonConfig   `toml:"addons,omitempty"`
	Aliases      map[string]string         `toml:"aliases,omitempty"`
	WorkloadRole string                    `toml:"workload_role,omitempty"`

	// Web says whether the app has a long-running web process. It is a pointer
	// because `false` is the zero value and "unset" has to stay distinguishable
	// from "explicitly false": unset preserves the historical behavior of
	// synthesizing a web service from the image entrypoint, while `web = false`
	// is how a task-only app opts out.
	Web *bool `toml:"web,omitempty"`
}

// WantsWeb reports whether a web service may be synthesized for this app, and
// whether the app said so explicitly. An app that has not spoken gets the
// historical default; changing that would silently drain the pool of every
// deployed app that relies on it.
func (ac *AppConfig) WantsWeb() (want bool, explicit bool) {
	if ac == nil || ac.Web == nil {
		return true, false
	}
	return *ac.Web, true
}

const AppConfigPath = ".miren/app.toml"

func LoadAppConfig() (*AppConfig, error) {
	ac, _, err := LoadAppConfigWithPath()
	return ac, err
}

// LoadAppConfigWithPath loads the app config and returns the file path it was loaded from.
// Returns (nil, "", nil) if no config file is found.
func LoadAppConfigWithPath() (*AppConfig, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	for dir != "/" {
		path := filepath.Join(dir, AppConfigPath)
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, "", err
			}
			dir = filepath.Dir(dir)
			continue
		}
		ac, parseErr := decodeAndValidate(data, path)
		if parseErr != nil {
			return nil, "", parseErr
		}
		return ac, path, nil
	}

	return nil, "", nil
}

func LoadAppConfigUnder(dir string) (*AppConfig, error) {
	path := filepath.Join(dir, AppConfigPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return decodeAndValidate(data, path)
}

func Parse(data []byte) (*AppConfig, error) {
	return decodeAndValidate(data, "<input>")
}

// decodeAndValidate decodes TOML data into an AppConfig and validates it,
// enriching any errors with file path, source locations, and suggestions.
func decodeAndValidate(data []byte, filePath string) (*AppConfig, error) {
	var ac AppConfig
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ac); err != nil {
		return nil, enrichDecodeError(filePath, data, err)
	}
	if err := ac.Validate(); err != nil {
		return nil, enrichValidationError(filePath, data, err)
	}
	return &ac, nil
}

// Validate checks that the AppConfig has valid values.
// Returns *ValidationError with a key path for AST-based line resolution.
func (ac *AppConfig) Validate() error {
	// Validate global environment variables
	// Note: empty values are allowed - secrets may be stored server-side
	for i, ev := range ac.EnvVars {
		if ev.Key == "" {
			return &ValidationError{KeyPath: "env", Message: fmt.Sprintf("env[%d]: key is required", i)}
		}
		if err := ev.validateReference(fmt.Sprintf("env[%d]", i)); err != nil {
			return err
		}
	}

	// Validate build secrets
	if ac.Build != nil {
		seen := make(map[string]bool, len(ac.Build.Secrets))
		for i, bs := range ac.Build.Secrets {
			if err := bs.validate(fmt.Sprintf("build.secrets[%d]", i)); err != nil {
				return err
			}
			if seen[bs.ID] {
				return &ValidationError{
					KeyPath: fmt.Sprintf("build.secrets[%d].id", i),
					Message: fmt.Sprintf("build.secrets[%d]: duplicate id %q — each build secret needs a unique mount id", i, bs.ID),
				}
			}
			seen[bs.ID] = true
		}
	}

	// Validate service configurations
	for serviceName, svcConfig := range ac.Services {
		if svcConfig == nil {
			continue
		}

		svcPrefix := "services." + serviceName

		if svcConfig.Args != nil {
			if serviceName == ConsoleName {
				return &ValidationError{
					KeyPath: svcPrefix + ".args",
					Message: "service console: args is not supported on deprecated [services.console]; use [tasks.console] with a command instead",
				}
			}
			if len(svcConfig.Args) == 0 {
				return &ValidationError{
					KeyPath: svcPrefix + ".args",
					Message: fmt.Sprintf("service %s: args must contain at least one argument", serviceName),
				}
			}
			if svcConfig.Command != "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".args",
					Message: fmt.Sprintf("service %s: command and args cannot both be set", serviceName),
				}
			}
		}

		// Validate concurrency if present
		if svcConfig.Concurrency != nil {
			concurrency := svcConfig.Concurrency
			concurrencyPath := svcPrefix + ".concurrency"

			// Validate mode
			if concurrency.Mode != "" && concurrency.Mode != "auto" && concurrency.Mode != "fixed" {
				return &ValidationError{
					KeyPath: concurrencyPath + ".mode",
					Message: fmt.Sprintf("service %s: invalid concurrency mode %q, must be \"auto\" or \"fixed\"", serviceName, concurrency.Mode),
				}
			}

			// Validate auto mode settings
			if concurrency.Mode == "auto" || concurrency.Mode == "" {
				if concurrency.RequestsPerInstance < 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".requests_per_instance",
						Message: fmt.Sprintf("service %s: requests_per_instance must be non-negative", serviceName),
					}
				}
				if concurrency.ScaleDownDelay != "" {
					if _, err := time.ParseDuration(concurrency.ScaleDownDelay); err != nil {
						return &ValidationError{
							KeyPath: concurrencyPath + ".scale_down_delay",
							Message: fmt.Sprintf("service %s: invalid scale_down_delay %q: %v", serviceName, concurrency.ScaleDownDelay, err),
						}
					}
				}
				if concurrency.NumInstances > 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".num_instances",
						Message: fmt.Sprintf("service %s: num_instances cannot be set in auto mode", serviceName),
					}
				}
			}

			// Validate fixed mode settings
			if concurrency.Mode == "fixed" {
				if concurrency.NumInstances <= 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".num_instances",
						Message: fmt.Sprintf("service %s: num_instances must be at least 1 for fixed mode", serviceName),
					}
				}
				if concurrency.RequestsPerInstance > 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".requests_per_instance",
						Message: fmt.Sprintf("service %s: requests_per_instance cannot be set in fixed mode", serviceName),
					}
				}
				if concurrency.ScaleDownDelay != "" {
					return &ValidationError{
						KeyPath: concurrencyPath + ".scale_down_delay",
						Message: fmt.Sprintf("service %s: scale_down_delay cannot be set in fixed mode", serviceName),
					}
				}
			}

			// Validate shutdown_timeout (applies to both modes)
			if concurrency.ShutdownTimeout != "" {
				if _, err := time.ParseDuration(concurrency.ShutdownTimeout); err != nil {
					return &ValidationError{
						KeyPath: concurrencyPath + ".shutdown_timeout",
						Message: fmt.Sprintf("service %s: invalid shutdown_timeout %q: %v", serviceName, concurrency.ShutdownTimeout, err),
					}
				}
			}
		}

		// Validate port_timeout (parsed downstream by resolvePortWaitTimeout;
		// catch typos like "120" missing the unit suffix at deploy time rather
		// than silently falling back to the 15s default).
		if svcConfig.PortTimeout != "" {
			if _, err := time.ParseDuration(svcConfig.PortTimeout); err != nil {
				return &ValidationError{
					KeyPath: svcPrefix + ".port_timeout",
					Message: fmt.Sprintf("service %s: invalid port_timeout %q: %v", serviceName, svcConfig.PortTimeout, err),
				}
			}
		}

		// Validate service environment variables
		// Note: empty values are allowed - secrets may be stored server-side
		for i, ev := range svcConfig.EnvVars {
			if err := ev.validateReference(fmt.Sprintf("%s.env[%d]", svcPrefix, i)); err != nil {
				return err
			}
			if ev.Key == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".env",
					Message: fmt.Sprintf("service %s: env[%d] key is required", serviceName, i),
				}
			}
		}

		// Validate ports configuration
		if len(svcConfig.Ports) > 0 {
			// Mutual exclusion: cannot use both ports[] and scalar port fields
			if svcConfig.Port != 0 || svcConfig.PortName != "" || svcConfig.PortType != "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".ports",
					Message: fmt.Sprintf("service %s: cannot use both 'ports' array and scalar port/port_name/port_type fields", serviceName),
				}
			}

			seenNames := make(map[string]bool)
			type portProto struct {
				port     int
				protocol string
			}
			seenPortProto := make(map[portProto]bool)
			for i, p := range svcConfig.Ports {
				if p.Port <= 0 || p.Port > 65535 {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] port must be between 1 and 65535", serviceName, i),
					}
				}
				if p.Name == "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] name is required", serviceName, i),
					}
				}
				if p.Type != "" && p.Type != "http" && p.Type != "tcp" && p.Type != "udp" {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] type must be \"http\", \"tcp\", or \"udp\"", serviceName, i),
					}
				}
				proto := "tcp"
				if p.Type == "udp" {
					proto = "udp"
				}
				if p.NodePort < 0 || p.NodePort > 65535 {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] node_port must be between 0 and 65535", serviceName, i),
					}
				}
				if seenNames[p.Name] {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] duplicate port name %q", serviceName, i, p.Name),
					}
				}
				seenNames[p.Name] = true
				pp := portProto{p.Port, proto}
				if seenPortProto[pp] {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] duplicate port number %d (protocol %q)", serviceName, i, p.Port, proto),
					}
				}
				seenPortProto[pp] = true
			}
		}

		if metrics := svcConfig.Metrics; metrics != nil {
			metricsPath := svcPrefix + ".metrics"
			if metrics.Path != "" && (!strings.HasPrefix(metrics.Path, "/") || strings.ContainsAny(metrics.Path, "?#")) {
				return &ValidationError{
					KeyPath: metricsPath + ".path",
					Message: fmt.Sprintf("service %s: metrics path must be an absolute URL path without a query or fragment", serviceName),
				}
			}

			if metrics.Interval != "" {
				interval, err := time.ParseDuration(metrics.Interval)
				if err != nil {
					return &ValidationError{
						KeyPath: metricsPath + ".interval",
						Message: fmt.Sprintf("service %s: invalid metrics interval %q: %v", serviceName, metrics.Interval, err),
					}
				}
				if interval < MinimumMetricsInterval {
					return &ValidationError{
						KeyPath: metricsPath + ".interval",
						Message: fmt.Sprintf("service %s: metrics interval must be at least %s", serviceName, MinimumMetricsInterval),
					}
				}
			}

			port := metrics.Port
			if port == 0 {
				port = defaultMetricsPort(serviceName, svcConfig)
			}
			if metrics.Enabled && port == 0 {
				return &ValidationError{
					KeyPath: metricsPath + ".port",
					Message: fmt.Sprintf("service %s: metrics port is required when the service has no HTTP port", serviceName),
				}
			}
			if port != 0 && !serviceHasTCPPort(serviceName, svcConfig, port) {
				return &ValidationError{
					KeyPath: metricsPath + ".port",
					Message: fmt.Sprintf("service %s: metrics port %d must be a declared HTTP or TCP service port", serviceName, port),
				}
			}
		}

		// Validate disk configurations
		//
		// Miren disks are leased exclusively, so a service holding one must run
		// a single fixed instance.
		hasSingleWriterDisks := false
		for i, disk := range svcConfig.Disks {
			switch disk.Provider {
			case "", DiskProviderMiren, DiskProviderLocal:
			default:
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) has invalid provider %q, must be \"miren\" or \"local\"", serviceName, i, disk.Name, disk.Provider),
				}
			}
			if disk.Provider == "" || disk.Provider == DiskProviderMiren {
				hasSingleWriterDisks = true
			}
			if disk.Name == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] must have a name", serviceName, i),
				}
			}
			if disk.MountPath == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) must have a mount_path", serviceName, i, disk.Name),
				}
			}
			if !filepath.IsAbs(disk.MountPath) {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) mount_path must be an absolute path", serviceName, i, disk.Name),
				}
			}
			if disk.Provider == DiskProviderLocal {
				if disk.SizeGB != 0 {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) size_gb is not supported for %s disks", serviceName, i, disk.Name, disk.Provider),
					}
				}
				if disk.Filesystem != "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) filesystem is not supported for %s disks", serviceName, i, disk.Name, disk.Provider),
					}
				}
				if disk.LeaseTimeout != "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) lease_timeout is not supported for %s disks", serviceName, i, disk.Name, disk.Provider),
					}
				}
			} else {
				if disk.Filesystem != "" && disk.Filesystem != "ext4" && disk.Filesystem != "xfs" && disk.Filesystem != "btrfs" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) has invalid filesystem %q, must be ext4, xfs, or btrfs", serviceName, i, disk.Name, disk.Filesystem),
					}
				}
				if disk.SizeGB < 0 {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) size_gb must be non-negative", serviceName, i, disk.Name),
					}
				}
				if disk.LeaseTimeout != "" {
					if _, err := time.ParseDuration(disk.LeaseTimeout); err != nil {
						return &ValidationError{
							KeyPath: svcPrefix + ".disks",
							Message: fmt.Sprintf("service %s: disk[%d] (%s) invalid lease_timeout %q: %v", serviceName, i, disk.Name, disk.LeaseTimeout, err),
						}
					}
				}
			}
		}

		// Miren disks require fixed concurrency with a single instance
		if hasSingleWriterDisks {
			if svcConfig.Concurrency == nil || svcConfig.Concurrency.Mode != "fixed" {
				return &ValidationError{
					KeyPath: svcPrefix + ".concurrency",
					Message: fmt.Sprintf("service %s: miren disks can only be attached to services with fixed concurrency mode", serviceName),
				}
			}
			if svcConfig.Concurrency.NumInstances != 1 {
				return &ValidationError{
					KeyPath: svcPrefix + ".concurrency.num_instances",
					Message: fmt.Sprintf("service %s: miren disks can only be attached to services with fixed concurrency mode and num_instances=1", serviceName),
				}
			}
		}
	}

	if err := ac.validateTasks(); err != nil {
		return err
	}

	// An addon that supplies single-writer storage constrains the services it
	// attaches to. Catching it here means a deploy fails with the reason rather
	// than succeeding and leaving the app with no database, which is what
	// happens further down: appspec skips a disk on a service that can scale.
	//
	// The set is named here rather than read from the addon registry because
	// app.toml is parsed client-side, where no registry exists. It is short and
	// changes rarely; a provider joining it needs a line here too.
	for addonName, cfg := range ac.Addons {
		if !singleWriterAddons[addonName] {
			continue
		}

		targets := cfg.services()
		for serviceName, svcConfig := range ac.Services {
			if svcConfig == nil || !targets(serviceName) {
				continue
			}
			c := svcConfig.Concurrency
			if c == nil || c.Mode != "fixed" || c.NumInstances != 1 {
				return &ValidationError{
					KeyPath: "services." + serviceName + ".concurrency",
					Message: fmt.Sprintf(
						"service %s: addon %s supplies a database that allows one writer, so the service must set mode = \"fixed\" with num_instances = 1, or the addon must name other services with services = [...]",
						serviceName, addonName),
				}
			}
		}
	}

	for name, target := range ac.Aliases {
		words := strings.Fields(name)
		if len(words) == 0 {
			return &ValidationError{
				KeyPath: "aliases",
				Message: fmt.Sprintf("alias %q: name must not be empty", name),
			}
		}
		for _, word := range words {
			if !aliasWordRegexp.MatchString(word) {
				return &ValidationError{
					KeyPath: "aliases." + name,
					Message: fmt.Sprintf("alias %q: each word must start with a lowercase letter and contain only lowercase letters, numbers, dashes, and underscores", name),
				}
			}
		}
		if strings.TrimSpace(target) == "" {
			return &ValidationError{
				KeyPath: "aliases." + name,
				Message: fmt.Sprintf("alias %q: command must not be empty", name),
			}
		}
	}

	return nil
}

// validateTasks checks every [tasks.<name>] block.
func (ac *AppConfig) validateTasks() error {
	for taskName, task := range ac.Tasks {
		if task == nil {
			continue
		}

		prefix := "tasks." + taskName

		if task.Command == "" {
			return &ValidationError{
				KeyPath: prefix + ".command",
				Message: fmt.Sprintf("task %s: command is required", taskName),
			}
		}

		trigger := task.ResolvedTrigger()
		switch trigger {
		case TriggerManual, TriggerDeploy, TriggerSchedule:
		default:
			return &ValidationError{
				KeyPath: prefix + ".trigger",
				Message: fmt.Sprintf("task %s: invalid trigger %q, must be %q, %q, or %q",
					taskName, task.Trigger, TriggerManual, TriggerDeploy, TriggerSchedule),
			}
		}

		// every and schedule are two spellings of one mechanism, so setting
		// both is ambiguous rather than additive.
		if task.Every != "" && task.Schedule != "" {
			return &ValidationError{
				KeyPath: prefix + ".every",
				Message: fmt.Sprintf("task %s: cannot set both 'every' and 'schedule'; they are two spellings of the same thing", taskName),
			}
		}

		if trigger == TriggerSchedule {
			if task.Every == "" && task.Schedule == "" {
				return &ValidationError{
					KeyPath: prefix + ".trigger",
					Message: fmt.Sprintf("task %s: trigger = %q requires either 'every' (e.g. \"6h\") or 'schedule' (e.g. \"Mon *-*-* 09:00:00\")", taskName, TriggerSchedule),
				}
			}
		} else if task.Every != "" || task.Schedule != "" {
			field, set := "every", task.Every
			if task.Schedule != "" {
				field, set = "schedule", task.Schedule
			}
			return &ValidationError{
				KeyPath: prefix + "." + field,
				Message: fmt.Sprintf("task %s: %s = %q has no effect without trigger = %q", taskName, field, set, TriggerSchedule),
			}
		}

		if task.Every != "" {
			d, err := time.ParseDuration(task.Every)
			if err != nil {
				return &ValidationError{
					KeyPath: prefix + ".every",
					Message: fmt.Sprintf("task %s: invalid every %q: %v", taskName, task.Every, err),
				}
			}
			// Desugaring is where the day-tiling rule is enforced, so failures
			// surface here rather than at schedule time.
			if _, err := oncalendar.DesugarEvery(d); err != nil {
				return &ValidationError{
					KeyPath: prefix + ".every",
					Message: fmt.Sprintf("task %s: %v", taskName, err),
				}
			}
		}

		if task.Schedule != "" {
			if _, err := oncalendar.Parse(task.Schedule); err != nil {
				return &ValidationError{
					KeyPath: prefix + ".schedule",
					Message: fmt.Sprintf("task %s: %v", taskName, err),
				}
			}
		}

		if task.Timeout != "" {
			d, err := time.ParseDuration(task.Timeout)
			if err != nil {
				return &ValidationError{
					KeyPath: prefix + ".timeout",
					Message: fmt.Sprintf("task %s: invalid timeout %q: %v", taskName, task.Timeout, err),
				}
			}
			// Zero is the documented way to say "unbounded"; negative is a typo.
			if d < 0 {
				return &ValidationError{
					KeyPath: prefix + ".timeout",
					Message: fmt.Sprintf("task %s: timeout must not be negative (use 0 for unbounded)", taskName),
				}
			}
		}

		if task.Retries < 0 {
			return &ValidationError{
				KeyPath: prefix + ".retries",
				Message: fmt.Sprintf("task %s: retries must be non-negative", taskName),
			}
		}

		// Retries exist for triggers nobody is watching. A manual run that
		// fails just fails, and the caller decides what to do about it.
		if task.Retries > 0 && trigger == TriggerManual {
			return &ValidationError{
				KeyPath: prefix + ".retries",
				Message: fmt.Sprintf("task %s: retries has no effect on a manually-triggered task; the caller decides whether to retry", taskName),
			}
		}

		if task.MaxConcurrent < 0 {
			return &ValidationError{
				KeyPath: prefix + ".max_concurrent",
				Message: fmt.Sprintf("task %s: max_concurrent must be at least 1", taskName),
			}
		}

		for i, ev := range task.EnvVars {
			if ev.Key == "" {
				return &ValidationError{
					KeyPath: prefix + ".env",
					Message: fmt.Sprintf("task %s: env[%d] key is required", taskName, i),
				}
			}
		}
	}

	return nil
}

// ResolveDefaults populates Services map for all service names with fully-resolved defaults.
// If a service already has explicit config in app.toml, it is preserved.
// Otherwise, defaults are applied based on service name:
//   - "web": auto mode, requests_per_instance=10, scale_down_delay=15m
//   - others: fixed mode, num_instances=1
func (ac *AppConfig) ResolveDefaults(services []string) {
	if ac.Services == nil {
		ac.Services = make(map[string]*ServiceConfig)
	}

	for _, serviceName := range services {
		// Skip if service already has config
		if _, exists := ac.Services[serviceName]; exists {
			continue
		}

		// Apply defaults based on service name
		if serviceName == "web" {
			ac.Services[serviceName] = &ServiceConfig{
				Concurrency: &ServiceConcurrencyConfig{
					Mode:                "auto",
					RequestsPerInstance: 10,
					ScaleDownDelay:      "15m",
					ShutdownTimeout:     "10s",
				},
			}
		} else {
			ac.Services[serviceName] = &ServiceConfig{
				Concurrency: &ServiceConcurrencyConfig{
					Mode:            "fixed",
					NumInstances:    1,
					ShutdownTimeout: "10s",
				},
			}
		}
	}

	for _, serviceName := range services {
		svc := ac.Services[serviceName]
		if svc == nil || svc.Metrics == nil {
			continue
		}
		if svc.Metrics.Path == "" {
			svc.Metrics.Path = DefaultMetricsPath
		}
		if svc.Metrics.Interval == "" {
			svc.Metrics.Interval = DefaultMetricsInterval
		}
		if svc.Metrics.Port == 0 {
			svc.Metrics.Port = defaultMetricsPort(serviceName, svc)
		}
	}
}

func defaultMetricsPort(serviceName string, svc *ServiceConfig) int {
	if len(svc.Ports) > 0 {
		for _, port := range svc.Ports {
			if port.Type == "" || port.Type == "http" {
				return port.Port
			}
		}
		return 0
	}

	if svc.Port > 0 && (svc.PortType == "" || svc.PortType == "http") {
		return svc.Port
	}
	if svc.Port > 0 {
		return 0
	}
	if serviceName == "web" {
		return 3000
	}
	return 0
}

func serviceHasTCPPort(serviceName string, svc *ServiceConfig, want int) bool {
	if len(svc.Ports) > 0 {
		for _, port := range svc.Ports {
			if port.Port == want && port.Type != "udp" {
				return true
			}
		}
		return false
	}
	if svc.Port > 0 {
		return svc.Port == want && svc.PortType != "udp"
	}
	return serviceName == "web" && want == 3000
}

// GetDefaultsForServices returns an AppConfig with defaults resolved for given service names.
// This is useful for migration - it provides the same defaults used at build time.
func GetDefaultsForServices(serviceNames []string) *AppConfig {
	ac := &AppConfig{}
	ac.ResolveDefaults(serviceNames)
	return ac
}

// AliasLineNumbers parses the TOML file at configPath and returns a map from
// alias name to the line number where it is defined. Uses the go-toml/v2 AST
// parser for accurate source locations.
func AliasLineNumbers(configPath string) map[string]int {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var p tomlast.Parser
	p.Reset(data)

	result := make(map[string]int)

	for p.NextExpression() {
		node := p.Expression()
		if node.Kind != tomlast.Table {
			continue
		}

		// Check if this is the [aliases] table
		keyIter := node.Key()
		if !keyIter.Next() {
			continue
		}
		if string(keyIter.Node().Data) != "aliases" {
			continue
		}

		// Iterate subsequent KeyValue expressions under [aliases]
		for p.NextExpression() {
			kv := p.Expression()
			if kv.Kind != tomlast.KeyValue {
				break
			}
			keyIter := kv.Key()
			if !keyIter.Next() {
				continue
			}
			keyNode := keyIter.Node()
			name := string(keyNode.Data)
			shape := p.Shape(keyNode.Raw)
			result[name] = shape.Start.Line
		}
		break
	}

	return result
}

// validateReference checks the secret-reference form of an env var. A reference
// replaces the value rather than accompanying it, so accepting both would leave
// it ambiguous which one is meant to reach the app — and the wrong guess ships a
// literal path where a credential belongs.
func (ev AppEnvVar) validateReference(keyPath string) error {
	switch {
	case ev.Ref == "" && ev.Backend == "":
		return nil
	case ev.Ref == "":
		return &ValidationError{
			KeyPath: keyPath + ".ref",
			Message: fmt.Sprintf("%s: backend %q needs a ref naming the secret", keyPath, ev.Backend),
		}
	case ev.Backend == "":
		return &ValidationError{
			KeyPath: keyPath + ".backend",
			Message: fmt.Sprintf("%s: ref %q needs a backend to resolve it against", keyPath, ev.Ref),
		}
	case ev.Value != "":
		return &ValidationError{
			KeyPath: keyPath + ".value",
			Message: fmt.Sprintf("%s: set either value or ref, not both — a referenced secret gets its value from the backend", keyPath),
		}
	}
	return nil
}

// validate checks a build secret. A build secret has no inline form, so id and
// ref are always required. Backend is optional and defaults to the "cluster"
// store at resolve time, so it is not checked here.
func (bs BuildSecret) validate(keyPath string) error {
	switch {
	case bs.ID == "":
		return &ValidationError{
			KeyPath: keyPath + ".id",
			Message: fmt.Sprintf("%s: id is required — it is the mount identifier a Dockerfile references with --mount=type=secret,id=<id>", keyPath),
		}
	case !buildSecretIDRegexp.MatchString(bs.ID):
		return &ValidationError{
			KeyPath: keyPath + ".id",
			Message: fmt.Sprintf("%s: id %q must contain only letters, digits, and _.- — BuildKit rejects other characters in a secret mount id", keyPath, bs.ID),
		}
	case bs.Ref == "":
		return &ValidationError{
			KeyPath: keyPath + ".ref",
			Message: fmt.Sprintf("%s: ref is required to name the secret within the backend", keyPath),
		}
	}
	return nil
}
