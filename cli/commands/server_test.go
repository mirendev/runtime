//go:build linux

package commands

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"miren.dev/runtime/pkg/serverconfig"
)

func TestWarnIngressTLSOverride(t *testing.T) {
	cases := []struct {
		name           string
		standardTLS    bool
		selfSigned     bool
		acmeEmail      string
		acmeProvider   string
		additionalIPs  []string
		additionalDNS  []string
		wantContains   []string
		notWantContain []string
	}{
		{
			name:           "default tls config: silent",
			notWantContain: []string{"ignored"},
		},
		{
			// standard_tls defaults to true, so any naïve "warn when set"
			// implementation would cry wolf on every default config. Pin the
			// exclusion so it doesn't silently regress.
			name:           "standard_tls true: silent (excluded by design)",
			standardTLS:    true,
			notWantContain: []string{"standard_tls", "ignored"},
		},
		{
			name:           "self_signed warns",
			selfSigned:     true,
			wantContains:   []string{"tls.self_signed"},
			notWantContain: []string{"acme", "additional"},
		},
		{
			name:           "acme_email warns by name",
			acmeEmail:      "ops@example.com",
			wantContains:   []string{"tls.acme_email"},
			notWantContain: []string{"acme_dns_provider", "self_signed"},
		},
		{
			name:           "acme_dns_provider warns by name",
			acmeProvider:   "cloudflare",
			wantContains:   []string{"tls.acme_dns_provider"},
			notWantContain: []string{"acme_email", "self_signed"},
		},
		{
			name:           "additional_ips warns",
			additionalIPs:  []string{"10.0.0.1"},
			wantContains:   []string{"tls.additional_ips"},
			notWantContain: []string{"additional_names"},
		},
		{
			name:          "all warn together, each named individually",
			selfSigned:    true,
			acmeEmail:     "ops@example.com",
			acmeProvider:  "cloudflare",
			additionalIPs: []string{"10.0.0.1"},
			additionalDNS: []string{"alt.example.com"},
			wantContains: []string{
				"tls.self_signed",
				"tls.acme_email",
				"tls.acme_dns_provider",
				"tls.additional_ips",
				"tls.additional_names",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tls := &serverconfig.TLSConfig{
				AdditionalIPs:   tc.additionalIPs,
				AdditionalNames: tc.additionalDNS,
			}
			tls.SetStandardTLS(tc.standardTLS)
			tls.SetSelfSigned(tc.selfSigned)
			tls.SetAcmeEmail(tc.acmeEmail)
			tls.SetAcmeDNSProvider(tc.acmeProvider)

			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

			warnIngressTLSOverride(log, tls)

			out := buf.String()
			for _, want := range tc.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("expected log output to contain %q, got: %s", want, out)
				}
			}
			for _, bad := range tc.notWantContain {
				if strings.Contains(out, bad) {
					t.Errorf("expected log output NOT to contain %q, got: %s", bad, out)
				}
			}
		})
	}
}

func TestSelectIngressMode(t *testing.T) {
	cases := []struct {
		name         string
		httpAddr     string
		httpsAddr    string
		standardTLS  bool
		selfSigned   bool
		dnsProvider  string
		wantMode     ingressMode
		wantAddr     string
		wantErr      bool
		wantContains string
	}{
		{
			name:        "default config picks standard TLS",
			standardTLS: true,
			wantMode:    ingressModeStandardTLS,
		},
		{
			name:     "standard_tls=false with no ingress falls back to plain :80",
			wantMode: ingressModeStandardHTTP,
		},
		{
			name:     "ingress.http_address picks custom HTTP",
			httpAddr: "127.0.0.1:8080",
			wantMode: ingressModeCustomHTTP,
			wantAddr: "127.0.0.1:8080",
		},
		{
			name:        "ingress.https_address with dns provider picks custom HTTPS",
			httpsAddr:   "127.0.0.1:8444",
			dnsProvider: "cloudflare",
			wantMode:    ingressModeCustomHTTPS,
			wantAddr:    "127.0.0.1:8444",
		},
		{
			name:       "ingress.https_address with self_signed picks custom HTTPS",
			httpsAddr:  "127.0.0.1:8444",
			selfSigned: true,
			wantMode:   ingressModeCustomHTTPS,
			wantAddr:   "127.0.0.1:8444",
		},
		{
			name:         "both ingress addresses set is rejected",
			httpAddr:     "127.0.0.1:8080",
			httpsAddr:    "127.0.0.1:8444",
			wantErr:      true,
			wantContains: "mutually exclusive",
		},
		{
			name:         "ingress.https_address without cert source is rejected",
			httpsAddr:    "127.0.0.1:8444",
			wantErr:      true,
			wantContains: "self_signed",
		},
		{
			name:        "ingress.http_address wins over standard_tls",
			httpAddr:    "127.0.0.1:8080",
			standardTLS: true,
			wantMode:    ingressModeCustomHTTP,
			wantAddr:    "127.0.0.1:8080",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &serverconfig.Config{}
			cfg.Ingress.SetHTTPAddress(tc.httpAddr)
			cfg.Ingress.SetHTTPSAddress(tc.httpsAddr)
			cfg.TLS.SetStandardTLS(tc.standardTLS)
			cfg.TLS.SetSelfSigned(tc.selfSigned)
			cfg.TLS.SetAcmeDNSProvider(tc.dnsProvider)

			mode, addr, err := selectIngressMode(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode {
				t.Errorf("mode: got %d, want %d", mode, tc.wantMode)
			}
			if addr != tc.wantAddr {
				t.Errorf("addr: got %q, want %q", addr, tc.wantAddr)
			}
		})
	}
}

func TestValidateIngressHTTPSAddressConfig(t *testing.T) {
	cases := []struct {
		name         string
		selfSigned   bool
		dnsProvider  string
		wantErr      bool
		wantContains string
	}{
		{name: "valid: dns provider set", dnsProvider: "cloudflare"},
		{name: "valid: self-signed", selfSigned: true},
		{name: "valid: self-signed wins when both set", selfSigned: true, dnsProvider: "cloudflare"},
		{name: "rejects neither cert source", wantErr: true, wantContains: "self_signed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tls := &serverconfig.TLSConfig{}
			tls.SetSelfSigned(tc.selfSigned)
			tls.SetAcmeDNSProvider(tc.dnsProvider)

			err := validateIngressHTTPSAddressConfig(tls)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
