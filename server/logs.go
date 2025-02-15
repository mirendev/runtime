package server

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api"
	"miren.dev/runtime/app"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/rpc/standard"
)

type RPCLogs struct {
	Log *slog.Logger
	App *app.AppAccess

	LogReader *observability.LogReader
}

var _ api.Logs = &RPCLogs{}

func (s *RPCLogs) AppLogs(ctx context.Context, req *api.LogsAppLogs) error {
	args := req.Args()

	ac, err := s.App.LoadApp(ctx, args.Application())
	if err != nil {
		return err
	}

	var opts []observability.LogReaderOption

	if args.HasFrom() {
		opts = append(opts, observability.WithFromTime(
			standard.FromTimestamp(args.From())))
	}

	s.Log.Debug("reading logs", "app", ac.Xid, "from", args.From())

	entries, err := s.LogReader.Read(ctx, ac.Xid, opts...)
	if err != nil {
		return err
	}

	var ret []*api.LogEntry

	for _, entry := range entries {
		var le api.LogEntry

		le.SetTimestamp(standard.ToTimestamp(entry.Timestamp))
		le.SetLine(entry.Body)
		le.SetStream(string(entry.Stream))

		ret = append(ret, &le)
	}

	req.Results().SetLogs(ret)

	return nil
}

var _ = autoreg.Register[RPCLogs]()
