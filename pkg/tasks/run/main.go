package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/tasks"
)

var (
	fProcfile = pflag.StringP("file", "f", "Procfile", "path to Procfile")
	fPath     = pflag.StringArrayP("path", "p", nil, "entries to add to PATH")
)

func main() {
	pflag.Parse()

	// Parse Procfile
	procfile, err := tasks.ParseFile(*fProcfile)
	if err != nil {
		log.Fatalf("Error parsing Procfile: %v", err)
	}

	ctx := context.Background()

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, os.Kill, unix.SIGTERM)
	defer cancel()

	os.Setenv("PATH", os.Getenv("PATH")+":"+strings.Join(*fPath, ":"))

	tmpPath, err := os.MkdirTemp("", "task")
	if err != nil {
		fmt.Printf("error creating temp dir: %s\n", err)
		os.Exit(1)
	}

	defer os.RemoveAll(tmpPath)

	os.Setenv("WORKTMP", tmpPath)

	if pflag.NArg() == 0 {
		procfile.Proceses = append(procfile.Proceses, &tasks.Proc{
			Name:         "command",
			Command:      pflag.Args(),
			ExitWhenDone: true,
		})
	}

	err = tasks.Run(ctx, procfile)
	if err != nil {
		fmt.Printf("error running procfile: %s\n", err)
		os.Exit(1)
	}
}
