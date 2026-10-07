package serverconfig

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
)

// ValidateIngressCoherence runs cross-field validations on top of the generated
// Config.Validate(). Callers invoke it right after Load() so operators see
// configuration errors before any listener is wired.
//
// Two concerns live here:
//
//  1. ingress.address format checks, including a clear-message rejection of the
//     reserved-but-not-yet-supported unix:/path form (see RFD-84).
//  2. Coherence between ingress.mode and the [tls] block: behind-proxy-http
//     does not consult [tls] at all, so populating those fields is almost
//     certainly an operator mistake. Hard-error rather than silently ignore.
func (c *Config) ValidateIngressCoherence() error {
	mode := c.Ingress.GetMode()
	addr := c.Ingress.GetAddress()

	if addr != "" {
		if strings.HasPrefix(addr, "unix:") {
			return fmt.Errorf("ingress.address: unix socket binding (%q) is reserved for a future release; use a host:port form for now", addr)
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return fmt.Errorf("ingress.address %q: must be a host:port form (e.g. \"0.0.0.0:80\", \"127.0.0.1:443\", \"[::1]:8080\"): %w", addr, err)
		}
	}

	if mode == IngressModeAutoprovision && addr != "" {
		return fmt.Errorf("ingress.address must be empty when ingress.mode = %q; autoprovision binds :443 + :80 structurally to support HTTP-01 ACME challenges", mode)
	}

	if mode == IngressModeBehindProxyHTTP {
		var populated []string
		if c.TLS.GetSelfSigned() {
			populated = append(populated, "tls.self_signed")
		}
		if c.TLS.GetAcmeEmail() != "" {
			populated = append(populated, "tls.acme_email")
		}
		if c.TLS.GetAcmeDNSProvider() != "" {
			populated = append(populated, "tls.acme_dns_provider")
		}
		// tls.additional_names / tls.additional_ips are intentionally NOT
		// gated here: they're applied to the API server cert (always-on
		// TLS on the API port) and the etcd cert. Both exist regardless
		// of ingress.mode. The fields gated above (self_signed,
		// acme_email, acme_dns_provider) really are ingress-cert-only.
		if len(populated) > 0 {
			return fmt.Errorf("ingress.mode = %q does not terminate TLS, but the following [tls] fields are set and would be ignored: %s. Either remove them or pick a TLS-terminating mode (tls-autoprovision, behind-proxy-https)", mode, strings.Join(populated, ", "))
		}
	}

	return nil
}

// ResolveDeprecatedConfig carries values set under retired keys onto their
// replacements, so everything downstream reads one location. Call it right
// after Load and before any Validate*Coherence.
//
// [metrics.remote_write] shipped in v0.15 and moved to [telemetry.metrics] so
// metrics and traces destinations sit side by side. A retired key only fills
// in when its replacement is unset; setting both to different values is an
// error rather than a silent pick, because the operator is mid-migration and
// one of the two is stale.
func (c *Config) ResolveDeprecatedConfig() error {
	old := &c.Metrics.RemoteWrite
	cur := &c.Telemetry.Metrics
	if err := carryDeprecated(old.URL, &cur.RemoteWriteURL,
		"metrics.remote_write.url", "telemetry.metrics.remote_write_url"); err != nil {
		return err
	}
	return carryDeprecated(old.WorkloadIdentityAudience, &cur.WorkloadIdentityAudience,
		"metrics.remote_write.workload_identity_audience", "telemetry.metrics.workload_identity_audience")
}

func carryDeprecated(old *string, cur **string, oldKey, curKey string) error {
	if old == nil || *old == "" {
		return nil
	}
	if *cur == nil || **cur == "" {
		v := *old
		*cur = &v
		return nil
	}
	if **cur != *old {
		return fmt.Errorf("%s (deprecated) is %q but %s is %q; remove the deprecated key", oldKey, *old, curKey, **cur)
	}
	return nil
}

// ValidateTelemetryCoherence rejects half-configured or unsafe telemetry
// destinations before the coordinator starts shipping anything.
func (c *Config) ValidateTelemetryCoherence() error {
	if err := c.Telemetry.Metrics.validateCoherence(); err != nil {
		return err
	}
	return c.Telemetry.Traces.validateCoherence()
}

