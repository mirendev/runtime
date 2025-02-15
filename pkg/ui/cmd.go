package ui

import (
	"bufio"
	"fmt"
	"os/exec"
	"sync"
)

const Indent = "  │"

func Run(prefix string, cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	brstdout := bufio.NewReader(stdout)
	brstderr := bufio.NewReader(stderr)

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()

		for {
			line, err := brstdout.ReadString('\n')
			if err != nil {
				break
			}

			fmt.Print(prefix + string(line))
		}
	}()

	go func() {
		defer wg.Done()

		for {
			line, err := brstderr.ReadString('\n')
			if err != nil {
				break
			}

			fmt.Print(prefix + string(line))
		}
	}()

	return cmd.Run()
}
