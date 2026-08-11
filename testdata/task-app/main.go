package main

import (
	"fmt"
	"os"
	"time"
)

// A tiny program with one job per task, so blackbox tests can assert on exit
// codes and output without needing a real workload.
func main() {
	mode := "hello"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "hello":
		fmt.Println("hello from a task")
	case "env":
		// Echoes a declared task env var so a test can tell whether
		// [tasks.<name>.env] actually reached the container.
		fmt.Printf("TASK_SECRET=%s\n", os.Getenv("TASK_SECRET"))
	case "fail":
		fmt.Fprintln(os.Stderr, "task failed on purpose")
		os.Exit(3)
	case "slow":
		fmt.Println("starting slow task")
		time.Sleep(10 * time.Minute)
	case "brief":
		// Long enough that a client returning early is unambiguous, short
		// enough to sit in a test. The exit code is distinctive so a premature
		// return -- which reports zero -- cannot be mistaken for success.
		fmt.Println("starting brief task")
		time.Sleep(3 * time.Second)
		fmt.Println("brief task done")
		os.Exit(7)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", mode)
		os.Exit(64)
	}
}
