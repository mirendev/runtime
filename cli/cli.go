package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"miren.dev/mflags"
	"miren.dev/runtime/cli/commands"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/version"
)

func Run(args []string) int {
	helpJSON := os.Getenv("MIREN_HELP_JSON") != ""

	// When generating help JSON, enable all labs features so every command
	// is registered and included in the output.
	if helpJSON {
		labs.EnableAll()
	}

	// Initialize labs feature flags from environment before registering commands
	if labsEnv := os.Getenv("MIREN_LABS"); labsEnv != "" {
		labs.Init(nil, strings.Split(labsEnv, ","))
	}

	d := mflags.NewDispatcher("miren")

	commands.RegisterAll(d)

	if helpJSON {
		data, err := d.HelpJSON()
		if err != nil {
			printError(err)
			return 1
		}
		fmt.Println(string(data))
		return 0
	}

	execArgs, err := expandAlias(d, args[1:])
	if err != nil {
		printError(err)
		return 1
	}

	if shouldShowTopLevelHelp(execArgs) {
		commands.RenderTopLevelHelp(d)
		return 0
	}

	err = d.Execute(execArgs)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}

		// Check for ErrExitCode
		if exitErr, ok := err.(commands.ErrExitCode); ok {
			return int(exitErr)
		}

		printError(err)
		return 1
	}

	return 0
}

// shouldShowTopLevelHelp returns true when the args indicate we should render
// top-level help (no args, or just -h/--help with no command).
func shouldShowTopLevelHelp(args []string) bool {
	if len(args) == 0 {
		return true
	}
	// Only help flags, no command words
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			continue
		}
		return false
	}
	return true
}

// printError renders an error to stderr. If the error implements
// ui.TerminalError, it gets colorized multi-line output; otherwise
// it falls back to a plain "ERROR: ..." line.
func printError(err error) {
	// Errors that render their own severity label own the whole line, prefix
	// included, so that the label can be colored to match the block.
	var se ui.SeverityTerminalError
	if errors.As(err, &se) {
		se.WriteWithSeverity(os.Stderr, ui.SeverityError)
		return
	}

	var te ui.TerminalError
	if errors.As(err, &te) {
		fmt.Fprintf(os.Stderr, "ERROR: ")
		te.WriteForTerminal(os.Stderr)
		return
	}

	fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
}

// Version returns the version string
func Version() string {
	return version.Version
}
