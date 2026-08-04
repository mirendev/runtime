package rpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"testing"
	"time"
)

// captureLogger returns a slog.Logger writing JSON records into buf, so tests
// can assert on the exact fields an audit line carries.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// decodeRecords parses each JSON log line in buf into a map.
func decodeRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decoding log record: %v", err)
		}
		records = append(records, rec)
	}
	return records
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8443":     true,
		"[::1]:8443":         true, // IPv6 loopback (dual-stack / v6-only hosts)
		"192.168.144.7:8444": false,
		"203.0.113.7:44321":  false,
		"[2001:db8::1]:443":  false, // routable IPv6
		"garbage":            false, // unparseable -> treated as non-loopback
		"":                   false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestLogCertAuth(t *testing.T) {
	rawDER := []byte("some-der-certificate-bytes")
	cert := &x509.Certificate{
		Raw:          rawDER,
		Subject:      pkix.Name{CommonName: "miren-server"},
		Issuer:       pkix.Name{CommonName: "miren-ca"},
		SerialNumber: big.NewInt(0x0abc),
	}

	withVerifiedCert := func(req *http.Request) {
		req.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		}
	}

	t.Run("verified cert from a network peer emits a full audit line at info", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = "203.0.113.7:44321"
		withVerifiedCert(req)

		logCertAuth(context.Background(), captureLogger(&buf), nil, req)

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		rec := records[0]

		sum := sha256.Sum256(rawDER)
		wantFP := hex.EncodeToString(sum[:])

		checks := map[string]any{
			"msg":         "cert auth",
			"level":       "INFO",
			"remote":      "203.0.113.7:44321",
			"subject":     "CN=miren-server",
			"issuer":      "CN=miren-ca",
			"serial":      "2748", // 0x0abc in decimal
			"fingerprint": wantFP,
			"verified":    true,
		}
		for k, want := range checks {
			if got := rec[k]; got != want {
				t.Errorf("field %q: got %v, want %v", k, got, want)
			}
		}
		// chains is a JSON number; it decodes to float64(1).
		if got := rec["chains"]; got != float64(1) {
			t.Errorf("field chains: got %v, want 1", got)
		}
	})

	t.Run("verified cert over loopback drops to debug", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = "127.0.0.1:8443"
		withVerifiedCert(req)

		logCertAuth(context.Background(), captureLogger(&buf), nil, req)

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if got := records[0]["level"]; got != "DEBUG" {
			t.Errorf("expected DEBUG level for loopback, got %v", got)
		}
	})

	t.Run("no TLS emits nothing", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)

		logCertAuth(context.Background(), captureLogger(&buf), nil, req)

		if buf.Len() != 0 {
			t.Errorf("expected no log output, got %q", buf.String())
		}
	})

	t.Run("TLS without peer certs emits nothing", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.TLS = &tls.ConnectionState{}

		logCertAuth(context.Background(), captureLogger(&buf), nil, req)

		if buf.Len() != 0 {
			t.Errorf("expected no log output, got %q", buf.String())
		}
	})
}

// TestAuditLoggerLevelFloor verifies the audit logger holds an Info floor no
// matter what level the base logger is at. Without the floor, a verbosely-run
// process would never suppress the audit stream's own Debug records (loopback
// traffic). It must also emit Info even when the base is quieter than Info, so
// the audit trail can't be silenced by lowering -v.
func TestAuditLoggerLevelFloor(t *testing.T) {
	levels := map[string]slog.Level{
		"base at debug (-vv)": slog.LevelDebug,
		"base at info":        slog.LevelInfo,
		"base at warn":        slog.LevelWarn,
	}

	for name, baseLevel := range levels {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			base := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: baseLevel}))
			audit := newAuditLogger(base)

			audit.Debug("loopback line")
			audit.Info("remote line")
			audit.Warn("denial line")

			var msgs []string
			for _, rec := range decodeRecords(t, &buf) {
				msgs = append(msgs, rec["msg"].(string))
			}

			// Debug is always dropped (floor is Info); Info and Warn always emit,
			// even when the base handler is at Warn.
			want := []string{"remote line", "denial line"}
			if len(msgs) != len(want) {
				t.Fatalf("got messages %v, want %v", msgs, want)
			}
			for i := range want {
				if msgs[i] != want[i] {
					t.Errorf("message %d: got %q, want %q", i, msgs[i], want[i])
				}
			}
		})
	}
}

