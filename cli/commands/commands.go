package commands

import "github.com/mitchellh/cli"

func AllCommands() map[string]cli.CommandFactory {
	return map[string]cli.CommandFactory{
		"version": func() (cli.Command, error) {
			return Infer("version", "Print the version", Version), nil
		},
		"run": func() (cli.Command, error) {
			return Infer("run", "Run the application", Run), nil
		},
		"server": func() (cli.Command, error) {
			return Infer("server", "Start the server", Server), nil
		},

		"console": func() (cli.Command, error) {
			return Infer("console", "Start a console", Console), nil
		},

		"app new": func() (cli.Command, error) {
			return Infer("app new", "Create a new application", AppNew), nil
		},

		"debug ctr nuke": func() (cli.Command, error) {
			return Infer("debug ctr nuke", "Nuke a containerd namespace", CtrNuke), nil
		},
	}
}
