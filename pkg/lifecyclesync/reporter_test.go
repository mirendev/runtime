package lifecyclesync

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/pkg/uplink"
)

type fakeLink struct {
	offer    uplink.CapabilityOfferFunc
	sessions []func(context.Context, uplink.Session)
	handlers map[string]uplink.MessageHandler
	sent     chan sentMessage
}

type sentMessage struct {
	msgType string
	data    any
}

func newFakeLink() *fakeLink {
	return &fakeLink{handlers: make(map[string]uplink.MessageHandler), sent: make(chan sentMessage, 64)}
}

func (f *fakeLink) OfferCapabilityFunc(_ string, _ []uint, provide uplink.CapabilityOfferFunc) {
	f.offer = provide
}

func (f *fakeLink) OnSession(fn func(context.Context, uplink.Session)) {
	f.sessions = append(f.sessions, fn)
}

func (f *fakeLink) Handle(msgType string, handler uplink.MessageHandler) {
	f.handlers[msgType] = handler
}

func (f *fakeLink) SendMessageBlocking(ctx context.Context, msgType string, data any) error {
	select {
	case f.sent <- sentMessage{msgType: msgType, data: data}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeLink) openSession(ctx context.Context, config Config) {
	raw, _ := json.Marshal(config)
	session := uplink.Session{ID: "s1", Capabilities: []uplink.CapabilitySelection{{Name: Capability, Version: Version1, Config: raw}}}
	for _, fn := range f.sessions {
		fn(ctx, session)
	}
}

func (f *fakeLink) expect(t *testing.T, msgType string) any {
	t.Helper()
	select {
	case msg := <-f.sent:
		require.Equal(t, msgType, msg.msgType)
		return msg.data
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s message sent", msgType)
		return nil
	}
}

type fixedIdentity struct{}

func (fixedIdentity) InstanceID() string  { return "inst-1" }
func (fixedIdentity) InstallKind() string { return "systemd" }

type recordingLauncher struct {
	mu       sync.Mutex
	launched []string
}

func (l *recordingLauncher) Launch(_ context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.launched = append(l.launched, id)
	return nil
}

func newTestReporter(t *testing.T) (*Reporter, *serverlifecycle.Store, *recordingLauncher, *fakeLink) {
	t.Helper()
	store := testStore(t)
	launcher := &recordingLauncher{}
	watcher := NewWatcher(slog.Default(), store)
	reporter := NewReporter(slog.Default(), store, launcher, watcher, fixedIdentity{})
	link := newFakeLink()
	require.NoError(t, reporter.Register(context.Background(), link))
	return reporter, store, launcher, link
}

func TestReporterOffersClosedActionSet(t *testing.T) {
	_, _, _, link := newTestReporter(t)
	raw, ok := link.offer(context.Background())
	require.True(t, ok)
	var offer Offer
	require.NoError(t, json.Unmarshal(raw, &offer))
	require.Equal(t, []string{"restart", "upgrade"}, offer.Actions)
	require.Equal(t, "inst-1", offer.RuntimeInstanceID)
	require.Equal(t, "systemd", offer.InstallKind)
}

func TestReporterSyncReportsWatchedAndMissing(t *testing.T) {
	_, store, _, link := newTestReporter(t)

	finished := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(finished))
	finished.Phase = serverlifecycle.PhaseSucceeded
	require.NoError(t, store.Update(finished))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	link.openSession(ctx, Config{Watch: []string{finished.ID, serverlifecycle.NewID()}})

	sync := link.expect(t, TypeSync).(*Sync)
	require.Equal(t, "inst-1", sync.RuntimeInstanceID)
	require.Len(t, sync.Operations, 1)
	require.Equal(t, finished.ID, sync.Operations[0].ID)
	require.Len(t, sync.Missing, 1)
}

func TestReporterSyncBoundsFinishedHistory(t *testing.T) {
	_, store, _, link := newTestReporter(t)
	var ids []string
	for i := 0; i < recentLimit+5; i++ {
		op := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
		require.NoError(t, store.Create(op))
		op.Phase = serverlifecycle.PhaseFailed
		require.NoError(t, store.Update(op))
		ids = append(ids, op.ID)
	}
	running := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(running))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The oldest finished one is past the bound but explicitly watched.
	link.openSession(ctx, Config{Watch: []string{ids[0]}})

	sync := link.expect(t, TypeSync).(*Sync)
	require.Len(t, sync.Operations, recentLimit+2)
	require.Equal(t, ids[0], sync.Operations[0].ID)
	require.Equal(t, running.ID, sync.Operations[len(sync.Operations)-1].ID)
	require.Empty(t, sync.Missing)
}

func TestReporterStartsRequestedOperation(t *testing.T) {
	_, store, launcher, link := newTestReporter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	link.openSession(ctx, Config{})
	link.expect(t, TypeSync)

	id := serverlifecycle.NewID()
	raw, _ := json.Marshal(Request{OperationID: id, Action: "upgrade", TargetVersion: "v0.16.0", RequestedBy: "cloud:paul"})
	require.NoError(t, link.handlers[TypeRequest](ctx, raw))

	status := link.expect(t, TypeStatus).(Status)
	require.Equal(t, id, status.Operation.ID)
	require.Equal(t, serverlifecycle.ActionUpgrade, status.Operation.Action)
	require.Equal(t, "cloud:paul", status.Operation.RequestedBy)
	require.Equal(t, "inst-1", status.RuntimeInstanceID)

	stored, err := store.Get(id)
	require.NoError(t, err)
	require.Equal(t, "v0.16.0", stored.TargetVersion)
	launcher.mu.Lock()
	require.Equal(t, []string{id}, launcher.launched)
	launcher.mu.Unlock()

	// A repeated request answers with the record, and starts nothing.
	require.NoError(t, link.handlers[TypeRequest](ctx, raw))
	status = link.expect(t, TypeStatus).(Status)
	require.Equal(t, id, status.Operation.ID)
	launcher.mu.Lock()
	require.Len(t, launcher.launched, 1)
	launcher.mu.Unlock()
}

func TestReporterRejectsUnknownAction(t *testing.T) {
	_, _, launcher, link := newTestReporter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	link.openSession(ctx, Config{})
	link.expect(t, TypeSync)

	id := serverlifecycle.NewID()
	raw, _ := json.Marshal(Request{OperationID: id, Action: "reboot-the-moon"})
	require.NoError(t, link.handlers[TypeRequest](ctx, raw))

	reject := link.expect(t, TypeReject).(Reject)
	require.Equal(t, id, reject.OperationID)
	require.Contains(t, reject.Reason, "unsupported action")
	require.Empty(t, launcher.launched)
}

func TestReporterIgnoresRequestsWithoutSession(t *testing.T) {
	_, _, launcher, link := newTestReporter(t)
	raw, _ := json.Marshal(Request{OperationID: serverlifecycle.NewID(), Action: "restart"})
	require.NoError(t, link.handlers[TypeRequest](context.Background(), raw))
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, launcher.launched)
}
