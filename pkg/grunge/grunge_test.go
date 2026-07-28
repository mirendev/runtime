package grunge

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"testing"
)

// An unset backend used to fall through to vxlan. Callers reach SetupConfig
// with whatever their config produced, so a runner joined before the field
// existed would quietly publish a plaintext overlay to the whole cluster.
func TestSetupConfigRejectsEmptyBackend(t *testing.T) {
	n := &Network{log: slog.Default()}

	err := n.SetupConfig(context.Background(),
		netip.MustParsePrefix("10.8.0.0/16"),
		netip.MustParsePrefix("fd47:ace::/64"),
	)
	if err == nil {
		t.Fatal("expected an error for an unconfigured backend, got nil")
	}
	if err != errNoBackend {
		t.Fatalf("expected errNoBackend, got %v", err)
	}
}

// Each backend must clean up after the others without listing anything it
// creates itself. Getting this wrong deletes the device we are about to
// register on, so it is worth pinning rather than trusting the table by eye.
func TestStaleBackendDevicesExcludeOwnDevices(t *testing.T) {
	owned := map[string][]string{
		"vxlan":     {"flannel.1"},
		"wireguard": {"flannel-wg", "flannel-wg-v6"},
	}

	for backend, ours := range owned {
		stale, ok := staleBackendDevices[backend]
		if !ok {
			t.Errorf("backend %q has no stale device list", backend)
			continue
		}

		for _, device := range stale {
			for _, own := range ours {
				if device == own {
					t.Errorf("backend %q would delete its own device %q", backend, device)
				}
			}
		}
	}

	// Every device one backend owns should be cleaned up by the other, or
	// switching backends leaves a more specific route behind.
	for backend, ours := range owned {
		for other, stale := range staleBackendDevices {
			if other == backend {
				continue
			}
			for _, own := range ours {
				if !slices.Contains(stale, own) {
					t.Errorf("backend %q does not clean up %q left by %q", other, own, backend)
				}
			}
		}
	}
}

// An unrecognized backend has nothing registered to clean up, and must not be
// treated as a reason to start deleting interfaces.
func TestRemoveStaleBackendDevicesIgnoresUnknownBackend(t *testing.T) {
	n := &Network{log: slog.Default()}

	if err := n.removeStaleBackendDevices("hostgw"); err != nil {
		t.Fatalf("expected no error for an unknown backend, got %v", err)
	}
}

// A device that isn't there is the ordinary case on a node that never ran the
// other backend, so it has to read as success rather than as a lookup failure.
// This covers the netlink lookup for real; the unknown-backend case above
// never reaches it, since that backend has no devices registered to check.
func TestRemoveDeviceIfPresentTreatsAbsenceAsSuccess(t *testing.T) {
	n := &Network{log: slog.Default()}

	// Deliberately a name nothing will have, so the test can exercise the
	// lookup without any chance of deleting a real interface.
	if err := n.removeDeviceIfPresent("miren-absent-dev0", "wireguard"); err != nil {
		t.Fatalf("expected absence to be treated as success, got %v", err)
	}
}
