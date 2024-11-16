package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
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
	f, err := os.OpenFile("/tmp/log-debug", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	fmt.Fprintf(f, "starting: %+v %#v\n", os.Args, os.Environ())

	flag.Parse()

	fmt.Fprintf(f, "%q %q\n", *fDB, *fEnt)

	var lw observability.PersistentLogWriter
	lw.DB = clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{*fDB},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		DialTimeout: time.Second * 30,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Debug: true,
	})

	logging.Run(func(ctx context.Context, cfg *logging.Config, ready func() error) error {
		var wg sync.WaitGroup

		wg.Add(2)
		go func() {
			defer wg.Done()

			br := bufio.NewReader(cfg.Stderr)
			var ent LogEntry

			for {
				line, err := br.ReadString('\n')
				if err != nil {
					fmt.Fprintf(f, "err: %v\n", err)
					return
				}

				line = strings.TrimRight(line, "\t\n\r")

				fmt.Fprintln(f, "stderr")
				ent.Line = line
				err = lw.WriteEntry(cfg.ID, line)
				if err != nil {
					fmt.Fprintf(f, "err: %v\n", err)
					return
				}
			}
		}()

		go func() {
			defer wg.Done()

			br := bufio.NewReader(cfg.Stdout)

			var ent LogEntry
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					fmt.Fprintf(f, "err: %v\n", err)
					return
				}

				line = strings.TrimRight(line, "\t\n\r")

				fmt.Fprintln(f, "stdout")
				ent.Line = line
				err = lw.WriteEntry(cfg.ID, line)
				if err != nil {
					fmt.Fprintf(f, "err: %v\n", err)
					return
				}
			}
		}()

		err = ready()
		if err != nil {
			return err
		}

		fmt.Println("waiting")
		wg.Wait()

		return nil
	})
}
