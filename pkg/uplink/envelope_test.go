package uplink

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	router := NewMessageRouter()

	var dispatched bool
	router.Handle("test.msg", func(_ context.Context, data json.RawMessage) error {
		dispatched = true
		return nil
	})

	env := Envelope{
		Type: "test.msg",
		Data: []byte(`{}`),
	}

	if err := router.Dispatch(t.Context(), env); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if !dispatched {
		t.Error("handler was not called")
	}
}

func TestMessageRouterUnknownType(t *testing.T) {
	router := NewMessageRouter()

	env := Envelope{Type: "unknown.type", Data: []byte(`{}`)}
	err := router.Dispatch(t.Context(), env)
	if err == nil {
		t.Error("expected error for unknown type")
	}
}
