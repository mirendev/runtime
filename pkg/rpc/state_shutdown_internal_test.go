package rpc

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStateShutdownClosesOwnedCallstreamConnectionBeforeDrain(t *testing.T) {
	state, err := NewState(t.Context(), WithSkipVerify)
	require.NoError(t, err)
	state.Server().ExposeValue("stream", &Interface{
		name: "ShutdownTest",
		methods: map[string]Method{
			"emit": {
				Name:          "emit",
				InterfaceName: "ShutdownTest",
				Handler:       func(context.Context, Call) error { return nil },
			},
		},
	})

	client, err := state.Connect(state.LoopbackAddr(), "stream")
	require.NoError(t, err)
	url := "https://" + client.remote + "/_rpc/callstream/" + string(client.oid) + "/emit"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, url, nil)
	require.NoError(t, err)
	require.NoError(t, client.prepareRequest(t.Context(), req))
	_, session, err := client.dialWebTransport(t.Context(), url, req.Header)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Leave the state-owned session between the WebTransport upgrade and its
	// first control stream. The server is now blocked accepting that stream and
	// cannot drain until the owning client connection closes.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelShutdown()
	require.NoError(t, state.Shutdown(shutdownCtx))
}
