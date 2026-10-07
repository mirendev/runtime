package boot_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/boot"
)

func TestValidateRejectsDanglingInputProducer(t *testing.T) {
	g := boot.NewGraph()
	_, output := boot.Provide0("producer", func(context.Context) (int, error) { return 1, nil })
	consumer := boot.Run1("consumer", output, func(context.Context, int) error { return nil })
	require.NoError(t, g.Add(consumer))
	require.ErrorContains(t, g.Validate(), `component "consumer" has undeclared input producer "producer"`)
}

func TestValidateRejectsDanglingOrderOnlyDependency(t *testing.T) {
	g := boot.NewGraph()
	dependency := boot.Run0("dependency", func(context.Context) error { return nil })
	dependent := boot.Run0("dependent", func(context.Context) error { return nil }, boot.DependsOn(dependency))
	require.NoError(t, g.Add(dependent))
	require.ErrorContains(t, g.Validate(), `component "dependent" has undeclared input producer "dependency"`)
}

func TestOutputPublishesOnlyAfterSuccessfulStart(t *testing.T) {
	g := boot.NewGraph()
	producer, output := boot.Provide0("producer", func(context.Context) (int, error) { return 42, nil })
	require.PanicsWithValue(t, `boot: output from "producer" read before publication`, func() { output.Value() })
	require.NoError(t, g.Add(producer))
	require.NoError(t, g.Start(t.Context()))
	require.Equal(t, 42, output.Value())
}

func TestGraphResolvesInputsAndRunsPeersConcurrently(t *testing.T) {
	g := boot.NewGraph()
	peerStarted := make(chan string, 2)
	releasePeers := make(chan struct{})
	var mu sync.Mutex
	var order []string
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}

	root, rootOutput := boot.Provide0("root", func(context.Context) (string, error) {
		record("root")
		return "root-value", nil
	})
	newPeer := func(name string) (*boot.Component, boot.Output[string]) {
		return boot.Provide1(name, rootOutput, func(ctx context.Context, rootValue string) (string, error) {
			if rootValue != "root-value" {
				return "", errors.New("received the wrong root output")
			}
			peerStarted <- name
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-releasePeers:
				record(name)
				return name + "-value", nil
			}
		})
	}
	left, leftOutput := newPeer("left")
	right, rightOutput := newPeer("right")
	leaf := boot.Run2("leaf", leftOutput, rightOutput, func(_ context.Context, left, right string) error {
		if left != "left-value" || right != "right-value" {
			return errors.New("received the wrong peer outputs")
		}
		record("leaf")
		return nil
	})
	for _, component := range []*boot.Component{root, left, right, leaf} {
		require.NoError(t, g.Add(component))
	}

	done := make(chan error, 1)
	go func() { done <- g.Start(t.Context()) }()
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

func TestProducerFailureDoesNotPublishOrStartConsumer(t *testing.T) {
	g := boot.NewGraph()
	producer, output := boot.Provide0("producer", func(context.Context) (string, error) {
		return "", errors.New("boom")
	})
	consumerStarted := false
	consumer := boot.Run1("consumer", output, func(context.Context, string) error {
		consumerStarted = true
		return nil
	})
	require.NoError(t, g.Add(producer))
	require.NoError(t, g.Add(consumer))
	require.ErrorContains(t, g.Start(t.Context()), "starting producer: boom")
	require.False(t, consumerStarted)
	require.Panics(t, func() { output.Value() })
}

func TestResolvedOutputDoesNotAddAGraphEdge(t *testing.T) {
	g := boot.NewGraph()
	input := boot.ResolvedOutput("ready")
	component := boot.Run1("component", input, func(_ context.Context, value string) error {
		if value != "ready" {
			return errors.New("resolved output had the wrong value")
		}
		return nil
	})
	require.NoError(t, g.Add(component))
	require.NoError(t, g.Start(t.Context()))
}

func TestStopCleansUpAComponentWhoseStartFailed(t *testing.T) {
	g := boot.NewGraph()
	stopped := false
	component := boot.Run0("component", func(context.Context) error {
		return errors.New("partially started")
	}, boot.WithStop(func(context.Context) error {
		stopped = true
		return nil
	}, 0))
	require.NoError(t, g.Add(component))
	require.Error(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
	require.True(t, stopped)
}

func TestStopUsesReverseDataflowOrder(t *testing.T) {
	g := boot.NewGraph()
	var stopped []string
	root, rootOutput := boot.Provide0("root", func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}, boot.WithStop(func(context.Context) error {
		stopped = append(stopped, "root")
		return nil
	}, 0))
	leaf := boot.Run1("leaf", rootOutput, func(context.Context, struct{}) error { return nil },
		boot.WithStop(func(context.Context) error {
			stopped = append(stopped, "leaf")
			return nil
		}, 0),
	)
	require.NoError(t, g.Add(root))
	require.NoError(t, g.Add(leaf))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
	require.Equal(t, []string{"leaf", "root"}, stopped)
}

func TestDependsOnOrdersComponentsWithoutPublishingAValue(t *testing.T) {
	g := boot.NewGraph()
	release := make(chan struct{})
	dependencyStarted := make(chan struct{})
	dependentStarted := make(chan struct{})

	dependency := boot.Run0("dependency", func(ctx context.Context) error {
		close(dependencyStarted)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	})
	dependent := boot.Run0("dependent", func(context.Context) error {
		close(dependentStarted)
		return nil
	}, boot.DependsOn(dependency))

	require.NoError(t, g.Add(dependency))
	require.NoError(t, g.Add(dependent))
	done := make(chan error, 1)
	go func() { done <- g.Start(t.Context()) }()

	<-dependencyStarted
	select {
	case <-dependentStarted:
		t.Fatal("dependent started before its order-only dependency completed")
	default:
	}
	close(release)
	require.NoError(t, <-done)
	select {
	case <-dependentStarted:
	default:
		t.Fatal("dependent did not start after its dependency completed")
	}
}

