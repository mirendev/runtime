package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/lab47/lsvd/logger"
	"miren.dev/runtime/lsvd/lve"
	"miren.dev/runtime/lsvd/lve/cli"
)

func main() {
	level := logger.Info

	if os.Getenv("LSVD_DEBUG") != "" {
		level = logger.Debug
	}

	log := slog.Default()

	log.Debug("log level configured", "level", level)

	c, err := cli.NewCLI(log, os.Args[1:])
	if err != nil {
		log.Error("error creating CLI", "error", err)
		os.Exit(1)
		return
	}

	code, err := c.Run()
	if err != nil {
		log.Error("error running CLI", "error", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func oldmain() {
	log := slog.Default()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	co := lve.CommonOptions{
		WorkDir:  pwd,
		CPUs:     2,
		MemoryMB: 512,
		UserNat:  true,
		MacVtap:  "macvtap1",

		LSVDSocket: "./disk.sock",
		LSVDVolume: "ubuntu",

		LNFSocket: "./lnf.sock",
		HostMount: pwd,
	}

	var q lve.QemuInstance

	log.Info("boot qemu")

	q.Start(ctx, log, &co)
}
