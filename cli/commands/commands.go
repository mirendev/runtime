package commands

import (
	"github.com/mitchellh/cli"
)

func AllCommands() map[string]cli.CommandFactory {
	base := map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},

		"init": func() (cli.Command, error) {
			return Infer("init", "Initialize a new runtime application", Init), nil
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
		"sandbox metrics": func() (cli.Command, error) {
			return Infer("sandbox metrics", "Get metrics from a sandbox", SandboxMetrics), nil
		},

		"app": func() (cli.Command, error) {
			return Infer("app", "Get information about an application", App), nil
		},

		"apps": func() (cli.Command, error) {
			return Infer("apps", "List all applications", Apps), nil
		},

		"set": func() (cli.Command, error) {
			return Infer("set", "Set environment variables for an application", Set), nil
		},
		"default-route set": func() (cli.Command, error) {
			return Infer("default-route set", "Set an app as the default route", DefaultRouteSet), nil
		},

		"default-route unset": func() (cli.Command, error) {
			return Infer("default-route unset", "Remove default route flag from all apps", DefaultRouteUnset), nil
		},

		"default-route show": func() (cli.Command, error) {
			return Infer("default-route show", "Show which app is currently the default route", DefaultRouteShow), nil
		},

		"set host": func() (cli.Command, error) {
			return Infer("set host", "Set the hostname of an application", SetHost), nil
		},

		"logs": func() (cli.Command, error) {
			return Infer("logs", "Get logs for an application", Logs), nil
		},

		"config": func() (cli.Command, error) {
			return Section("config", "Commands related to client configuration"), nil
		},

		"config info": func() (cli.Command, error) {
			return Infer("config info", "Get information about the client configuration", ConfigInfo), nil
		},

		"config load": func() (cli.Command, error) {
			return Infer("config load", "Load config and merge it with your current config", ConfigLoad), nil
		},

		"config set-active": func() (cli.Command, error) {
			return Infer("config set-active", "Set the active cluster", ConfigSetActive), nil
		},

		"config remove": func() (cli.Command, error) {
			return Infer("config remove", "Remove a cluster from the configuration", ConfigRemove), nil
		},

		"disk create": func() (cli.Command, error) {
			return Infer("disk create", "Create a new disk", DiskCreate), nil
		},

		"server": func() (cli.Command, error) {
			return Infer("server", "Start the runtime server", Server), nil
		},

		"download release": func() (cli.Command, error) {
			return Infer("download release", "Download and extract runtime release", DownloadRelease), nil
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
