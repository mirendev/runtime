package commands

import (
	"github.com/mitchellh/cli"
)

func AllCommands() map[string]cli.CommandFactory {
	base := map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},

		"login": func() (cli.Command, error) {
			return Infer("login", "Authenticate with miren.cloud", Login), nil
		},

		"debug": func() (cli.Command, error) {
			return Section("debug", "Debug and troubleshooting commands"), nil
		},

		"debug connection": func() (cli.Command, error) {
			return Infer("debug connection", "Test connectivity and authentication with a server", DebugConnection), nil
		},

		"debug reindex": func() (cli.Command, error) {
			return Infer("debug reindex", "Rebuild all entity indexes from scratch", DebugReindex), nil
		},

		"init": func() (cli.Command, error) {
			return Infer("init", "Initialize a new application", Init), nil
		},

		"deploy": func() (cli.Command, error) {
			return Infer("Deploy", "Deploy an application", Deploy), nil
		},

		// TODO: This hangs when pointed at a deployed hw-bun but works w/ combo.yaml
		"console": func() (cli.Command, error) {
			return Infer("console", "Start a console", Console), nil
		},

		// TODO: This hangs when pointed at a deployed hw-bun but works w/ combo.yaml
		"sandbox exec": func() (cli.Command, error) {
			return Infer("sandbox exec", "Execute a command in a sandbox", SandboxExec), nil
		},
		"sandbox list": func() (cli.Command, error) {
			return Infer("sandbox list", "List all sandboxes", SandboxList), nil
		},
		"sandbox stop": func() (cli.Command, error) {
			return Infer("sandbox stop", "Stop a sandbox", SandboxStop), nil
		},
		"sandbox delete": func() (cli.Command, error) {
			return Infer("sandbox delete", "Delete a dead sandbox", SandboxDelete), nil
		},
		"sandbox metrics": func() (cli.Command, error) {
			return Infer("sandbox metrics", "Get metrics from a sandbox", SandboxMetrics), nil
		},

		"sandbox-pool list": func() (cli.Command, error) {
			return Infer("sandbox-pool list", "List all sandbox pools", SandboxPoolList), nil
		},
		"sandbox-pool set-desired": func() (cli.Command, error) {
			return Infer("sandbox-pool set-desired", "Set desired instance count for a sandbox pool", SandboxPoolSetDesired), nil
		},

		"app": func() (cli.Command, error) {
			return Infer("app", "Get information about an application", App), nil
		},

		"app history": func() (cli.Command, error) {
			return Infer("app history", "Show deployment history for an application", AppHistory), nil
		},

		"app status": func() (cli.Command, error) {
			return Infer("app status", "Show current status of an application", AppStatus), nil
		},

		"app list": func() (cli.Command, error) {
			return Infer("app list", "List all applications", AppList), nil
		},

		"app delete": func() (cli.Command, error) {
			return Infer("app delete", "Delete an application and all its resources", AppDelete), nil
		},

		"apps": func() (cli.Command, error) {
			return Infer("apps", "List all applications (alias for 'app list')", AppList), nil
		},

		"env": func() (cli.Command, error) {
			return Section("env", "Environment variable management commands"), nil
		},

		"env set": func() (cli.Command, error) {
			return Infer("env set", "Set environment variables for an application", EnvSet), nil
		},

		"env get": func() (cli.Command, error) {
			return Infer("env get", "Get an environment variable value", EnvGet), nil
		},

		"env list": func() (cli.Command, error) {
			return Infer("env list", "List all environment variables", EnvList), nil
		},

		"env delete": func() (cli.Command, error) {
			return Infer("env delete", "Delete environment variables", EnvDelete), nil
		},

		"set": func() (cli.Command, error) {
			return Infer("set", "Set concurrency for an application", Set), nil
		},

		"route": func() (cli.Command, error) {
			return Infer("route", "List all HTTP routes", Route), nil
		},

		"route list": func() (cli.Command, error) {
			return Infer("route list", "List all HTTP routes", RouteList), nil
		},

		"route set": func() (cli.Command, error) {
			return Infer("route set", "Create or update an HTTP route", RouteSet), nil
		},

		"route remove": func() (cli.Command, error) {
			return Infer("route remove", "Remove an HTTP route", RouteRemove), nil
		},

		"route show": func() (cli.Command, error) {
			return Infer("route show", "Show details of an HTTP route", RouteShow), nil
		},

		"route set-default": func() (cli.Command, error) {
			return Infer("route set-default", "Set an app as the default route", RouteSetDefault), nil
		},

		"route unset-default": func() (cli.Command, error) {
			return Infer("route unset-default", "Remove the default route", RouteUnsetDefault), nil
		},

		"logs": func() (cli.Command, error) {
			return Infer("logs", "Get logs for an application", Logs), nil
		},

		// Config commands - for config file management
		"config": func() (cli.Command, error) {
			return Section("config", "Configuration file management"), nil
		},

		"config info": func() (cli.Command, error) {
			return Infer("config info", "Show configuration file locations and format", ConfigInfo), nil
		},

		"config load": func() (cli.Command, error) {
			return Infer("config load", "Load config and merge it with your current config", ConfigLoad), nil
		},

		// Cluster commands - for cluster management
		"cluster": func() (cli.Command, error) {
			return Infer("cluster", "List configured clusters", Cluster), nil
		},

		"cluster list": func() (cli.Command, error) {
			return Infer("cluster list", "List all configured clusters", ClusterList), nil
		},

		"cluster switch": func() (cli.Command, error) {
			return Infer("cluster switch", "Switch to a different cluster", ClusterSwitch), nil
		},

		"cluster add": func() (cli.Command, error) {
			return Infer("cluster add", "Add a new cluster configuration", ClusterAdd), nil
		},

		"cluster remove": func() (cli.Command, error) {
			return Infer("cluster remove", "Remove a cluster from the configuration", ClusterRemove), nil
		},

		"server": func() (cli.Command, error) {
			return Infer("server", "Start the miren server", Server), nil
		},

		"server config": func() (cli.Command, error) {
			return Section("server config", "Server configuration management commands"), nil
		},

		"server config generate": func() (cli.Command, error) {
			return Infer("server config generate", "Generate a server configuration file from current settings", ServerConfigGenerate), nil
		},

		"server config validate": func() (cli.Command, error) {
			return Infer("server config validate", "Validate a server configuration file", ServerConfigValidate), nil
		},

		"download release": func() (cli.Command, error) {
			return Infer("download release", "Download and extract miren release", DownloadRelease), nil
		},

		"upgrade": func() (cli.Command, error) {
			return Infer("upgrade", "Upgrade miren CLI to latest version", Upgrade), nil
		},

		"server upgrade": func() (cli.Command, error) {
			return Infer("server upgrade", "Upgrade miren server", ServerUpgrade), nil
		},

		"server upgrade rollback": func() (cli.Command, error) {
			return Infer("server upgrade rollback", "Rollback server to previous version", ServerUpgradeRollback), nil
		},

		"auth generate": func() (cli.Command, error) {
			return Infer("auth generate", "Generate authentication config file", AuthGenerate), nil
		},

		"entity get": func() (cli.Command, error) {
			return Infer("entity get", "Get an entity", EntityGet), nil
		},

		"entity put": func() (cli.Command, error) {
			return Infer("entity put", "Put an entity", EntityPut), nil
		},

		"entity delete": func() (cli.Command, error) {
			return Infer("entity delete", "Delete an entity", EntityDelete), nil
		},

		"entity list": func() (cli.Command, error) {
			return Infer("entity list", "List entities", EntityList), nil
		},

		// TODO: Unclear if this is still useful; leaving it in for now
		"internal dial-stdio": func() (cli.Command, error) {
			return Infer("internal dial-stdio", "Dial a stdio connection", DialStdio), nil
		},

		"test load": func() (cli.Command, error) {
			return Infer("test load", "Loadtest a URL", TestLoad), nil
		},

		"debug ctr nuke": func() (cli.Command, error) {
			return Infer("debug ctr nuke", "Nuke a containerd namespace", CtrNuke), nil
		},

		"debug colors": func() (cli.Command, error) {
			return Infer("debug colors", "Print some colors", Colors), nil
		},

		"debug rbac": func() (cli.Command, error) {
			return Infer("debug rbac", "Fetch and display RBAC rules from miren.cloud", DebugRBAC), nil
		},

		"debug rbac test": func() (cli.Command, error) {
			return Infer("debug rbac test", "Test RBAC evaluation with fetched rules", DebugRBACTest), nil
		},

		"debug entity create": func() (cli.Command, error) {
			return Infer("debug entity create", "Create a new entity", EntityCreate), nil
		},

		"debug entity replace": func() (cli.Command, error) {
			return Infer("debug entity replace", "Replace an existing entity", EntityReplace), nil
		},

		"debug entity patch": func() (cli.Command, error) {
			return Infer("debug entity patch", "Patch an existing entity", EntityPatch), nil
		},

		"debug entity ensure": func() (cli.Command, error) {
			return Infer("debug entity ensure", "Ensure an entity exists", EntityEnsure), nil
		},

		"debug disk": func() (cli.Command, error) {
			return Section("debug disk", "Disk entity debug commands"), nil
		},

		"debug disk create": func() (cli.Command, error) {
			return Infer("debug disk create", "Create a disk entity for testing", DebugDiskCreate), nil
		},

		"debug disk list": func() (cli.Command, error) {
			return Infer("debug disk list", "List all disk entities", DebugDiskList), nil
		},

		"debug disk delete": func() (cli.Command, error) {
			return Infer("debug disk delete", "Delete a disk entity", DebugDiskDelete), nil
		},

		"debug disk status": func() (cli.Command, error) {
			return Infer("debug disk status", "Show status of a disk entity", DebugDiskStatus), nil
		},

		"debug disk lease": func() (cli.Command, error) {
			return Infer("debug disk lease", "Create a disk lease for testing", DebugDiskLease), nil
		},

		"debug disk lease-list": func() (cli.Command, error) {
			return Infer("debug disk lease-list", "List all disk lease entities", DebugDiskLeaseList), nil
		},

		"debug disk lease-release": func() (cli.Command, error) {
			return Infer("debug disk lease-release", "Release a disk lease", DebugDiskLeaseRelease), nil
		},

		"debug disk lease-delete": func() (cli.Command, error) {
			return Infer("debug disk lease-delete", "Delete a disk lease entity", DebugDiskLeaseDelete), nil
		},

		"debug disk lease-status": func() (cli.Command, error) {
			return Infer("debug disk lease-status", "Show detailed status of a disk lease", DebugDiskLeaseStatus), nil
		},

		"debug disk mounts": func() (cli.Command, error) {
			return Infer("debug disk mounts", "List all mounted disks from /proc/mounts", DebugDiskMounts), nil
		},
	}

	addCommands(base)

	return base
}

func HiddenCommands() []string {
	return []string{
		"internal",
		"debug",
	}
}
