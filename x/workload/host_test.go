package workload

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type metadataFixture struct {
	mu              sync.Mutex
	snapshot        Snapshot
	changed         chan struct{}
	activity        chan string
	deadline        time.Time
	ack             func(string) int
	sessionActivity func(string, string) int
	detachAck       func(string, time.Time) int
}

func (f *metadataFixture) update(s Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = s
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *metadataFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer fixture-secret" {
		t.Error("incorrect metadata authentication")
	}
	switch r.URL.Path {
	case "/v1/sessions":
		f.mu.Lock()
		s, changed := f.snapshot, f.changed
		f.mu.Unlock()
		if r.URL.Query().Get("wait") == s.Version {
			select {
			case <-r.Context().Done():
				return
			case <-changed:
			}
			f.mu.Lock()
			s = f.snapshot
			f.mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(s)
	case "/v1/activity":
		var report struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		f.activity <- report.State
		f.mu.Lock()
		deadline := f.deadline
		f.mu.Unlock()
		if !deadline.IsZero() {
			w.Header().Set("Miren-Shutdown-At", deadline.Format(time.RFC3339Nano))
		}
		w.WriteHeader(http.StatusNoContent)
	case "/v1/sessions/activity":
		if f.sessionActivity != nil {
			var report struct {
				Session string `json:"session"`
				State   string `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&report)
			w.WriteHeader(f.sessionActivity(report.Session, report.State))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "/v1/sessions/detachments/ack":
		var ack struct {
			Session    string    `json:"session"`
			DetachedAt time.Time `json:"detached_at"`
		}
		_ = json.NewDecoder(r.Body).Decode(&ack)
		w.WriteHeader(f.detachAck(ack.Session, ack.DetachedAt))
	case "/v1/sessions/deletions/ack":
		var ack struct {
			Session string `json:"session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&ack)
		w.WriteHeader(f.ack(ack.Session))
	default:
		t.Errorf("unexpected metadata path %s", r.URL.Path)
		w.WriteHeader(404)
	}
}

func eventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startHost(t *testing.T, f *metadataFixture, start StartFunc) *Host {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	t.Cleanup(s.Close)
	h, err := NewHost(Config{URL: s.URL + "/v1", Secret: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx, start) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(4 * time.Second):
			t.Error("host did not stop")
		}
	})
	eventually(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.assignments) == len(f.snapshot.Sessions) })
	return h
}

func fixture(ids ...string) *metadataFixture {
	details := map[string]Session{}
	for _, id := range ids {
		details[id] = Session{App: "app/worker", Version: "app_version/one", Service: "worker", Spec: json.RawMessage(`{"container":[{"image":"agent:v7"}]}`)}
	}
	return &metadataFixture{snapshot: Snapshot{Sessions: ids, Details: details, Version: "initial"}, changed: make(chan struct{}), activity: make(chan string, 128)}
}

