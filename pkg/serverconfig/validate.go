package serverconfig

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
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

// ValidateMetricsCoherence rejects half-configured or unsafe remote-write
// destinations before the coordinator starts any managed metrics machinery.
func (c *Config) ValidateMetricsCoherence() error {
	remoteWriteURL := c.Metrics.RemoteWrite.GetURL()
	audience := c.Metrics.RemoteWrite.GetWorkloadIdentityAudience()

	if remoteWriteURL == "" && audience == "" {
		return nil
	}
	if remoteWriteURL == "" {
		return fmt.Errorf("metrics.remote_write.url is required when workload_identity_audience is set")
	}
	if audience == "" {
		return fmt.Errorf("metrics.remote_write.workload_identity_audience is required when url is set")
	}

	destination, err := url.Parse(remoteWriteURL)
	if err != nil || destination.Host == "" || (destination.Scheme != "http" && destination.Scheme != "https") {
		return fmt.Errorf("metrics.remote_write.url %q must be an absolute http or https URL", remoteWriteURL)
	}
	if destination.User != nil {
		return fmt.Errorf("metrics.remote_write.url must not contain credentials; authentication uses workload identity")
	}
	return nil
}

// WarnDeprecatedConfig logs warnings for any deprecated configuration fields
// that have been explicitly set by the operator. Call after Load so the
// warnings surface at startup. Currently only tls.standard_tls is treated this
// way: it's retained as a no-op for backwards compatibility (existing systemd
// unit files, env vars, and config files from pre-RFD-84 installs) but the
// operator should migrate to ingress.mode.
func (c *Config) WarnDeprecatedConfig(log *slog.Logger) {
	if c.TLS.StandardTLS != nil {
		log.Warn("tls.standard_tls (also --serve-tls / MIREN_TLS_STANDARD_TLS) is deprecated and ignored; use ingress.mode to pick the deployment shape. See RFD-84 at rfd.miren.garden/rfd/84.",
			"value", *c.TLS.StandardTLS)
	}
}
