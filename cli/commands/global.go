package commands

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/pkg/slogrus"
)

type GlobalFlags struct {
	Verbose       []bool `short:"v" long:"verbose" description:"Enable verbose output"`
	ServerAddress string `long:"server-address" description:"Server address to connect to" default:"127.0.0.1:8443" asm:"server-addr"`
	Config        string `long:"config" description:"Path to configuration file"`
}

type Context struct {
	context.Context

	verbose int
	Log     *slog.Logger

	Stdout io.Writer
	Stderr io.Writer

	Client *asm.Registry
	Server *asm.Registry

	cancels []func()

	Config struct {
		ServerAddress string `asm:"server-addr"`
	}

	exitCode int
}

func setup(ctx context.Context, flags *GlobalFlags, opts any) *Context {
	s := &Context{
		verbose: len(flags.Verbose),
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,

		Server: &asm.Registry{},
		Client: &asm.Registry{},
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

	s.Log = slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: dynLevel,
	}))

	ctx = slogrus.WithLogger(ctx, s.Log)
	slogrus.OverrideGlobal(s.Log)

	s.Server.Log = s.Log
	s.Client.Log = s.Log

	s.setupServerComponents(ctx, s.Server)

	s.Server.InferFrom(opts)
	s.Client.InferFrom(flags)

	s.Client.Populate(&s.Config)

	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, os.Interrupt, unix.SIGQUIT, unix.SIGTERM,
		unix.SIGTTIN, unix.SIGTTOU,
	)

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

func (c *Context) SetExitCode(code int) {
	c.exitCode = code
}

func (c *Context) Printf(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, format, args...)
}

func (c *Context) RPCClient(name string) (*rpc.Client, error) {
	cs, err := rpc.NewState(c, rpc.WithSkipVerify, rpc.WithLogger(c.Log))
	if err != nil {
		return nil, err
	}

	client, err := cs.Connect(c.Config.ServerAddress, name)
	if err != nil {
		return nil, err
	}

	return client, nil
}
