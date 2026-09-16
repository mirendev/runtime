package clusterresources

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/uplink"
)

type fakeLink struct {
	mu       sync.Mutex
	offers   []uplink.CapabilityOffer
	sessions []func(context.Context, uplink.Session)
	samples  []Sample
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
	defer f.mu.Unlock()
	if msgType != TypeSample {
		panic("unexpected message type " + msgType)
	}
	f.samples = append(f.samples, data.(Sample))
	return nil
}

func (f *fakeLink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.samples)
}

func (f *fakeLink) first() Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.samples[0]
}

type fakeSource struct{}

func (fakeSource) ResourceSample() Sample {
	return Sample{CPUCores: 8, CPUPercent: 12.5, MemoryBytes: 16 << 30, MemoryPercent: 40, StorageBytes: 512 << 30, StoragePercent: 61}
}

func start(t *testing.T, config json.RawMessage, selected bool) (*fakeLink, context.CancelFunc) {
	t.Helper()
	link := &fakeLink{}
	require.NoError(t, NewReporter(slog.Default(), fakeSource{}).Register(context.Background(), link))
	require.Equal(t, []uplink.CapabilityOffer{{Name: uplink.CapabilityClusterResources, Versions: []uint{Version1}}}, link.offers)
	session := uplink.Session{ID: "s1"}
	if selected {
		session.Capabilities = []uplink.CapabilitySelection{{Name: uplink.CapabilityClusterResources, Version: Version1, Config: config}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	for _, fn := range link.sessions {
		fn(ctx, session)
	}
	synctest.Wait()
	return link, cancel
}

// A sample goes out on session start and then at the interval cloud asked
// for, stamped with when it was taken.
func TestSamplesAtConfiguredInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, json.RawMessage(`{"interval_seconds":60}`), true)
		defer cancel()
		require.Equal(t, 1, link.count())
		require.False(t, link.first().ObservedAt.IsZero())
		require.Equal(t, 12.5, link.first().CPUPercent)
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		require.Equal(t, 6, link.count())
	})
}

// Without a configured interval the poll's five minutes stands in, and an
// absurd interval is clamped rather than obeyed.
func TestIntervalDefaultsAndBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, nil, true)
		defer cancel()
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Equal(t, 3, link.count())
	})
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, json.RawMessage(`{"interval_seconds":1}`), true)
		defer cancel()
		time.Sleep(minInterval)
		synctest.Wait()
		require.Equal(t, 2, link.count(), "one second is clamped up to the floor")
	})
}

func TestSilentWhenNotSelectedAndStopsWithSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, nil, false)
		defer cancel()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 0, link.count())
	})
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, json.RawMessage(`{"interval_seconds":60}`), true)
		cancel()
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 1, link.count())
	})
}

// A malformed config declines the capability for this session rather than
// guessing a cadence.
func TestMalformedConfigIsSilent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		link, cancel := start(t, json.RawMessage(`{"interval_seconds":`), true)
		defer cancel()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 0, link.count())
	})
}