func TestLogAuthReject(t *testing.T) {
	var buf bytes.Buffer
	req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
	req.RemoteAddr = "203.0.113.9:5555"

	logAuthReject(captureLogger(&buf), req, OID("cap-123"), "signature verification failed")

	records := decodeRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]

	want := map[string]any{
		"msg":    "auth rejected",
		"level":  "WARN",
		"remote": "203.0.113.9:5555",
		"oid":    "cap-123",
		"reason": "signature verification failed",
	}
	for k, v := range want {
		if got := rec[k]; got != v {
			t.Errorf("field %q: got %v, want %v", k, got, v)
		}
	}
}

func TestLogAccess(t *testing.T) {
	mm := Method{Name: "List", InterfaceName: "AppServer"}

	newReq := func() *http.Request {
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = "198.51.100.4:5555"
		return req
	}

	ctxWithIdentity := ContextWithIdentity(context.Background(), &Identity{
		Subject: "user-42",
		Method:  AuthMethodJWT,
	})

	t.Run("ok outcome logs at info with identity fields", func(t *testing.T) {
		var buf bytes.Buffer
		logAccess(ctxWithIdentity, captureLogger(&buf), newReq(), mm, "ok")

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		rec := records[0]

		want := map[string]any{
			"msg":         "rpc access",
			"level":       "INFO",
			"remote":      "198.51.100.4:5555",
			"rpc":         "AppServer.List",
			"subject":     "user-42",
			"auth_method": "jwt",
			"outcome":     "ok",
		}
		for k, v := range want {
			if got := rec[k]; got != v {
				t.Errorf("field %q: got %v, want %v", k, got, v)
			}
		}
	})

	t.Run("ok outcome over loopback drops to debug", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = "127.0.0.1:8443"
		logAccess(ctxWithIdentity, captureLogger(&buf), req, mm, "ok")

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if got := records[0]["level"]; got != "DEBUG" {
			t.Errorf("expected DEBUG level for loopback ok, got %v", got)
		}
	})

	t.Run("denial over loopback still logs at warn", func(t *testing.T) {
		var buf bytes.Buffer
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = "127.0.0.1:8443"
		logAccess(ctxWithIdentity, captureLogger(&buf), req, mm, "forbidden", "error", "denied")

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if got := records[0]["level"]; got != "WARN" {
			t.Errorf("expected WARN level for loopback denial, got %v", got)
		}
	})

	t.Run("forbidden outcome logs at warn and carries extra fields", func(t *testing.T) {
		var buf bytes.Buffer
		logAccess(ctxWithIdentity, captureLogger(&buf), newReq(), mm, "forbidden", "error", "access denied by RBAC policy")

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		rec := records[0]

		if rec["level"] != "WARN" {
			t.Errorf("expected WARN level, got %v", rec["level"])
		}
		if rec["outcome"] != "forbidden" {
			t.Errorf("expected outcome=forbidden, got %v", rec["outcome"])
		}
		if rec["error"] != "access denied by RBAC policy" {
			t.Errorf("expected error field passed through, got %v", rec["error"])
		}
	})

	t.Run("unauthorized outcome without identity logs empty subject", func(t *testing.T) {
		var buf bytes.Buffer
		logAccess(context.Background(), captureLogger(&buf), newReq(), mm, "unauthorized")

		records := decodeRecords(t, &buf)
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		rec := records[0]

		if rec["level"] != "WARN" {
			t.Errorf("expected WARN level, got %v", rec["level"])
		}
		if rec["subject"] != "" {
			t.Errorf("expected empty subject, got %v", rec["subject"])
		}
		if rec["auth_method"] != "" {
			t.Errorf("expected empty auth_method, got %v", rec["auth_method"])
		}
	})
}

// fakeClockDeduper returns a deduper whose clock the test drives directly, plus
// a pointer to the current time so the test can advance it.
func fakeClockDeduper(interval time.Duration) (*certAuthDeduper, *time.Time) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	d := newCertAuthDeduper()
	d.interval = interval
	d.now = func() time.Time { return now }
	return d, &now
}

