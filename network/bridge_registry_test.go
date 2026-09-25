//go:build linux

package network

import (
	"os"
	"strings"
	"testing"

	"github.com/coreos/go-iptables/iptables"
	"github.com/stretchr/testify/require"
)

func TestAllowRegistryFromWireGuardRules(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to inspect the INPUT chain")
	}
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	require.NoError(t, err)

	match := []string{"-d", "10.8.0.0/16", "-p", "tcp", "--dport", "5000"}
	legacy := []string{"-i", "rt0", "-p", "tcp", "--dport", "5000", "-j", "ACCEPT"}
	rules := [][]string{legacy, append(append([]string{}, match...), "-j", "DROP")}
	for _, iface := range []string{"lo", "rt0", "flannel-wg"} {
		rules = append(rules, append(append([]string{"-i", iface}, match...), "-j", "ACCEPT"))
	}
	for _, rule := range rules {
		present, err := ipt.Exists("filter", "INPUT", rule...)
		require.NoError(t, err)
		if !present {
			rule := rule
			t.Cleanup(func() { require.NoError(t, ipt.DeleteIfExists("filter", "INPUT", rule...)) })
		}
	}
	require.NoError(t, ipt.InsertUnique("filter", "INPUT", 1, legacy...))
	require.NoError(t, AllowRegistryFromWireGuard())

	installed, err := ipt.List("filter", "INPUT")
	require.NoError(t, err)
	allowed := map[string]int{}
	denied, oldAccept := -1, -1
	for i, rule := range installed {
		if !strings.Contains(rule, "--dport 5000") {
			continue
		}
		switch {
		case strings.Contains(rule, "-d 10.8.0.0/16") && strings.Contains(rule, "-j DROP"):
			denied = i
		case strings.Contains(rule, "-d 10.8.0.0/16") && strings.Contains(rule, "-j ACCEPT"):
			for _, iface := range []string{"lo", "rt0", "flannel-wg"} {
				if strings.Contains(rule, "-i "+iface) {
					allowed[iface] = i
				}
			}
		case strings.Contains(rule, "-i rt0") && strings.Contains(rule, "-j ACCEPT"):
			oldAccept = i
		}
	}
	require.GreaterOrEqual(t, denied, 0)
	for _, iface := range []string{"lo", "rt0", "flannel-wg"} {
		index, ok := allowed[iface]
		require.True(t, ok, "missing registry accept for %s", iface)
		require.Less(t, index, denied, "%s must be allowed before the registry drop", iface)
	}
	require.Greater(t, oldAccept, denied, "registry drop must precede old bridge allow")
}
