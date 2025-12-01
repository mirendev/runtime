package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"miren.dev/runtime/lsvd"
)

var (
	flagSeed       = flag.Int64("seed", 0, "Random seed (0 = random)")
	flagOps        = flag.Int("ops", 10000, "Number of operations to run")
	flagDuration   = flag.Duration("duration", 0, "Run for this duration (overrides -ops)")
	flagConfig     = flag.String("config", "", "Base64-encoded config for reproduction")
	flagVariation  = flag.String("variation", "", "Test variation: default, no-close-reopen, high-overlap, heavy-zero, durability, boundaries")
	flagVerify     = flag.Int("verify", 1000, "Verify every N operations")
	flagMaxLBA     = flag.Int64("max-lba", 100000, "Maximum LBA to use")
	flagMaxBlocks  = flag.Int("max-blocks", 64, "Maximum blocks per operation")
	flagDir        = flag.String("dir", "", "Directory for test data (default: temp dir)")
	flagLoop       = flag.Bool("loop", false, "Run continuously until failure")
	flagQuiet      = flag.Bool("quiet", false, "Reduce output verbosity")
	flagDebug      = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Parse()

	level := slog.LevelInfo
	if *flagQuiet {
		level = slog.LevelError
	} else if *flagDebug {
		level = slog.LevelDebug
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nReceived interrupt, stopping after current operation...")
		cancel()
	}()

	// Determine configuration
	var cfg lsvd.TortureConfig

	if *flagConfig != "" {
		var err error
		cfg, err = lsvd.DecodeTortureConfig(*flagConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode config: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Using config: seed=%d ops=%d weights=%+v\n",
			cfg.Seed, cfg.Operations, cfg.Weights)
	} else {
		cfg = lsvd.DefaultTortureConfig
		cfg.Operations = *flagOps
		cfg.VerifyEvery = *flagVerify
		cfg.MaxLBA = lsvd.LBA(*flagMaxLBA)
		cfg.MaxBlocks = uint32(*flagMaxBlocks)

		if *flagSeed != 0 {
			cfg.Seed = *flagSeed
		} else {
			cfg.Seed = rand.Int63()
		}

		// Apply variation if specified
		if *flagVariation != "" {
			found := false
			for _, v := range lsvd.DefaultTortureVariations() {
				if v.Name == *flagVariation {
					cfg.Weights = v.Weights
					cfg.OverlapProbability = v.Overlap
					cfg.MaxLBA = v.MaxLBA
					if *flagVariation == "boundaries" {
						cfg.MaxBlocks = 100
					}
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Unknown variation: %s (available: default, no-close-reopen, high-overlap, heavy-zero, durability, boundaries)\n", *flagVariation)
				os.Exit(1)
			}
		}
	}

	var err error
	if *flagDuration > 0 {
		err = runTortureTimed(ctx, log, *flagDir, cfg, *flagDuration, *flagLoop, *flagQuiet)
	} else if *flagLoop {
		err = runTortureLoop(ctx, log, *flagDir, cfg, *flagQuiet)
	} else {
		err = runTortureSingle(ctx, log, *flagDir, cfg, *flagQuiet)
	}

	if err != nil {
		os.Exit(1)
	}
}

func runTortureSingle(ctx context.Context, log *slog.Logger, dir string, cfg lsvd.TortureConfig, quiet bool) error {
	fmt.Fprintf(os.Stderr, "Starting torture test with seed: %d\n", cfg.Seed)
	fmt.Fprintf(os.Stderr, "Reproduce with: torture -config %s\n", lsvd.EncodeTortureConfig(cfg))

	runner, err := lsvd.NewTortureRunner(ctx, log, dir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
		return err
	}
	defer runner.Cleanup()

	result := runner.Run()

	if !result.Success {
		runner.DumpHistory(50)
		fmt.Fprintf(os.Stderr, "\nTorture test FAILED at operation %d\n", result.Operations)
		fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
		fmt.Fprintf(os.Stderr, "Reproduce with: torture -config %s\n", lsvd.EncodeTortureConfig(cfg))
		return result.Error
	}

	fmt.Fprintf(os.Stderr, "\nTorture test PASSED: %d operations, %d unique LBAs\n",
		result.Operations, result.LBAsUsed)
	return nil
}

func runTortureLoop(ctx context.Context, log *slog.Logger, dir string, cfg lsvd.TortureConfig, quiet bool) error {
	iteration := 0
	variations := lsvd.DefaultTortureVariations()

	fmt.Fprintf(os.Stderr, "Starting continuous torture test (Ctrl+C to stop)\n")

	go func() {
		<-ctx.Done()
		fmt.Fprintf(os.Stderr, "\nStopping torture test after current iteration...\n")
	}()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\nStopped after %d successful iterations\n", iteration)
			return nil
		default:
		}

		variation := variations[iteration%len(variations)]
		cfg.Seed = rand.Int63()
		cfg.Weights = variation.Weights
		cfg.OverlapProbability = variation.Overlap
		if variation.MaxLBA != 0 {
			cfg.MaxLBA = variation.MaxLBA
		}
		if variation.Name == "boundaries" {
			cfg.MaxBlocks = 100
		}

		fmt.Fprintf(os.Stderr, "[%d] Running variation '%s' with seed %d\n",
			iteration+1, variation.Name, cfg.Seed)

		runner, err := lsvd.NewTortureRunner(ctx, log, "", cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
			return err
		}

		result := runner.Run()
		runner.Cleanup()

		if !result.Success {
			runner.DumpHistory(50)
			fmt.Fprintf(os.Stderr, "\nTorture test FAILED on iteration %d, variation '%s'\n",
				iteration+1, variation.Name)
			fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
			fmt.Fprintf(os.Stderr, "Reproduce with: torture -config %s\n", lsvd.EncodeTortureConfig(cfg))
			return result.Error
		}

		fmt.Fprintf(os.Stderr, "[%d] Passed: %d operations\n", iteration+1, result.Operations)
		iteration++
	}
}

