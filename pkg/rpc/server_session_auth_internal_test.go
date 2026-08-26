package rpc

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"
)

// signatureTestInterface is a minimal exposed interface with one non-public
// method. Built by hand rather than generated because this file is inside
// package rpc, which the generated example package imports.
func signatureTestInterface(ran *bool) *Interface {
	return &Interface{
		name: "Meter",
		methods: map[string]Method{
			"readTemperature": {
				Name:          "readTemperature",
				InterfaceName: "Meter",
				Handler: func(ctx context.Context, call Call) error {
					*ran = true
					return nil
				},
			},
		},
	}
}

// The capability signature is what proves the caller holds the capability it is
// invoking. On a message transport it is the only such proof available — there
// is no TLS client certificate to fall back on — so a request whose signature
// does not verify must be refused before anything is dispatched.
//
// This matters more on the cloud-routed path than anywhere else: those frames
// arrive from a relay rather than from the caller's own socket, so the
// signature and the bearer inside them are the entire basis for trusting them.
// Nothing about arriving over the uplink grants a request anything.
func TestPrepareMethodCallRequiresAValidSignature(t *testing.T) {
	ctx := context.Background()

	state, err := NewState(ctx, WithSkipVerify)
	if err != nil {
		t.Fatalf("failed to build state: %v", err)
	}
	srv := state.Server()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate a key: %v", err)
	}

	var ran bool
	hc := &heldCapability{
		heldInterface: &heldInterface{Interface: signatureTestInterface(&ran)},
		pub:           pub,
	}

	const oid = OID("cap-under-test")
	srv.mu.Lock()
	srv.objects[oid] = hc
	srv.mu.Unlock()

	req := func(sign ed25519.PrivateKey) opRequest {
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		r := opRequest{
			Op:        "call",
			OID:       oid,
			Method:    "readTemperature",
			Timestamp: ts,
		}
		r.Signature = signBytes(sign, msgCanonical(r.Op, string(oid)+"/"+r.Method, ts))
		return r
	}

	// Positive control. Without it, a test that refused everything would look
	// identical to one enforcing the signature correctly.
	if _, _, _, _, reply, ok := srv.prepareMethodCall(ctx, req(priv)); !ok {
		t.Fatalf("a correctly signed request was refused: %s", reply.Error)
	}

	// Signed by a key the capability was not issued to: the shape a relay or
	// anyone else on the path would produce trying to author a call.
	_, wrongPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate a second key: %v", err)
	}

	_, _, _, _, reply, ok := srv.prepareMethodCall(ctx, req(wrongPriv))
	if ok {
		t.Fatal("a request signed by the wrong key was accepted")
	}
	if reply.Error != "failed to verify signature" {
		t.Errorf("reply.Error = %q, want the signature failure", reply.Error)
	}

	// A tampered signature on an otherwise valid request.
	tampered := req(priv)
	tampered.Signature[0] ^= 0xff

	if _, _, _, _, _, ok := srv.prepareMethodCall(ctx, tampered); ok {
		t.Fatal("a request with a tampered signature was accepted")
	}

	// A request carrying no signature at all.
	unsigned := req(priv)
	unsigned.Signature = nil

	if _, _, _, _, _, ok := srv.prepareMethodCall(ctx, unsigned); ok {
		t.Fatal("an unsigned request was accepted")
	}

	// prepareMethodCall only authorizes; it never dispatches. Asserted so the
	// refusals above cannot be confused with a handler that ran and failed.
	if ran {
		t.Error("the handler ran during authorization")
	}
}

// A stale timestamp is refused too: a signature stays valid forever otherwise,
// so a captured frame could be replayed by anyone who saw it go past.
func TestPrepareMethodCallRefusesAStaleRequest(t *testing.T) {
	ctx := context.Background()

	state, err := NewState(ctx, WithSkipVerify)
	if err != nil {
		t.Fatalf("failed to build state: %v", err)
	}
	srv := state.Server()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate a key: %v", err)
	}

	const oid = OID("cap-stale")
	srv.mu.Lock()
	var ran bool
	srv.objects[oid] = &heldCapability{
		heldInterface: &heldInterface{Interface: signatureTestInterface(&ran)},
		pub:           pub,
	}
	srv.mu.Unlock()

	ts := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	r := opRequest{Op: "call", OID: oid, Method: "readTemperature", Timestamp: ts}
	r.Signature = signBytes(priv, msgCanonical(r.Op, string(oid)+"/"+r.Method, ts))

	if _, _, _, _, _, ok := srv.prepareMethodCall(ctx, r); ok {
		t.Fatal("a request with a day-old timestamp was accepted")
	}
}
