package logs

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/logfilter"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
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
	if len(entry.Attributes) > 0 {
		attrs := make(map[string]string, len(entry.Attributes))
		for k, v := range entry.Attributes {
			if k == "source" {
				continue
			}
			attrs[k] = v
		}
		if len(attrs) > 0 {
			le.SetAttributes(attrs)
		}
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

	if !rpc.AllowApp(ctx, args.Application()) {
		return rpc.AppAccessError(ctx, args.Application())
	}

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

	sandboxID, err := s.resolveSandboxID(ctx, args.Sandbox())
	if err != nil {
		return err
	}

	s.Log.Debug("reading logs", "sandbox", sandboxID, "from", args.From())

	entries, err := s.LogReader.ReadBySandbox(ctx, sandboxID, opts...)
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

	if target.HasApp() && target.App() != "" {
		if !rpc.AllowApp(ctx, target.App()) {
			return rpc.AppAccessError(ctx, target.App())
		}
	} else if rpc.BoundApp(ctx) != "" {
		return fmt.Errorf("%w: app-scoped caller must specify app target", rpc.ErrUnauthorized)
	}

	var opts []observability.LogReaderOption
	if args.HasFrom() {
		fromTime := standard.FromTimestamp(args.From())
		opts = append(opts, observability.WithFromTime(fromTime))
	} else if !args.Follow() {
		opts = append(opts, observability.WithLimit(defaultTailLimit))
	}

	logTarget, err := s.resolveLogTarget(ctx, target)
	if err != nil {
		return err
	}
	s.Log.Debug("streaming logs", "target", logTarget, "follow", args.Follow())

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
const defaultTailLimit = 100

// resolveSandboxID resolves a sandbox identifier (full entity ID, short ID,
// or sandbox/-prefixed short ID) to the full sandbox entity ID string.
func (s *Server) resolveSandboxID(ctx context.Context, sandboxID string) (string, error) {
	ret, err := s.EC.EAC().Get(ctx, sandboxID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve sandbox %q: %w", sandboxID, err)
	}
	return ret.Entity().Id(), nil
}

// resolveLogTarget converts an RPC LogTarget into an observability.LogTarget,
// resolving app names to entity IDs as needed.
func (s *Server) resolveLogTarget(ctx context.Context, target *app_v1alpha.LogTarget) (observability.LogTarget, error) {
	var logTarget observability.LogTarget

	hasSystem := target.HasSystem() && target.System()
	hasSandbox := target.HasSandbox() && target.Sandbox() != ""
	hasApp := target.HasApp() && target.App() != ""

	selected := 0
	if hasSystem {
		selected++
	}
	if hasSandbox {
		selected++
	}
	if hasApp {
		selected++
	}
	if selected != 1 {
		return logTarget, fmt.Errorf("target must specify exactly one of app, sandbox, or system")
	}

	if hasSystem {
		logTarget.EntityID = observability.SystemLogEntityID
		return logTarget, nil
	}

	if hasSandbox {
		resolved, err := s.resolveSandboxID(ctx, target.Sandbox())
		if err != nil {
			return logTarget, err
		}
		logTarget.SandboxID = resolved
		return logTarget, nil
	}

	var appRec core_v1alpha.App
	err := s.EC.Get(ctx, target.App(), &appRec)
	if err != nil {
		s.Log.Error("failed to get app", "app", target.App(), "err", err)
		return logTarget, err
	}
	logTarget.EntityID = appRec.EntityId().String()
	return logTarget, nil
}

func (s *Server) StreamLogChunks(ctx context.Context, state *app_v1alpha.LogsStreamLogChunks) error {
	args := state.Args()
	filter := ""
	if args.HasFilter() {
		var err error
		filter, err = compileLogFilter(args.Filter())
		if err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}
	}
	return s.streamLogChunks(ctx, args.Target(), args.From(), args.HasFrom(), args.To(), args.HasTo(), args.Follow(), filter, args.Chunks())
}

func (s *Server) StreamLogChunksV2(ctx context.Context, state *app_v1alpha.LogsStreamLogChunksV2) error {
	args := state.Args()
	filter, err := compileSeparatedFilter(args.Filter(), args.Grep())
	if err != nil {
		return fmt.Errorf("invalid filter: %w", err)
	}
	return s.streamLogChunks(ctx, args.Target(), args.From(), args.HasFrom(), args.To(), args.HasTo(), args.Follow(), filter, args.Chunks())
}

