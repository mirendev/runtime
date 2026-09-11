// Package deployevents defines the `miren deploy --format jsonl` event stream:
// the shape of each event and the vocabulary of the status values inside them.
//
// The stream is a published contract: a script that parses it freezes the
// spelling of every value it branches on. So the values that already have an
// owner are reused rather than restated. A Result's Status is a
// deploylifecycle.Status, the same one `miren app history` reports for the
// deployment record; a Deployment's Phase is a deploylifecycle.Phase; and a
// Health event carries the apphealth classification alongside the poll's own
// outcome. The event names themselves (start, build_step, result, ...) are
// this package's own.
//
// It lives beside deploylifecycle and apphealth rather than in the CLI so any
// consumer of the stream, Miren Cloud included, can decode it with the same
// types the CLI wrote it with.
package deployevents

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"miren.dev/runtime/pkg/deploylifecycle"
)

// Event names. Each line's "event" field is one of these.
const (
	EventStart          = "start"
	EventMessage        = "message"
	EventUpload         = "upload"
	EventUploadComplete = "upload_complete"
	EventBuildStep      = "build_step"
	EventBuildLog       = "build_log"
	EventBuildError     = "build_error"
	EventBuildComplete  = "build_complete"
	EventDeployment     = "deployment"
	EventWarning        = "warning"
	EventHealth         = "health"
	EventPortWarning    = "port_warning"
	EventAppLog         = "app_log"
	EventLog            = "log"
	EventResult         = "result"
)

// Header leads every line. It is embedded first in each event so "event" is
// the first key, which keeps the stream skimmable by eye as well as by machine.
type Header struct {
	Event string    `json:"event"`
	Time  time.Time `json:"time"`
}

// NewHeader stamps a header for the named event with the current time.
func NewHeader(event string) Header {
	return Header{Event: event, Time: time.Now().UTC()}
}

// Start opens the stream: which app is being deployed to which cluster.
type Start struct {
	Header
	App     string `json:"app"`
	Cluster string `json:"cluster"`
}

// Message is a server-side progress note such as "Reading application data".
type Message struct {
	Header
	Message string `json:"message"`
}

// Upload is a periodic report while project files are sent. Fraction is 0
// when the total is unknown (the file manifest could not be computed).
type Upload struct {
	Header
	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Fraction       float64 `json:"fraction"`
	ETAMs          int64   `json:"eta_ms,omitempty"`
}

// UploadComplete closes the upload phase.
type UploadComplete struct {
	Header
	Bytes       int64 `json:"bytes"`
	DurationMs  int64 `json:"duration_ms"`
	ReusedFiles int   `json:"reused_files"`
	TotalFiles  int   `json:"total_files"`
	SavedBytes  int64 `json:"saved_bytes"`
}

// StepStatus is the state of one build step.
type StepStatus string

const (
	StepStarted StepStatus = "started"
	StepDone    StepStatus = "done"
	StepCached  StepStatus = "cached"
	StepError   StepStatus = "error"
)

// BuildStep is emitted each time a build step changes state. Digest identifies
// the step across events; Step is its human name.
type BuildStep struct {
	Header
	Step   string     `json:"step"`
	Digest string     `json:"digest"`
	Status StepStatus `json:"status"`
	Error  string     `json:"error,omitempty"`
}

// BuildLog is one line of a build step's output.
type BuildLog struct {
	Header
	Step   string `json:"step"`
	Digest string `json:"digest"`
	Line   string `json:"line"`
}

// BuildError is a build failure reported outside any single step.
type BuildError struct {
	Header
	Message string `json:"message"`
}

// BuildComplete closes the build phase. Image is set when an upstream image
// was used directly and nothing was built.
type BuildComplete struct {
	Header
	Image      string `json:"image,omitempty"`
	Steps      int    `json:"steps"`
	Cached     int    `json:"cached"`
	DurationMs int64  `json:"duration_ms"`
}

// Deployment reports the deployment record advancing to a new phase.
type Deployment struct {
	Header
	DeployID string                `json:"deploy_id"`
	Phase    deploylifecycle.Phase `json:"phase"`
}

// Warning is a build-time warning the server attached to the deploy.
type Warning struct {
	Header
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Link    string `json:"link,omitempty"`
}

// Outcome is where the health wait stands: still waiting, or one of the
// terminal states the poll can settle on. OK on the Health event says whether
// the outcome counts as a successful rollout; scaled-to-zero and task-only
// apps are successes with no serving instance.
type Outcome string

const (
	OutcomeWaiting      Outcome = "waiting"
	OutcomeHealthy      Outcome = "healthy"
	OutcomeScaledToZero Outcome = "scaled_to_zero"
	OutcomeTaskOnly     Outcome = "task_only"
	OutcomeCrashed      Outcome = "crashed"
	OutcomeTimeout      Outcome = "timeout"
)

