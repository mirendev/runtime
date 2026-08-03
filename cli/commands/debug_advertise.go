package commands

import (
	"fmt"
	"net"
	"sort"
	"time"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/ipdiscovery"
)

// DebugAdvertise runs the same advertise-address computation the server uses
// (coordinate.ComputeAdvertise) and prints a per-candidate explanation plus
// the final advertised list, so we can debug cases where the server
// advertises addresses that aren't actually reachable from clients.
func DebugAdvertise(ctx *Context, opts struct {
	FormatOptions
	CloudURL      string   `long:"cloud-url" description:"Cloud URL to use for netcheck (default: https://api.miren.cloud)"`
	SkipNetcheck  bool     `long:"skip-netcheck" description:"Skip the netcheck call and only report interface scan"`
	AdditionalIPs []string `long:"additional-ip" description:"Simulate a server-configured AdditionalIP (repeatable)"`
	ListenAddr    string   `long:"listen" description:"Simulate the server's listen address (default: 0.0.0.0:8443)"`
}) error {
	cloudURL := opts.CloudURL
	if cloudURL == "" {
		cloudURL = coordinate.DefaultCloudURL
	}
	listenAddr := opts.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:8443"
	}

	// JSON output silences the human-oriented progress logs so the resulting
	// document is the only thing on stdout.
	humanInfo := func(format string, args ...any) {
		if opts.IsJSON() {
			return
		}
		ctx.Info(format, args...)
	}

	humanInfo("debug advertise — reproducing server advertisement logic")
	humanInfo("  cloud URL:    %s", cloudURL)
	humanInfo("  listen:       %s", listenAddr)
	humanInfo("  netcheck:     %s", boolWord(!opts.SkipNetcheck, "enabled", "skipped"))
	humanInfo("")

	discoveryOpts := ipdiscovery.Options{}
	if !opts.SkipNetcheck {
		discoveryOpts.NetcheckURL = cloudURL
	}

	humanInfo("Step 1: interface scan")
	discovery, err := ipdiscovery.DiscoverWithTimeout(15*time.Second, ctx.Log, discoveryOpts)
	if err != nil {
		// Match server.go: warn and keep going so the command can still
		// exercise the explicit-IP and netcheck paths it's designed to
		// diagnose, even when interface discovery itself misbehaves.
		ctx.Warn("ipdiscovery.Discover failed: %v", err)
		discovery = &ipdiscovery.Discovery{}
	}

	ipSet := coordinate.NewIPSet()
	for _, a := range discovery.Addresses {
		ip := net.ParseIP(a.IP)
		if ip == nil {
			continue
		}
		// server.go drops link-local addresses before handing the list
		// to the coordinator — mirror that here.
		if ip.IsLinkLocalUnicast() {
			humanInfo("  %-15s %-40s [skipped: link-local]", a.Interface, a.IP)
			continue
		}
		humanInfo("  %-15s %-40s (discovered)", a.Interface, a.IP)
		ipSet.AddDiscoveredFrom(ip, a.Interface)
	}

	for _, s := range opts.AdditionalIPs {
		ip := net.ParseIP(s)
		if ip == nil {
			ctx.Warn("--additional-ip %q is not a valid IP, skipping", s)
			continue
		}
		humanInfo("  %-15s %-40s (explicit)", "user", ip.String())
		ipSet.AddExplicit(ip)
	}
	humanInfo("")

	humanInfo("Step 2: dual-stack netcheck")
	var netcheckResult *cloudauth.NetcheckDualStackResult
	if opts.SkipNetcheck {
		humanInfo("  skipped (--skip-netcheck)")
	} else {
		ports := []cloudauth.NetcheckPort{
			{Port: 8443, Protocol: "https"},
			{Port: 8443, Protocol: "http3"},
		}
		netcheckResult, err = cloudauth.NetcheckDualStack(ctx, cloudURL, ports)
		if err != nil {
			ctx.Warn("netcheck failed: %v", err)
			netcheckResult = nil
		} else if !opts.IsJSON() {
			printNetcheckResponse(ctx, "IPv4", netcheckResult.IPv4)
			printNetcheckResponse(ctx, "IPv6", netcheckResult.IPv6)
		}
	}
	humanInfo("")

	candidates, final := coordinate.ComputeAdvertise(coordinate.AdvertiseInput{
		ListenAddr: listenAddr,
		IPs:        ipSet.All(),
		Netcheck:   netcheckResult,
	})

	if opts.IsJSON() {
		type candidateJSON struct {
			Source         string `json:"source"`
			HostPort       string `json:"host_port"`
			IP             string `json:"ip,omitempty"`
			Interface      string `json:"interface,omitempty"`
			Classification string `json:"classification,omitempty"`
			Included       bool   `json:"included"`
			Reason         string `json:"reason"`
		}
		type output struct {
			Candidates []candidateJSON `json:"candidates"`
			Advertised []string        `json:"advertised"`
		}
		out := output{
			Candidates: make([]candidateJSON, len(candidates)),
			Advertised: final,
		}
		for i, c := range candidates {
			out.Candidates[i] = candidateJSON{
				Source:         c.Source,
				HostPort:       c.HostPort,
				Interface:      c.Interface,
				Classification: c.Classification,
				Included:       c.Included,
				Reason:         c.Reason,
			}
			if c.IP != nil {
				out.Candidates[i].IP = c.IP.String()
			}
		}
		return PrintJSON(out)
	}

	ctx.Info("Step 3: per-candidate classification and inclusion decision")
	ctx.Info("")
	ctx.Info("  %-12s %-40s %-12s %-16s %-10s %s", "SOURCE", "IP:PORT", "IFACE", "CLASS", "DECISION", "REASON")
	ctx.Info("  %s", "-------------------------------------------------------------------------------------------------------------------------")
	for _, c := range candidates {
		decision := "SKIPPED"
		if c.Included {
			decision = "ADVERTISED"
		}
		iface := c.Interface
		if iface == "" {
			iface = "-"
		}
		ctx.Info("  %-12s %-40s %-12s %-16s %-10s %s",
			c.Source, c.HostPort, iface, c.Classification, decision, c.Reason)
	}

	ctx.Info("")
	ctx.Info("Final advertised list (%d entries):", len(final))
	for _, a := range final {
		ctx.Info("  %s", a)
	}

	return nil
}

func printNetcheckResponse(ctx *Context, family string, resp *cloudauth.NetcheckResponse) {
	if resp == nil {
		ctx.Info("  %s: no response", family)
		return
	}
	var reachable []string
	var unreachable []string
	for _, r := range resp.Results {
		entry := fmt.Sprintf("%s/%d", r.Protocol, r.Port)
		if r.Reachable {
			reachable = append(reachable, entry)
		} else {
			unreachable = append(unreachable, entry)
		}
	}
	sort.Strings(reachable)
	sort.Strings(unreachable)
	ctx.Info("  %s source=%s reachable=%v unreachable=%v duration=%dms",
		family, resp.SourceAddress, reachable, unreachable, resp.DurationMs)
}

func boolWord(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
