package logs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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

func toLogEntry(entry observability.LogEntry) *app_v1alpha.LogEntry {
	le := &app_v1alpha.LogEntry{}
	le.SetTimestamp(standard.ToTimestamp(entry.Timestamp))
	le.SetLine(entry.Body)
	le.SetStream(string(entry.Stream))
	if source, ok := entry.Attributes["source"]; ok {
		le.SetSource(source)
	}
	return le
}

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
		ret = append(ret, toLogEntry(entry))
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
		ret = append(ret, toLogEntry(entry))
	}

	s.Log.Debug("returning logs", "lineCount", len(entries))

	state.Results().SetLogs(ret)

	return nil
}

func (s *Server) StreamLogs(ctx context.Context, state *app_v1alpha.LogsStreamLogs) error {
	args := state.Args()
	send := args.Logs()
	target := args.Target()

	var opts []observability.LogReaderOption
	if args.HasFrom() {
		fromTime := standard.FromTimestamp(args.From())
		opts = append(opts, observability.WithFromTime(fromTime))
	}

	// Build log target from RPC target
	var logTarget observability.LogTarget

	if target.HasSandbox() && target.Sandbox() != "" {
		logTarget.SandboxID = target.Sandbox()
		s.Log.Debug("streaming logs by sandbox", "sandbox", logTarget.SandboxID, "follow", args.Follow())
	} else if target.HasApp() && target.App() != "" {
		// Resolve app to entity ID
		var appRec core_v1alpha.App
		err := s.EC.Get(ctx, target.App(), &appRec)
		if err != nil {
			s.Log.Error("failed to get app", "app", target.App(), "err", err)
			return err
		}
		logTarget.EntityID = appRec.EntityId().String()
		s.Log.Debug("streaming logs by app", "app", target.App(), "entityID", logTarget.EntityID, "follow", args.Follow())
	} else {
		return fmt.Errorf("target must specify either app or sandbox")
	}

	// Create channel for log entries
	logCh := make(chan observability.LogEntry, 100)
	errCh := make(chan error, 1)

	// Start reader goroutine
	go func() {
		defer close(logCh)
		var err error
		if args.Follow() {
			err = s.LogReader.TailStream(ctx, logTarget, logCh, opts...)
		} else {
			err = s.LogReader.ReadStream(ctx, logTarget, logCh, opts...)
		}
		if err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	// Stream logs to client
	for entry := range logCh {
		if _, err := send.Send(ctx, toLogEntry(entry)); err != nil {
			s.Log.Debug("client disconnected", "err", err)
			return err
		}
	}

	// Check for reader errors
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

const defaultChunkSize = 100

func (s *Server) StreamLogChunks(ctx context.Context, state *app_v1alpha.LogsStreamLogChunks) error {
	args := state.Args()
	send := args.Chunks()
	target := args.Target()

	var opts []observability.LogReaderOption
	if args.HasFrom() {
		fromTime := standard.FromTimestamp(args.From())
		opts = append(opts, observability.WithFromTime(fromTime))
	}

	// Build log target from RPC target
	var logTarget observability.LogTarget

	if target.HasSandbox() && target.Sandbox() != "" {
		logTarget.SandboxID = target.Sandbox()
		s.Log.Debug("streaming log chunks by sandbox", "sandbox", logTarget.SandboxID, "follow", args.Follow())
	} else if target.HasApp() && target.App() != "" {
		// Resolve app to entity ID
		var appRec core_v1alpha.App
		err := s.EC.Get(ctx, target.App(), &appRec)
		if err != nil {
			s.Log.Error("failed to get app", "app", target.App(), "err", err)
			return err
		}
		logTarget.EntityID = appRec.EntityId().String()
		s.Log.Debug("streaming log chunks by app", "app", target.App(), "entityID", logTarget.EntityID, "follow", args.Follow())
	} else {
		return fmt.Errorf("target must specify either app or sandbox")
	}

	// Create channel for log entries
	logCh := make(chan observability.LogEntry, 100)
	errCh := make(chan error, 1)

	// Start reader goroutine
	go func() {
		defer close(logCh)
		var err error
		if args.Follow() {
			err = s.LogReader.TailStream(ctx, logTarget, logCh, opts...)
		} else {
			err = s.LogReader.ReadStream(ctx, logTarget, logCh, opts...)
		}
		if err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	// Buffer entries into chunks
	chunk := &app_v1alpha.LogChunk{}
	entries := make([]*app_v1alpha.LogEntry, 0, defaultChunkSize)

	sendChunk := func() error {
		if len(entries) == 0 {
			return nil
		}
		chunk.SetEntries(entries)
		if _, err := send.Send(ctx, chunk); err != nil {
			s.Log.Debug("client disconnected", "err", err)
			return err
		}
		// Reset for next chunk
		chunk = &app_v1alpha.LogChunk{}
		entries = make([]*app_v1alpha.LogEntry, 0, defaultChunkSize)
		return nil
	}

	// In follow mode, use a ticker to flush chunks periodically
	if args.Follow() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case entry, ok := <-logCh:
				if !ok {
					// Channel closed, send remaining entries
					if err := sendChunk(); err != nil {
						return err
					}
					goto done
				}
				entries = append(entries, toLogEntry(entry))
				if len(entries) >= defaultChunkSize {
					if err := sendChunk(); err != nil {
						return err
					}
				}
			case <-ticker.C:
				// Periodic flush for timely delivery in tail mode
				if err := sendChunk(); err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	} else {
		// Non-follow mode: batch efficiently without time constraints
		for entry := range logCh {
			entries = append(entries, toLogEntry(entry))
			if len(entries) >= defaultChunkSize {
				if err := sendChunk(); err != nil {
					return err
				}
			}
		}

		// Send remaining entries
		if err := sendChunk(); err != nil {
			return err
		}
	}

done:

	// Check for reader errors
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