// TestCertAuthDeduper covers the collapsing rules for repeated cert-auth
// records. The volume problem this solves is real: a coordinator with two
// distributed runners emitted 5,958 identical-except-for-timestamp cert auth
// lines per hour, 47% of everything it logged.
func TestCertAuthDeduper(t *testing.T) {
	key := certAuthKey{fingerprint: "fp-a", host: "10.0.0.1"}

	t.Run("first sight logs, repeats within the interval do not", func(t *testing.T) {
		d, _ := fakeClockDeduper(5 * time.Minute)

		if logged, suppressed := d.record(key); !logged || suppressed != 0 {
			t.Fatalf("first record: got (%v, %d), want (true, 0)", logged, suppressed)
		}
		for i := 0; i < 100; i++ {
			if logged, _ := d.record(key); logged {
				t.Fatalf("repeat %d logged, want suppressed", i)
			}
		}
	})

	t.Run("crossing the interval logs again and reports what it absorbed", func(t *testing.T) {
		d, now := fakeClockDeduper(5 * time.Minute)

		d.record(key)
		for i := 0; i < 4999; i++ {
			d.record(key)
		}

		*now = now.Add(5 * time.Minute)

		logged, suppressed := d.record(key)
		if !logged {
			t.Fatal("expected a record once the interval elapsed")
		}
		if suppressed != 4999 {
			t.Errorf("suppressed count: got %d, want 4999", suppressed)
		}

		// The counter resets, so the next window reports only its own.
		if _, s := d.record(key); s != 0 {
			t.Errorf("counter did not reset: got %d", s)
		}
	})

	t.Run("ephemeral source ports collapse into one entry", func(t *testing.T) {
		d, _ := fakeClockDeduper(5 * time.Minute)

		// remoteHost strips the port, which is what makes dedup effective:
		// a peer reconnecting from a new source port is the same peer.
		first := certAuthKey{fingerprint: "fp-a", host: remoteHost("10.0.0.1:8444")}
		second := certAuthKey{fingerprint: "fp-a", host: remoteHost("10.0.0.1:41857")}

		if logged, _ := d.record(first); !logged {
			t.Fatal("first port should log")
		}
		if logged, _ := d.record(second); logged {
			t.Error("a different source port should not produce a second record")
		}
	})

	t.Run("distinct peers and distinct certs are tracked separately", func(t *testing.T) {
		d, _ := fakeClockDeduper(5 * time.Minute)

		sameCertOtherHost := certAuthKey{fingerprint: "fp-a", host: "10.0.0.2"}
		otherCertSameHost := certAuthKey{fingerprint: "fp-b", host: "10.0.0.1"}

		for _, k := range []certAuthKey{key, sameCertOtherHost, otherCertSameHost} {
			if logged, _ := d.record(k); !logged {
				t.Errorf("key %+v should log on first sight", k)
			}
		}
	})

	t.Run("a nil deduper logs everything", func(t *testing.T) {
		var d *certAuthDeduper
		for i := 0; i < 3; i++ {
			if logged, suppressed := d.record(key); !logged || suppressed != 0 {
				t.Fatalf("nil deduper: got (%v, %d), want (true, 0)", logged, suppressed)
			}
		}
	})

	t.Run("the table stays bounded as peers churn", func(t *testing.T) {
		d, now := fakeClockDeduper(5 * time.Minute)
		d.max = 16

		for i := 0; i < 500; i++ {
			d.record(certAuthKey{fingerprint: "fp-a", host: fmt.Sprintf("10.0.%d.%d", i/256, i%256)})
			// Advance a little so entries age out rather than all looking current.
			*now = now.Add(time.Second)
		}

		if len(d.seen) > d.max {
			t.Errorf("table grew past its cap: %d entries, max %d", len(d.seen), d.max)
		}
	})
}

// TestLogCertAuthDeduplicates checks the emitted records end to end: one line
// for a burst, and a follow-up line carrying the suppressed count rather than
// silently dropping the fact that authentication kept happening.
func TestLogCertAuthDeduplicates(t *testing.T) {
	cert := &x509.Certificate{
		Raw:          []byte("cert-der"),
		Subject:      pkix.Name{CommonName: "runner-1"},
		Issuer:       pkix.Name{CommonName: "miren-ca"},
		SerialNumber: big.NewInt(7),
	}

	newReq := func(addr string) *http.Request {
		req, _ := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.RemoteAddr = addr
		req.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		}
		return req
	}

	var buf bytes.Buffer
	d, now := fakeClockDeduper(5 * time.Minute)
	log := captureLogger(&buf)

	// A burst from one peer, including a reconnect on a fresh source port.
	logCertAuth(context.Background(), log, d, newReq("10.0.0.1:8444"))
	for i := 0; i < 999; i++ {
		logCertAuth(context.Background(), log, d, newReq("10.0.0.1:41857"))
	}

	records := decodeRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected the burst to collapse to 1 record, got %d", len(records))
	}
	if got := records[0]["subject"]; got != "CN=runner-1" {
		t.Errorf("subject: got %v, want CN=runner-1", got)
	}
	if _, ok := records[0]["suppressed"]; ok {
		t.Error("the first record should not claim to have suppressed anything")
	}

	// Once the interval passes, the next authentication reports the backlog.
	buf.Reset()
	*now = now.Add(5 * time.Minute)
	logCertAuth(context.Background(), log, d, newReq("10.0.0.1:8444"))

	records = decodeRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record after the interval, got %d", len(records))
	}
	if got := records[0]["suppressed"]; got != float64(999) {
		t.Errorf("suppressed: got %v, want 999", got)
	}
}
