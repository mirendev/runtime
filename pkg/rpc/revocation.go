package rpc

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
)

// AuthErrorCertRevoked marks a client certificate the cluster has revoked.
const AuthErrorCertRevoked = "cert-revoked"

// CertRevokedError refuses a certificate that verified against the cluster CA
// but has since been revoked. Its reason is disclosed: the caller already holds
// the certificate, so this reveals nothing, and it tells a legitimate user why
// their config stopped working.
type CertRevokedError struct {
	Serial string
}

func (e *CertRevokedError) Error() string {
	return fmt.Sprintf("client certificate %s has been revoked", e.Serial)
}

// AuthErrorCode implements DisclosableAuthError.
func (e *CertRevokedError) AuthErrorCode() string { return AuthErrorCertRevoked }

var errCertRevocationsNotLoaded = errors.New("certificate revocations not loaded")

// CertSerial returns the canonical form of a certificate serial number:
// lowercase hex with no separators or leading zeros. Every revocation is stored
// and matched in this form.
func CertSerial(serial *big.Int) string {
	return serial.Text(16)
}

// ParseCertSerial validates a hex serial and returns its canonical form. It
// rejects what no certificate could carry: RFC 5280 serials are positive and fit
// in 20 octets.
func ParseCertSerial(serial string) (string, error) {
	n, ok := new(big.Int).SetString(serial, 16)
	if !ok || strings.HasPrefix(serial, "+") || strings.HasPrefix(serial, "-") {
		return "", fmt.Errorf("invalid certificate serial %q: must be hexadecimal", serial)
	}
	if n.Sign() <= 0 || n.BitLen() > 160 {
		return "", fmt.Errorf("invalid certificate serial %q: must be positive and at most 20 bytes", serial)
	}
	return CertSerial(n), nil
}

// CertRevocations is the set of client certificates a server refuses. Requests
// consult only this in-memory set, so a store outage cannot fail authentication:
// the server keeps enforcing what it last knew. It starts unloaded and refuses
// every certificate until the owner has loaded it, because the listener opens
// before the entity store exists.
//
// It also tracks in-flight certificate-authenticated requests. Authentication
// happens when a request starts, so without this a stream opened before a
// revocation would outlive it.
type CertRevocations struct {
	mu      sync.Mutex
	loaded  bool
	revoked map[string]struct{}
	active  map[string]map[*context.CancelCauseFunc]struct{}
}

// NewCertRevocations returns an unloaded set.
func NewCertRevocations() *CertRevocations {
	return &CertRevocations{
		revoked: make(map[string]struct{}),
		active:  make(map[string]map[*context.CancelCauseFunc]struct{}),
	}
}

// Load adds a store snapshot and marks the set loaded. It never removes: a
// snapshot can predate a revocation already enforced here, so a serial's absence
// proves nothing. A revocation therefore lasts until the process restarts.
func (c *CertRevocations) Load(serials []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.loaded = true
	for _, serial := range serials {
		c.revokeLocked(serial)
	}
}

// Add revokes a serial and ends its in-flight requests.
func (c *CertRevocations) Add(serial string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.revokeLocked(serial)
}

// Loaded reports whether the set has been installed.
func (c *CertRevocations) Loaded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loaded
}

// Contains reports whether a canonical serial is revoked.
func (c *CertRevocations) Contains(serial string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, revoked := c.revoked[serial]
	return revoked
}

func (c *CertRevocations) revokeLocked(serial string) {
	c.revoked[serial] = struct{}{}
	for cancel := range c.active[serial] {
		(*cancel)(&CertRevokedError{Serial: serial})
	}
	delete(c.active, serial)
}

// closeOnRevocation ends a transport session when its certificate is revoked.
// Cancellation alone is not enough: the session does not inherit the request's
// context, and handlers that detach, as builds do, keep sending over it after
// their context ends. A request that ends for any other reason leaves the session
// to its usual teardown.
func closeOnRevocation(ctx context.Context, sess rpcSession) (stop func() bool) {
	return context.AfterFunc(ctx, func() {
		if _, revoked := errors.AsType[*CertRevokedError](context.Cause(ctx)); revoked {
			_ = sess.Close()
		}
	})
}

// admit decides whether a request presenting cert may proceed. The check and the
// registration share one critical section, so a revocation either refuses the
// request here or finds it registered and cancels it. Nothing slips between.
func (c *CertRevocations) admit(ctx context.Context, cert *x509.Certificate) (context.Context, func(), error) {
	serial := CertSerial(cert.SerialNumber)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		return nil, nil, errCertRevocationsNotLoaded
	}
	if _, revoked := c.revoked[serial]; revoked {
		return nil, nil, &CertRevokedError{Serial: serial}
	}

	ctx, cancel := context.WithCancelCause(ctx)
	if c.active[serial] == nil {
		c.active[serial] = make(map[*context.CancelCauseFunc]struct{})
	}
	c.active[serial][&cancel] = struct{}{}

	release := func() {
		cancel(nil)

		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.active[serial], &cancel)
		if len(c.active[serial]) == 0 {
			delete(c.active, serial)
		}
	}

	return ContextWithCallerCertSerial(ctx, serial), release, nil
}

type callerCertSerialKey struct{}

// ContextWithCallerCertSerial records the serial of the caller's verified client
// certificate.
func ContextWithCallerCertSerial(ctx context.Context, serial string) context.Context {
	return context.WithValue(ctx, callerCertSerialKey{}, serial)
}

// CallerCertSerial returns the canonical serial of the verified client
// certificate on this request, or "" when there is none or the server does not
// enforce revocations.
func CallerCertSerial(ctx context.Context) string {
	serial, _ := ctx.Value(callerCertSerialKey{}).(string)
	return serial
}
