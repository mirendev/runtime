//go:build linux
// +build linux

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/pkg/containerdx"
)

func Server(ctx *Context, opts struct {
	Port               int    `short:"p" long:"port" description:"Port to listen on" default:"8443" asm:"server_port"`
	HTTPAddress        string `long:"http-addr" description:"Address to listen on for HTTP" default:":80" asm:"http-address"`
	ClickhouseAddress  string `long:"clickhouse-addr" asm:"clickhouse-address" type:"address"`
	TempDir            string `long:"temp-dir" description:"Directory to store temporary files" asm:"tempdir" type:"path"`
	DataPath           string `long:"data-path" description:"Path to store data" asm:"data-path" type:"path"`
	RunscBinary        string `long:"runsc-binary" description:"Path to the runsc binary" asm:"runsc_binary" type:"path"`
	Id                 string `long:"id" description:"Unique identifier for the server" asm:"server-id"`
	Local              string `long:"local" description:"Run the server locally" asm:"local-path"`
	RunContainerd      bool   `long:"run-containerd" description:"Run containerd in the background"`
	RequireClientCerts bool   `long:"require-client-certs" description:"Require client certificates for all connections" asm:"require-client-certs"`
}) error {
	if opts.RunContainerd {
		ctx.Log.Info("starting containerd")
		cmd := exec.CommandContext(ctx, "containerd")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			return err
		}

		go func() {
			err := cmd.Wait()
			if err != nil {
				ctx.Log.Error("containerd exited", "error", err)
			}
		}()

		// Wait for containerd to start

		for {
			cl, err := containerd.New(containerdx.DefaultSocket)
			if err == nil {
				_, err = cl.Server(ctx)
				if err == nil {
					cl.Close()
					break
				}
			}

			time.Sleep(100 * time.Millisecond)
		}
	}
	/*

		var server server.Server

		err := ctx.Server.Populate(&server)
		if err != nil {
			return fmt.Errorf("populating server: %w", err)
		}

		err = server.Setup(ctx)
		if err != nil {
			return fmt.Errorf("setting up server: %w", err)
		}

		if opts.Local != "" {
			dir := filepath.Dir(opts.Local)

			err := os.MkdirAll(dir, 0755)
			if err != nil {
				return err
			}

			path := filepath.Join(dir, "clientconfig.yaml")

			ctx.Log.Info("writing config file for local server", "path", path)

			cfg, err := server.LocalConfig()
			if err != nil {
				return err
			}

			err = cfg.SaveTo(path)
			if err != nil {
				return err
			}
		}

		err = server.Run(ctx.Context)
		return err
	*/
	return fmt.Errorf("server not implemented yet")
}
