package commands

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/progress/upload"
)

// deployEventStream is the --format jsonl feed: one JSON object per line, each
// carrying an "event" name and a "time", so a program can follow a deploy as it
// happens instead of waiting for the final document. In this mode nothing else
// is written to either stdout or stderr; every fact the human output would have
// shown (upload progress, build steps and their logs, deployment phases, the
// health verdict, warnings, crash logs) is an event here, and the last line is
// always a "result" event with the same fields as the --format json document.
//
// Methods are safe to call from the RPC callback goroutines that deliver build
// status.
type deployEventStream struct {
	mu    sync.Mutex
	enc   *json.Encoder
	names map[string]string // vertex digest → step name
	state map[string]string // vertex digest → last reported status
}

func newDeployEventStream(w io.Writer) *deployEventStream {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &deployEventStream{
		enc:   enc,
		names: map[string]string{},
		state: map[string]string{},
	}
}

// eventHeader leads every line. Embedding it first keeps "event" as the first
// key, which makes the stream skimmable by eye as well as by machine.
type eventHeader struct {
	Event string    `json:"event"`
	Time  time.Time `json:"time"`
}

func header(name string) eventHeader {
	return eventHeader{Event: name, Time: time.Now().UTC()}
}

func (s *deployEventStream) emit(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A write failure (closed pipe) has nowhere useful to go; the deploy itself
	// carries on and the exit code still reports the outcome.
	_ = s.enc.Encode(v)
}

type startEvent struct {
	eventHeader
	App     string `json:"app"`
	Cluster string `json:"cluster"`
}

func (s *deployEventStream) start(app, cluster string) {
	s.emit(startEvent{header("start"), app, cluster})
}

type messageEvent struct {
	eventHeader
	Message string `json:"message"`
}

// message reports a server-side progress note ("Reading application data").
func (s *deployEventStream) message(msg string) {
	if msg == "" {
		return
	}
	s.emit(messageEvent{header("message"), msg})
}

type uploadEvent struct {
	eventHeader
	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Fraction       float64 `json:"fraction"`
	ETAMs          int64   `json:"eta_ms,omitempty"`
}

func (s *deployEventStream) upload(p upload.Progress) {
	s.emit(uploadEvent{
		eventHeader:    header("upload"),
		Bytes:          p.BytesRead,
		BytesPerSecond: p.BytesPerSecond,
		Fraction:       p.Fraction,
		ETAMs:          p.ETA.Milliseconds(),
	})
}

type uploadCompleteEvent struct {
	eventHeader
	Bytes       int64 `json:"bytes"`
	DurationMs  int64 `json:"duration_ms"`
	ReusedFiles int   `json:"reused_files"`
	TotalFiles  int   `json:"total_files"`
	SavedBytes  int64 `json:"saved_bytes"`
}

func (s *deployEventStream) uploadComplete(bytes int64, d time.Duration, reused, total int, saved int64) {
	s.emit(uploadCompleteEvent{header("upload_complete"), bytes, d.Milliseconds(), reused, total, saved})
}

type buildStepEvent struct {
	eventHeader
	Step   string `json:"step"`
	Digest string `json:"digest"`
	Status string `json:"status"` // started, done, cached, error
	Error  string `json:"error,omitempty"`
}

type buildLogEvent struct {
	eventHeader
	Step   string `json:"step"`
	Digest string `json:"digest"`
	Line   string `json:"line"`
}

// observeSolveStatus turns one BuildKit status update into build_step events
// (only when a step's state actually changes) and one build_log event per log
// line. This is the replacement for BuildKit's own printer, which is silenced
// in JSONL mode.
func (s *deployEventStream) observeSolveStatus(st *client.SolveStatus) {
	for _, v := range st.Vertexes {
		d := v.Digest.String()
		status := ""
		switch {
		case v.Error != "":
			status = "error"
		case v.Cached:
			status = "cached"
		case v.Completed != nil:
			status = "done"
		case v.Started != nil:
			status = "started"
		}

		s.mu.Lock()
		s.names[d] = v.Name
		changed := status != "" && s.state[d] != status
		if changed {
			s.state[d] = status
		}
		s.mu.Unlock()

		if changed {
			s.emit(buildStepEvent{header("build_step"), v.Name, d, status, v.Error})
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
			s.emit(buildLogEvent{header("build_log"), name, d, line})
		}
	}
}

type buildCompleteEvent struct {
	eventHeader
	Image      string `json:"image,omitempty"`
	Steps      int    `json:"steps"`
	Cached     int    `json:"cached"`
	DurationMs int64  `json:"duration_ms"`
}

func (s *deployEventStream) buildComplete(image string, p buildProgress, d time.Duration) {
	s.emit(buildCompleteEvent{header("build_complete"), image, p.total, p.cached, d.Milliseconds()})
}

type buildErrorEvent struct {
	eventHeader
	Message string `json:"message"`
}

func (s *deployEventStream) buildError(msg string) {
	s.emit(buildErrorEvent{header("build_error"), msg})
}

type deploymentEvent struct {
	eventHeader
	DeployID string `json:"deploy_id"`
	Phase    string `json:"phase"`
}

func (s *deployEventStream) deployment(id, phase string) {
	s.emit(deploymentEvent{header("deployment"), id, phase})
}

type warningEvent struct {
	eventHeader
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Link    string `json:"link,omitempty"`
}

func (s *deployEventStream) warning(entry *build_v1alpha.LogEntry) {
	ev := warningEvent{eventHeader: header("warning"), Message: entry.Text()}
	for _, f := range entry.Fields() {
		switch f.Key() {
		case "detail":
			ev.Detail = f.Value()
		case "link":
			ev.Link = f.Value()
		}
	}
	s.emit(ev)
}

// Health events implement healthObserver.

type healthEvent struct {
	eventHeader
	Version    string `json:"version"`
	Status     string `json:"status"` // waiting, healthy, failed
	Message    string `json:"message,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

func (s *deployEventStream) healthWaiting(version string) {
	s.emit(healthEvent{eventHeader: header("health"), Version: version, Status: "waiting"})
}

func (s *deployEventStream) healthVerdict(version, text string, ok bool, elapsed time.Duration) {
	status := "failed"
	if ok {
		status = "healthy"
	}
	s.emit(healthEvent{header("health"), version, status, text, elapsed.Milliseconds()})
}

type portWarningEvent struct {
	eventHeader
	Port    int    `json:"port"`
	Address string `json:"address,omitempty"`
	Message string `json:"message"`
}

func (s *deployEventStream) healthPortWarning(port int, address string) {
	s.emit(portWarningEvent{
		eventHeader: header("port_warning"),
		Port:        port,
		Address:     address,
		Message:     "app bound a port other than the one Miren configured via $PORT; traffic is auto-routed there",
	})
}

type appLogEvent struct {
	eventHeader
	Line string `json:"line"`
}

func (s *deployEventStream) healthAppLog(line string) {
	s.emit(appLogEvent{header("app_log"), line})
}

type resultEvent struct {
	eventHeader
	deploySummary
}

func (s *deployEventStream) result(summary *deploySummary) {
	s.emit(resultEvent{header("result"), *summary})
}
