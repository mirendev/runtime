package autotls

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeCertProvider struct {
	cert *tls.Certificate
}

func (f *fakeCertProvider) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return f.cert, nil
}

func TestServeTLSWithControllerOnAddr(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	cp := &fakeCertProvider{cert: &cert}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := serveTLSOnListener(ctx, slog.Default(), cp, handler, ln); err != nil {
		t.Fatalf("serveTLSOnListener: %v", err)
	}

	addr := ln.Addr().String()
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // test against in-process self-signed cert
		Timeout:   2 * time.Second,
	}

	// The serving goroutine is started but may not yet be accepting; retry briefly.
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get("https://" + addr + "/")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", string(body))
	}
}

func TestServeTLSWithControllerOnAddrInvalidAddress(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	cp := &fakeCertProvider{cert: &cert}

	const malformed = "127.0.0.1:not-a-port"
	err = ServeTLSWithControllerOnAddr(context.Background(), slog.Default(), cp, http.NotFoundHandler(), malformed)
	if err == nil {
		t.Fatal("expected error for malformed address, got nil")
	}
	// Verify our wrap (fmt.Errorf "listen %s: %w") rather than just probing stdlib behavior.
	wantPrefix := "listen " + malformed + ":"
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Fatalf("expected error prefix %q, got: %v", wantPrefix, err)
	}
}
