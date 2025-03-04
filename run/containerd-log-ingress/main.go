package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/containerd/containerd/runtime/v2/logging"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rotatinglog"
)

var (
	fDB  = flag.String("d", "", "db")
	fEnt = flag.String("e", "", "ent")
	fDir = flag.String("l", "", "log")
)

type LogEntry struct {
	Line string `cbor:"line"`
}

func main() {
	var lf io.Writer = io.Discard

	flag.Parse()

	if *fDir != "" {
		rl, err := rotatinglog.Open(filepath.Join(*fDir, "log"), 10, 10)
		if err == nil {
			defer rl.Close()
			lf = rl

			fmt.Fprintf(lf, "%s: restart %+v\n", time.Now().Format(time.RFC3339), os.Args)
		}
	}

	entity := *fEnt

	var lw observability.PersistentLogWriter
	lw.DB = clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{*fDB},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "default",
		},
		DialTimeout: time.Second * 30,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Debug: false,
	})

	logging.Run(func(ctx context.Context, cfg *logging.Config, ready func() error) error {
		var wg sync.WaitGroup

		wg.Add(2)
		go func() {
			defer wg.Done()

			br := bufio.NewReader(cfg.Stderr)

			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}

				ts := time.Now()

				line = strings.TrimRight(line, "\t\n\r")

				stream := observability.Stderr

				if strings.HasPrefix(line, "!USER ") {
					line = strings.TrimPrefix(line, "!USER ")
					stream = observability.UserOOB
				} else if strings.HasPrefix(line, "!ERROR ") {
					line = strings.TrimPrefix(line, "!ERROR ")
					stream = observability.Error
				}

				fmt.Fprintf(lf, "%s: [%s] %s\n", ts.Format(time.RFC3339), stream, line)

				err = lw.WriteEntry(entity, observability.LogEntry{
					Timestamp: ts,
					Stream:    stream,
					Body:      line,
				})
				if err != nil {
					fmt.Fprintf(lf, "%s: error: %v\n", ts.Format(time.RFC3339), err)
				}
			}
		}()

		go func() {
			defer wg.Done()

			br := bufio.NewReader(cfg.Stdout)

			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}

				ts := time.Now()

				line = strings.TrimRight(line, "\t\n\r")

				stream := observability.Stdout

				if strings.HasPrefix(line, "!USER ") {
					line = strings.TrimPrefix(line, "!USER ")
					stream = observability.UserOOB
				} else if strings.HasPrefix(line, "!ERROR ") {
					line = strings.TrimPrefix(line, "!ERROR ")
					stream = observability.Error
				}

				fmt.Fprintf(lf, "%s: [%s] %s\n", ts.Format(time.RFC3339), stream, line)

				err = lw.WriteEntry(entity, observability.LogEntry{
					Timestamp: ts,
					Stream:    stream,
					Body:      line,
				})
				if err != nil {
					fmt.Fprintf(lf, "%s error: %v\n", ts.Format(time.RFC3339), err)
				}
			}
		}()

		err := ready()
		if err != nil {
			return err
		}

		wg.Wait()

		return nil
	})
}
