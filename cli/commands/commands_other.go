//go:build !linux

package commands

import (
	"context"

	"github.com/mitchellh/cli"
	"miren.dev/runtime/pkg/asm"
)

func addCommands(cmds map[string]cli.CommandFactory) {}

func (c *Context) setupServerComponents(_ context.Context, _ *asm.Registry) {}
