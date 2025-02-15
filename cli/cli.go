package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/mitchellh/cli"
	"miren.dev/runtime/cli/commands"
	"miren.dev/runtime/version"
)

func Run(args []string) int {
	c := cli.NewCLI("runtime", version.Version)
	c.Commands = commands.AllCommands()
	c.HiddenCommands = commands.HiddenCommands()
	c.Args = args[1:]

	exitStatus, err := c.Run()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Printf("ERROR: %s\n", err)
			return 1
		}
	}

	return exitStatus
}
