package commands

import "github.com/mitchellh/cli"

func AllCommands() map[string]cli.CommandFactory {
	return map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},

		"deploy": func() (cli.Command, error) {
			return Infer("Deploy", "Deploy an application", Deploy), nil
		},
		"server": func() (cli.Command, error) {
			return Infer("server", "Start the server", Server), nil
		},

		"console": func() (cli.Command, error) {
			return Infer("console", "Start a console", Console), nil
		},

		"app": func() (cli.Command, error) {
			return Infer("app", "Get information about an application", App), nil
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

		"config info": func() (cli.Command, error) {
			return Infer("config info", "Get information about the configuration", ConfigInfo), nil
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
}
