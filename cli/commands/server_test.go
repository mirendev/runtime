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
			name: "all warn together, each named individually",
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
