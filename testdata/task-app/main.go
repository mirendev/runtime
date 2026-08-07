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
	case "fail":
		fmt.Fprintln(os.Stderr, "task failed on purpose")
		os.Exit(3)
	case "slow":
		fmt.Println("starting slow task")
		time.Sleep(10 * time.Minute)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", mode)
		os.Exit(64)
	}
}
