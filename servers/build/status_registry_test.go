package build

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

// recordingSender captures every Send* call so tests can assert on the
// sequence of progress messages a saga action emitted. Thread-safe so
// it works regardless of which goroutine the action runs on.
type recordingSender struct {
	mu          sync.Mutex
	Messages    []string
	Phases      []string
	Images      []string
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

func (r *recordingSender) SendPhase(_ context.Context, phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Phases = append(r.Phases, phase)
}

func (r *recordingSender) SendImage(image string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Images = append(r.Images, image)
}

func (r *recordingSender) SendBuildkit(_ context.Context, payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Buildkit = append(r.Buildkit, append([]byte(nil), payload...))
}

func (r *recordingSender) SendError(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
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
	sender.SendPhase(t.Context(), "solving")
	sender.SendImage("example/app:v1")
	sender.SendBuildkit(t.Context(), []byte("x"))
	sender.SendError("oops")
	sender.SendLog("info", "text")
}

func TestStatusRegistry_RegisteredSenderReceives(t *testing.T) {
	reg := NewStatusRegistry()
	rec := &recordingSender{}
	reg.Register("s1", rec)

	sender := reg.SenderFor("s1")
	sender.SendMessage("hello")
	sender.SendPhase(t.Context(), "solving")
	sender.SendImage("example/app:v1")
	sender.SendBuildkit(t.Context(), []byte("payload"))
	sender.SendError("bad %s", "thing")

	if got, want := rec.Messages, []string{"hello"}; !equalStringSlice(got, want) {
		t.Errorf("Messages = %v, want %v", got, want)
	}
	if got, want := rec.Phases, []string{"solving"}; !equalStringSlice(got, want) {
		t.Errorf("Phases = %v, want %v", got, want)
	}
	if got, want := rec.Images, []string{"example/app:v1"}; !equalStringSlice(got, want) {
		t.Errorf("Images = %v, want %v", got, want)
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

func TestRPCStatusSenderBoundsStalledReceiver(t *testing.T) {
	sender, received := stalledStatusReceiver(t)
	finished := make(chan struct{})
	go func() {
		sender.SendPhase(t.Context(), "solving")
		close(finished)
	}()
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("receiver never got the phase update")
	}
	select {
	case <-finished:
	case <-time.After(7 * time.Second):
		t.Fatal("a live build without a deadline is blocked by its status receiver")
	}
}

func stalledStatusReceiver(t *testing.T) (StatusSender, <-chan struct{}) {
	t.Helper()
	received := make(chan struct{}, 1)
	release := make(chan struct{})
	server, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	server.Server().ExposeValue("status", stream.ReadStream(func(*build_v1alpha.Status) error {
		select {
		case received <- struct{}{}:
		default:
		}
		<-release
		return nil
	}))
	client, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	t.Cleanup(func() { close(release) })
	conn, err := client.Connect(server.ListenAddr(), "status")
	if err != nil {
		t.Fatal(err)
	}
	return NewRPCStatusSender(stream.NewSendStreamClient[*build_v1alpha.Status](conn), slog.Default()), received
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
