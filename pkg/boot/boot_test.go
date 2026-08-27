package boot_test

import (
	"context"
	"errors"
	"sync"
	"testing"
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
