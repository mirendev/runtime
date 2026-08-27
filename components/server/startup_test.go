//go:build linux

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

func TestStartupGraphValidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)
	runtime := &Runtime{graph: readiness.NewGraph()}
	boot := newStartup(runtime, StartOptions{
		Log:     testLogger(),
		Context: groupCtx,
		Group:   group,
		Config:  serverconfig.DefaultConfig(),
	})

	if err := boot.addComponents(); err != nil {
		t.Fatalf("addComponents() error = %v", err)
	}
	if err := boot.declareConditions(); err != nil {
		t.Fatalf("declareConditions() error = %v", err)
	}
	if err := runtime.graph.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