// Health reports the post-activation health wait. Health is the apphealth
// classification the server reported (empty against a server that predates
// health reporting), with the instance counts behind it, so a consumer can
// tell "serving" from "deliberately idle" without reading Message.
type Health struct {
	Header
	Version         string  `json:"version"`
	Outcome         Outcome `json:"outcome"`
	OK              bool    `json:"ok"`
	Health          string  `json:"health,omitempty"`
	Ready           int32   `json:"ready"`
	Desired         int32   `json:"desired"`
	CrashCount      int64   `json:"crash_count,omitempty"`
	CooldownSeconds int32   `json:"cooldown_seconds,omitempty"`
	Message         string  `json:"message,omitempty"`
	DurationMs      int64   `json:"duration_ms,omitempty"`
}

// PortWarning says the app bound a port other than the one Miren configured.
type PortWarning struct {
	Header
	Port    int    `json:"port"`
	Address string `json:"address,omitempty"`
	Message string `json:"message"`
}

// AppLog is one recent line of the app's own output, shown when a rollout
// fails so the reader sees why.
type AppLog struct {
	Header
	Line string `json:"line"`
}

// Log is a record from the CLI's own logger, which would otherwise have gone
// to stderr as text.
type Log struct {
	Header
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Ephemeral describes an ephemeral preview deploy.
type Ephemeral struct {
	Label string `json:"label"`
	TTL   string `json:"ttl"`
}

// Result is the outcome of a deploy. It is the last line of a jsonl stream,
// the whole of a `--format json` document, and the content of a
// `--summary-json` file. Status is the deployment record's own vocabulary:
// succeeded, failed, or cancelled. DeployID is empty for ephemeral deploys,
// which have no deployment record. URLs is always an array, never null.
type Result struct {
	Status     deploylifecycle.Status `json:"status,omitempty"`
	App        string                 `json:"app,omitempty"`
	Cluster    string                 `json:"cluster,omitempty"`
	DeployID   string                 `json:"deploy_id"`
	AppVersion string                 `json:"app_version"`
	URLs       []string               `json:"urls"`
	Ephemeral  *Ephemeral             `json:"ephemeral,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// SetEphemeral records the ephemeral preview details, or clears them when
// label is empty.
func (r *Result) SetEphemeral(label, ttl string) {
	if label == "" {
		r.Ephemeral = nil
		return
	}
	r.Ephemeral = &Ephemeral{Label: label, TTL: ttl}
}

// ResultEvent is Result as a stream line.
type ResultEvent struct {
	Header
	Result
}

// Record is the union of every field any event carries, for decoding a line
// without knowing its event first. The events are flat, so one struct decodes
// them all; which fields are set depends on Event.
type Record struct {
	Header

	App     string `json:"app"`
	Cluster string `json:"cluster"`
	Message string `json:"message"`

	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Fraction       float64 `json:"fraction"`
	ETAMs          int64   `json:"eta_ms"`
	DurationMs     int64   `json:"duration_ms"`
	ReusedFiles    int     `json:"reused_files"`
	TotalFiles     int     `json:"total_files"`
	SavedBytes     int64   `json:"saved_bytes"`

	Step   string         `json:"step"`
	Digest string         `json:"digest"`
	Status string         `json:"status"`
	Error  string         `json:"error"`
	Line   string         `json:"line"`
	Image  string         `json:"image"`
	Steps  int            `json:"steps"`
	Cached int            `json:"cached"`
	Phase  string         `json:"phase"`
	Detail string         `json:"detail"`
	Link   string         `json:"link"`
	Level  string         `json:"level"`
	Fields map[string]any `json:"fields"`

	DeployID string `json:"deploy_id"`

	Version         string  `json:"version"`
	Outcome         Outcome `json:"outcome"`
	OK              bool    `json:"ok"`
	Health          string  `json:"health"`
	Ready           int32   `json:"ready"`
	Desired         int32   `json:"desired"`
	CrashCount      int64   `json:"crash_count"`
	CooldownSeconds int32   `json:"cooldown_seconds"`
	Port            int     `json:"port"`
	Address         string  `json:"address"`

	AppVersion string     `json:"app_version"`
	URLs       []string   `json:"urls"`
	Ephemeral  *Ephemeral `json:"ephemeral"`
}

// Writer emits events as JSON lines. It is safe for concurrent use: a deploy
// produces events from several goroutines at once.
type Writer struct {
	mu       sync.Mutex
	enc      *json.Encoder
	firstErr error
}

// NewWriter returns a Writer emitting to w.
func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

// Emit writes one event as a line. A write failure (a closed pipe) has nowhere
// useful to go mid-stream, so the deploy carries on; the first failure is
// remembered so the caller can refuse to report success for a stream the
// consumer may not have seen to the end.
func (w *Writer) Emit(event any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(event); err != nil && w.firstErr == nil {
		w.firstErr = err
	}
}

// Err reports the first write failure, or nil if every event went out.
func (w *Writer) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.firstErr
}
