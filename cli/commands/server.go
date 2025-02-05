package commands

import (
	"miren.dev/runtime/server"
)

func Server(ctx *Context, opts struct {
	Port              int    `short:"p" long:"port" description:"Port to listen on" default:"8443" asm:"server_port"`
	PostgresAddress   string `long:"pg-addr" asm:"postgres-address" type:"address"`
	ClickhouseAddress string `long:"clickhouse-addr" asm:"clickhouse-address" type:"address"`
	TempDir           string `long:"temp-dir" description:"Directory to store temporary files" asm:"tempdir" type:"path"`
	RunscBinary       string `long:"runsc-binary" description:"Path to the runsc binary" asm:"runsc_binary" type:"path"`
	Id                string `long:"id" description:"Unique identifier for the server" asm:"server-id"`
	Local             string `long:"local" description:"Run the server locally" asm:"local-path"`
}) error {
	var server server.Server

	err := ctx.Server.Populate(&server)
	if err != nil {
		return err
	}

	err = server.Run(ctx.Context)
	return err
}
