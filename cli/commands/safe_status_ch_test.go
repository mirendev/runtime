package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
)

func TestSafeStatusChNoPanicOnConcurrentClose(t *testing.T) {
	const senders = 16
	const sendsPerGoroutine = 1000

	s := newSafeStatusCh(make(chan *client.SolveStatus, 4))

	// Drain the channel so senders don't block forever.
	var drainerWG sync.WaitGroup
	drainerWG.Add(1)
	go func() {
		defer drainerWG.Done()
		for range s.ch {
		}
	}()

	ctx := context.Background()

	// midflight signals that every sender has issued at least one Send, so
	// Close is guaranteed to land while sends are in flight rather than after
	// everyone has finished.
	var midflightWG sync.WaitGroup
	midflightWG.Add(senders)

	var senderWG sync.WaitGroup
	senderWG.Add(senders)
	for range senders {
		go func() {
			defer senderWG.Done()
			for j := range sendsPerGoroutine {
				if err := s.Send(ctx, &client.SolveStatus{}); err != nil {
					t.Errorf("Send returned error: %v", err)
					return
				}
				if j == 0 {
					midflightWG.Done()
				}
			}
		}()
	}

	midflightWG.Wait()
	s.Close()
	// Idempotent Close.
	s.Close()

	senderWG.Wait()
	drainerWG.Wait()
}

func TestSafeStatusChSendAfterCloseIsNoop(t *testing.T) {
	s := newSafeStatusCh(make(chan *client.SolveStatus, 1))
	s.Close()

	if err := s.Send(context.Background(), &client.SolveStatus{}); err != nil {
		t.Fatalf("Send after Close returned error: %v", err)
	}
}

func TestSafeStatusChCloseUnblocksParkedSend(t *testing.T) {
	// Unbuffered channel with no drainer: a Send is parked inside the select.
	// Close must wake it up rather than deadlocking on the channel send.
	s := newSafeStatusCh(make(chan *client.SolveStatus))

	parked := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(parked)
		done <- s.Send(context.Background(), &client.SolveStatus{})
	}()

	<-parked
	// Give the Send a moment to enter the select.
	for i := range 1000 {
		_ = i
	}

	closeReturned := make(chan struct{})
	go func() {
		s.Close()
		close(closeReturned)
	}()

	select {
	case <-closeReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return; parked Send caused a deadlock")
	}

	if err := <-done; err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
}

func TestSafeStatusChSendRespectsContext(t *testing.T) {
	// Unbuffered channel with no consumer → Send blocks until ctx is cancelled.
	s := newSafeStatusCh(make(chan *client.SolveStatus))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.Send(ctx, &client.SolveStatus{})
	}()

	cancel()

	if err := <-done; err == nil {
		t.Fatal("Send did not return ctx.Err() after cancellation")
	}

	// Drain so a follow-up Close on a buffered scenario wouldn't leak; here the
	// channel has no buffered values so just closing is safe.
	s.Close()
}