func TestHostCleanupPrecedesAckAndRetriesWithoutRestartingNeighbours(t *testing.T) {
	f := fixture("session/a", "session/b")
	var allowCleanup, cleaned atomic.Bool
	var starts, failures, acks atomic.Int32
	contexts := make(map[string]context.Context)
	var contextsMu sync.Mutex
	f.ack = func(id string) int {
		if id != "session/a" || !cleaned.Load() {
			t.Error("ack before cleanup or for wrong Session")
		}
		if acks.Add(1) == 1 {
			return http.StatusServiceUnavailable
		}
		f.update(Snapshot{Sessions: []string{"session/b"}, Details: f.snapshot.Details, Version: "acked"})
		// A retried ack may find a binding already swept after the first write.
		return http.StatusNotFound
	}
	h := startHost(t, f, func(ctx context.Context, s Session) (StopFunc, error) {
		if s.ID == "" || s.App != "app/worker" || string(s.Spec) != `{"container":[{"image":"agent:v7"}]}` {
			t.Error("missing assignment details")
		}
		starts.Add(1)
		contextsMu.Lock()
		contexts[s.ID] = ctx
		contextsMu.Unlock()
		return func(_ context.Context, reason StopReason) error {
			if ctx.Err() == nil {
				t.Error("cleanup must be preceded by cancellation")
			}
			if s.ID == "session/a" {
				if reason != StopDeleted {
					t.Errorf("deletion cleanup reason: %s", reason)
				}
				if !allowCleanup.Load() {
					failures.Add(1)
					return errors.New("still closing")
				}
				cleaned.Store(true)
			}
			return nil
		}, nil
	})
	f.update(Snapshot{Sessions: []string{"session/b"}, Details: f.snapshot.Details, Deleted: []string{"session/a"}, Version: "deleted"})
	eventually(t, func() bool { return failures.Load() > 0 })
	if acks.Load() != 0 {
		t.Fatal("acknowledged failed cleanup")
	}
	if _, err := h.Begin(t.Context(), "session/a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("removed Session accepted work: %v", err)
	}
	contextsMu.Lock()
	a, b := contexts["session/a"], contexts["session/b"]
	contextsMu.Unlock()
	if a.Err() == nil || b.Err() != nil {
		t.Fatal("deletion did not isolate cancellation")
	}
	allowCleanup.Store(true)
	eventually(t, func() bool { return acks.Load() >= 2 })
	if starts.Load() != 2 {
		t.Fatal("retry restarted a live agent")
	}
}

