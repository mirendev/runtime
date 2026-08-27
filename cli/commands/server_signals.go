//go:build linux

package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"miren.dev/runtime/components/runner"
)

func watchServerSignals(ctx *Context, serverCtx context.Context, runner *runner.Runner) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(signals)

	for {
		select {
		case <-serverCtx.Done():
			return nil
		case received := <-signals:
			switch received {
			case syscall.SIGUSR1:
				ctx.Log.Info("SIGUSR1 received - restart mode")
				runner.SetRestartMode(true)
				return fmt.Errorf("restart requested via SIGUSR1")
			case syscall.SIGUSR2:
				ctx.Log.Info("SIGUSR2 received - draining runner")
				drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				err := runner.Drain(drainCtx)
				cancel()
				if err != nil {
					ctx.Log.Error("failed to drain runner", "error", err)
					return err
				}
				ctx.Log.Info("runner drained successfully, shutting down")
				return fmt.Errorf("runner drained, shutting down")
			}
		}
	}
}
