package logs

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rpc/standard"
)

type Server struct {
	Log       *slog.Logger
	EC        *entityserver.Client
	LogReader *observability.LogReader
}

var _ app_v1alpha.Logs = &Server{}

func NewServer(log *slog.Logger, ec *entityserver.Client, lr *observability.LogReader) *Server {
	return &Server{
		Log:       log.With("module", "logserver"),
		EC:        ec,
		LogReader: lr,
	}
}

func (s *Server) AppLogs(ctx context.Context, state *app_v1alpha.LogsAppLogs) error {
	args := state.Args()

	var appRec core_v1alpha.App

	err := s.EC.Get(ctx, args.Application(), &appRec)
	if err != nil {
		s.Log.Error("failed to get app", "app", args.Application(), "err", err)
		return err
	}

	var opts []observability.LogReaderOption

	if args.HasFrom() {
		fromTime := standard.FromTimestamp(args.From())
		opts = append(opts, observability.WithFromTime(fromTime))
	}

	s.Log.Debug("reading logs", "app", appRec.EntityId().String(), "from", args.From())

	entries, err := s.LogReader.Read(ctx, appRec.EntityId().String(), opts...)
	if err != nil {
		s.Log.Error("failed to read logs", "app", appRec.EntityId().String(), "err", err)
		return err
	}

	var ret []*app_v1alpha.LogEntry

	for _, entry := range entries {
		le := &app_v1alpha.LogEntry{}
		le.SetTimestamp(standard.ToTimestamp(entry.Timestamp))
		le.SetLine(entry.Body)
		le.SetStream(string(entry.Stream))
		if source, ok := entry.Attributes["source"]; ok {
			le.SetSource(source)
		}

		ret = append(ret, le)
	}

	s.Log.Debug("returning logs", "lineCount", len(entries))

	state.Results().SetLogs(ret)

	return nil
}

func (s *Server) SandboxLogs(ctx context.Context, state *app_v1alpha.LogsSandboxLogs) error {
	args := state.Args()

	var opts []observability.LogReaderOption

	if args.HasFrom() {
		fromTime := standard.FromTimestamp(args.From())
		opts = append(opts, observability.WithFromTime(fromTime))
	}

	s.Log.Debug("reading logs", "sandbox", args.Sandbox(), "from", args.From())

	entries, err := s.LogReader.ReadBySandbox(ctx, args.Sandbox(), opts...)
	if err != nil {
		s.Log.Error("failed to read logs", "sandbox", args.Sandbox(), "err", err)
		return err
	}

	var ret []*app_v1alpha.LogEntry

	for _, entry := range entries {
		le := &app_v1alpha.LogEntry{}
		le.SetTimestamp(standard.ToTimestamp(entry.Timestamp))
		le.SetLine(entry.Body)
		le.SetStream(string(entry.Stream))
		if source, ok := entry.Attributes["source"]; ok {
			le.SetSource(source)
		}

		ret = append(ret, le)
	}

	s.Log.Debug("returning logs", "lineCount", len(entries))

	state.Results().SetLogs(ret)

	return nil
}