func TestHostAggregatesQueuedWorkAndLatchesDrainBeforeAdmission(t *testing.T) {
	f := fixture("session/a", "session/b")
	h := startHost(t, f, func(context.Context, Session) (StopFunc, error) {
		return func(context.Context, StopReason) error { return nil }, nil
	})
	releaseA, err := h.Begin(t.Context(), "session/a")
	if err != nil {
		t.Fatal(err)
	}
	releaseB, err := h.Begin(t.Context(), "session/b")
	if err != nil {
		t.Fatal(err)
	}
	releaseA()
	releaseA()
	if err := h.report(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Inspect the last completed report, not an earlier active request.
	var state string
	for len(f.activity) > 0 {
		state = <-f.activity
	}
	if state != "active" {
		t.Fatalf("neighbour's queued work became idle: %s", state)
	}
	releaseB()
	if err := h.report(t.Context()); err != nil {
		t.Fatal(err)
	}
	for len(f.activity) > 0 {
		state = <-f.activity
	}
	if state != "idle" {
		t.Fatalf("released work stayed active: %s", state)
	}
	deadline := time.Now().Add(time.Minute).UTC()
	f.mu.Lock()
	f.deadline = deadline
	f.mu.Unlock()
	if release, err := h.Begin(t.Context(), "session/a"); !errors.Is(err, ErrDraining) || release != nil {
		t.Fatalf("shutdown race admitted work: %v", err)
	}
	select {
	case <-h.Draining():
	default:
		t.Fatal("no drain notification")
	}
	if !h.ShutdownAt().Equal(deadline) {
		t.Fatal("lost shutdown deadline")
	}
	f.mu.Lock()
	f.deadline = time.Time{}
	f.mu.Unlock()
	if err := h.report(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Begin(t.Context(), "session/b"); !errors.Is(err, ErrDraining) {
		t.Fatal("missing header reopened admission")
	}
}

func TestClientProtocolAndRedirectSafety(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("wait") != "cursor+&/" {
			t.Error("cursor was not escaped")
		}
		if r.Header.Get("Authorization") != "Bearer private-secret" {
			t.Error("missing authentication")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer s.Close()
	c, err := NewClient(Config{URL: s.URL + "/v1/", Secret: "private-secret"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Sessions(t.Context(), "cursor+&/")
	if err != nil || result != nil {
		t.Fatal("304 must mean unchanged")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.URL+"/v1/sessions", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c, err = NewClient(Config{URL: redirect.URL + "/v1", Secret: "private-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Sessions(t.Context(), "")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTemporaryRedirect || requests.Load() != 1 {
		t.Fatalf("redirect followed or wrong error: %v", err)
	}
}

func TestAdmissionWaitsForEarlierIdleReport(t *testing.T) {
	idleStarted := make(chan struct{})
	idleRelease := make(chan struct{})
	var once sync.Once
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sessions/activity" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var report struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.State == "idle" {
			close(idleStarted)
			<-idleRelease
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(s.Close)
	t.Cleanup(func() { once.Do(func() { close(idleRelease) }) })
	h, err := NewHost(Config{URL: s.URL + "/v1", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	h.assignments["session/a"] = &assignment{ctx: t.Context(), accepting: true}
	reportDone := make(chan error, 1)
	go func() { reportDone <- h.report(t.Context()) }()
	<-idleStarted
	beginDone := make(chan error, 1)
	go func() { _, err := h.Begin(t.Context(), "session/a"); beginDone <- err }()
	select {
	case err := <-beginDone:
		t.Fatalf("admission overtook an earlier idle report: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	once.Do(func() { close(idleRelease) })
	if err := <-reportDone; err != nil {
		t.Fatal(err)
	}
	if err := <-beginDone; err != nil {
		t.Fatal(err)
	}
}

func TestFailedAdmissionReleasesItsActivityReservation(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var lastState atomic.Value
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&report)
		lastState.Store(report.State)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer s.Close()
	h, err := NewHost(Config{URL: s.URL + "/v1", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.assignments["session/a"] = &assignment{ctx: ctx, accepting: true}
	if release, err := h.Begin(t.Context(), "session/a"); err == nil || release != nil {
		t.Fatal("failed report admitted work")
	}
	fail.Store(false)
	if err := h.report(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lastState.Load() != "idle" {
		t.Fatalf("failed admission leaked active work: %v", lastState.Load())
	}
	cancel()
	if _, err := h.Begin(t.Context(), "session/a"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("canceled assignment accepted work before cleanup ran")
	}
}

func TestHostIndividualActivityAndParkedAdmission(t *testing.T) {
	f := fixture("session/a", "session/b")
	var mu sync.Mutex
	states := map[string]string{}
	var parked atomic.Bool
	f.sessionActivity = func(id, state string) int {
		mu.Lock()
		states[id] = state
		mu.Unlock()
		if id == "session/a" && parked.Load() {
			return http.StatusConflict
		}
		return http.StatusNoContent
	}
	h := startHost(t, f, func(context.Context, Session) (StopFunc, error) {
		return func(context.Context, StopReason) error { return nil }, nil
	})
	release, err := h.Begin(t.Context(), "session/a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	mu.Lock()
	a, b := states["session/a"], states["session/b"]
	mu.Unlock()
	if a != "active" || b != "idle" {
		t.Fatalf("individual reports: a=%s b=%s", a, b)
	}
	parked.Store(true)
	if release, err := h.Begin(t.Context(), "session/a"); !errors.Is(err, ErrUnavailable) || release != nil {
		t.Fatalf("parked Session accepted work: %v", err)
	}
	releaseB, err := h.Begin(t.Context(), "session/b")
	if err != nil {
		t.Fatalf("parking a blocked b: %v", err)
	}
	releaseB()
}

func TestHostDetachmentCleanupAndReassignment(t *testing.T) {
	f := fixture("session/a", "session/b")
	var allow, cleaned atomic.Bool
	var starts, attempts, acks atomic.Int32
	notice := time.Now()
	f.detachAck = func(id string, at time.Time) int {
		if id != "session/a" || !at.Equal(notice) || !cleaned.Load() {
			t.Error("detachment acknowledged before cleanup or with wrong notice")
		}
		if acks.Add(1) == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusNoContent
	}
	h := startHost(t, f, func(_ context.Context, s Session) (StopFunc, error) {
		starts.Add(1)
		return func(_ context.Context, reason StopReason) error {
			if reason != StopShutdown && reason != StopDetached {
				t.Errorf("detachment cleanup reason: %s", reason)
			}
			if s.ID == "session/a" {
				attempts.Add(1)
				if !allow.Load() {
					return errors.New("cleanup blocked")
				}
				cleaned.Store(true)
			}
			return nil
		}, nil
	})
	f.update(Snapshot{Sessions: []string{"session/b"}, Details: f.snapshot.Details,
		Detached: map[string]time.Time{"session/a": notice}, Version: "detaching"})
	eventually(t, func() bool { return attempts.Load() > 0 })
	if acks.Load() != 0 {
		t.Fatal("failed cleanup released capacity")
	}
	if _, err := h.Begin(t.Context(), "session/a"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	allow.Store(true)
	eventually(t, func() bool { return acks.Load() >= 2 })
	f.update(Snapshot{Sessions: []string{"session/a", "session/b"}, Details: f.snapshot.Details, Version: "reassigned"})
	eventually(t, func() bool { return starts.Load() == 3 })
	release, err := h.Begin(t.Context(), "session/a")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestHostStopReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason StopReason
		next   StopReason
	}{
		{"detachment", StopDetached, ""},
		{"deletion", StopDeleted, ""},
		{"unexplained removal", StopRemoved, ""},
		{"shutdown", StopShutdown, ""},
		{"shutdown during deletion cleanup", StopDeleted, StopShutdown},
		{"shutdown during detachment cleanup", StopDetached, StopShutdown},
		{"deletion during detachment cleanup", StopDetached, StopDeleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture("session/a")
			f.ack = func(string) int { return http.StatusNoContent }
			f.detachAck = func(string, time.Time) int { return http.StatusNoContent }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
			t.Cleanup(server.Close)
			h, err := NewHost(Config{URL: server.URL + "/v1", Secret: "fixture-secret"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			started := make(chan struct{})
			reasons := make(chan StopReason, 4)
			proceed := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- h.Run(ctx, func(loopCtx context.Context, _ Session) (StopFunc, error) {
					close(started)
					attempts := 0
					return func(cleanup context.Context, reason StopReason) error {
						if loopCtx.Err() == nil {
							t.Error("cleanup preceded cancellation")
						}
						attempts++
						reasons <- reason
						if tc.next != "" && attempts == 1 {
							select {
							case <-cleanup.Done():
								return cleanup.Err()
							case <-proceed:
								return errors.New("cleanup pending")
							}
						}
						return nil
					}, nil
				})
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(4 * time.Second):
					t.Error("host did not stop")
				}
			})
			select {
			case <-started:
			case <-time.After(4 * time.Second):
				t.Fatal("assignment did not start")
			}
			if tc.reason == StopShutdown {
				cancel()
			} else {
				snapshot := Snapshot{Version: "removed"}
				switch tc.reason {
				case StopDeleted:
					snapshot.Deleted = []string{"session/a"}
				case StopDetached:
					snapshot.Detached = map[string]time.Time{"session/a": time.Now()}
				case StopRemoved, StopShutdown:
				}
				f.update(snapshot)
			}
			for attempt := 0; attempt < 1 || (tc.next != "" && attempt < 2); attempt++ {
				want := tc.reason
				if attempt > 0 && tc.next == StopDeleted {
					want = StopDeleted
				}
				select {
				case reason := <-reasons:
					if reason != want {
						t.Fatalf("stop reason = %s, want %s", reason, want)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("cleanup was not called")
				}
				if attempt == 0 && tc.next != "" {
					if tc.next == StopShutdown {
						cancel()
					} else {
						f.update(Snapshot{Deleted: []string{"session/a"}, Version: "deleted"})
					}
					close(proceed)
				}
			}
		})
	}
}
