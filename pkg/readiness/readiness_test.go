package readiness_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/readiness"
)

func TestValidateRejectsDanglingDependency(t *testing.T) {
	g := readiness.NewGraph()
	b := readiness.NewComponent("b", readiness.Spec{})
	a := readiness.NewComponent("a", readiness.Spec{Dependencies: []readiness.Dependency{readiness.ReadyDep(b)}})
	require.NoError(t, g.Add(a))
	require.ErrorContains(t, g.Validate(), `component "a" has undeclared dependency "b"`)
}

func TestConditionRequiresDeclaredProviders(t *testing.T) {
	g := readiness.NewGraph()
	condition := readiness.NewCondition("serve")
	require.ErrorContains(t, g.AddCondition(condition), "has no providers")
	require.ErrorContains(t,
		g.AddCondition(condition, readiness.NewComponent("missing", readiness.Spec{})),
		`has undeclared provider "missing"`,
	)
}

func TestStartHonorsDependenciesAndRunsPeersConcurrently(t *testing.T) {
	g := readiness.NewGraph()

	rootReady := make(chan struct{})
	peerStarted := make(chan string, 2)
	releasePeers := make(chan struct{})
	var mu sync.Mutex
	var order []string
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}

	root := readiness.NewComponent("root", readiness.Spec{Start: func(_ context.Context, report readiness.Reporter) error {
		record("root")
		report.Started()
		close(rootReady)
		return nil
	}})
	newPeer := func(name string) *readiness.Component {
		return readiness.NewComponent(name, readiness.Spec{
			Dependencies: []readiness.Dependency{readiness.ReadyDep(root)},
			Start: func(ctx context.Context, _ readiness.Reporter) error {
				peerStarted <- name
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-releasePeers:
					record(name)
					return nil
				}
			},
		})
	}
	left := newPeer("left")
	right := newPeer("right")
	leaf := readiness.NewComponent("leaf", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(left), readiness.ReadyDep(right)},
		Start: func(context.Context, readiness.Reporter) error {
			record("leaf")
			return nil
		},
	})
	for _, component := range []*readiness.Component{root, left, right, leaf} {
		require.NoError(t, g.Add(component))
	}

	done := make(chan error, 1)
	go func() { done <- g.Start(t.Context()) }()

	select {
	case <-rootReady:
	case <-time.After(time.Second):
		t.Fatal("root did not start")
	}
	for range 2 {
		select {
		case <-peerStarted:
		case <-time.After(time.Second):
			t.Fatal("peer components did not start concurrently")
		}
	}
	close(releasePeers)
	require.NoError(t, <-done)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "root", order[0])
	require.Equal(t, "leaf", order[len(order)-1])
}

func TestStartedDependencyCanProceedBeforeProviderIsReady(t *testing.T) {
	g := readiness.NewGraph()
	providerStarted := make(chan struct{})
	consumerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})

	provider := readiness.NewComponent("provider", readiness.Spec{Start: func(ctx context.Context, report readiness.Reporter) error {
		report.Started()
		close(providerStarted)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-releaseProvider:
			return nil
		}
	}})
	consumer := readiness.NewComponent("consumer", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.StartDep(provider)},
		Start: func(context.Context, readiness.Reporter) error {
			close(consumerStarted)
			return nil
		},
	})
	require.NoError(t, g.Add(provider))
	require.NoError(t, g.Add(consumer))

	done := make(chan error, 1)
	go func() { done <- g.Start(t.Context()) }()
	<-providerStarted
	select {
	case <-consumerStarted:
	case <-time.After(time.Second):
		t.Fatal("consumer did not proceed from the started signal")
	}
	close(releaseProvider)
	require.NoError(t, <-done)
}

