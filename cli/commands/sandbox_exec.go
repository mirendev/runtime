package commands

import (
	"io"
	"os"
	"strings"

	"github.com/containerd/console"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func SandboxExec(ctx *Context, opts struct {
	Id     string `long:"id" description:"Sandbox ID" default:"miren-sandbox"`
	Server string `long:"server" description:"Server address to connect to" default:"localhost:8444"`

	Rest struct {
		Args []string
	} `positional-args:"yes"`
}) error {
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithLogger(ctx.Log))
	if err != nil {
		return err
	}

	client, err := rs.Connect(opts.Server, "dev.miren.runtime/exec")
	if err != nil {
		return err
	}

	sec := &exec_v1alpha.SandboxExecClient{Client: client}

	var (
		in  io.Reader
		out io.Writer
	)

	if con := console.Current(); con != nil {
		in = con
		out = con

		/*
			if csz, err := con.Size(); err == nil {
				ws := new(shell.WindowSize)
				ws.SetHeight(int32(csz.Height))
				ws.SetWidth(int32(csz.Width))
				opt.SetWinSize(ws)
			}
		*/

		defer con.Reset()
		con.SetRaw()

		/*
			signal.Notify(winCh, unix.SIGWINCH)
			defer signal.Stop(winCh)
		*/

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
					/*
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
					*/
				}
			}
		}()
	} else {
		in = os.Stdin
		out = os.Stdout
	}

	input := stream.ServeReader(ctx, in)
	output := stream.ServeWriter(ctx, out)

	res, err := sec.Exec(ctx, opts.Id, strings.Join(opts.Rest.Args, " "), input, output)
	if err != nil {
		return err
	}

	ctx.SetExitCode(int(res.Code()))
	return nil
}
