package rpc

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mr-tron/base58"
)

// authFreshness bounds how old a request timestamp may be.
const authFreshness = 10 * time.Minute

// httpCanonical is the legacy signed string for the HTTP transports:
// "METHOD PATH timestamp". Kept verbatim so existing peers remain compatible.
func httpCanonical(method, path, ts string) string {
	return fmt.Sprintf("%s %s %s", method, path, ts)
}

// msgCanonical is the signed string for the message transport:
// "op target timestamp", where target identifies the operation's subject
// (e.g. "oid/method" for call, "oid" for deref). Transport-neutral by
// construction; a signature never travels inside a capability, so the per-
// transport string format is safe.
func msgCanonical(op, target, ts string) string {
	return fmt.Sprintf("%s %s %s", op, target, ts)
}

// verifyString checks a base58-encoded signature over canonical. The returned
// error distinguishes the failure modes so callers can record why a request was
// rejected in the audit log.
func verifyString(pub ed25519.PublicKey, canonical, sigB58 string) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid capability public key")
	}
	bsign, err := base58.Decode(sigB58)
	if err != nil {
		return fmt.Errorf("malformed signature")
	}
	if !ed25519.Verify(pub, []byte(canonical), bsign) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// signBytes / verifyBytes are the raw-signature variants used by the message
// transport (no base58 round-trip; the signature travels as bytes in the frame).
func signBytes(priv ed25519.PrivateKey, canonical string) []byte {
	return ed25519.Sign(priv, []byte(canonical))
}

func verifyBytes(pub ed25519.PublicKey, canonical string, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, []byte(canonical), sig)
}

// freshTimestamp parses an RFC3339Nano timestamp and rejects it if it is older
// than authFreshness.
func freshTimestamp(ts string) error {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if time.Since(t) > authFreshness {
		return fmt.Errorf("timestamp too old")
	}
	return nil
}
