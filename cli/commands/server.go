package commands

import (
	"miren.dev/runtime/server"
)

func Server(ctx *Context, opts struct {
	Port              int    `short:"p" long:"port" description:"Port to listen on" default:"8443" asm:"server_port"`
	PostgresAddress   string `long:"pg-addr" asm:"postgres-address"`
	ClickhouseAddress string `long:"clickhouse-addr" asm:"clickhouse-address"`
	TempDir           string `long:"temp-dir" description:"Directory to store temporary files" asm:"tempdir"`
	RunscBinary       string `long:"runsc-binary" description:"Path to the runsc binary" asm:"runsc_binary"`
}) error {
	var server server.Server

	err := ctx.Server.Populate(&server)
	if err != nil {
		return err
	}

	err = server.Run(ctx.Context)
	return err
}