func (s *Server) streamLogChunks(
	ctx context.Context,
	target *app_v1alpha.LogTarget,
	from *standard.Timestamp,
	hasFrom bool,
	to *standard.Timestamp,
	hasTo bool,
	follow bool,
	filter string,
	send interface {
		Send(context.Context, *app_v1alpha.LogChunk) (*stream.SendStreamClientSendResults[*app_v1alpha.LogChunk], error)
	},
) error {

	if target.HasApp() && target.App() != "" {
		if !rpc.AllowApp(ctx, target.App()) {
			return rpc.AppAccessError(ctx, target.App())
		}
	} else if rpc.BoundApp(ctx) != "" {
		return fmt.Errorf("%w: app-scoped caller must specify app target", rpc.ErrUnauthorized)
	}

	var opts []observability.LogReaderOption
	if hasFrom {
		fromTime := standard.FromTimestamp(from)
		opts = append(opts, observability.WithFromTime(fromTime))
	} else if !follow {
		opts = append(opts, observability.WithLimit(defaultTailLimit))
	}
	if hasTo {
		toTime := standard.FromTimestamp(to)
		opts = append(opts, observability.WithUntilTime(toTime))
	}

	logTarget, err := s.resolveLogTarget(ctx, target)
	if err != nil {
		return err
	}
	s.Log.Debug("streaming log chunks", "target", logTarget, "follow", follow)
	if filter != "" {
		logTarget.Filter = filter
		s.Log.Debug("applying filter", "logsql", logTarget.Filter)
	}

	// Create channel for log entries
	logCh := make(chan observability.LogEntry, 100)
	errCh := make(chan error, 1)

	// Start reader goroutine
	go func() {
		defer close(logCh)
		var err error
		if follow {
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
	if follow {
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

func compileLogFilter(input string) (string, error) {
	structural, grep := splitStructuralFilter(input)
	filter, err := logfilter.Parse(grep)
	if err != nil {
		return "", err
	}
	if filter == nil {
		return structural, nil
	}
	if structural == "" {
		return filter.ToLogsQL(), nil
	}
	return structural + " " + filter.ToLogsQL(), nil
}

func compileSeparatedFilter(structural, grep string) (string, error) {
	structural, remainder := splitStructuralFilter(structural)
	if remainder != "" {
		return "", fmt.Errorf("invalid structural filter %q", remainder)
	}
	filter, err := logfilter.Parse(grep)
	if err != nil {
		return "", err
	}
	if filter == nil {
		return structural, nil
	}
	if structural == "" {
		return filter.ToLogsQL(), nil
	}
	return structural + " " + filter.ToLogsQL(), nil
}

func splitStructuralFilter(input string) (string, string) {
	rest := strings.TrimSpace(input)
	var clauses []string

	// Keep these in sync with the structural builders in cli/commands/logs.go:
	// systemExclusion, buildFilterWithService, buildRunFilter,
	// buildBuildFilter, and buildSystemFilter. New CLI selectors must also be
	// allowlisted here or the V2 RPC rejects them.
	for rest != "" {
		var n int
		switch {
		case hasTokenPrefix(rest, `-source:"system"`):
			n = len(`-source:"system"`)
		case serviceSelector.MatchString(rest):
			n = len(serviceSelector.FindString(rest))
		case runSelector.MatchString(rest):
			n = len(runSelector.FindString(rest))
		case hasTokenPrefix(rest, "source:build"):
			n = len("source:build")
		case hasTokenPrefix(rest, `source:"system"`):
			n = len(`source:"system"`)
		case strings.HasPrefix(rest, `version:"`):
			n = quotedFieldEnd(rest, len("version:"))
		case strings.HasPrefix(rest, `module:"`):
			n = quotedFieldEnd(rest, len("module:"))
		}
		if n <= 0 || !tokenBoundary(rest, n) {
			break
		}
		clauses = append(clauses, rest[:n])
		rest = strings.TrimSpace(rest[n:])
	}

	return strings.Join(clauses, " "), rest
}

var (
	serviceSelector = regexp.MustCompile(`^\(service:"(?:\\.|[^"\\])*" OR miren\.service:"(?:\\.|[^"\\])*"\)`)
	runSelector     = regexp.MustCompile(`^\(run:"(?:\\.|[^"\\])*" OR miren\.run:"(?:\\.|[^"\\])*"\)`)
)

func hasTokenPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix) && tokenBoundary(s, len(prefix))
}

func tokenBoundary(s string, end int) bool {
	return end == len(s) || s[end] == ' ' || s[end] == '\t'
}

func quotedFieldEnd(s string, valueStart int) int {
	escaped := false
	for i := valueStart + 1; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' {
			escaped = true
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return 0
}
