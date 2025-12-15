package commands

import (
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/containerd/console"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/rpc/stream"
)

func SandboxExec(ctx *Context, opts struct {
	ConfigCentric
	Id string `short:"i" long:"id" description:"Sandbox ID" default:"miren-sandbox"`

	Args []string `rest:"true"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/exec")
	if err != nil {
		return err
	}

	sec := exec_v1alpha.NewSandboxExecClient(cl)

	winCh := make(chan os.Signal, 1)
	winUpdates := make(chan *exec_v1alpha.WindowSize, 1)

	opt := new(exec_v1alpha.ShellOptions)

	var (
		in  io.Reader
		out io.Writer
	)

	opt.SetCommand(opts.Args)

	if con := console.Current(); con != nil {
		in = con
		out = con

		if csz, err := con.Size(); err == nil {
			ws := new(exec_v1alpha.WindowSize)
			ws.SetHeight(int32(csz.Height))
			ws.SetWidth(int32(csz.Width))
			opt.SetWinSize(ws)
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

					ws := new(exec_v1alpha.WindowSize)
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

	input := stream.ServeReader(ctx, in)
	output := stream.ServeWriter(ctx, out)
	winUS := stream.ChanReader(winUpdates)

	res, err := sec.Exec(
		ctx,
		"id", opts.Id,
		strings.Join(opts.Args, " "),
		opt,
		input, output,
		winUS,
	)
	if err != nil {
		return err
	}

	ctx.SetExitCode(int(res.Code()))
	return nil
}
