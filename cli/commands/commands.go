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
	}
}
