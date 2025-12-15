package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"miren.dev/mflags"
	"miren.dev/runtime/cli/commands"
	"miren.dev/runtime/version"
)

func Run(args []string) int {
	d := mflags.NewDispatcher("miren")

	commands.RegisterAll(d)

	err := d.Execute(args[1:])
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}

		// Check for ErrExitCode
		if exitErr, ok := err.(commands.ErrExitCode); ok {
			return int(exitErr)
		}

		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		return 1
	}

	return 0
}

// Version returns the version string
func Version() string {
	return version.Version
}
