package commands

import (
	"github.com/mitchellh/cli"
)

func AllCommands() map[string]cli.CommandFactory {
	base := map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},

		"setup": func() (cli.Command, error) {
			return Infer("setup", "Setup the runtime access", Setup), nil
		},

		"import": func() (cli.Command, error) {
			return Infer("import", "Import an image", Import), nil
		},

		"deploy": func() (cli.Command, error) {
			return Infer("Deploy", "Deploy an application", Deploy), nil
		},

		"console": func() (cli.Command, error) {
			return Infer("console", "Start a console", Console), nil
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

		"set host": func() (cli.Command, error) {
			return Infer("set host", "Set the hostname of an application", SetHost), nil
		},

		"logs": func() (cli.Command, error) {
			return Infer("logs", "Get logs for an application", Logs), nil
		},

		"app new": func() (cli.Command, error) {
			return Infer("app new", "Create a new application", AppNew), nil
		},

		"app destroy": func() (cli.Command, error) {
			return Infer("app destroy", "Create a new application", AppDestroy), nil
		},

		"config": func() (cli.Command, error) {
			return Section("config", "Commands related to client configuration"), nil
		},

		"config info": func() (cli.Command, error) {
			return Infer("config info", "Get information about the client configuration", ConfigInfo), nil
		},

		"user": func() (cli.Command, error) {
			return Section("user", "Commands related to cluster users"), nil
		},

		"user whoami": func() (cli.Command, error) {
			return Infer("user whoami", "Get information about the current user", WhoAmI), nil
		},

		"internal dial-stdio": func() (cli.Command, error) {
			return Infer("internal dial-stdio", "Dial a stdio connection", DialStdio), nil
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
