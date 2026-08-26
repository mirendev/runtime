//go:build linux

package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestInitialSweepGateReleasesAfterCleanControllerPass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gate := newInitialSweepGate()
	controller := deploymentattempts.New(log, inmem.Store, inmem.EAC, gate.Complete)

	waitResult := make(chan error, 1)
	go func() { waitResult <- gate.Wait(ctx) }()

	for range 3 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-waitResult:
		t.Fatal("initial entity sync released before reconciliation completed")
	default:
	}

	require.NoError(t, controller.Step(ctx))
	require.NoError(t, <-waitResult)
	require.NoError(t, gate.Wait(ctx), "a waiter arriving after completion returns immediately")

	gate.Complete()
	gate.Complete()
}

func TestInitialSweepGateWaitHonorsContext(t *testing.T) {
	gate := newInitialSweepGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, gate.Wait(ctx), context.Canceled)

	gate.Complete()
	require.NoError(t, gate.Wait(context.Background()))
}
