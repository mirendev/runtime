package commands

import (
	"miren.dev/mflags"
	"miren.dev/runtime/pkg/labs"
)

func RegisterAll(d *mflags.Dispatcher) {
	// Core commands
	d.Dispatch("version", Infer("version", "Print the version", Version,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Print version",
			Body: "miren version",
		}),
		WithExample(mflags.Example{
			Name: "JSON output",
			Body: "miren version --format json",
		}),
	))
	d.Dispatch("login", Infer("login", "Authenticate with miren.cloud", Login,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Login",
			Body: "miren login",
		}),
		WithExample(mflags.Example{
			Name: "Login to a specific cloud instance",
			Body: "miren login --url https://cloud.example.com",
		}),
	))
	d.Dispatch("logout", Infer("logout", "Remove local authentication credentials", Logout,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Logout",
			Body: "miren logout",
		}),
	))
	d.Dispatch("whoami", Infer("whoami", "Display information about the current authenticated user", Whoami,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Show current user",
			Body: "miren whoami",
		}),
		WithExample(mflags.Example{
			Name: "JSON output",
			Body: "miren whoami --json",
		}),
	))

	// Doctor commands
	d.Dispatch("doctor", Infer("doctor", "Diagnose miren environment and connectivity", Doctor,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Run all diagnostics",
			Body: "miren doctor",
		}),
	))
	d.Dispatch("doctor config", Infer("doctor config", "Check configuration files", DoctorConfig,
		WithExample(mflags.Example{
			Name: "Check config files",
			Body: "miren doctor config",
		}),
	))
	d.Dispatch("doctor server", Infer("doctor server", "Check server health and connectivity", DoctorServer,
		WithExample(mflags.Example{
			Name: "Check server connectivity",
			Body: "miren doctor server",
		}),
	))
	d.Dispatch("doctor auth", Infer("doctor auth", "Check authentication and user information", DoctorAuth,
		WithExample(mflags.Example{
			Name: "Check authentication",
			Body: "miren doctor auth",
		}),
	))

	// App lifecycle commands
	d.Dispatch("init", Infer("init", "Initialize a new application", Init,
		WithGroup(GroupGettingStarted),
		WithExample(mflags.Example{
			Name: "Initialize in current directory",
			Body: "miren init",
		}),
		WithExample(mflags.Example{
			Name: "Initialize with a specific name",
			Body: "miren init --name myapp",
		}),
	))
	d.Dispatch("deploy", Infer("deploy", "Deploy an application", Deploy,
		WithGroup(GroupGettingStarted),
		WithExample(mflags.Example{
			Name: "Basic",
			Body: "miren deploy",
		}),
		WithExample(mflags.Example{
			Name: "Analyze",
			Body: `Before deploying, the system can tell you how it's going
to treat your application by running:

miren deploy --analyze
`,
		}),
		WithExample(mflags.Example{
			Name: "Set environment variables during deploy",
			Body: "miren deploy -e DATABASE_URL=postgres://localhost/mydb",
		}),
		WithExample(mflags.Example{
			Name: "Deploy a previously built version",
			Body: "miren deploy --version v3",
		}),
	))
	d.Dispatch("deploy cancel", Infer("deploy cancel", "Cancel an in-progress deployment", DeployCancel,
		WithExample(mflags.Example{
			Name: "Cancel the current deployment",
			Body: "miren deploy cancel",
		}),
		WithExample(mflags.Example{
			Name: "Cancel a specific deployment",
			Body: "miren deploy cancel -d dep_abc123",
		}),
	))
	d.Dispatch("rollback", Infer("rollback", "Roll back to a previous version", Rollback,
		WithGroup(GroupGettingStarted),
		WithExample(mflags.Example{
			Name: "Rollback the app in the current directory",
			Body: "miren rollback",
		}),
		WithExample(mflags.Example{
			Name: "Rollback a specific app",
			Body: "miren rollback -a myapp",
		}),
	))
	d.Dispatch("logs", Infer("logs", "View logs (defaults to app logs)", LogsApp,
		WithGroup(GroupMonitoring),
		WithDescription(logsDescription),
		WithExample(mflags.Example{
			Name: "View logs for the current app",
			Body: "miren logs",
		}),
		WithExample(mflags.Example{
			Name: "Follow logs in real time",
			Body: "miren logs -f",
		}),
		WithExample(mflags.Example{
			Name: "Show logs from the last 5 minutes, filtered for errors",
			Body: "miren logs --last 5m -g error",
		}),
	))
	d.Dispatch("logs app", Infer("logs app", "View application logs", LogsApp,
		WithDescription(logsDescription),
		WithExample(mflags.Example{
			Name: "View logs for the current app",
			Body: "miren logs app",
		}),
		WithExample(mflags.Example{
			Name: "Follow logs for a specific app",
			Body: "miren logs app -a myapp -f",
		}),
		WithExample(mflags.Example{
			Name: "Filter logs by service",
			Body: "miren logs app --service web -f",
		}),
	))
	d.Dispatch("logs sandbox", Infer("logs sandbox", "View sandbox logs", LogsSandbox,
		WithExample(mflags.Example{
			Name: "View logs for a sandbox",
			Body: "miren logs sandbox sb_abc123",
		}),
		WithExample(mflags.Example{
			Name: "Follow sandbox logs",
			Body: "miren logs sandbox sb_abc123 -f",
		}),
	))
	d.Dispatch("logs build", Infer("logs build", "View build logs", LogsBuild,
		WithExample(mflags.Example{
			Name: "View build logs for the currently deployed version",
			Body: "miren logs build",
		}),
		WithExample(mflags.Example{
			Name: "View build logs for a specific version",
			Body: "miren logs build myapp-v1a2b3c4",
		}),
		WithExample(mflags.Example{
			Name: "Follow build logs in real time during a deploy",
			Body: "miren logs build -f",
		}),
	))
	d.Dispatch("logs system", Infer("logs system", "View system logs", LogsSystem,
		WithExample(mflags.Example{
			Name: "View all system logs",
			Body: "miren logs system",
		}),
		WithExample(mflags.Example{
			Name: "View logs for a specific component",
			Body: "miren logs system etcd",
		}),
		WithExample(mflags.Example{
			Name: "Follow system logs",
			Body: "miren logs system -f",
		}),
	))

	// App management commands
	d.Dispatch("app", Infer("app", "Get information about an application", App,
		WithGroup(GroupMonitoring),
		WithExample(mflags.Example{
			Name: "Show app info for the current directory",
			Body: "miren app",
		}),
		WithExample(mflags.Example{
			Name: "Show info for a specific app",
			Body: "miren app -a myapp",
		}),
		WithExample(mflags.Example{
			Name: "Watch app stats in real time",
			Body: "miren app --watch",
		}),
		WithExample(mflags.Example{
			Name: "Show only the app configuration",
			Body: "miren app --config-only",
		}),
	))
	d.Dispatch("app list", Infer("app list", "List all applications", AppList,
		WithExample(mflags.Example{
			Name: "List all apps",
			Body: "miren app list",
		}),
		WithExample(mflags.Example{
			Name: "List apps as JSON",
			Body: "miren app list --format json",
		}),
	))
	d.Dispatch("app status", Infer("app status", "Show current status of an application", AppStatus,
		WithExample(mflags.Example{
			Name: "Show status for the current app",
			Body: "miren app status",
		}),
		WithExample(mflags.Example{
			Name: "Show status for a specific app",
			Body: "miren app status -a myapp",
		}),
	))
	d.Dispatch("app history", Infer("app history", "Show deployment history for an application", AppHistory,
		WithExample(mflags.Example{
			Name: "Show deployment history",
			Body: "miren app history",
		}),
		WithExample(mflags.Example{
			Name: "Show detailed history with git info",
			Body: "miren app history --detailed",
		}),
		WithExample(mflags.Example{
			Name: "Show only active deployments, limited to 5",
			Body: "miren app history --status active --limit 5",
		}),
	))
	d.Dispatch("app versions", Infer("app versions", "List app versions with status", AppVersions,
		WithExample(mflags.Example{
			Name: "List all versions",
			Body: "miren app versions",
		}),
		WithExample(mflags.Example{
			Name: "List only ephemeral versions",
			Body: "miren app versions --ephemeral",
		}),
	))
	d.Dispatch("app restart", Infer("app restart", "Restart an application", AppRestart,
		WithExample(mflags.Example{
			Name: "Restart the current app",
			Body: "miren app restart",
		}),
		WithExample(mflags.Example{
			Name: "Restart a specific service",
			Body: "miren app restart -s web",
		}),
	))
	d.Dispatch("app delete", Infer("app delete", "Delete an application and all its resources", AppDelete,
		WithExample(mflags.Example{
			Name: "Delete an app (with confirmation prompt)",
			Body: "miren app delete myapp",
		}),
		WithExample(mflags.Example{
			Name: "Delete without confirmation",
			Body: "miren app delete myapp --force",
		}),
	))
	d.Dispatch("app run", Infer("app run", "Open interactive shell in a new sandbox", AppRun,
		WithDescription(appRunDescription),
		WithExample(mflags.Example{
			Name: "Open a shell in your app's environment",
			Body: "miren app run",
		}),
		WithExample(mflags.Example{
			Name: "Run a specific command",
			Body: "miren app run -- bin/rails console",
		}),
		WithExample(mflags.Example{
			Name: "Run database migrations",
			Body: "miren app run -- bin/rails db:migrate",
		}),
	))
	d.Dispatch("apps", Infer("apps", "List all applications (alias for 'app list')", AppList,
		WithGroup(GroupMonitoring),
		WithExample(mflags.Example{
			Name: "List all apps",
			Body: "miren apps",
		}),
	))

	// Sandbox commands
	d.Dispatch("sandbox", Section("sandbox", "Sandbox management commands", "", WithSectionDescription(sandboxSectionDescription), WithSectionGroup(GroupMonitoring)))
	d.Dispatch("sandbox list", Infer("sandbox list", "List sandboxes (excludes dead by default)", SandboxList,
		WithExample(mflags.Example{
			Name: "List running sandboxes",
			Body: "miren sandbox list",
		}),
		WithExample(mflags.Example{
			Name: "Include dead sandboxes",
			Body: "miren sandbox list --all",
		}),
		WithExample(mflags.Example{
			Name: "List as JSON",
			Body: "miren sandbox list --format json",
		}),
	))
	d.Dispatch("sandbox stop", Infer("sandbox stop", "Stop a sandbox", SandboxStop,
		WithExample(mflags.Example{
			Name: "Stop a sandbox by ID",
			Body: "miren sandbox stop sb_abc123",
		}),
	))
	d.Dispatch("sandbox delete", Infer("sandbox delete", "Delete a dead sandbox", SandboxDelete,
		WithExample(mflags.Example{
			Name: "Delete a sandbox",
			Body: "miren sandbox delete sb_abc123",
		}),
		WithExample(mflags.Example{
			Name: "Force delete without confirmation",
			Body: "miren sandbox delete sb_abc123 --force",
		}),
	))
	d.Dispatch("sandbox exec", Infer("sandbox exec", "Open interactive shell in an existing sandbox", SandboxExec,
		WithDescription(sandboxExecDescription),
		WithExample(mflags.Example{
			Name: "Open a shell in a running sandbox",
			Body: "miren sandbox exec sb_abc123",
		}),
		WithExample(mflags.Example{
			Name: "Run a command in a sandbox",
			Body: "miren sandbox exec sb_abc123 -- ls -la /app",
		}),
	))

	// Sandbox pool commands
	d.Dispatch("sandbox-pool", Section("sandbox-pool", "Sandbox pool management commands", "", WithSectionGroup(GroupMonitoring)))
	d.Dispatch("sandbox-pool list", Infer("sandbox-pool list", "List all sandbox pools", SandboxPoolList,
		WithExample(mflags.Example{
			Name: "List all pools",
			Body: "miren sandbox-pool list",
		}),
	))
	d.Dispatch("sandbox-pool set-desired", Infer("sandbox-pool set-desired", "Set desired instance count for a sandbox pool", SandboxPoolSetDesired,
		WithExample(mflags.Example{
			Name: "Scale a pool to 3 instances",
			Body: "miren sandbox-pool set-desired web 3",
		}),
	))

	// Environment variable commands
	d.Dispatch("env", Section("env", "Environment variable management commands", "", WithSectionGroup(GroupConfiguring)))
	d.Dispatch("env set", Infer("env set", "Set environment variables for an application", EnvSet,
		WithExample(mflags.Example{
			Name: "Set an environment variable",
			Body: "miren env set -e DATABASE_URL=postgres://localhost/mydb",
		}),
		WithExample(mflags.Example{
			Name: "Set a sensitive variable (prompted with masking)",
			Body: "miren env set -s SECRET_KEY",
		}),
		WithExample(mflags.Example{
			Name: "Set a variable from a file",
			Body: "miren env set -e CONFIG=@config.json",
		}),
		WithExample(mflags.Example{
			Name: "Set a variable for a specific service",
			Body: "miren env set -e WORKERS=4 --service worker",
		}),
	))
	d.Dispatch("env get", Infer("env get", "Get an environment variable value", EnvGet,
		WithExample(mflags.Example{
			Name: "Get a variable value",
			Body: "miren env get DATABASE_URL",
		}),
		WithExample(mflags.Example{
			Name: "Reveal a sensitive variable",
			Body: "miren env get SECRET_KEY --unmask",
		}),
	))
	d.Dispatch("env list", Infer("env list", "List all environment variables", EnvList,
		WithExample(mflags.Example{
			Name: "List all variables",
			Body: "miren env list",
		}),
		WithExample(mflags.Example{
			Name: "List as JSON",
			Body: "miren env list --format json",
		}),
	))
	d.Dispatch("env delete", Infer("env delete", "Delete environment variables", EnvDelete,
		WithExample(mflags.Example{
			Name: "Delete a variable",
			Body: "miren env delete DATABASE_URL",
		}),
		WithExample(mflags.Example{
			Name: "Delete without confirmation",
			Body: "miren env delete DATABASE_URL --force",
		}),
		WithExample(mflags.Example{
			Name: "Delete a service-specific variable",
			Body: "miren env delete WORKERS --service worker",
		}),
	))

	// Addon commands
	d.Dispatch("addon", Section("addon", "Addon management commands", "", WithSectionGroup(GroupConfiguring)))
	d.Dispatch("addon list-available", Infer("addon list-available", "List available addons", AddonListAvailable,
		WithExample(mflags.Example{
			Name: "List available addons",
			Body: "miren addon list-available",
		}),
	))
	d.Dispatch("addon variants", Infer("addon variants", "Show variants for an addon", AddonVariants,
		WithExample(mflags.Example{
			Name: "Show variants for PostgreSQL",
			Body: "miren addon variants miren-postgresql",
		}),
	))
	d.Dispatch("addon create", Infer("addon create", "Attach an addon to an application", AddonCreate,
		WithExample(mflags.Example{
			Name: "Attach a PostgreSQL addon",
			Body: "miren addon create miren-postgresql:small",
		}),
		WithExample(mflags.Example{
			Name: "Attach a PostgreSQL addon with a specific version",
			Body: "miren addon create miren-postgresql:small --version 16",
		}),
	))
	d.Dispatch("addon list", Infer("addon list", "List addons attached to an application", AddonList,
		WithExample(mflags.Example{
			Name: "List addons for the current app",
			Body: "miren addon list",
		}),
	))
	d.Dispatch("addon destroy", Infer("addon destroy", "Remove an addon from an application", AddonDestroy,
		WithExample(mflags.Example{
			Name: "Remove an addon",
			Body: "miren addon destroy miren-postgresql",
		}),
		WithExample(mflags.Example{
			Name: "Remove without confirmation",
			Body: "miren addon destroy miren-postgresql --force",
		}),
	))

	// Route commands
	d.Dispatch("route", Infer("route", "List all HTTP routes", Route,
		WithGroup(GroupConfiguring),
		WithExample(mflags.Example{
			Name: "List all routes",
			Body: "miren route",
		}),
	))
	d.Dispatch("route list", Infer("route list", "List all HTTP routes", RouteList,
		WithExample(mflags.Example{
			Name: "List all routes",
			Body: "miren route list",
		}),
		WithExample(mflags.Example{
			Name: "List as JSON",
			Body: "miren route list --format json",
		}),
	))
	d.Dispatch("route set", Infer("route set", "Create or update an HTTP route", RouteSet,
		WithExample(mflags.Example{
			Name: "Route a domain to an app",
			Body: "miren route set example.com myapp",
		}),
	))
	d.Dispatch("route remove", Infer("route remove", "Remove an HTTP route", RouteRemove,
		WithExample(mflags.Example{
			Name: "Remove a route",
			Body: "miren route remove example.com",
		}),
	))
	d.Dispatch("route show", Infer("route show", "Show details of an HTTP route", RouteShow,
		WithExample(mflags.Example{
			Name: "Show route details",
			Body: "miren route show example.com",
		}),
	))
	d.Dispatch("route set-default", Infer("route set-default", "Set an app as the default route", RouteSetDefault,
		WithExample(mflags.Example{
			Name: "Set the default route",
			Body: "miren route set-default myapp",
		}),
	))
	d.Dispatch("route unset-default", Infer("route unset-default", "Remove the default route", RouteUnsetDefault,
		WithExample(mflags.Example{
			Name: "Remove the default route",
			Body: "miren route unset-default",
		}),
	))

	d.Dispatch("route protect", Infer("route protect", "Protect an HTTP route with an identity provider", RouteProtect,
		WithExample(mflags.Example{
			Name: "Protect a route with an OIDC provider",
			Body: "miren route protect example.com --provider my-google-oidc --claim-header email:X-User-Email",
		}),
		WithExample(mflags.Example{
			Name: "Protect a route with a password",
			Body: "miren route protect example.com --provider my-pw",
		}),
		WithExample(mflags.Example{
			Name: "Protect the default route",
			Body: "miren route protect --default --provider my-google-oidc",
		}),
	))
	d.Dispatch("route unprotect", Infer("route unprotect", "Remove identity-provider protection from an HTTP route", RouteUnprotect,
		WithExample(mflags.Example{
			Name: "Remove protection from a route",
			Body: "miren route unprotect example.com",
		}),
	))

	d.Dispatch("route waf", Infer("route waf", "Manage WAF protection on an HTTP route", RouteWaf,
		WithExample(mflags.Example{
			Name: "Enable WAF on a route with default paranoia level",
			Body: "miren route waf example.com",
		}),
		WithExample(mflags.Example{
			Name: "Enable WAF with a specific paranoia level",
			Body: "miren route waf example.com --level 2",
		}),
		WithExample(mflags.Example{
			Name: "Enable WAF on the default route",
			Body: "miren route waf --default",
		}),
		WithExample(mflags.Example{
			Name: "Disable WAF on a route",
			Body: "miren route waf example.com --disable",
		}),
	))

	d.Dispatch("route protect", Infer("route protect", "Protect an HTTP route with an identity provider", RouteProtect,
		WithExample(mflags.Example{
			Name: "Protect a route with an identity provider",
			Body: "miren route protect example.com --provider my-google-oidc --claim-header email:X-User-Email",
		}),
		WithExample(mflags.Example{
			Name: "Protect the default route",
			Body: "miren route protect --default --provider my-google-oidc",
		}),
	))
	d.Dispatch("route unprotect", Infer("route unprotect", "Remove identity-provider protection from an HTTP route", RouteUnprotect,
		WithExample(mflags.Example{
			Name: "Remove protection from a route",
			Body: "miren route unprotect example.com",
		}),
	))

	// Config commands
	d.Dispatch("config", Section("config", "Configuration file management", "", WithSectionGroup(GroupClient)))
	d.Dispatch("config info", Infer("config info", "Show configuration file locations and format", ConfigInfo,
		WithExample(mflags.Example{
			Name: "Show config info",
			Body: "miren config info",
		}),
	))
	d.Dispatch("config load", Infer("config load", "Load config and merge it with your current config", ConfigLoad,
		WithExample(mflags.Example{
			Name: "Load a config file",
			Body: "miren config load --input cluster-config.yaml",
		}),
		WithExample(mflags.Example{
			Name: "Load and set as active cluster",
			Body: "miren config load --input cluster-config.yaml --set-active",
		}),
	))

	// Cluster commands
	d.Dispatch("cluster", Infer("cluster", "List configured clusters", Cluster,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "List clusters",
			Body: "miren cluster",
		}),
	))
	d.Dispatch("cluster list", Infer("cluster list", "List all configured clusters", ClusterList,
		WithExample(mflags.Example{
			Name: "List all clusters",
			Body: "miren cluster list",
		}),
		WithExample(mflags.Example{
			Name: "List as JSON",
			Body: "miren cluster list --format json",
		}),
	))
	d.Dispatch("cluster switch", Infer("cluster switch", "Switch to a different cluster", ClusterSwitch,
		WithExample(mflags.Example{
			Name: "Switch to a cluster",
			Body: "miren cluster switch production",
		}),
	))
	d.Dispatch("cluster add", Infer("cluster add", "Add a new cluster configuration", ClusterAdd,
		WithExample(mflags.Example{
			Name: "Add a cluster interactively",
			Body: "miren cluster add",
		}),
		WithExample(mflags.Example{
			Name: "Add a cluster with a specific address",
			Body: "miren cluster add --cluster my-cluster --address 10.0.0.1:8443",
		}),
	))
	d.Dispatch("cluster remove", Infer("cluster remove", "Remove a cluster from the configuration", ClusterRemove,
		WithExample(mflags.Example{
			Name: "Remove a cluster",
			Body: "miren cluster remove my-cluster",
		}),
	))
	d.Dispatch("cluster current", Infer("cluster current", "Show the pinned cluster for this app", ClusterCurrent,
		WithExample(mflags.Example{
			Name: "Show current cluster",
			Body: "miren cluster current",
		}),
	))
	d.Dispatch("cluster export-address", Infer("cluster export-address",
		"Export cluster address with TLS fingerprint for MIREN_CLUSTER",
		ClusterExportAddress,
		WithExample(mflags.Example{
			Name: "Export active cluster",
			Body: "miren cluster export-address",
		}),
		WithExample(mflags.Example{
			Name: "Export specific cluster",
			Body: "miren cluster export-address -C my-cluster",
		}),
	))

	// Runner commands (distributed runners) - behind feature flag
	if labs.DistributedRunners() {
		d.Dispatch("runner", Section("runner", "Runner management commands", "", WithSectionGroup(GroupServer)))
		d.Dispatch("runner token", Section("runner token", "Manage join tokens", ""))
		d.Dispatch("runner token create", Infer("runner token create", "Create a join token for a runner", RunnerTokenCreate,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Create a one-time join token",
				Body: "miren runner token create",
			}),
			WithExample(mflags.Example{
				Name: "Create a reusable token for automation",
				Body: "miren runner token create --reusable --name infra-terraform --ttl 0",
			}),
			WithExample(mflags.Example{
				Name: "Create a token with a specific coordinator address",
				Body: "miren runner token create --addr 10.0.0.5:8443",
			}),
		))
		d.Dispatch("runner join", Infer("runner join", "Join this machine to a coordinator as a runner", RunnerJoin,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Join using a token",
				Body: "miren runner join mren_...",
			}),
			WithExample(mflags.Example{
				Name: "Join with coordinator address override",
				Body: "miren runner join mren_... --coordinator 10.0.0.5:8443",
			}),
		))
		d.Dispatch("runner start", Infer("runner start", "Start this machine as a distributed runner", RunnerStart,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Start the runner",
				Body: "miren runner start",
			}),
		))
		d.Dispatch("runner list", Infer("runner list", "List all registered runners", RunnerList,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "List runners",
				Body: "miren runner list",
			}),
		))
		d.Dispatch("runner status", Infer("runner status", "Show runner health and configuration", RunnerStatus,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Check runner status",
				Body: "miren runner status",
			}),
		))
		d.Dispatch("runner token revoke", Infer("runner token revoke", "Revoke a join token", RunnerTokenRevoke,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Revoke a token",
				Body: "miren runner token revoke inv_abc123",
			}),
		))
		d.Dispatch("runner token list", Infer("runner token list", "List all join tokens", RunnerTokenList,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "List tokens",
				Body: "miren runner token list",
			}),
		))
		d.Dispatch("runner remove", Infer("runner remove", "Remove a registered runner and clean up resources", RunnerRemove,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Remove a runner by name",
				Body: "miren runner remove my-runner",
			}),
			WithExample(mflags.Example{
				Name: "Force remove a runner with active sandboxes",
				Body: "miren runner remove my-runner --force",
			}),
		))
		d.Dispatch("runner install", Infer("runner install", "Install systemd service for miren runner", RunnerInstall,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Install interactively",
				Body: "miren runner install",
			}),
			WithExample(mflags.Example{
				Name: "Install with token (for automation)",
				Body: "miren runner install --token mren_...",
			}),
		))
		d.Dispatch("runner uninstall", Infer("runner uninstall", "Remove systemd service for miren runner", RunnerUninstall,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Uninstall the runner service",
				Body: "miren runner uninstall",
			}),
			WithExample(mflags.Example{
				Name: "Uninstall and remove all runner data",
				Body: "miren runner uninstall --remove-data",
			}),
		))
		d.Dispatch("runner service-status", Infer("runner service-status", "Show miren-runner systemd service status", RunnerServiceStatus,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Show service status",
				Body: "miren runner service-status",
			}),
			WithExample(mflags.Example{
				Name: "Follow service logs",
				Body: "miren runner service-status --follow",
			}),
		))
		d.Dispatch("runner upgrade", Infer("runner upgrade", "Upgrade miren runner to the latest or specified version", RunnerUpgrade,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Upgrade to the latest version",
				Body: "miren runner upgrade",
			}),
			WithExample(mflags.Example{
				Name: "Check for available updates",
				Body: "miren runner upgrade --check",
			}),
			WithExample(mflags.Example{
				Name: "Upgrade to a specific version",
				Body: "miren runner upgrade --version v0.2.0",
			}),
		))
		d.Dispatch("runner upgrade rollback", Infer("runner upgrade rollback", "Rollback runner to previous version", RunnerUpgradeRollback,
			WithLabsFeature(labs.FeatureDistributedRunners),
			WithExample(mflags.Example{
				Name: "Rollback to the previous version",
				Body: "miren runner upgrade rollback",
			}),
		))
	}

	// Server commands
	d.Dispatch("server", Infer("server", "Start the miren server", Server,
		WithGroup(GroupServer),
		WithExample(mflags.Example{
			Name: "Start in standalone mode",
			Body: "miren server --mode standalone",
		}),
	))
	d.Dispatch("server config", Section("server config", "Server configuration management commands", ""))
	d.Dispatch("server config generate", Infer("server config generate", "Generate a server configuration file from current settings", ServerConfigGenerate,
		WithExample(mflags.Example{
			Name: "Generate config with defaults",
			Body: "miren server config generate --defaults",
		}),
		WithExample(mflags.Example{
			Name: "Generate and save to file",
			Body: "miren server config generate --defaults --output server.toml",
		}),
	))
	d.Dispatch("server config validate", Infer("server config validate", "Validate a server configuration file", ServerConfigValidate,
		WithExample(mflags.Example{
			Name: "Validate a config file",
			Body: "miren server config validate --file server.toml",
		}),
	))
	d.Dispatch("server upgrade", Infer("server upgrade", "Upgrade miren server", ServerUpgrade,
		WithExample(mflags.Example{
			Name: "Upgrade to the latest version",
			Body: "miren server upgrade",
		}),
		WithExample(mflags.Example{
			Name: "Check for available updates",
			Body: "miren server upgrade --check",
		}),
		WithExample(mflags.Example{
			Name: "Upgrade to a specific version",
			Body: "miren server upgrade --version v0.2.0",
		}),
	))
	d.Dispatch("server upgrade rollback", Infer("server upgrade rollback", "Rollback server to previous version", ServerUpgradeRollback,
		WithExample(mflags.Example{
			Name: "Rollback to the previous version",
			Body: "miren server upgrade rollback",
		}),
	))
	d.Dispatch("server docker", Section("server docker", "Docker-based server management commands", ""))
	d.Dispatch("server docker install", Infer("server docker install", "Install miren server using Docker", ServerInstallDocker,
		WithExample(mflags.Example{
			Name: "Install with cloud registration",
			Body: "miren server docker install",
		}),
		WithExample(mflags.Example{
			Name: "Install without cloud (local only)",
			Body: "miren server docker install --without-cloud",
		}),
		WithExample(mflags.Example{
			Name: "Install with a custom HTTP port",
			Body: "miren server docker install --http-port 8080",
		}),
		WithExample(mflags.Example{
			Name: "Install behind a TLS-terminating proxy (e.g. tailscale serve)",
			Body: "miren server docker install --ingress-mode behind-proxy-http",
		}),
	))
	d.Dispatch("server docker uninstall", Infer("server docker uninstall", "Uninstall miren server Docker container", ServerUninstallDocker,
		WithExample(mflags.Example{
			Name: "Uninstall the container",
			Body: "miren server docker uninstall",
		}),
		WithExample(mflags.Example{
			Name: "Uninstall and remove all data",
			Body: "miren server docker uninstall --remove-volume",
		}),
	))
	d.Dispatch("server docker status", Infer("server docker status", "Show status of miren server Docker container", ServerStatusDocker,
		WithExample(mflags.Example{
			Name: "Show status",
			Body: "miren server docker status",
		}),
		WithExample(mflags.Example{
			Name: "Follow logs",
			Body: "miren server docker status --follow",
		}),
	))

	// CLI management commands
	d.Dispatch("download", Section("download", "Download management commands", "", WithSectionGroup(GroupServer)))
	d.Dispatch("download release", Infer("download release", "Download and extract miren release", DownloadRelease,
		WithExample(mflags.Example{
			Name: "Download the latest release",
			Body: "miren download release",
		}),
	))
	d.Dispatch("upgrade", Infer("upgrade", "Upgrade miren CLI to latest version", Upgrade,
		WithGroup(GroupClient),
		WithExample(mflags.Example{
			Name: "Upgrade to latest",
			Body: "miren upgrade",
		}),
		WithExample(mflags.Example{
			Name: "Check for updates without installing",
			Body: "miren upgrade --check",
		}),
		WithExample(mflags.Example{
			Name: "Upgrade to a specific version",
			Body: "miren upgrade --version v0.2.0",
		}),
	))

	// Auth commands
	d.Dispatch("auth", Section("auth", "Authentication commands", "", WithSectionGroup(GroupConfiguring)))
	d.Dispatch("auth generate", Infer("auth generate", "Generate authentication config file", AuthGenerate,
		WithExample(mflags.Example{
			Name: "Generate auth config",
			Body: "miren auth generate",
		}),
	))
	d.Dispatch("auth ci", Section("auth ci", "CI authentication binding management", ""))
	d.Dispatch("auth ci add", Infer("auth ci add", "Add a CI authentication binding to an application", AuthCIAdd))
	d.Dispatch("auth ci list", Infer("auth ci list", "List CI authentication bindings for an application", AuthCIList))
	d.Dispatch("auth ci remove", Infer("auth ci remove", "Remove a CI authentication binding", AuthCIRemove))

	d.Dispatch("auth provider", Section("auth provider", "Identity provider management", ""))
	d.Dispatch("auth provider add", Section("auth provider add", "Add an identity provider for route protection", ""))
	d.Dispatch("auth provider add oidc", Infer("auth provider add oidc", "Add an OIDC identity provider", AuthProviderAddOIDC,
		WithExample(mflags.Example{
			Name: "Add a Google OIDC provider",
			Body: `miren auth provider add oidc my-google \
  --provider-url https://accounts.google.com \
  --client-id $CLIENT_ID \
  --client-secret $CLIENT_SECRET \
  --scope email --scope profile`,
		}),
	))
	d.Dispatch("auth provider add github", Infer("auth provider add github", "Add a GitHub identity provider", AuthProviderAddGitHub,
		WithExample(mflags.Example{
			Name: "Add a GitHub provider scoped to a team",
			Body: `miren auth provider add github my-gh \
  --client-id $CLIENT_ID \
  --client-secret $CLIENT_SECRET \
  --org mirendev:platform,eng`,
		}),
	))
	d.Dispatch("auth provider add password", Infer("auth provider add password", "Add a shared-password identity provider", AuthProviderAddPassword,
		WithExample(mflags.Example{
			Name: "Add a password provider",
			Body: `miren auth provider add password my-pw --password hunter2`,
		}),
	))
	d.Dispatch("auth provider list", Infer("auth provider list", "List identity providers", AuthProviderList))
	d.Dispatch("auth provider show", Infer("auth provider show", "Show an identity provider", AuthProviderShow))
	d.Dispatch("auth provider remove", Infer("auth provider remove", "Remove an identity provider", AuthProviderRemove))

	// Admin commands
	d.Dispatch("admin", Infer("admin", "Call an admin method on an application", Admin,
		WithGroup(GroupConfiguring),
		WithDescription(adminDescription),
		WithExample(mflags.Example{
			Name: "List available admin methods",
			Body: "miren admin --list -a myapp",
		}),
		WithExample(mflags.Example{
			Name: "Call an admin method",
			Body: "miren admin health -a myapp",
		}),
		WithExample(mflags.Example{
			Name: "Call a method with JSON output",
			Body: "miren admin stats -a myapp --json",
		}),
		WithExample(mflags.Example{
			Name: "Call a method with params from a file",
			Body: "miren admin migrate -a myapp -f params.json",
		}),
	))

	// Debug commands (unstable, may change without notice)
	d.Dispatch("debug", Section("debug", "Debug and troubleshooting commands", `
Use these commands to help diagnose issues with the miren runtime.

Warning: These commands are intended for advanced users and developers. They may change or be removed without notice.

`, WithSectionGroup(GroupServer)))
	d.Dispatch("debug connection", Infer("debug connection", "Test connectivity and authentication with a server", DebugConnection))
	d.Dispatch("debug advertise", Infer("debug advertise", "Show which addresses the server would advertise and why", DebugAdvertise))
	d.Dispatch("debug reindex", Infer("debug reindex", "Rebuild all entity indexes from scratch", DebugReindex))
	d.Dispatch("debug test", Section("debug test", "Debug test commands", ""))
	d.Dispatch("debug test load", Infer("debug test load", "Loadtest a URL", TestLoad))
	d.Dispatch("debug ctr", Infer("debug ctr", "Run ctr with miren defaults", DebugCtr))
	d.Dispatch("debug ctr nuke", Infer("debug ctr nuke", "Nuke a containerd namespace", CtrNuke))
	d.Dispatch("debug colors", Infer("debug colors", "Print some colors", Colors))
	d.Dispatch("debug bundle", Infer("debug bundle", "Create a support bundle with system debug information", DebugBundle))

	// Debug RBAC commands
	d.Dispatch("debug rbac", Infer("debug rbac", "Fetch and display RBAC rules from miren.cloud", DebugRBAC))
	d.Dispatch("debug rbac test", Infer("debug rbac test", "Test RBAC evaluation with fetched rules", DebugRBACTest))

	// Debug entity commands
	d.Dispatch("debug entity", Section("debug entity", "Entity store debug commands", "", WithSectionDescription(entitySectionDescription)))
	d.Dispatch("debug entity get", Infer("debug entity get", "Get an entity", EntityGet))
	d.Dispatch("debug entity put", Infer("debug entity put", "Put an entity", EntityPut,
		WithDescription(entityPutDescription),
	))
	d.Dispatch("debug entity delete", Infer("debug entity delete", "Delete an entity", EntityDelete))
	d.Dispatch("debug entity list", Infer("debug entity list", "List entities", EntityList))
	d.Dispatch("debug entity create", Infer("debug entity create", "Create a new entity", EntityCreate))
	d.Dispatch("debug entity replace", Infer("debug entity replace", "Replace an existing entity", EntityReplace))
	d.Dispatch("debug entity patch", Infer("debug entity patch", "Patch an existing entity", EntityPatch))
	d.Dispatch("debug entity ensure", Infer("debug entity ensure", "Ensure an entity exists", EntityEnsure))

	// Disk commands
	d.Dispatch("disk", Section("disk", "Disk backup and recovery", "", WithSectionGroup(GroupServer)))
	d.Dispatch("disk backup", Infer("disk backup", "Backup a disk to a snapshot file", DiskBackup))
	d.Dispatch("disk restore", Infer("disk restore", "Restore a disk from a snapshot file", DiskRestore))
	d.Dispatch("disk undelete", Infer("disk undelete", "Restore a recently deleted disk", DiskUndelete))
	d.Dispatch("disk list-deleted", Infer("disk list-deleted", "List deleted disks available for recovery", DiskListDeleted))

	// Debug disk commands
	d.Dispatch("debug disk", Section("debug disk", "Disk entity debug commands", "", WithSectionDescription(diskSectionDescription)))
	d.Dispatch("debug disk create", Infer("debug disk create", "Create a disk entity for testing", DebugDiskCreate,
		WithDescription(diskCreateDescription),
	))
	d.Dispatch("debug disk list", Infer("debug disk list", "List all disk entities", DebugDiskList))
	d.Dispatch("debug disk delete", Infer("debug disk delete", "Delete a disk entity", DebugDiskDelete,
		WithDescription(diskDeleteDescription),
	))
	d.Dispatch("debug disk status", Infer("debug disk status", "Show status of a disk entity", DebugDiskStatus))
	d.Dispatch("debug disk lease", Infer("debug disk lease", "Create a disk lease for testing", DebugDiskLease))
	d.Dispatch("debug disk lease-list", Infer("debug disk lease-list", "List all disk lease entities", DebugDiskLeaseList))
	d.Dispatch("debug disk lease-release", Infer("debug disk lease-release", "Release a disk lease", DebugDiskLeaseRelease))
	d.Dispatch("debug disk lease-delete", Infer("debug disk lease-delete", "Delete a disk lease entity", DebugDiskLeaseDelete))
	d.Dispatch("debug disk lease-status", Infer("debug disk lease-status", "Show detailed status of a disk lease", DebugDiskLeaseStatus))
	d.Dispatch("debug disk mounts", Infer("debug disk mounts", "List all mounted disks from /proc/mounts", DebugDiskMounts))
	d.Dispatch("debug disk migrate", Infer("debug disk migrate", "Migrate LSVD volume to raw disk image", DiskMigrate))

	// Debug netdb commands
	d.Dispatch("debug netdb", Section("debug netdb", "Network database debug commands", ""))
	d.Dispatch("debug netdb list", Infer("debug netdb list", "List all IP leases from netdb", DebugNetDBList))
	d.Dispatch("debug netdb status", Infer("debug netdb status", "Show IP allocation status by subnet", DebugNetDBStatus))
	d.Dispatch("debug netdb release", Infer("debug netdb release", "Manually release IP leases", DebugNetDBRelease))
	d.Dispatch("debug netdb gc", Infer("debug netdb gc", "Find and release orphaned IP leases", DebugNetDBGC))

	// Internal commands (hidden from help, used by miren internals)
	d.Dispatch("internal", Section("internal", "Internal commands used by miren components", "", WithSectionGroup(GroupHidden)))

	// Alias commands
	d.Dispatch("alias", Section("alias", "CLI alias management", "", WithSectionGroup(GroupClient)))
	d.Dispatch("alias list", Infer("alias list", "List configured CLI aliases", AliasList))

	// Help command (registered last so it can reference all other commands)
	d.Dispatch("help", NewHelpCommand(d))

	addCommands(d)
}
