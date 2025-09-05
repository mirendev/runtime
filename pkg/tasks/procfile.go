package tasks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Proc struct {
	Name    string
	Command []string

	ExitWhenDone bool
}

type Procfile struct {
	Proceses []*Proc
}

func ParseFile(path string) (*Procfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	br := bufio.NewReader(f)

	var procs []*Proc

	for {
		line, err := br.ReadBytes('\n')

		// Process the line if we got any data, even if there was an error
		if len(line) > 0 {
			line = bytes.TrimSpace(line)

			// Skip empty lines
			if len(line) == 0 {
				if err != nil {
					break
				}
				continue
			}

			name, command, found := bytes.Cut(line, []byte(":"))
			if !found {
				return nil, fmt.Errorf("invalid line: %s", string(line))
			}

			procs = append(procs, &Proc{
				Name:    string(name),
				Command: []string{"sh", "-c", strings.TrimSpace(string(command))},
			})
		}

		// If we hit EOF or another error, we're done
		if err != nil {
			// For non-EOF errors, we might want to return the error
			// but for now, we'll just stop parsing regardless of error type
			break
		}
	}

	return &Procfile{Proceses: procs}, nil
}

func Run(ctx context.Context, pf *Procfile) error {
	var (
		width   int
		waitFor *exec.Cmd
	)

	for _, proc := range pf.Proceses {
		if len(proc.Name) > width {
			width = len(proc.Name)
		}
	}

	for _, proc := range pf.Proceses {
		cmd, err := runProc(ctx, proc, width)
		if err != nil {
			return err
		}

		if proc.ExitWhenDone {
			waitFor = cmd
		}
	}

	if waitFor == nil {
		<-ctx.Done()
		return ctx.Err()
	}

	return waitFor.Wait()
}

func runProc(ctx context.Context, pr *Proc, width int) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, pr.Command[0], pr.Command[1:]...)

	outr, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	errr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(pr.Name)

	for i := 0; i < width-len(pr.Name); i++ {
		buf.WriteString(" ")
	}

	buf.WriteString(" | ")

	prefix := buf.Bytes()

	os.Stdout.Write(prefix)
	os.Stdout.Write([]byte("starting...\n"))

	go func() {
		defer outr.Close()
		sc := bufio.NewReader(outr)

		for {
			line, err := sc.ReadBytes('\n')
			if err != nil {
				break
			}

			os.Stdout.Write(prefix)
			os.Stdout.Write(line)
		}
	}()

	go func() {
		defer errr.Close()
		sc := bufio.NewReader(errr)

		for {
			line, err := sc.ReadBytes('\n')
			if err != nil {
				break
			}

			os.Stdout.Write(prefix)
			os.Stdout.Write(line)
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	go func() {
		err = cmd.Wait()
		if err != nil {
			os.Stdout.Write(prefix)
			fmt.Printf("error: %s\n", err)
		}
	}()

	return cmd, nil
}
