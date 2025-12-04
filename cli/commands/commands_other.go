//go:build !linux

package commands

import (
	"context"

	"github.com/mitchellh/cli"
	"miren.dev/runtime/pkg/asm"
)

func addCommands(cmds map[string]cli.CommandFactory) {
	// Server management commands - provide helpful errors directing to Docker
	cmds["server install"] = func() (cli.Command, error) {
		return Infer("server install", "Install miren server (Linux only)", ServerInstall), nil
	}

	cmds["server uninstall"] = func() (cli.Command, error) {
		return Infer("server uninstall", "Uninstall miren server (Linux only)", ServerUninstall), nil
	}

	cmds["server status"] = func() (cli.Command, error) {
		return Infer("server status", "Show miren service status (Linux only)", ServerStatus), nil
	}
}

func (c *Context) setupServerComponents(_ context.Context, _ *asm.Registry) {}
