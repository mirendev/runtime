package coordinate

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

// recordingSender captures what the reporter puts on the wire, standing in for
// the uplink so these tests exercise the message sequence rather than a socket.
type recordingSender struct {
	sent []sentMessage
}

type sentMessage struct {
	Type string
	Data any
}

func (r *recordingSender) SendMessageBlocking(_ context.Context, msgType string, data any) error {
	r.sent = append(r.sent, sentMessage{Type: msgType, Data: data})
	return nil
}

func (r *recordingSender) ofType(msgType string) []sentMessage {
	var out []sentMessage
	for _, m := range r.sent {
		if m.Type == msgType {
			out = append(out, m)
		}
	}
	return out
}

func testCoordinator() *Coordinator {
	return &Coordinator{Log: slog.Default()}
}

func appStates(names ...string) map[string]AppState {
	state := make(map[string]AppState, len(names))
	for _, n := range names {
		state[n] = AppState{Name: n, Health: "healthy", ReadyInstances: 1, DesiredInstances: 1}
	}
	return state
}

// A snapshot larger than one batch has to arrive as several messages that
// cloud can recognize as one epoch, and the closing count has to describe the
// whole snapshot rather than the last batch. Cloud's sweep is gated on exactly
// that comparison, so getting the count wrong here disables deletes entirely.
func TestSnapshotBatchesUnderOneEpoch(t *testing.T) {
	const total = snapshotBatchSize*2 + 5

	names := make([]string, 0, total)
	for i := range total {
		names = append(names, string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	state := appStates(names...)

	c := testCoordinator()
	sender := &recordingSender{}

	if err := c.sendAppSnapshot(t.Context(), sender, state); err != nil {
		t.Fatalf("sendAppSnapshot: %v", err)
	}

	batches := sender.ofType(TypeAppSnapshot)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches for %d apps, got %d", total, len(batches))
	}

	epoch := ""
	seen := map[string]bool{}
	for i, m := range batches {
		batch, ok := m.Data.(AppSnapshot)
		if !ok {
			t.Fatalf("batch %d carried %T, not AppSnapshot", i, m.Data)
		}
		if len(batch.Apps) > snapshotBatchSize {
			t.Errorf("batch %d carried %d apps, over the %d bound", i, len(batch.Apps), snapshotBatchSize)
		}
		if epoch == "" {
			epoch = batch.Epoch
		} else if batch.Epoch != epoch {
			t.Errorf("batch %d used epoch %q, expected %q", i, batch.Epoch, epoch)
		}
		for _, a := range batch.Apps {
			if seen[a.Name] {
				t.Errorf("app %q appeared in more than one batch", a.Name)
			}
			seen[a.Name] = true
		}
	}

	if len(seen) != total {
		t.Errorf("epoch carried %d distinct apps, expected %d", len(seen), total)
	}

	complete := sender.ofType(TypeAppSnapshotComplete)
	if len(complete) != 1 {
		t.Fatalf("expected exactly one complete message, got %d", len(complete))
	}
	done, ok := complete[0].Data.(AppSnapshotComplete)
	if !ok {
		t.Fatalf("complete carried %T, not AppSnapshotComplete", complete[0].Data)
	}
	if done.Epoch != epoch {
		t.Errorf("complete closed epoch %q, batches used %q", done.Epoch, epoch)
	}
	if done.AppCount != total {
		t.Errorf("complete claimed %d apps, sent %d", done.AppCount, total)
	}

	// The closing message must come last, or cloud would sweep against a
	// partially accumulated epoch and delete apps still in flight behind it.
	if last := sender.sent[len(sender.sent)-1]; last.Type != TypeAppSnapshotComplete {
		t.Errorf("epoch closed with a %s, expected the complete message last", last.Type)
	}
}

// The case where sweeping matters most is the one with nothing to send: every
// app was deleted. An epoch has to close even with no batches, which is why
// completion is its own message rather than a flag on the last batch.
func TestSnapshotClosesEpochWithNoApps(t *testing.T) {
	c := testCoordinator()
	sender := &recordingSender{}

	if err := c.sendAppSnapshot(t.Context(), sender, map[string]AppState{}); err != nil {
		t.Fatalf("sendAppSnapshot: %v", err)
	}

	if batches := sender.ofType(TypeAppSnapshot); len(batches) != 0 {
		t.Errorf("expected no batches for an empty cluster, got %d", len(batches))
	}

	complete := sender.ofType(TypeAppSnapshotComplete)
	if len(complete) != 1 {
		t.Fatalf("an empty cluster must still close its epoch, got %d complete messages", len(complete))
	}
	if done := complete[0].Data.(AppSnapshotComplete); done.AppCount != 0 {
		t.Errorf("expected a count of 0, got %d", done.AppCount)
	}
}

// Deltas exist to keep an idle cluster quiet between snapshots. If unchanged
// apps went on the wire, a 15-second sample interval would become a
// 15-second write cycle against every app in cloud.
func TestDeltasReportOnlyWhatMoved(t *testing.T) {
	previous := appStates("web", "worker", "cron")

	current := appStates("web", "worker", "cron")
	current["web"] = AppState{Name: "web", Health: "degraded", ReadyInstances: 1, DesiredInstances: 1}
	current["worker"] = AppState{Name: "worker", Health: "healthy", ReadyInstances: 3, DesiredInstances: 3}
	current["api"] = AppState{Name: "api", Health: "starting", ReadyInstances: 0, DesiredInstances: 1}

	c := testCoordinator()
	sender := &recordingSender{}

	if err := c.sendAppDeltas(t.Context(), sender, previous, current); err != nil {
		t.Fatalf("sendAppDeltas: %v", err)
	}

	// Everything that moved rides in one message. Changes cluster in time, so a
	// message per app would turn a rolling deploy into a burst of envelopes that
	// cloud pays for individually.
	messages := sender.ofType(TypeAppStatus)
	if len(messages) != 1 {
		t.Fatalf("expected one delta message for three changed apps, got %d", len(messages))
	}

	delta, ok := messages[0].Data.(AppStatus)
	if !ok {
		t.Fatalf("delta carried %T, not AppStatus", messages[0].Data)
	}

	reported := map[string]AppState{}
	for _, app := range delta.Apps {
		reported[app.Name] = app
	}

	if len(reported) != 3 {
		t.Fatalf("expected deltas for web, worker and api, got %v", reported)
	}
	if _, ok := reported["cron"]; ok {
		t.Error("reported a delta for an app that did not change")
	}
	if got := reported["web"].Health; got != "degraded" {
		t.Errorf("web reported health %q, expected degraded", got)
	}
	if got := reported["worker"].ReadyInstances; got != 3 {
		t.Errorf("worker reported %d ready instances, expected 3", got)
	}
}

// A delta cannot express a delete, and inventing one would reintroduce exactly
// the lose-a-message failure that mark-and-sweep exists to avoid. A vanished
// app must stay silent and wait for the next epoch to sweep it.
func TestDeltasStaySilentAboutRemovedApps(t *testing.T) {
	previous := appStates("web", "worker")
	current := appStates("web")

	c := testCoordinator()
	sender := &recordingSender{}

	if err := c.sendAppDeltas(t.Context(), sender, previous, current); err != nil {
		t.Fatalf("sendAppDeltas: %v", err)
	}

	if got := len(sender.sent); got != 0 {
		t.Fatalf("expected silence when an app disappears, got %d messages: %+v", got, sender.sent)
	}
}

// A removal is the one change a delta cannot carry, so it has to promote the
// tick into a full snapshot. Without this, how long a deleted app keeps
// showing up in the console would be set by the snapshot floor, which is tuned
// for a rare failure rather than for something a user did on purpose and is
// actively looking for.
func TestRemovalPromotesTheTickToASnapshot(t *testing.T) {
	previous := appStates("web", "worker")

	if !removedAny(previous, appStates("web")) {
		t.Error("an app disappearing should force a snapshot")
	}
	if !removedAny(previous, appStates()) {
		t.Error("every app disappearing should force a snapshot")
	}
}

// The cases that must stay on the cheap path. Snapshots are the expensive
// message, and promoting a tick that only changed a health value or gained an
// app would put the whole fleet back to re-sending everything on every change.
func TestOrdinaryChangesStayOnTheDeltaPath(t *testing.T) {
	previous := appStates("web", "worker")

	if removedAny(previous, appStates("web", "worker")) {
		t.Error("an unchanged app set should not force a snapshot")
	}
	if removedAny(previous, appStates("web", "worker", "api")) {
		t.Error("a new app arrives as a delta and should not force a snapshot")
	}

	flipped := appStates("web", "worker")
	flipped["web"] = AppState{Name: "web", Health: "crashed"}
	if removedAny(previous, flipped) {
		t.Error("a health change should not force a snapshot")
	}

	// A rename is a delete plus an add, and the delete half is what matters:
	// without a snapshot the old name would linger in the console forever.
	if !removedAny(previous, appStates("web", "worker-renamed")) {
		t.Error("a rename should force a snapshot so the old name is swept")
	}
}

// The wire shape is the contract every downstream consumer builds on, and
// cloud unmarshals these by field name. Renaming one is a silent break, so the
// JSON is pinned here rather than left to whatever the struct happens to emit.
func TestSnapshotWireShape(t *testing.T) {
	raw, err := json.Marshal(AppSnapshot{
		Epoch: "01J000000000000000000000AB",
		Apps: []AppState{{
			Name:             "web",
			Health:           "healthy",
			ReadyInstances:   2,
			DesiredInstances: 3,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var shape struct {
		Epoch      string `json:"epoch"`
		ObservedAt string `json:"observed_at"`
		Apps       []struct {
			Name             string `json:"name"`
			Health           string `json:"health"`
			ReadyInstances   int32  `json:"ready_instances"`
			DesiredInstances int32  `json:"desired_instances"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if shape.Epoch == "" || len(shape.Apps) != 1 {
		t.Fatalf("snapshot did not round-trip: %s", raw)
	}

	// The delta shares the snapshot's shape for its app list, so a consumer can
	// decode either with one type. Divergence here is the kind that only shows
	// up as a field silently reading zero on the far side.
	rawDelta, err := json.Marshal(AppStatus{
		Apps: []AppState{{Name: "web", Health: "healthy", ReadyInstances: 2}},
	})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}

	var deltaShape struct {
		ObservedAt string `json:"observed_at"`
		Apps       []struct {
			Name           string `json:"name"`
			Health         string `json:"health"`
			ReadyInstances int32  `json:"ready_instances"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(rawDelta, &deltaShape); err != nil {
		t.Fatalf("unmarshal delta: %v", err)
	}
	if len(deltaShape.Apps) != 1 || deltaShape.Apps[0].Name != "web" || deltaShape.Apps[0].ReadyInstances != 2 {
		t.Errorf("delta did not round-trip under the snapshot's app shape: %s", rawDelta)
	}

	// The completion message carries no timestamp on purpose.
	rawComplete, err := json.Marshal(AppSnapshotComplete{Epoch: "e", AppCount: 3})
	if err != nil {
		t.Fatalf("marshal complete: %v", err)
	}
	if bytes.Contains(rawComplete, []byte("observed_at")) {
		t.Errorf("complete grew a timestamp nothing reads: %s", rawComplete)
	}
	app := shape.Apps[0]
	if app.Name != "web" || app.Health != "healthy" {
		t.Errorf("app identity did not round-trip: %s", raw)
	}
	if app.ReadyInstances != 2 || app.DesiredInstances != 3 {
		t.Errorf("instance counts did not round-trip: %s", raw)
	}
}

// The three-way decision each tick makes — snapshot, deltas, or skip — is the
// one part of the reporter nothing else covers, because the real intervals are
// minutes long. These drive reportLoop directly with a stubbed state source and
// a millisecond clock.

// collectSent drains a recorder until it has seen wantTypes worth of messages
// or the deadline passes, so a test asserts on what the loop chose rather than
// on how fast the machine ran it.
func waitForMessages(t *testing.T, sender *lockedSender, n int) []sentMessage {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sender.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	return sender.snapshot()
}

// lockedSender is recordingSender with a mutex, since the loop runs on its own
// goroutine here.
type lockedSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (l *lockedSender) SendMessageBlocking(_ context.Context, msgType string, data any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sent = append(l.sent, sentMessage{Type: msgType, Data: data})
	return nil
}

func (l *lockedSender) snapshot() []sentMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.sent)
}

func fastTiming() reportTiming {
	return reportTiming{spread: 0, sample: 5 * time.Millisecond, floor: time.Hour}
}

func countOfType(msgs []sentMessage, msgType string) int {
	n := 0
	for _, m := range msgs {
		if m.Type == msgType {
			n++
		}
	}
	return n
}

// An opening snapshot that fails must leave lastSent nil, so the next tick
// tries a snapshot again rather than sending deltas against a baseline that was
// never established. Getting this wrong would have cloud's first picture of a
// cluster be a delta describing changes from nothing.
func TestFailedOpeningSnapshotRetriesAsSnapshot(t *testing.T) {
	c := testCoordinator()
	sender := &failThenRecord{failures: 2}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	state := appStates("web")
	go c.reportLoop(ctx, sender, func(context.Context) (map[string]AppState, error) {
		return state, nil
	}, fastTiming())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && countOfType(sender.snapshot(), TypeAppSnapshotComplete) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	got := sender.snapshot()
	if countOfType(got, TypeAppStatus) != 0 {
		t.Errorf("sent deltas before any snapshot succeeded: %+v", got)
	}
	if countOfType(got, TypeAppSnapshotComplete) == 0 {
		t.Error("never retried the snapshot after the opening one failed")
	}
}

// Once a snapshot lands, ordinary changes ride deltas. If this regressed to
// sending snapshots the fleet would re-send everything on every tick.
func TestSteadyStateSendsDeltasNotSnapshots(t *testing.T) {
	c := testCoordinator()
	sender := &lockedSender{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var mu sync.Mutex
	health := "healthy"
	go c.reportLoop(ctx, sender, func(context.Context) (map[string]AppState, error) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]AppState{"web": {Name: "web", Health: health}}, nil
	}, fastTiming())

	waitForMessages(t, sender, 2) // the opening snapshot plus its completion

	mu.Lock()
	health = "degraded"
	mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && countOfType(sender.snapshot(), TypeAppStatus) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	got := sender.snapshot()
	if countOfType(got, TypeAppStatus) == 0 {
		t.Error("a health change should have gone out as a delta")
	}
	if n := countOfType(got, TypeAppSnapshotComplete); n != 1 {
		t.Errorf("expected exactly the opening snapshot, got %d completions", n)
	}
}

// A removal promotes the tick to a full snapshot, end to end through the loop.
// Deltas cannot carry a delete, so without this the app lingers in the console
// until the floor expires.
func TestRemovalPromotesTheTickEndToEnd(t *testing.T) {
	c := testCoordinator()
	sender := &lockedSender{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var mu sync.Mutex
	apps := appStates("web", "worker")
	go c.reportLoop(ctx, sender, func(context.Context) (map[string]AppState, error) {
		mu.Lock()
		defer mu.Unlock()
		return apps, nil
	}, fastTiming())

	waitForMessages(t, sender, 2)

	mu.Lock()
	apps = appStates("web")
	mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && countOfType(sender.snapshot(), TypeAppSnapshotComplete) < 2 {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	if n := countOfType(sender.snapshot(), TypeAppSnapshotComplete); n < 2 {
		t.Errorf("a removal should have forced a second snapshot, saw %d completions", n)
	}
}

// failThenRecord fails its first few sends, standing in for a link that cannot
// carry the opening snapshot yet.
type failThenRecord struct {
	mu       sync.Mutex
	failures int
	sent     []sentMessage
}

func (f *failThenRecord) SendMessageBlocking(_ context.Context, msgType string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return context.DeadlineExceeded
	}
	f.sent = append(f.sent, sentMessage{Type: msgType, Data: data})
	return nil
}

func (f *failThenRecord) snapshot() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}
