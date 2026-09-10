package commands

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/deployevents"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/progress/upload"
)

// deployEventStream feeds the --format jsonl writer from the deploy's own
// vantage point: it turns BuildKit status, health polls, and log records into
// the events defined in pkg/deployevents. In that mode nothing else is written
// to either stdout or stderr; every fact the human output would have shown is
// an event here, and the last line is always a result.
//
// The schema and its status vocabulary live in pkg/deployevents so other
// consumers can decode the stream with the same types; this file is only the
// CLI's knowledge of where each fact comes from.
type deployEventStream struct {
	w     *deployevents.Writer
	mu    sync.Mutex
	names map[string]string                  // vertex digest → step name
	state map[string]deployevents.StepStatus // vertex digest → last reported status
}

func newDeployEventStream(out io.Writer) *deployEventStream {
	return &deployEventStream{
		w:     deployevents.NewWriter(out),
		names: map[string]string{},
		state: map[string]deployevents.StepStatus{},
	}
}

// Err reports the first write failure, if any.
func (s *deployEventStream) Err() error { return s.w.Err() }

func (s *deployEventStream) start(app, cluster string) {
	s.w.Emit(deployevents.Start{Header: deployevents.NewHeader(deployevents.EventStart), App: app, Cluster: cluster})
}

// message reports a server-side progress note ("Reading application data").
func (s *deployEventStream) message(msg string) {
	if msg == "" {
		return
	}
	s.w.Emit(deployevents.Message{Header: deployevents.NewHeader(deployevents.EventMessage), Message: msg})
}

func (s *deployEventStream) upload(p upload.Progress) {
	s.w.Emit(deployevents.Upload{
		Header:         deployevents.NewHeader(deployevents.EventUpload),
		Bytes:          p.BytesRead,
		BytesPerSecond: p.BytesPerSecond,
		Fraction:       p.Fraction,
		ETAMs:          p.ETA.Milliseconds(),
	})
}

func (s *deployEventStream) uploadComplete(bytes int64, d time.Duration, reused, total int, saved int64) {
	s.w.Emit(deployevents.UploadComplete{
		Header:      deployevents.NewHeader(deployevents.EventUploadComplete),
		Bytes:       bytes,
		DurationMs:  d.Milliseconds(),
		ReusedFiles: reused,
		TotalFiles:  total,
		SavedBytes:  saved,
	})
}

// observeSolveStatus turns one BuildKit status update into build_step events
// (only when a step's state actually changes) and one build_log event per log
// line. This is the replacement for BuildKit's own printer, which is silenced
// in JSONL mode.
func (s *deployEventStream) observeSolveStatus(st *client.SolveStatus) {
	for _, v := range st.Vertexes {
		d := v.Digest.String()
		var status deployevents.StepStatus
		switch {
		case v.Error != "":
			status = deployevents.StepError
		case v.Cached:
			status = deployevents.StepCached
		case v.Completed != nil:
			status = deployevents.StepDone
		case v.Started != nil:
			status = deployevents.StepStarted
		}

		s.mu.Lock()
		s.names[d] = v.Name
		changed := status != "" && s.state[d] != status
		if changed {
			s.state[d] = status
		}
		s.mu.Unlock()

		if changed {
			s.w.Emit(deployevents.BuildStep{
				Header: deployevents.NewHeader(deployevents.EventBuildStep),
				Step:   v.Name,
				Digest: d,
				Status: status,
				Error:  v.Error,
			})
		}
	}

	for _, l := range st.Logs {
		if len(l.Data) == 0 {
			continue
		}
		d := l.Vertex.String()
		s.mu.Lock()
		name := s.names[d]
		s.mu.Unlock()
		for _, line := range strings.Split(strings.TrimRight(string(l.Data), "\n"), "\n") {
			s.w.Emit(deployevents.BuildLog{
				Header: deployevents.NewHeader(deployevents.EventBuildLog),
				Step:   name,
				Digest: d,
				Line:   line,
			})
		}
	}
}

func (s *deployEventStream) buildComplete(image string, p buildProgress, d time.Duration) {
	s.w.Emit(deployevents.BuildComplete{
		Header:     deployevents.NewHeader(deployevents.EventBuildComplete),
		Image:      image,
		Steps:      p.total,
		Cached:     p.cached,
		DurationMs: d.Milliseconds(),
	})
}

func (s *deployEventStream) buildError(msg string) {
	s.w.Emit(deployevents.BuildError{Header: deployevents.NewHeader(deployevents.EventBuildError), Message: msg})
}

