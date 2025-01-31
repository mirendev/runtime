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

	entity := *fEnt

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

			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}

				line = strings.TrimRight(line, "\t\n\r")

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