func TestReadyAlsoReportsStarted(t *testing.T) {
	g := readiness.NewGraph()
	consumerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})

	provider := readiness.NewComponent("provider", readiness.Spec{Start: func(ctx context.Context, report readiness.Reporter) error {
		report.Ready()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-releaseProvider:
			return nil
		}
	}})
	consumer := readiness.NewComponent("consumer", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.StartDep(provider)},
		Start: func(context.Context, readiness.Reporter) error {
			close(consumerStarted)
			return nil
		},
	})
	require.NoError(t, g.Add(provider))
	require.NoError(t, g.Add(consumer))

	done := make(chan error, 1)
	go func() { done <- g.Start(t.Context()) }()
	select {
	case <-consumerStarted:
	case <-time.After(time.Second):
		t.Fatal("ready provider did not satisfy a started dependency")
	}
	close(releaseProvider)
	require.NoError(t, <-done)
}

func TestConditionTracksProviderReadiness(t *testing.T) {
	g := readiness.NewGraph()
	condition := readiness.NewCondition("serve")
	reporters := make(chan readiness.Reporter, 2)

	newProvider := func(name string) *readiness.Component {
		return readiness.NewComponent(name, readiness.Spec{Start: func(_ context.Context, report readiness.Reporter) error {
			reporters <- report
			return nil
		}})
	}
	a := newProvider("a")
	b := newProvider("b")
	for _, component := range []*readiness.Component{a, b} {
		require.NoError(t, g.Add(component))
	}
	require.NoError(t, g.AddCondition(condition, a, b))
	require.NoError(t, g.Start(t.Context()))

	ready, err := g.Ready(condition)
	require.NoError(t, err)
	require.True(t, ready)

	first := <-reporters
	first.NotReady()
	ready, err = g.Ready(condition)
	require.NoError(t, err)
	require.False(t, ready)

	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, g.Await(waitCtx, condition), context.DeadlineExceeded)

	first.Ready()
	require.NoError(t, g.Await(t.Context(), condition))
}

func TestBackgroundProducerCanManageReadinessAfterStart(t *testing.T) {
	g := readiness.NewGraph()
	condition := readiness.NewCondition("initial-sync")
	var report readiness.Reporter
	lifetimeCanceled := make(chan struct{})

	producer := readiness.NewComponent("migration", readiness.Spec{
		Start: func(ctx context.Context, reporter readiness.Reporter) error {
			report = reporter
			report.NotReady()
			go func() {
				<-ctx.Done()
				close(lifetimeCanceled)
			}()
			return nil
		},
	})
	require.NoError(t, g.Add(producer))
	require.NoError(t, g.AddCondition(condition, producer))

	// Starting the background producer does not block graph startup and its
	// explicit NotReady report suppresses the usual implicit readiness.
	require.NoError(t, g.Start(t.Context()))
	ready, err := g.Ready(condition)
	require.NoError(t, err)
	require.False(t, ready)

	consumerStarted := make(chan struct{})
	go func() {
		if g.Await(t.Context(), condition) == nil {
			close(consumerStarted)
		}
	}()
	report.Ready()
	select {
	case <-consumerStarted:
	case <-time.After(time.Second):
		t.Fatal("consumer did not start when the background producer became ready")
	}

	// A consumer arriving after the first sweep sees the latched readiness
	// immediately, without polling producer state.
	require.NoError(t, g.Await(t.Context(), condition))
	require.NoError(t, g.Stop(t.Context()))
	select {
	case <-lifetimeCanceled:
	case <-time.After(time.Second):
		t.Fatal("background producer context was not canceled")
	}
}

func TestStartCancelsDependentsAfterFailure(t *testing.T) {
	g := readiness.NewGraph()
	boom := errors.New("boom")

	provider := readiness.NewComponent("provider", readiness.Spec{Start: func(context.Context, readiness.Reporter) error {
		return boom
	}})
	dependentStarted := false
	dependent := readiness.NewComponent("dependent", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(provider)},
		Start: func(context.Context, readiness.Reporter) error {
			dependentStarted = true
			return nil
		},
	})
	require.NoError(t, g.Add(provider))
	require.NoError(t, g.Add(dependent))

	err := g.Start(t.Context())
	require.ErrorContains(t, err, "starting provider: boom")
	require.False(t, dependentStarted)
	require.NotContains(t, err.Error(), "starting dependent")
}

