package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/containerd/containerd/runtime/v2/logging"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rotatinglog"
)

var (
	fDB   = flag.String("d", "", "db")
	fEnt  = flag.String("e", "", "ent")
	fAttr = flag.String("a", "", "attrs")
	fDir  = flag.String("l", "", "log")
)

type LogEntry struct {
	Line string `cbor:"line"`
}

var traceIdRegx = regexp.MustCompile(`trace_id"?\s*[=:]\s*\"?(\w+)`)

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

	attrs := map[string]string{}

	if *fAttr != "" {
		entries := strings.Split(*fAttr, ",")
		for _, entry := range entries {
			key, value, ok := strings.Cut(entry, "=")
			if !ok {
				fmt.Fprintf(lf, "%s: error: invalid attribute %q\n", time.Now().Format(time.RFC3339), entry)
				os.Exit(1)
			}
			attrs[key] = value
		}
	}

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

	writeEntry := func(line string, stream observability.LogStream) {
		ts := time.Now()

		line = strings.TrimRight(line, "\t\n\r")

		if strings.HasPrefix(line, "!USER ") {
			line = strings.TrimPrefix(line, "!USER ")
			stream = observability.UserOOB
		} else if strings.HasPrefix(line, "!ERROR ") {
			line = strings.TrimPrefix(line, "!ERROR ")
			stream = observability.Error
		}

		fmt.Fprintf(lf, "%s: [%s] %s\n", ts.Format(time.RFC3339), stream, line)

		traceId := ""
		if matches := traceIdRegx.FindStringSubmatch(line); len(matches) > 1 {
			traceId = matches[1]
		}

		err := lw.WriteEntry(entity, observability.LogEntry{
			Timestamp:  ts,
			Stream:     stream,
			Body:       line,
			TraceID:    traceId,
			Attributes: attrs,
		})
		if err != nil {
			fmt.Fprintf(lf, "%s: error: %v\n", ts.Format(time.RFC3339), err)
		}

	}

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

				writeEntry(line, observability.Stderr)
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

				writeEntry(line, observability.Stdout)
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
