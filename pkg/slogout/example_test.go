package slogout_test

import (
	"log/slog"
	"os"

	"miren.dev/runtime/pkg/slogout"
)

// ExampleWithLogger demonstrates how to use slogout.WithLogger to route
// container output through structured logging instead of stdout/stderr.
func ExampleWithLogger() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Create a CIO creator for etcd (which outputs JSON logs)
	etcdCreator := slogout.WithLogger(logger, "etcd", slogout.WithJSONParsing())

	// Create a CIO creator for ClickHouse (which has timestamp prefixes to ignore)
	clickhouseCreator := slogout.WithLogger(logger, "clickhouse", 
		slogout.WithIgnorePattern(`^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}\.\d+`))

	// These creators can now be used with containerd containers:
	// task, err := container.NewTask(ctx, etcdCreator)
	// task, err := container.NewTask(ctx, clickhouseCreator)
	// Instead of:
	// task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))

	_ = etcdCreator      // Use the creator with containerd
	_ = clickhouseCreator // Use the creator with containerd
}