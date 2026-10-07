package serverconfig

import (
	"strings"
	"testing"
)

func TestValidateIngressCoherence(t *testing.T) {
	type setup func(*Config)

	cases := []struct {
		name         string
		setup        setup
		wantContains string // empty means no error
	}{
		{
			name: "default config is valid",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeAutoprovision)
			},
		},
		{
			name: "tls-autoprovision rejects address override",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeAutoprovision)
				c.Ingress.SetAddress("0.0.0.0:443")
			},
			wantContains: "ingress.address must be empty",
		},
		{
			name: "behind-proxy-https accepts default (empty) address",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTPS)
				c.TLS.SetSelfSigned(true)
			},
		},
		{
			name: "behind-proxy-https accepts custom address",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTPS)
				c.Ingress.SetAddress("127.0.0.1:8443")
				c.TLS.SetSelfSigned(true)
			},
		},
		{
			name: "behind-proxy-http accepts plain address",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.Ingress.SetAddress("0.0.0.0:80")
			},
		},
		{
			name: "behind-proxy-http rejects populated tls.self_signed",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.SetSelfSigned(true)
			},
			wantContains: "tls.self_signed",
		},
		{
			name: "behind-proxy-http rejects populated tls.acme_email",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.SetAcmeEmail("ops@example.com")
			},
			wantContains: "tls.acme_email",
		},
		{
			name: "behind-proxy-http reports all populated tls fields",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.SetAcmeEmail("ops@example.com")
				c.TLS.SetAcmeDNSProvider("cloudflare")
			},
			wantContains: "tls.acme_email, tls.acme_dns_provider",
		},
		{
			// tls.additional_names / tls.additional_ips also feed the API
			// server cert (and the etcd cert), which exist regardless of
			// ingress.mode. So they must be allowed in behind-proxy-http.
			name: "behind-proxy-http accepts tls.additional_names",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.AdditionalNames = []string{"miren.example.com"}
			},
		},
		{
			name: "behind-proxy-http accepts tls.additional_ips",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.AdditionalIPs = []string{"203.0.113.5"}
			},
		},
		{
			name: "behind-proxy-http rejects ingress-only fields even when additional_* are also set",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.TLS.SetAcmeEmail("ops@example.com")
				c.TLS.AdditionalNames = []string{"miren.example.com"}
			},
			wantContains: "tls.acme_email",
		},
		{
			name: "rejects unix: address with clear message",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.Ingress.SetAddress("unix:/var/run/miren.sock")
			},
			wantContains: "unix socket binding",
		},
		{
			name: "rejects malformed address",
			setup: func(c *Config) {
				c.Ingress.SetMode(IngressModeBehindProxyHTTP)
				c.Ingress.SetAddress("not-a-real-address")
			},
			wantContains: "must be a host:port form",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.setup(cfg)

			err := cfg.ValidateIngressCoherence()
			switch {
			case tc.wantContains == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantContains != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tc.wantContains)
			case tc.wantContains != "" && !strings.Contains(err.Error(), tc.wantContains):
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantContains)
			}
		})
	}
}

func TestValidateTelemetryMetricsCoherence(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		audience string
		wantErr  string
	}{
		{name: "disabled"},
		{name: "configured", url: "https://metrics.example.com/api/v1/write", audience: "metrics.example.com"},
		{name: "missing audience", url: "https://metrics.example.com/write", wantErr: "audience is required"},
		{name: "missing url", audience: "metrics.example.com", wantErr: "remote_write_url is required"},
		{name: "relative url", url: "/api/v1/write", audience: "metrics.example.com", wantErr: "absolute http or https URL"},
		{name: "unsupported scheme", url: "ftp://metrics.example.com/write", audience: "metrics.example.com", wantErr: "absolute http or https URL"},
		{name: "embedded credentials", url: "https://user:password@metrics.example.com/write", audience: "metrics.example.com", wantErr: "must not contain credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Telemetry.Metrics.SetRemoteWriteURL(tt.url)
			cfg.Telemetry.Metrics.SetWorkloadIdentityAudience(tt.audience)
			err := cfg.ValidateTelemetryCoherence()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTelemetryTracesCoherence(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		otelEnv    string
		audience   string
		configOnly bool
		wantErr    string
	}{
		{name: "disabled"},
		{name: "static auth from config endpoint", endpoint: "https://traces.example.com"},
		{name: "identity with config endpoint", endpoint: "https://traces.example.com", audience: "traces.example.com"},
		{name: "identity with OTel env endpoint", otelEnv: "https://traces.example.com", audience: "traces.example.com"},
		{name: "identity without any endpoint", audience: "traces.example.com", wantErr: "no traces endpoint"},
		{name: "endpoint-less config alone is coherent", audience: "traces.example.com", configOnly: true},
		{name: "relative endpoint", endpoint: "/v1/traces", wantErr: "absolute http or https URL"},
		{name: "embedded credentials", endpoint: "https://user:pw@traces.example.com", wantErr: "must not contain credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.otelEnv)
			cfg := DefaultConfig()
			cfg.Telemetry.Traces.SetEndpoint(tt.endpoint)
			cfg.Telemetry.Traces.SetWorkloadIdentityAudience(tt.audience)
			// config validate only runs the coherence check, since the
			// endpoint may come from an environment the operator's shell
			// doesn't have.
			err := cfg.ValidateTelemetryCoherence()
			if err == nil && !tt.configOnly {
				err = cfg.ValidateTelemetryEnvironment()
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestTracesResolvedEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel-env.example.com")

	cfg := DefaultConfig()
	if got := cfg.Telemetry.Traces.ResolvedEndpoint(); got != "https://otel-env.example.com" {
		t.Errorf("ResolvedEndpoint() = %q, want the OTel env fallback", got)
	}
	cfg.Telemetry.Traces.SetEndpoint("https://config.example.com")
	if got := cfg.Telemetry.Traces.ResolvedEndpoint(); got != "https://config.example.com" {
		t.Errorf("ResolvedEndpoint() = %q, want config to win over OTel env", got)
	}
}

func TestResolveDeprecatedConfig(t *testing.T) {
	tests := []struct {
		name    string
		oldURL  string
		newURL  string
		wantURL string
		wantErr string
	}{
		{name: "nothing set"},
		{name: "only deprecated", oldURL: "https://old.example.com/write", wantURL: "https://old.example.com/write"},
		{name: "only current", newURL: "https://new.example.com/write", wantURL: "https://new.example.com/write"},
		{name: "both agree", oldURL: "https://same.example.com/write", newURL: "https://same.example.com/write", wantURL: "https://same.example.com/write"},
		{name: "both disagree", oldURL: "https://old.example.com/write", newURL: "https://new.example.com/write", wantErr: "remove the deprecated key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			if tt.oldURL != "" {
				cfg.Metrics.RemoteWrite.SetURL(tt.oldURL)
			}
			if tt.newURL != "" {
				cfg.Telemetry.Metrics.SetRemoteWriteURL(tt.newURL)
			}
			err := cfg.ResolveDeprecatedConfig()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := cfg.Telemetry.Metrics.GetRemoteWriteURL(); got != tt.wantURL {
				t.Errorf("RemoteWriteURL = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
