package commands

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/slogfmt"
)

type GlobalFlags struct {
	Verbose []bool `short:"v" long:"verbose" description:"Enable verbose output"`
}

type Context struct {
	context.Context

	verbose int
	Log     *slog.Logger

	Stdout io.Writer
	Stderr io.Writer

	cancels []func()
}

func setup(ctx context.Context, flags *GlobalFlags) *Context {
	s := &Context{
		verbose: len(flags.Verbose),
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}

	var level slog.Level

	switch s.verbose {
	case 0:
		level = slog.LevelWarn
	case 1:
		level = slog.LevelInfo
	case 2:
		level = slog.LevelDebug
	default:
		level = slog.LevelDebug
	}

	dynLevel := new(slog.LevelVar)
	dynLevel.Set(level)

	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, os.Interrupt, unix.SIGQUIT, unix.SIGTERM,
		unix.SIGTTIN, unix.SIGTTOU,
	)

	s.Log = slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: dynLevel,
	}))

	ctx, cancel := context.WithCancel(ctx)
	s.cancels = append(s.cancels, cancel)

	sigCtx, sigCancel := context.WithCancel(ctx)
	s.cancels = append(s.cancels, sigCancel)

	go func() {
		defer close(sigCh)
		defer signal.Stop(sigCh)

		var shutdownRequests int
		for {
			select {
			case <-sigCtx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}

				var target slog.Level

				switch sig {
				case unix.SIGTTIN:
					target = dynLevel.Level() - 4
				case unix.SIGTTOU:
					target = dynLevel.Level() + 4
				case os.Interrupt, unix.SIGQUIT, unix.SIGTERM:
					shutdownRequests++
					switch shutdownRequests {
					case 1:
						s.Log.InfoContext(sigCtx, "Signal received, shutting down")
						cancel()
					case 2:
						s.Log.InfoContext(sigCtx, "Shutdown urgency detected, exitting")
						os.Exit(130)
					}

					continue
				}

				if target < slog.LevelDebug {
					continue
				}

				if target > slog.LevelError {
					continue
				}

				if dynLevel.Level() == target {
					continue
				}

				dynLevel.Set(target)

				s.Log.ErrorContext(sigCtx, "Log leveling changed", "level", target)
			}
		}
	}()

	s.Log.DebugContext(ctx, "Configured logging", "level", level)
	s.Log.DebugContext(ctx, "Dynamic leveling enabled via signals", "more-logging", "SIGTTIN", "less-logging", "SIGTTOU")

	s.Context = ctx
	return s
}

func (c *Context) Close() error {
	for _, cancel := range c.cancels {
		cancel()
	}

	return nil
}

func (c *Context) Printf(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, format, args...)
}
