package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/containerd/containerd/runtime/v2/logging"
	"miren.dev/runtime/observability"
)

var (
	fDB  = flag.String("d", "", "db")
	fEnt = flag.String("e", "", "ent")
)

type LogEntry struct {
	Line string `cbor:"line"`
}

func main() {
	var f io.Writer

	if os.Getenv("LOG_INGRESS_DEBUG") != "" {
		lf, err := os.OpenFile("/tmp/log-debug", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			panic(err)
		}

		defer lf.Close()
		f = lf
	} else {
		f = io.Discard
	}

	fmt.Fprintf(f, "starting: %+v %#v\n", os.Args, os.Environ())

	flag.Parse()

	fmt.Fprintf(f, "%q %q\n", *fDB, *fEnt)

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
		Debug: true,
		Debugf: func(format string, v ...interface{}) {
			fmt.Fprintf(f, format, v...)
		},
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

				line = strings.TrimRight(line, "\t\n\r")

				fmt.Fprintf(f, "stderr: %q\n", line)

				stream := observability.Stderr

				if strings.HasPrefix(line, "!USER ") {
					line = strings.TrimPrefix(line, "!USER ")
					stream = observability.UserOOB
				} else if strings.HasPrefix(line, "!ERROR ") {
					line = strings.TrimPrefix(line, "!ERROR ")
					stream = observability.Error
				}

				err = lw.WriteEntry(entity, observability.LogEntry{
					Timestamp: time.Now(),
					Stream:    stream,
					Body:      line,
				})
				if err != nil {
					fmt.Fprintf(f, "error: %v\n", err)
					return
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

				line = strings.TrimRight(line, "\t\n\r")

				fmt.Fprintf(f, "stdout: %q\n", line)
				stream := observability.Stdout

				if strings.HasPrefix(line, "!USER ") {
					line = strings.TrimPrefix(line, "!USER ")
					stream = observability.UserOOB
				} else if strings.HasPrefix(line, "!ERROR ") {
					line = strings.TrimPrefix(line, "!ERROR ")
					stream = observability.Error
				}

				err = lw.WriteEntry(entity, observability.LogEntry{
					Timestamp: time.Now(),
					Stream:    stream,
					Body:      line,
				})
				if err != nil {
					fmt.Fprintf(f, "error: %v\n", err)
					return
				}
			}
		}()

		err := ready()
		if err != nil {
			return err
		}

		fmt.Fprintln(f, "waiting")
		wg.Wait()

		return nil
	})
}
