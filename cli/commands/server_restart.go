package commands

import (
	"fmt"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverlifecycle"
)

func ServerRestart(ctx *Context, opts struct{}) error {
	if !release.IsServerRunning() {
		return fmt.Errorf("miren server is not running as a systemd service on this machine")
	}
	op, err := runLifecycleOperation(ctx, serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli"))
	if err != nil {
		return err
	}
	if !op.Succeeded() {
		return fmt.Errorf("restart %s: %s", op.Phase, op.Error)
	}
	ctx.Completed("Server restarted (%s)", op.NewVersion)
	return nil
}
