package commands

import (
	"time"

	"miren.dev/runtime/api"
	"miren.dev/runtime/pkg/rpc/standard"
)

func Logs(ctx *Context, opts struct {
	AppCentric

	Last *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
}) error {
	cl, err := ctx.RPCClient("logs")
	if err != nil {
		return err
	}

	ac := api.LogsClient{Client: cl}

	typ := map[string]string{
		"stdout":   "S",
		"stderr":   "E",
		"error":    "ERR",
		"user-oob": "U",
	}

	var ts *standard.Timestamp

	if opts.Last != nil {
		ts = standard.ToTimestamp(time.Now().Add(-*opts.Last))
	} else {
		start := time.Now()
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		ts = standard.ToTimestamp(start)
	}

	for {
		res, err := ac.AppLogs(ctx, opts.App, ts, false)
		if err != nil {
			return err
		}

		logs := res.Logs()

		for _, l := range logs {
			ctx.Printf("%s %s: %s\n",
				typ[l.Stream()],
				standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05"),
				l.Line())
		}

		if len(logs) != 100 {
			break
		}

		ts = logs[len(logs)-1].Timestamp()
	}

	return nil
}
