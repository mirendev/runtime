package commands

import (
	"miren.dev/runtime/server"
)

func Server(ctx *Context, opts struct {
	Port            int    `short:"p" long:"port" description:"Port to listen on" default:"8443" asm:"server_port"`
	PostgresAddress string `long:"pg-addr" asm:"postgres-address"`
}) error {
	var server server.Server

	err := ctx.Server.Populate(&server)
	if err != nil {
		return err
	}

	err = server.Run(ctx.Context)
	return err
}