func TestStopCleansUpAComponentWhoseStartFailed(t *testing.T) {
	g := readiness.NewGraph()
	stopped := false
	component := readiness.NewComponent("component", readiness.Spec{
		Start: func(context.Context, readiness.Reporter) error {
			return errors.New("partially started")
		},
		Stop: func(context.Context) error {
			stopped = true
			return nil
		},
	})
	require.NoError(t, g.Add(component))

	require.Error(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
	require.True(t, stopped)
}

func TestStopUsesReverseDependencyOrder(t *testing.T) {
	g := readiness.NewGraph()
	var mu sync.Mutex
	var stopped []string
	stop := func(name string) readiness.StopFunc {
		return func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			stopped = append(stopped, name)
			return nil
		}
	}

	root := readiness.NewComponent("root", readiness.Spec{Stop: stop("root")})
	leaf := readiness.NewComponent("leaf", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(root)},
		Stop:         stop("leaf"),
	})
	require.NoError(t, g.Add(root))
	require.NoError(t, g.Add(leaf))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
	require.Equal(t, []string{"leaf", "root"}, stopped)
}

func TestStopDropsConditionsForComponentsWithoutStopHooks(t *testing.T) {
	g := readiness.NewGraph()
	component := readiness.NewComponent("component", readiness.Spec{})
	condition := readiness.NewCondition("condition")
	require.NoError(t, g.Add(component))
	require.NoError(t, g.AddCondition(condition, component))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))

	ready, err := g.Ready(condition)
	require.NoError(t, err)
	require.False(t, ready)
}

func TestStopCancelsComponentLifetimeContext(t *testing.T) {
	g := readiness.NewGraph()
	stopped := make(chan struct{})
	component := readiness.NewComponent("component", readiness.Spec{
		Start: func(ctx context.Context, _ readiness.Reporter) error {
			go func() {
				<-ctx.Done()
				close(stopped)
			}()
			return nil
		},
	})
	require.NoError(t, g.Add(component))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("component lifetime context was not canceled")
	}
}

func TestStopCancelsLifetimeBeforeCallingStop(t *testing.T) {
	g := readiness.NewGraph()
	var lifetime context.Context
	component := readiness.NewComponent("component", readiness.Spec{
		Start: func(ctx context.Context, _ readiness.Reporter) error {
			lifetime = ctx
			return nil
		},
		Stop: func(context.Context) error {
			if lifetime.Err() == nil {
				return errors.New("lifetime context is still active")
			}
			return nil
		},
	})
	require.NoError(t, g.Add(component))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
}

func TestStopGivesEachComponentItsOwnTimeout(t *testing.T) {
	g := readiness.NewGraph()
	leafTimedOut := make(chan struct{})
	first := readiness.NewComponent("first", readiness.Spec{
		Stop: func(ctx context.Context) error {
			select {
			case <-leafTimedOut:
			case <-ctx.Done():
				return errors.New("first inherited leaf's expired timeout")
			}
			return nil
		},
		StopTimeout: time.Second,
	})
	second := readiness.NewComponent("second", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(first)},
		Stop: func(ctx context.Context) error {
			<-ctx.Done()
			close(leafTimedOut)
			return nil
		},
		StopTimeout: time.Millisecond,
	})
	require.NoError(t, g.Add(first))
	require.NoError(t, g.Add(second))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
}

func TestReporterCannotRestoreReadinessAfterStop(t *testing.T) {
	g := readiness.NewGraph()
	condition := readiness.NewCondition("condition")
	var reporter readiness.Reporter
	component := readiness.NewComponent("component", readiness.Spec{
		Start: func(_ context.Context, report readiness.Reporter) error {
			reporter = report
			return nil
		},
	})
	require.NoError(t, g.Add(component))
	require.NoError(t, g.AddCondition(condition, component))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))

	reporter.Ready()
	ready, err := g.Ready(condition)
	require.NoError(t, err)
	require.False(t, ready)
}
