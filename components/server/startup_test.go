//go:build linux

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

func TestStartupGraphValidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)
	runtime := &Runtime{graph: boot.NewGraph()}
	components := newStartup(runtime, StartOptions{
		Log:     testLogger(),
		Context: groupCtx,
		Group:   group,
		Config:  serverconfig.DefaultConfig(),
	})

	if err := components.addComponents(); err != nil {
		t.Fatalf("addComponents() error = %v", err)
	}
	if err := runtime.graph.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
