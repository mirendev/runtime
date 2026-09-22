package clusternetwork

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/uplink"
)

type fakeLink struct {
	mu       sync.Mutex
	offers   []uplink.CapabilityOffer
	sessions []func(context.Context, uplink.Session)
	reports  []Report
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
	if msgType != TypeReport {
		panic("unexpected message type " + msgType)
	}
	f.reports = append(f.reports, data.(Report))
	return nil
}

func (f *fakeLink) sent() []Report {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Report(nil), f.reports...)
}

type fakeSource struct {
	mu    sync.Mutex
	facts Report
	calls int
}

func (s *fakeSource) NetworkFacts(context.Context) Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.facts
}

func (s *fakeSource) set(facts Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = facts
}

func start(t *testing.T, selected bool) (*fakeSource, *fakeLink, context.CancelFunc) {
	t.Helper()
	source := &fakeSource{facts: Report{APIAddresses: []string{"203.0.113.7:8443"}, CACertFingerprint: "abc", Containerized: false}}
	link := &fakeLink{}
	require.NoError(t, NewReporter(slog.Default(), source).Register(context.Background(), link))
	require.Equal(t, []uplink.CapabilityOffer{{Name: uplink.CapabilityClusterNetwork, Versions: []uint{Version1}}}, link.offers)

	session := uplink.Session{ID: "s1"}
	if selected {
		session.Capabilities = []uplink.CapabilitySelection{{Name: uplink.CapabilityClusterNetwork, Version: Version1}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	for _, fn := range link.sessions {
		fn(ctx, session)
	}
	synctest.Wait()
	return source, link, cancel
}

// The facts go out as soon as the session selects the capability, and again
// only when they change.
func TestReportsOnSessionAndOnChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, link, cancel := start(t, true)
		defer cancel()
		require.Len(t, link.sent(), 1)
		require.Equal(t, []string{"203.0.113.7:8443"}, link.sent()[0].APIAddresses)

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Len(t, link.sent(), 1, "unchanged facts are not resent every check")

		source.set(Report{
			APIAddresses:  []string{"203.0.113.7:8443", "[2001:db8::1]:8443"},
			Reachability:  &cloudauth.ReachabilityVerdict{Reachable: true, PublicAddress: "203.0.113.7"},
			Containerized: false,
		})
		time.Sleep(checkInterval)
		synctest.Wait()
		require.Len(t, link.sent(), 2)
		require.Len(t, link.sent()[1].APIAddresses, 2)
		require.NotNil(t, link.sent()[1].Reachability)
	})
}

// Unchanged facts are still repeated hourly, so a report cloud lost is
// repaired without waiting for something to change.
func TestRepeatsUnchangedFactsHourly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, link, cancel := start(t, true)
		defer cancel()
		require.Len(t, link.sent(), 1)
		time.Sleep(resendInterval + checkInterval)
		synctest.Wait()
		require.Len(t, link.sent(), 2)
	})
}

// A session that did not select the capability gets nothing, and neither
// computes the facts.
func TestSilentWhenNotSelected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, link, cancel := start(t, false)
		defer cancel()
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		require.Empty(t, link.sent())
		require.Equal(t, 0, source.calls)
	})
}

// Reporting stops with the session.
func TestStopsWithSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, link, cancel := start(t, true)
		cancel()
		synctest.Wait()
		before := source.calls
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		require.Equal(t, before, source.calls)
		require.Len(t, link.sent(), 1)
	})
}
