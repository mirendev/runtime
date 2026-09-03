package rpc_test

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

type shutdownBlockingStream struct {
	started chan struct{}
}

func (s *shutdownBlockingStream) Emit(ctx context.Context, _ *example.EmitTempsEmit) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestStateShutdownDrainsActiveHTTP3Request(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	state, err := rpc.NewState(t.Context(),
		rpc.WithSkipVerify,
		rpc.WithHTTPHandler("GET /drain", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(requestStarted)
			<-releaseRequest
			_, _ = io.WriteString(w, "drained")
		})),
	)
	require.NoError(t, err)

	transport := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(func() { _ = transport.Close() })
	client := &http.Client{Transport: transport}

	type response struct {
		body string
		err  error
	}
	responseDone := make(chan response, 1)
	go func() {
		resp, err := client.Get("https://" + state.LoopbackAddr() + "/drain")
		if err != nil {
			responseDone <- response{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		responseDone <- response{body: string(body), err: err}
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach the server")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- state.Shutdown(shutdownCtx) }()

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before the active request completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseRequest)
	result := <-responseDone
	require.NoError(t, result.err)
	require.Equal(t, "drained", result.body)
	require.NoError(t, <-shutdownDone)
}

func TestStateContextCancellationStillStopsHTTP3(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	state, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithHTTPHandler("GET /probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = state.Close() })
	url := "https://" + state.LoopbackAddr() + "/probe"

	warmTransport := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	warmClient := &http.Client{Transport: warmTransport}
	resp, err := warmClient.Get(url)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, warmTransport.Close())

	cancel()
	require.Eventually(t, func() bool {
		transport := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		defer transport.Close()
		requestCtx, cancelRequest := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancelRequest()
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := transport.RoundTrip(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		return err != nil
	}, time.Second, 10*time.Millisecond)
}

func TestStateShutdownCancelsActiveHTTP3CallStreamsBeforeDrain(t *testing.T) {
	streamHandler := &shutdownBlockingStream{started: make(chan struct{})}
	serverState, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	require.NoError(t, err)
	serverState.Server().ExposeValue("stream", example.AdaptEmitTemps(streamHandler))

	clientState, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientState.Close() })
	client, err := clientState.Connect(serverState.LoopbackAddr(), "stream")
	require.NoError(t, err)

	callDone := make(chan error, 1)
	go func() {
		_, err := example.NewEmitTempsClient(client).Emit(t.Context(), stream.StreamRecv(func(float32) error {
			return nil
		}))
		callDone <- err
	}()

	select {
	case <-streamHandler.started:
	case <-time.After(time.Second):
		t.Fatal("streaming RPC did not reach the server")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, serverState.Shutdown(shutdownCtx))

	select {
	case err := <-callDone:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("streaming RPC remained active after shutdown")
	}
}

func TestStateShutdownUnregistersMemServer(t *testing.T) {
	name := "shutdown-" + t.Name()
	serverState, err := rpc.NewState(t.Context(), rpc.WithSkipVerify, rpc.WithMemServer(name))
	require.NoError(t, err)
	serverState.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

	clientState, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientState.Close() })
	_, err = clientState.Connect("mem://"+name, "meter")
	require.NoError(t, err)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, serverState.Shutdown(shutdownCtx))
	require.Eventually(t, func() bool {
		_, err := clientState.Connect("mem://"+name, "meter")
		return err != nil
	}, time.Second, time.Millisecond)
}
