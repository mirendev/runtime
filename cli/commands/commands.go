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

		"logout": func() (cli.Command, error) {
			return Infer("logout", "Remove local authentication credentials", Logout), nil
		},

		"whoami": func() (cli.Command, error) {
			return Infer("whoami", "Display information about the current authenticated user", Whoami), nil
		},

		"doctor": func() (cli.Command, error) {
			return Infer("doctor", "Diagnose miren environment and connectivity", Doctor), nil
		},

		"doctor config": func() (cli.Command, error) {
			return Infer("doctor config", "Check configuration files", DoctorConfig), nil
		},

		"doctor server": func() (cli.Command, error) {
			return Infer("doctor server", "Check server health and connectivity", DoctorServer), nil
		},

		"doctor apps": func() (cli.Command, error) {
			return Infer("doctor apps", "Check apps and their routes", DoctorApps), nil
		},

		"doctor auth": func() (cli.Command, error) {
			return Infer("doctor auth", "Check authentication and user information", DoctorAuth), nil
		},

		"doctor all": func() (cli.Command, error) {
			return Infer("doctor all", "Run all diagnostic checks", DoctorAll), nil
		},

		"init": func() (cli.Command, error) {
			return Infer("init", "Initialize a new application", Init), nil
		},

		"deploy": func() (cli.Command, error) {
			return Infer("Deploy", "Deploy an application", Deploy), nil
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
		"sandbox exec": func() (cli.Command, error) {
			return Infer("sandbox exec", "Open interactive shell in an existing sandbox", SandboxExec), nil
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

		"app run": func() (cli.Command, error) {
			return Infer("app run", "Open interactive shell in a new sandbox", AppRun), nil
		},

		"apps": func() (cli.Command, error) {
			return Infer("apps", "List all applications (alias for 'app list')", AppList), nil
		},

		"env": func() (cli.Command, error) {
			return Section("env", "Environment variable management commands", ""), nil
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
			return Section("config", "Configuration file management", ""), nil
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
			return Section("server config", "Server configuration management commands", ""), nil
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

		"server docker": func() (cli.Command, error) {
			return Section("server docker", "Docker-based server management commands", ""), nil
		},

		"server docker install": func() (cli.Command, error) {
			return Infer("server docker install", "Install miren server using Docker", ServerInstallDocker), nil
		},

		"server docker uninstall": func() (cli.Command, error) {
			return Infer("server docker uninstall", "Uninstall miren server Docker container", ServerUninstallDocker), nil
		},

		"server docker status": func() (cli.Command, error) {
			return Infer("server docker status", "Show status of miren server Docker container", ServerStatusDocker), nil
		},

		"auth generate": func() (cli.Command, error) {
			return Infer("auth generate", "Generate authentication config file", AuthGenerate), nil
		},

		// Debug Commands. These have no guarantees of stability and may change or be removed without notice.

		"debug": func() (cli.Command, error) {
			return Section("debug", "Debug and troubleshooting commands", `
Use these commands to help diagnose issues with the miren runtime.

Warning: These commands are intended for advanced users and developers. They may change or be removed without notice.

`), nil
		},

		"debug connection": func() (cli.Command, error) {
			return Infer("debug connection", "Test connectivity and authentication with a server", DebugConnection), nil
		},

		"debug reindex": func() (cli.Command, error) {
			return Infer("debug reindex", "Rebuild all entity indexes from scratch", DebugReindex), nil
		},

		"debug test load": func() (cli.Command, error) {
			return Infer("debug test load", "Loadtest a URL", TestLoad), nil
		},

		"debug ctr": func() (cli.Command, error) {
			return Infer("debug ctr", "Run ctr with miren defaults", DebugCtr), nil
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

		"debug entity get": func() (cli.Command, error) {
			return Infer("debug entity get", "Get an entity", EntityGet), nil
		},

		"debug entity put": func() (cli.Command, error) {
			return Infer("debug entity put", "Put an entity", EntityPut), nil
		},

		"debug entity delete": func() (cli.Command, error) {
			return Infer("debug entity delete", "Delete an entity", EntityDelete), nil
		},

		"debug entity list": func() (cli.Command, error) {
			return Infer("debug entity list", "List entities", EntityList), nil
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
			return Section("debug disk", "Disk entity debug commands", ""), nil
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

		"debug netdb list": func() (cli.Command, error) {
			return Infer("debug netdb list", "List all IP leases from netdb", DebugNetDBList), nil
		},

		"debug netdb status": func() (cli.Command, error) {
			return Infer("debug netdb status", "Show IP allocation status by subnet", DebugNetDBStatus), nil
		},

		"debug netdb release": func() (cli.Command, error) {
			return Infer("debug netdb release", "Manually release IP leases", DebugNetDBRelease), nil
		},

		"debug netdb gc": func() (cli.Command, error) {
			return Infer("debug netdb gc", "Find and release orphaned IP leases", DebugNetDBGC), nil
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