func (s *deployEventStream) deployment(id string, phase deploylifecycle.Phase) {
	s.w.Emit(deployevents.Deployment{Header: deployevents.NewHeader(deployevents.EventDeployment), DeployID: id, Phase: phase})
}

func (s *deployEventStream) warning(entry *build_v1alpha.LogEntry) {
	ev := deployevents.Warning{Header: deployevents.NewHeader(deployevents.EventWarning), Message: entry.Text()}
	for _, f := range entry.Fields() {
		switch f.Key() {
		case "detail":
			ev.Detail = f.Value()
		case "link":
			ev.Link = f.Value()
		}
	}
	s.w.Emit(ev)
}

// Health events implement healthObserver.

func (s *deployEventStream) healthWaiting(version string) {
	s.w.Emit(deployevents.Health{
		Header:  deployevents.NewHeader(deployevents.EventHealth),
		Version: version,
		Outcome: deployevents.OutcomeWaiting,
	})
}

func (s *deployEventStream) healthVerdict(version string, outcome terminalOutcome, snap healthSnapshot, text string, ok bool, elapsed time.Duration) {
	s.w.Emit(deployevents.Health{
		Header:          deployevents.NewHeader(deployevents.EventHealth),
		Version:         version,
		Outcome:         outcomeEvent(outcome),
		OK:              ok,
		Health:          snap.health,
		Ready:           snap.ready,
		Desired:         snap.desired,
		CrashCount:      snap.crashCount,
		CooldownSeconds: snap.cooldownSeconds,
		Message:         text,
		DurationMs:      elapsed.Milliseconds(),
	})
}

// outcomeEvent maps the poll's terminal outcome onto the published vocabulary.
func outcomeEvent(o terminalOutcome) deployevents.Outcome {
	switch o {
	case outcomeHealthy:
		return deployevents.OutcomeHealthy
	case outcomeScaledToZero:
		return deployevents.OutcomeScaledToZero
	case outcomeTaskOnly:
		return deployevents.OutcomeTaskOnly
	case outcomeCrashed:
		return deployevents.OutcomeCrashed
	case outcomeTimeout:
		return deployevents.OutcomeTimeout
	case outcomeCanceled:
		// Never reported: a cancelled wait returns before any verdict.
		return deployevents.OutcomeWaiting
	default:
		return deployevents.OutcomeTimeout
	}
}

func (s *deployEventStream) healthPortWarning(port int, address string) {
	s.w.Emit(deployevents.PortWarning{
		Header:  deployevents.NewHeader(deployevents.EventPortWarning),
		Port:    port,
		Address: address,
		Message: "app bound a port other than the one Miren configured via $PORT; traffic is auto-routed there",
	})
}

func (s *deployEventStream) healthAppLog(line string) {
	s.w.Emit(deployevents.AppLog{Header: deployevents.NewHeader(deployevents.EventAppLog), Line: line})
}

func (s *deployEventStream) result(r *deployevents.Result) {
	s.w.Emit(deployevents.ResultEvent{Header: deployevents.NewHeader(deployevents.EventResult), Result: *r})
}

// eventLogHandler routes the CLI's own log records into the stream as "log"
// events. The RPC layer and other components log through ctx.Log; without
// this, a warning or error from them would land on stderr as text in the
// middle of an otherwise structured run.
type eventLogHandler struct {
	stream *deployEventStream
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newEventLogHandler(stream *deployEventStream, level slog.Leveler) *eventLogHandler {
	return &eventLogHandler{stream: stream, level: level}
}

func (h *eventLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// prefix is the dotted path of the open groups, ending in "." when non-empty.
func (h *eventLogHandler) prefix() string {
	if len(h.groups) == 0 {
		return ""
	}
	return strings.Join(h.groups, ".") + "."
}

func (h *eventLogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := map[string]any{}
	// Attributes attached earlier already carry the prefix that was open when
	// they were attached; only the record's own attributes take the current one.
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.Resolve().Any()
	}
	prefix := h.prefix()
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "" {
			fields[prefix+a.Key] = a.Value.Resolve().Any()
		}
		return true
	})
	h.stream.w.Emit(deployevents.Log{
		Header:  deployevents.Header{Event: deployevents.EventLog, Time: r.Time.UTC()},
		Level:   r.Level.String(),
		Message: r.Message,
		Fields:  fields,
	})
	return nil
}

func (h *eventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append([]slog.Attr(nil), h.attrs...)
	prefix := h.prefix()
	for _, a := range attrs {
		if a.Key != "" {
			next.attrs = append(next.attrs, slog.Attr{Key: prefix + a.Key, Value: a.Value})
		}
	}
	return &next
}

func (h *eventLogHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}