func runTortureTimed(ctx context.Context, log *slog.Logger, dir string, cfg lsvd.TortureConfig, duration time.Duration, loop bool, quiet bool) error {
	deadline := time.Now().Add(duration)
	variations := lsvd.DefaultTortureVariations()
	iteration := 0
	opsPerRun := 3000

	fmt.Fprintf(os.Stderr, "Running torture tests for %v (until %v)\n",
		duration, deadline.Format(time.RFC3339))

	for {
		if !loop && time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "\nTimed torture test completed: %d iterations passed in %v\n",
				iteration, duration)
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\nStopped after %d successful iterations\n", iteration)
			return nil
		default:
		}

		variation := variations[iteration%len(variations)]
		cfg.Seed = rand.Int63()
		cfg.Operations = opsPerRun
		cfg.Weights = variation.Weights
		cfg.OverlapProbability = variation.Overlap
		if variation.MaxLBA != 0 {
			cfg.MaxLBA = variation.MaxLBA
		}
		if variation.Name == "boundaries" {
			cfg.MaxBlocks = 100
		}
		cfg.VerifyEvery = 300

		remaining := time.Until(deadline)
		if remaining < 0 && !loop {
			remaining = 0
		}

		fmt.Fprintf(os.Stderr, "[%d] Running variation '%s' with seed %d",
			iteration+1, variation.Name, cfg.Seed)
		if !loop {
			fmt.Fprintf(os.Stderr, " (remaining: %v)", remaining.Round(time.Second))
		}
		fmt.Fprintln(os.Stderr)

		runner, err := lsvd.NewTortureRunner(ctx, log, "", cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
			return err
		}

		result := runner.Run()
		runner.Cleanup()

		if !result.Success {
			runner.DumpHistory(50)
			fmt.Fprintf(os.Stderr, "\nTorture test FAILED on iteration %d, variation '%s'\n",
				iteration+1, variation.Name)
			fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
			fmt.Fprintf(os.Stderr, "Reproduce with: torture -config %s\n", lsvd.EncodeTortureConfig(cfg))
			return result.Error
		}

		fmt.Fprintf(os.Stderr, "[%d] Passed: %d operations\n", iteration+1, result.Operations)
		iteration++
	}
}
