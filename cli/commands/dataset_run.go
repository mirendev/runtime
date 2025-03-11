package commands

import (
	"net/http"

	"miren.dev/runtime/dataset"
	"miren.dev/runtime/pkg/rpc"
)

func DataSetServe(ctx *Context, opts struct {
	Addr     string `short:"a" long:"addr" help:"Address to bind to"`
	DataAddr string `short:"x" long:"data-addr" help:"Address to bind to"`
	DataDir  string `short:"d" long:"datadir" help:"Directory to store datasets"`
}) error {
	dsm, err := dataset.NewManager(ctx.Log, opts.DataDir, opts.DataAddr)
	if err != nil {
		return err
	}

	ss, err := rpc.NewState(ctx, rpc.WithBindAddr(opts.Addr))
	if err != nil {
		return err
	}

	serv := ss.Server()
	serv.ExposeValue("dataset", dataset.AdaptDataSets(dsm))

	ctx.Log.Info("Serving dataset manager", "addr", opts.Addr)

	go http.ListenAndServe(opts.DataAddr, dsm)

	<-ctx.Done()

	return ctx.Err()
}
