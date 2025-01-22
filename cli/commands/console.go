package commands

import (
	"io"
	"os"
	"os/signal"

	"github.com/containerd/console"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/shell"
)

func currentConsole() console.Console {
	// Usually all three streams (stdin, stdout, and stderr)
	// are open to the same console, but some might be redirected,
	// so try all three.
	for _, s := range []*os.File{os.Stderr, os.Stdout, os.Stdin} {
		if c, err := console.ConsoleFromFile(s); err == nil {
			return c
		}
	}

	return nil
}

func Console(ctx *Context, opts struct {
	App  string   `short:"a" long:"app" description:"Application to run"`
	Pool string   `long:"pool" default:"shell" description:"Pool to use"`
	Args []string `positional-args:"yes"`
}) error {
	opt := new(shell.ShellOptions)

	if len(opts.Args) > 0 {
		opt.SetCommand(opts.Args)
	}

	if opts.Pool != "" {
		opt.SetPool(opts.Pool)
	}

	winCh := make(chan os.Signal, 1)
	winUpdates := make(chan *shell.WindowSize, 1)

	var (
		in  io.Reader
		out io.Writer
	)

	if con := console.Current(); con != nil {
		in = con
		out = con

		if csz, err := con.Size(); err == nil {
			ws := new(shell.WindowSize)
			ws.SetHeight(int32(csz.Height))
			ws.SetWidth(int32(csz.Width))
			opt.SetWin_size(ws)
		}

		defer con.Reset()
		con.SetRaw()

		signal.Notify(winCh, unix.SIGWINCH)
		defer signal.Stop(winCh)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-winCh:
					csz, err := con.Size()
					if err != nil {
						ctx.Log.Error("failed to get console size", "error", err)
						continue
					}

					ws := new(shell.WindowSize)
					ws.SetHeight(int32(csz.Height))
					ws.SetWidth(int32(csz.Width))

					winUpdates <- ws
				}
			}
		}()
	} else {
		in = os.Stdin
		out = os.Stdout
	}

	cl, err := ctx.RPCClient("shell")
	if err != nil {
		return err
	}

	sc := shell.ShellAccessClient{Client: cl}

	input := stream.ServeReader(ctx, in)
	output := stream.ServeWriter(ctx, out)

	winUS := stream.ChanReader(winUpdates)

	results, err := sc.Open(ctx, opts.App, opt, input, output, winUS)
	if err != nil {
		return err
	}

	status := results.Status()
	ctx.SetExitCode(int(status))

	return nil
}