func (c *TelemetryMetricsConfig) validateCoherence() error {
	remoteWriteURL := c.GetRemoteWriteURL()
	audience := c.GetWorkloadIdentityAudience()

	if remoteWriteURL == "" && audience == "" {
		return nil
	}
	if remoteWriteURL == "" {
		return fmt.Errorf("telemetry.metrics.remote_write_url is required when workload_identity_audience is set")
	}
	if audience == "" {
		return fmt.Errorf("telemetry.metrics.workload_identity_audience is required when remote_write_url is set")
	}

	destination, err := url.Parse(remoteWriteURL)
	if err != nil || destination.Host == "" || (destination.Scheme != "http" && destination.Scheme != "https") {
		return fmt.Errorf("telemetry.metrics.remote_write_url %q must be an absolute http or https URL", remoteWriteURL)
	}
	if destination.User != nil {
		return fmt.Errorf("telemetry.metrics.remote_write_url must not contain credentials; authentication uses workload identity")
	}
	return nil
}

// Unlike metrics, traces may authenticate without workload identity: hosted
// trace backends take API keys, which operators pass the standard way in
// OTEL_EXPORTER_OTLP_HEADERS.
func (c *TelemetryTracesConfig) validateCoherence() error {
	if endpoint := c.GetEndpoint(); endpoint != "" {
		destination, err := url.Parse(endpoint)
		if err != nil || destination.Host == "" || (destination.Scheme != "http" && destination.Scheme != "https") {
			return fmt.Errorf("telemetry.traces.endpoint %q must be an absolute http or https URL", endpoint)
		}
		if destination.User != nil {
			return fmt.Errorf("telemetry.traces.endpoint must not contain credentials; use workload_identity_audience or OTEL_EXPORTER_OTLP_HEADERS")
		}
	}
	return nil
}

// ValidateTelemetryEnvironment checks what depends on the server's own
// environment rather than its config: a traces audience needs an endpoint,
// and that may come from OTEL_EXPORTER_OTLP_ENDPOINT. Only the server process
// should call it. `miren server config validate` runs in an operator's shell,
// which usually lacks the unit's environment, and would reject a config the
// server accepts.
func (c *Config) ValidateTelemetryEnvironment() error {
	traces := c.Telemetry.Traces
	if traces.GetWorkloadIdentityAudience() != "" && traces.ResolvedEndpoint() == "" {
		return fmt.Errorf("telemetry.traces.workload_identity_audience is set but there is no traces endpoint; set telemetry.traces.endpoint or OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	return nil
}

// ResolvedEndpoint is the OTLP/HTTP base URL traces export to:
// telemetry.traces.endpoint when set, otherwise the standard
// OTEL_EXPORTER_OTLP_ENDPOINT that clusters configured before this section
// existed still rely on.
func (c *TelemetryTracesConfig) ResolvedEndpoint() string {
	if endpoint := c.GetEndpoint(); endpoint != "" {
		return endpoint
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
}

// WarnDeprecatedConfig logs warnings for any deprecated configuration fields
// that have been explicitly set by the operator. Call after Load so the
// warnings surface at startup. tls.standard_tls is retained as a no-op for
// backwards compatibility (existing systemd unit files, env vars, and config
// files from pre-RFD-84 installs) but the operator should migrate to
// ingress.mode. [metrics.remote_write] still works through
// ResolveDeprecatedConfig but has moved to [telemetry.metrics].
func (c *Config) WarnDeprecatedConfig(log *slog.Logger) {
	if c.TLS.StandardTLS != nil {
		log.Warn("tls.standard_tls (also --serve-tls / MIREN_TLS_STANDARD_TLS) is deprecated and ignored; use ingress.mode to pick the deployment shape. See RFD-84 at rfd.miren.garden/rfd/84.",
			"value", *c.TLS.StandardTLS)
	}
	if c.Metrics.RemoteWrite.URL != nil || c.Metrics.RemoteWrite.WorkloadIdentityAudience != nil {
		log.Warn("[metrics.remote_write] (also MIREN_METRICS_REMOTE_WRITE_URL / MIREN_METRICS_REMOTE_WRITE_AUDIENCE) is deprecated; " +
			"use [telemetry.metrics] remote_write_url and workload_identity_audience " +
			"(MIREN_TELEMETRY_METRICS_REMOTE_WRITE_URL / MIREN_TELEMETRY_METRICS_AUDIENCE)")
	}
}
