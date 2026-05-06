//go:build linux

package commands

import (
	"strings"
	"testing"

	"miren.dev/runtime/pkg/serverconfig"
)

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
