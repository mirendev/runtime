package apphealthsync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/uplink"
)

type fakeLink struct {
	mu       sync.Mutex
	offers   []uplink.CapabilityOffer
	sessions []func(context.Context, uplink.Session)
	batches  []SampleBatch
	sendErr  error
	handler  uplink.MessageHandler
}

func newFakeLink() *fakeLink {
	return &fakeLink{}
}

func (f *fakeLink) OfferCapability(offer uplink.CapabilityOffer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offers = append(f.offers, offer)
}

func (f *fakeLink) OnSession(fn func(context.Context, uplink.Session)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, fn)
}

func (f *fakeLink) SendMessageBlocking(_ context.Context, msgType string, data any) error {
	f.mu.Lock()
	if f.sendErr != nil {
		err := f.sendErr
		f.mu.Unlock()
		return err
	}
	if msgType != TypeSamples {
		f.mu.Unlock()
		return errors.New("unexpected message type " + msgType)
	}
	f.batches = append(f.batches, data.(SampleBatch))
	f.mu.Unlock()

	return nil
}

func (f *fakeLink) allBatches() []SampleBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SampleBatch(nil), f.batches...)
}

// appsIn flattens every batch the link received into one name-keyed map, which
// is what a receiver that does not distinguish full from delta actually sees.
func (f *fakeLink) appsIn() map[string]Sample {
	out := map[string]Sample{}
	for _, batch := range f.allBatches() {
		for _, sample := range batch.Apps {
			out[sample.Name] = sample
		}
	}
	return out
}

func (f *fakeLink) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

type fakeSource struct {
	mu    sync.Mutex
	apps  []apphealth.State
	err   error
	calls int
}

func (f *fakeSource) ListAppHealth(context.Context) ([]apphealth.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.apps, nil
}

func (f *fakeSource) set(apps ...apphealth.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = apps
}

func (f *fakeSource) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeSource) derivations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeLink) Handle(_ string, handler uplink.MessageHandler) { f.handler = handler }

func startSampler(t *testing.T) (*Reporter, *fakeSource, *fakeLink, context.CancelFunc) {
	t.Helper()
	source := &fakeSource{apps: []apphealth.State{{Name: "web", Health: apphealth.Crashed, InCooldown: true, CooldownSeconds: 0}}}
	link := newFakeLink()
	reporter := NewReporter(slog.New(slog.DiscardHandler), source)
	require.NoError(t, reporter.Register(context.Background(), link))
	ctx, cancel := context.WithCancel(context.Background())
	link.sessions[0](ctx, uplink.Session{ID: "s1", Capabilities: []uplink.CapabilitySelection{{Name: uplink.CapabilityAppHealth, Version: Version1, Config: json.RawMessage(`{"background_seconds":300}`)}}})
	time.Sleep(time.Minute)
	synctest.Wait()
	require.Equal(t, 1, link.sendCount())
	return reporter, source, link, cancel
}

func demand(t *testing.T, link *fakeLink, session string, interval, lease int) {
	t.Helper()
	raw, err := json.Marshal(Demand{SessionID: session, IntervalSeconds: interval, LeaseSeconds: lease})
	require.NoError(t, err)
	require.NoError(t, link.handler(context.Background(), raw))
	synctest.Wait()
}

func TestSamplingFollowsDemandAndExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, source, link, cancel := startSampler(t)
		defer cancel()
		source.set(apphealth.State{Name: "web", Health: apphealth.Starting})
		demand(t, link, "s1", 5, 20)
		require.Equal(t, 2, link.sendCount())
		require.Equal(t, apphealth.Starting, link.appsIn()["web"].Health)
		for range 12 {
			time.Sleep(time.Second)
			demand(t, link, "s1", 5, 20)
		}
		require.Equal(t, 4, link.sendCount(), "renewals neither derive nor postpone sampling")
		time.Sleep(21 * time.Second)
		synctest.Wait()
		count := link.sendCount()
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, count, link.sendCount(), "expired interest returns to background cadence")
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		require.Greater(t, link.sendCount(), count, "background samples repair unchanged state")
	})
}

func TestSessionFencingAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, source, link, cancel := startSampler(t)
		demand(t, link, "old-session", 5, 20)
		require.Equal(t, 1, link.sendCount())
		cancel()
		synctest.Wait()
		before := source.derivations()
		demand(t, link, "s1", 5, 20)
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Equal(t, before, source.derivations())
	})
}

func TestSamplesRecoverAfterReadAndSendFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, source, link, cancel := startSampler(t)
		defer cancel()
		source.setErr(errors.New("unavailable"))
		demand(t, link, "s1", 5, 20)
		require.Equal(t, 1, link.sendCount())
		source.setErr(nil)
		link.mu.Lock()
		link.sendErr = errors.New("full")
		link.mu.Unlock()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.Equal(t, 1, link.sendCount())
		link.mu.Lock()
		link.sendErr = nil
		link.mu.Unlock()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.Equal(t, 2, link.sendCount())
	})
}

func TestBatchesShareObservationTime(t *testing.T) {
	source := &fakeSource{apps: make([]apphealth.State, 205)}
	link := newFakeLink()
	NewReporter(slog.New(slog.DiscardHandler), source).sample(context.Background(), link)
	batches := link.allBatches()
	require.Len(t, batches, 3)
	require.Len(t, batches[0].Apps, 100)
	require.Len(t, batches[2].Apps, 5)
	require.Equal(t, batches[0].ObservedAt, batches[2].ObservedAt)
}

func TestUnselectedCapabilityAndInvalidDemand(t *testing.T) {
	source := &fakeSource{}
	link := newFakeLink()
	require.NoError(t, NewReporter(slog.New(slog.DiscardHandler), source).Register(context.Background(), link))
	require.Equal(t, uplink.CapabilityAppHealth, link.offers[0].Name)
	link.sessions[0](context.Background(), uplink.Session{ID: "s1"})
	require.Zero(t, source.derivations())
	for _, raw := range []string{`{`, `{"interval_seconds":0,"lease_seconds":20}`, `{"interval_seconds":5,"lease_seconds":61}`} {
		require.Error(t, link.handler(context.Background(), json.RawMessage(raw)))
	}
}

func TestDemandBypassesConnectSpreadAndReconnectDropsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeSource{apps: []apphealth.State{{Name: "web", Health: apphealth.Healthy}}}
		link := newFakeLink()
		reporter := NewReporter(slog.New(slog.DiscardHandler), source)
		require.NoError(t, reporter.Register(context.Background(), link))
		ctx, cancel := context.WithCancel(context.Background())
		session := uplink.Session{ID: "first", Capabilities: []uplink.CapabilitySelection{{Name: uplink.CapabilityAppHealth, Version: Version1}}}
		link.sessions[0](ctx, session)
		demand(t, link, "first", 5, 20)
		require.Equal(t, 1, link.sendCount(), "a viewer does not wait for the connection spread")
		cancel()
		synctest.Wait()
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		session.ID = "second"
		link.sessions[0](ctx, session)
		demand(t, link, "first", 5, 20)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, link.sendCount(), "new session only sends its opening sample")
	})
}

func TestNegotiatedBackgroundCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeSource{apps: []apphealth.State{{Name: "web"}}}
		link := newFakeLink()
		reporter := NewReporter(slog.New(slog.DiscardHandler), source)
		require.NoError(t, reporter.Register(context.Background(), link))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		link.sessions[0](ctx, uplink.Session{ID: "s1", Capabilities: []uplink.CapabilitySelection{{Name: uplink.CapabilityAppHealth, Version: Version1, Config: json.RawMessage(`{"background_seconds":120}`)}}})
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 1, link.sendCount())
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, link.sendCount())
	})
}
