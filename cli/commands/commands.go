package commands

import (
	"github.com/mitchellh/cli"
)

func AllCommands() map[string]cli.CommandFactory {
	base := map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},

		// TODO: Rework this command in context of MVP, currently assumes it can derive things from Docker,
		//       which we no longer use as a container engine.
		// "setup": func() (cli.Command, error) {
		// 	return Infer("setup", "Setup the runtime access", Setup), nil
		// },

		// TODO: Rework for MVP, currently assumes old "app" concept
		// "import": func() (cli.Command, error) {
		// 	return Infer("import", "Import an image", Import), nil
		// },

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
		"app default set": func() (cli.Command, error) {
			return Infer("app default set", "Set an app as the default", AppDefaultSet), nil
		},

		"app default unset": func() (cli.Command, error) {
			return Infer("app default unset", "Remove default flag from all apps", AppDefaultUnset), nil
		},

		"app default show": func() (cli.Command, error) {
			return Infer("app default show", "Show which app is currently default", AppDefaultShow), nil
		},

		"set host": func() (cli.Command, error) {
			return Infer("set host", "Set the hostname of an application", SetHost), nil
		},

		"logs": func() (cli.Command, error) {
			return Infer("logs", "Get logs for an application", Logs), nil
		},

		// TODO: Errors with "unknown object: app"
		// "app new": func() (cli.Command, error) {
		// 	return Infer("app new", "Create a new application", AppNew), nil
		// },

		// TODO: Errors with "unknown object: app"
		// "app destroy": func() (cli.Command, error) {
		// 	return Infer("app destroy", "Create a new application", AppDestroy), nil
		// },

		// TODO: Errors with "unknown object: addons"
		// "app addon add": func() (cli.Command, error) {
		// 	return Infer("app addon add", "Add an addon to an application", AppAddonsAdd), nil
		// },

		// TODO: Errors with "unknown object: addons"
		// "app addon destroy": func() (cli.Command, error) {
		// 	return Infer("app addon destroy", "Destroy an addon", AppAddonsDestroy), nil
		// },

		// TODO: Errors with "unknown object: addons"
		// "app addons": func() (cli.Command, error) {
		// 	return Infer("app addon list", "List addons for an application", AppAddonsList), nil
		// },

		"config": func() (cli.Command, error) {
			return Section("config", "Commands related to client configuration"), nil
		},

		"config info": func() (cli.Command, error) {
			return Infer("config info", "Get information about the client configuration", ConfigInfo), nil
		},

		"config load": func() (cli.Command, error) {
			return Infer("config load", "Load config and merge it with your current config", ConfigLoad), nil
		},

		// TODO: Errors with "unknown object: user"
		// "user": func() (cli.Command, error) {
		// 	return Section("user", "Commands related to cluster users"), nil
		// },
		// "user whoami": func() (cli.Command, error) {
		// 	return Infer("user whoami", "Get information about the current user", WhoAmI), nil
		// },

		"disk create": func() (cli.Command, error) {
			return Infer("disk create", "Create a new disk", DiskCreate), nil
		},

		// TODO: Errors with "mkdir : no such file or directory"
		// "disk run": func() (cli.Command, error) {
		// 	return Infer("disk run", "Run a disk", DiskRun), nil
		// },

		// TODO: Errors with "when considering miren.dev/runtime/disk/Provisioner.Subnet, unable to find component of type *netdb.Subnet available"
		// "disk provision": func() (cli.Command, error) {
		// 	return Infer("disk provision", "Provision a disk", DiskProvision), nil
		// },

		// TODO: Unclear if this works, needs someone to go over it
		// "dataset serve": func() (cli.Command, error) {
		// 	return Infer("dataset serve", "Serve a dataset", DataSetServe), nil
		// },

		// TODO: Errors with "error performing http request: http3: no Host in request URL"
		// "runner run": func() (cli.Command, error) {
		// 	return Infer("runner run", "Run a runner", RunnerRun), nil
		// },

		// TODO: Errors with "conflict in entity"
		// "coordinator run": func() (cli.Command, error) {
		// 	return Infer("coordinator run", "Run a coordinator", CoordinatorRun), nil
		// },

		"dev": func() (cli.Command, error) {
			return Infer("dev", "Run the dev server", Dev), nil
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

		// TODO: Errors with "open idx: no such file or directory"
		// "internal disk cleanup": func() (cli.Command, error) {
		// 	return Infer("internal disk cleanup", "Cleanup disk data", DiskCleanup), nil
		// },

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
