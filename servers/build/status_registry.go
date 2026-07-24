package build

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/rpc/stream"
)

// StatusSender lets saga actions emit progress, log lines, and errors
// back to the client that started the build without holding a direct
// reference to the per-request RPC stream. The SagaBuilder wraps a
// concrete stream into a sender and registers it under the stream ID
// for the duration of a saga; actions look it up by the same ID they
// already carry through saga inputs.
//
// During recovery the sender returned by StatusRegistry.SenderFor is a
// noop, which is the right default — nobody's listening for live
// progress when the original CLI invocation is long gone.
type StatusSender interface {
	// SendMessage emits a free-form progress message ("Reading
	// application data", "Launching builder", etc.).
	SendMessage(msg string)

	// SendPhase translates a buildkit phase name into a user-facing
	// progress message, matching the mapping the pre-saga path used.
	SendPhase(phase string)

	// SendBuildkit emits the raw buildkit JSON status payload so the
	// CLI can render live vertex/log output.
	SendBuildkit(payload []byte)

	// SendError emits a user-facing error message. Returning the error
	// from the action is what surfaces failure through the saga; this
	// is the channel for the human-readable explanation.
	SendError(format string, args ...any)

	// SendLog emits a structured log entry. Currently only the
	// pre-saga "warn" path for local storage migration uses this.
	SendLog(level, text string, fields ...*build_v1alpha.LogField)

	// SendDeployment emits the server-owned deployment record's ID and
	// current phase, so the client can display and cancel a deployment it
	// did not create.
	SendDeployment(deploymentID, phase string)
}

// noopStatusSender is the zero-cost StatusSender used during recovery
// or any other time no live RPC stream is registered for a given ID.
type noopStatusSender struct{}

func (noopStatusSender) SendMessage(string)                                 {}
func (noopStatusSender) SendPhase(string)                                   {}
func (noopStatusSender) SendBuildkit([]byte)                                {}
func (noopStatusSender) SendError(string, ...any)                           {}
func (noopStatusSender) SendLog(string, string, ...*build_v1alpha.LogField) {}
func (noopStatusSender) SendDeployment(string, string)                      {}

// rpcStatusSender adapts the existing per-request SendStreamClient into
// the StatusSender interface so saga actions don't have to know about
// the RPC wiring. Errors on Send are logged and swallowed; a dropped
// client stream shouldn't fail the saga.
type rpcStatusSender struct {
	stream *stream.SendStreamClient[*build_v1alpha.Status]
	log    *slog.Logger
}

// NewRPCStatusSender wraps an RPC status stream. Passing a nil stream
// returns a sender that no-ops cleanly so callers don't have to special-
// case the "client didn't request status updates" path.
func NewRPCStatusSender(s *stream.SendStreamClient[*build_v1alpha.Status], log *slog.Logger) StatusSender {
	if s == nil {
		return noopStatusSender{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &rpcStatusSender{stream: s, log: log}
}

func (r *rpcStatusSender) send(so *build_v1alpha.Status) {
	if _, err := r.stream.Send(context.Background(), so); err != nil {
		r.log.Warn("status send", "error", err)
	}
}

func (r *rpcStatusSender) SendMessage(msg string) {
	so := new(build_v1alpha.Status)
	so.Update().SetMessage(msg)
	r.send(so)
}

func (r *rpcStatusSender) SendPhase(phase string) {
	// Mapping matches the pre-saga path's WithPhaseUpdates callback so
	// the CLI's progress display behaves identically on both code paths.
	var msg string
	switch phase {
	case "export":
		msg = "Registering image"
	case "solving":
		msg = "Calculating build"
	case "solved":
		msg = "Building image"
	default:
		msg = phase
	}
	r.SendMessage(msg)
}

func (r *rpcStatusSender) SendBuildkit(payload []byte) {
	so := new(build_v1alpha.Status)
	so.Update().SetBuildkit(payload)
	r.send(so)
}

func (r *rpcStatusSender) SendError(format string, args ...any) {
	so := new(build_v1alpha.Status)
	so.Update().SetError(fmt.Sprintf(format, args...))
	r.send(so)
}

func (r *rpcStatusSender) SendDeployment(deploymentID, phase string) {
	progress := &build_v1alpha.DeploymentProgress{}
	progress.SetDeploymentId(deploymentID)
	progress.SetPhase(phase)

	so := new(build_v1alpha.Status)
	so.Update().SetDeployment(progress)
	r.send(so)
}

func (r *rpcStatusSender) SendLog(level, text string, fields ...*build_v1alpha.LogField) {
	so := new(build_v1alpha.Status)
	entry := &build_v1alpha.LogEntry{}
	entry.SetLevel(level)
	entry.SetText(text)
	if len(fields) > 0 {
		entry.SetFields(fields)
	}
	so.Update().SetLog(entry)
	r.send(so)
}

// StatusRegistry maps stream IDs (the same handles StreamRegistry uses)
// to live StatusSenders. Saga actions retrieve a sender by ID without
// caring whether anyone's currently listening — SenderFor returns noop
// when nothing's registered, which gives recovery the right behavior
// for free.
type StatusRegistry struct {
	mu      sync.Mutex
	senders map[string]StatusSender
}

func NewStatusRegistry() *StatusRegistry {
	return &StatusRegistry{senders: make(map[string]StatusSender)}
}

// Register associates a sender with a stream ID. The SagaBuilder calls
// this before saga.Start and Unregister after, so a sender's lifetime
// is bounded by one saga execution.
func (s *StatusRegistry) Register(streamID string, sender StatusSender) {
	if sender == nil {
		sender = noopStatusSender{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.senders[streamID] = sender
}

// Unregister drops the live sender for an ID. Idempotent — calling
// twice (e.g., from both a defer and an error path) is fine.
func (s *StatusRegistry) Unregister(streamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.senders, streamID)
}

// SenderFor returns the registered sender or a noop. Always returns a
// usable StatusSender so callers can avoid nil checks.
func (s *StatusRegistry) SenderFor(streamID string) StatusSender {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sender, ok := s.senders[streamID]; ok {
		return sender
	}
	return noopStatusSender{}
}
