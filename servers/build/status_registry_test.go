package build

import (
	"sync"
	"testing"

	"miren.dev/runtime/api/build/build_v1alpha"
)

// recordingSender captures every Send* call so tests can assert on the
// sequence of progress messages a saga action emitted. Thread-safe so
// it works regardless of which goroutine the action runs on.
type recordingSender struct {
	mu          sync.Mutex
	Messages    []string
	Phases      []string
	Buildkit    [][]byte
	Errors      []string
	Logs        []recordedLog
	Deployments []recordedDeployment
}

type recordedDeployment struct {
	DeploymentID string
	Phase        string
}

type recordedLog struct {
	Level  string
	Text   string
	Fields []*build_v1alpha.LogField
}

func (r *recordingSender) SendMessage(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Messages = append(r.Messages, msg)
}

func (r *recordingSender) SendPhase(phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Phases = append(r.Phases, phase)
}

func (r *recordingSender) SendBuildkit(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Buildkit = append(r.Buildkit, append([]byte(nil), payload...))
}

func (r *recordingSender) SendError(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Errors = append(r.Errors, format) // tests don't need full Sprintf
}

func (r *recordingSender) SendLog(level, text string, fields ...*build_v1alpha.LogField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Logs = append(r.Logs, recordedLog{Level: level, Text: text, Fields: fields})
}

func (r *recordingSender) SendDeployment(deploymentID, phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Deployments = append(r.Deployments, recordedDeployment{DeploymentID: deploymentID, Phase: phase})
}

func TestStatusRegistry_UnregisteredIDReturnsNoop(t *testing.T) {
	reg := NewStatusRegistry()
	// SenderFor must always return a usable sender; the noop is what
	// makes the recovery path work without special-casing.
	sender := reg.SenderFor("not-registered")
	// Calling every method should not panic.
	sender.SendMessage("hi")
	sender.SendPhase("solving")
	sender.SendBuildkit([]byte("x"))
	sender.SendError("oops")
	sender.SendLog("info", "text")
}

func TestStatusRegistry_RegisteredSenderReceives(t *testing.T) {
	reg := NewStatusRegistry()
	rec := &recordingSender{}
	reg.Register("s1", rec)

	sender := reg.SenderFor("s1")
	sender.SendMessage("hello")
	sender.SendPhase("solving")
	sender.SendBuildkit([]byte("payload"))
	sender.SendError("bad %s", "thing")

	if got, want := rec.Messages, []string{"hello"}; !equalStringSlice(got, want) {
		t.Errorf("Messages = %v, want %v", got, want)
	}
	if got, want := rec.Phases, []string{"solving"}; !equalStringSlice(got, want) {
		t.Errorf("Phases = %v, want %v", got, want)
	}
	if len(rec.Buildkit) != 1 || string(rec.Buildkit[0]) != "payload" {
		t.Errorf("Buildkit = %v, want one entry with payload bytes", rec.Buildkit)
	}
	if len(rec.Errors) != 1 {
		t.Errorf("Errors len = %d, want 1", len(rec.Errors))
	}
}

func TestStatusRegistry_UnregisterReturnsNoop(t *testing.T) {
	reg := NewStatusRegistry()
	rec := &recordingSender{}
	reg.Register("s1", rec)
	reg.Unregister("s1")
	// Double unregister is fine.
	reg.Unregister("s1")

	sender := reg.SenderFor("s1")
	sender.SendMessage("after-unregister")

	if len(rec.Messages) != 0 {
		t.Errorf("recorder should not have received post-unregister messages, got %v", rec.Messages)
	}
}

func TestStatusRegistry_NilSenderRegistersAsNoop(t *testing.T) {
	reg := NewStatusRegistry()
	// Passing nil shouldn't blow up callers who didn't construct an
	// rpcStatusSender — Register normalizes to noop.
	reg.Register("s1", nil)
	sender := reg.SenderFor("s1")
	sender.SendMessage("should be safe") // no panic
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