func TestDependsOnControlsReverseShutdownOrder(t *testing.T) {
	g := boot.NewGraph()
	var stopped []string
	dependency := boot.Run0("dependency", func(context.Context) error { return nil },
		boot.WithStop(func(context.Context) error {
			stopped = append(stopped, "dependency")
			return nil
		}, 0),
	)
	dependent := boot.Run0("dependent", func(context.Context) error { return nil },
		boot.DependsOn(dependency),
		boot.WithStop(func(context.Context) error {
			stopped = append(stopped, "dependent")
			return nil
		}, 0),
	)
	require.NoError(t, g.Add(dependency))
	require.NoError(t, g.Add(dependent))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
	require.Equal(t, []string{"dependent", "dependency"}, stopped)
}

func TestStopCancelsLifetimeBeforeCallingStop(t *testing.T) {
	g := boot.NewGraph()
	var lifetime context.Context
	component := boot.Run0("component", func(ctx context.Context) error {
		lifetime = ctx
		return nil
	}, boot.WithStop(func(context.Context) error {
		if lifetime.Err() == nil {
			return errors.New("lifetime context is still active")
		}
		return nil
	}, 0))
	require.NoError(t, g.Add(component))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
}

func TestStopGivesEachComponentItsOwnTimeout(t *testing.T) {
	g := boot.NewGraph()
	leafTimedOut := make(chan struct{})
	root, rootOutput := boot.Provide0("root", func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}, boot.WithStop(func(ctx context.Context) error {
		select {
		case <-leafTimedOut:
		case <-ctx.Done():
			return errors.New("root inherited leaf's expired timeout")
		}
		return nil
	}, time.Second))
	leaf := boot.Run1("leaf", rootOutput, func(context.Context, struct{}) error { return nil },
		boot.WithStop(func(ctx context.Context) error {
			<-ctx.Done()
			close(leafTimedOut)
			return nil
		}, time.Millisecond),
	)
	require.NoError(t, g.Add(root))
	require.NoError(t, g.Add(leaf))
	require.NoError(t, g.Start(t.Context()))
	require.NoError(t, g.Stop(t.Context()))
}

func TestStopNamesSlowComponents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		g := boot.NewGraph(boot.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

		sleepingStop := func(d time.Duration) boot.StopFunc {
			return func(context.Context) error {
				time.Sleep(d)
				return nil
			}
		}
		// A stop that honors its context and returns nil at its deadline is
		// exactly the case that otherwise leaves no trace.
		budgetedStop := func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}
		require.NoError(t, g.Add(boot.Run0("quick", func(context.Context) error { return nil },
			boot.WithStop(sleepingStop(10*time.Millisecond), time.Minute))))
		require.NoError(t, g.Add(boot.Run0("slow", func(context.Context) error { return nil },
			boot.WithStop(sleepingStop(2*time.Second), time.Minute))))
		require.NoError(t, g.Add(boot.Run0("budget-spender", func(context.Context) error { return nil },
			boot.WithStop(budgetedStop, 30*time.Second))))
		require.NoError(t, g.Add(boot.Run0("context-ignorer", func(context.Context) error { return nil },
			boot.WithStop(sleepingStop(5*time.Second), 2*time.Second))))

		require.NoError(t, g.Start(t.Context()))
		require.NoError(t, g.Stop(context.Background()))

		out := logs.String()
		require.NotContains(t, out, "component=quick")
		require.Contains(t, out, `level=INFO msg="component stop was slow" component=slow duration=2s budget=1m0s`)
		require.Contains(t, out, `level=WARN msg="component stop used its entire budget" component=budget-spender duration=30s budget=30s`)
		require.Contains(t, out, `level=WARN msg="component stop used its entire budget" component=context-ignorer duration=5s budget=2s`)
	})
}

func TestStopMeasuresAgainstTheDeadlineItActuallyHad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		g := boot.NewGraph(boot.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
		// The component's own budget is a minute, but the caller's overall
		// deadline leaves it ten seconds. Using all ten is using all it had.
		require.NoError(t, g.Add(boot.Run0("squeezed", func(context.Context) error { return nil },
			boot.WithStop(func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}, time.Minute))))

		require.NoError(t, g.Start(t.Context()))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, g.Stop(ctx))

		require.Contains(t, logs.String(), `level=WARN msg="component stop used its entire budget" component=squeezed duration=10s budget=10s`)
	})
}

func TestStopFlagsAnOverrunAfterTheDeadlineHasPassed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		g := boot.NewGraph(boot.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
		// Earlier layers have spent the whole shutdown deadline, so this stop
		// starts with no budget left and then ignores its context anyway.
		require.NoError(t, g.Add(boot.Run0("late", func(context.Context) error { return nil },
			boot.WithStop(func(context.Context) error {
				time.Sleep(3 * time.Second)
				return nil
			}, time.Minute))))

		require.NoError(t, g.Start(t.Context()))
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		require.NoError(t, g.Stop(ctx))

		require.Contains(t, logs.String(), `level=WARN msg="component stop used its entire budget" component=late duration=3s budget=0s`)
	})
}
