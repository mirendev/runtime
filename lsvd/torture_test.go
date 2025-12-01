package lsvd

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"testing"
)

// TestTortureQuick runs a quick torture test suitable for CI
func TestTortureQuick(t *testing.T) {
	cfg := DefaultTortureConfig
	cfg.Seed = rand.Int63()
	cfg.Operations = 1000
	cfg.VerifyEvery = 100

	t.Logf("Torture test starting with seed: %d", cfg.Seed)
	t.Logf("Reproduce with: go run ./lsvd/cmd/torture -config %s", EncodeTortureConfig(cfg))

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	runner, err := NewTortureRunner(context.Background(), log, t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	defer runner.Cleanup()

	result := runner.Run()

	if !result.Success {
		runner.DumpHistory(50)
		t.Fatalf("Torture test failed: %v", result.Error)
	}

	t.Logf("Torture test passed: %d operations, %d unique LBAs written",
		result.Operations, result.LBAsUsed)
}
