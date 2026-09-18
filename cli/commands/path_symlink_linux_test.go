//go:build linux

package commands

import (
	"testing"

	"miren.dev/runtime/pkg/serverinfo"
)

func TestPathSymlinkHealAllowed(t *testing.T) {
	cases := []struct {
		name          string
		systemd, boot string
		want          bool
	}{
		{"systemd install", "abc", "", true},
		{"nothing set", "", "", false},
		{"container-boot", "", "1", false},
		// The image binary is container-boot's fallback and must survive even
		// when systemd is also visible; DetectInstallKind would say systemd.
		{"container-boot under systemd", "abc", "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("INVOCATION_ID", tc.systemd)
			t.Setenv(serverinfo.ContainerBootEnv, tc.boot)
			if got := pathSymlinkHealAllowed(); got != tc.want {
				t.Fatalf("pathSymlinkHealAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}
