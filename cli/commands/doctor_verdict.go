package commands

import (
	"errors"
	"fmt"
	"net"
	"runtime"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// checkServer turns the gathered evidence into a single verdict about the
// cluster.
//
// The old version asked only "did the RPC connection fail, and does its message
// contain the word timeout", which is why a stopped local server produced
// "Connection timed out. The server may be starting up" while the TCP probe two
// lines above said "connection refused". Every probe result is now an input to
// the answer rather than an independent row of output.
func checkServer(env *doctorEnv) checkResult {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}
	}

	addr := env.cluster.Hostname

	if env.connErr == nil {
		return checkResult{
			Status:  checkOK,
			Summary: fmt.Sprintf("connected to %s", addr),
		}
	}

	re, ok := errors.AsType[*rpc.ResolveError](env.connErr)
	if !ok {
		return checkResult{
			Status:  checkFail,
			Summary: "not connected",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("couldn't connect to %s", addr),
				Detail:  env.connErr.Error(),
				Actions: fallbackActions(env),
				Cause:   env.connErr,
			},
		}
	}

	switch re.Kind {
	case rpc.ResolveUnreachableError:
		return unreachableVerdict(env, addr)

	case rpc.ResolveWentSilentError:
		return checkResult{
			Status:  checkFail,
			Summary: "stopped responding",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("%s stopped responding mid-request", addr),
				Detail: "The connection was established and then went quiet. The server " +
					"was reachable moments ago, so it crashed, hung, or lost its " +
					"network path partway through.",
				Actions: serverLogActions(env),
				Cause:   env.connErr,
			},
		}

	case rpc.ResolveNoAnswerError:
		return checkResult{
			Status:  checkFail,
			Summary: "not answering",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("%s accepted the connection but never answered", addr),
				Detail: "The API port is open and the connection stayed up, but no reply " +
					"came back. A server that is still starting up looks exactly like " +
					"this, so if you restarted it recently, try again in a moment.",
				Actions: serverLogActions(env),
				Cause:   env.connErr,
			},
		}

	case rpc.ResolveLookupError:
		// The server answered. It is running and reachable; it just doesn't
		// have this capability registered yet, which is what a booting server
		// looks like from here.
		return checkResult{
			Status:  checkWarn,
			Summary: "up, but not ready",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("%s is running but isn't serving requests yet", addr),
				Detail: "The server answered and said it doesn't have the capability this " +
					"check needs. That is normal for a few seconds after a restart.",
				Causes: []string{
					"the server is still starting up",
					"the cluster is running an older miren than your CLI",
				},
				Actions: []ui.Action{
					{Command: "miren version", Note: "compare CLI and cluster versions"},
				},
				Cause: env.connErr,
			},
		}

	case rpc.ResolveHTTPError, rpc.ResolveDecodeError:
		// Unclassified transport failure, or a response we couldn't parse.
		// Neither gives us anything to say beyond the underlying error, which
		// the fallthrough below reports verbatim.

	case rpc.ResolveStatusError:
		if re.StatusCode == 401 {
			return checkResult{
				Status:  checkFail,
				Summary: "reachable, but access denied",
				Problem: &ui.Diagnostic{
					Summary: fmt.Sprintf("%s refused your credentials", addr),
					Detail: "The server is healthy and reachable. It just won't accept who " +
						"you're signed in as.",
					Actions: []ui.Action{
						{Command: "miren login", Note: "sign in again"},
						{Command: "miren whoami", Note: "check your current identity"},
					},
					Cause: env.connErr,
				},
			}
		}
	}

	return checkResult{
		Status:  checkFail,
		Summary: "not connected",
		Problem: &ui.Diagnostic{
			Summary: fmt.Sprintf("couldn't connect to %s", addr),
			Detail:  re.Error(),
			Actions: fallbackActions(env),
			Cause:   env.connErr,
		},
	}
}

// fallbackActions is the advice of last resort, for failures we couldn't
// classify. It exists so the invariant holds everywhere: a check that reports a
// problem always names something the reader can do about it, even when all we
// can honestly say is "here's how to look closer".
func fallbackActions(env *doctorEnv) []ui.Action {
	actions := []ui.Action{
		{Command: "miren cluster list", Note: "confirm the address"},
	}
	return append(actions, serverLogActions(env)...)
}

// unreachableVerdict is the cross product that makes the probes worth running.
// Nothing answered on the API port, and TCP and UDP together say why.
func unreachableVerdict(env *doctorEnv, addr string) checkResult {
	switch {
	// The host is up and actively refusing on the API port: nothing is
	// listening there. This is the overwhelmingly common local failure, and the
	// one the old code misreported as "the server may be starting up".
	case env.tcp.Outcome == probeRefused:
		return checkResult{
			Status:  checkFail,
			Summary: "not running",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("nothing is running on %s", addr),
				Detail: "The host is reachable and refused the connection, which means the " +
					"machine is fine and the miren server isn't running on it.",
				Actions: startServerActions(env),
				Cause:   env.connErr,
			},
		}

	// TCP gets through on the ingress port but the API's UDP port is silent.
	// The host is reachable; the API specifically is not.
	//
	// Note what this does and doesn't prove. TCP answering shows the machine is
	// up and something is serving, but it says nothing about the miren server
	// itself, since the ingress is a different listener. So this lists both
	// explanations rather than declaring a firewall, which is what the old code
	// did on much weaker evidence.
	case env.tcp.Outcome == probeOpen && env.udp.Outcome == probeSilent:
		return checkResult{
			Status:  checkFail,
			Summary: "API port not answering",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("%s is reachable but its API port isn't answering", addr),
				Detail: "The host answered on TCP, so the machine is up and reachable from " +
					"here. The API port stayed silent. miren speaks QUIC, which is " +
					"UDP, so a firewall allowing only TCP blocks every command while " +
					"still looking healthy to most tools.",
				Causes: []string{
					"a firewall is allowing TCP but dropping UDP",
					"the miren server isn't running on that port",
				},
				Actions: firewallActions(env),
				Cause:   env.connErr,
			},
		}

	// Nothing on either protocol: we can't reach the host at all.
	case env.tcp.Outcome == probeSilent:
		return checkResult{
			Status:  checkFail,
			Summary: "unreachable",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("couldn't reach %s at all", addr),
				Detail: "Neither TCP nor UDP got a response. The address resolved, so the " +
					"host is either down, behind something that drops the traffic, or " +
					"not where the config says it is.",
				Causes: []string{
					"the host is down or unreachable from here",
					"a network or firewall rule is dropping the traffic",
					"the cluster's address has changed",
				},
				Actions: []ui.Action{
					{Command: "miren cluster list", Note: "check the configured address"},
				},
				Cause: env.connErr,
			},
		}

	// The probes didn't agree on a story we can name: TCP got somewhere but
	// UDP failed for a reason other than silence, or a probe couldn't run at
	// all. Report what we saw rather than pick one of the confident verdicts
	// above and be wrong about it.
	default:
		return checkResult{
			Status:  checkFail,
			Summary: "not connected",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("couldn't connect to %s", addr),
				Detail: fmt.Sprintf("The API port didn't answer. Probing the host separately gave "+
					"TCP: %s, UDP: %s, which isn't a combination we can draw a firm "+
					"conclusion from.", env.tcp.Detail, env.udp.Detail),
				Actions: fallbackActions(env),
				Cause:   env.connErr,
			},
		}
	}
}

// startServerActions gives advice that works on the machine the user is
// actually on. The old doctor printed "sudo systemctl start miren"
// unconditionally, including on macOS, where `server install` explicitly
// refuses to run and directs people to the container instead.
func startServerActions(env *doctorEnv) []ui.Action {
	if !env.local() {
		return []ui.Action{
			{Command: "miren cluster list", Note: "confirm the address"},
		}
	}

	if runtime.GOOS == "linux" {
		return []ui.Action{
			{Command: "sudo systemctl start miren", Note: "start the server"},
			{Command: "sudo journalctl -u miren -f", Note: "watch its logs"},
		}
	}

	return []ui.Action{
		{Command: "miren server container install", Note: "run the server in Docker or Podman"},
	}
}

// serverLogActions points at the server's own logs. For a local Linux host we
// know exactly how; everywhere else we can still say where to look, which beats
// returning nothing and leaving a verdict with no next step.
func serverLogActions(env *doctorEnv) []ui.Action {
	if env.local() && runtime.GOOS == "linux" {
		return []ui.Action{
			{Command: "sudo systemctl status miren", Note: "check the service"},
			{Command: "sudo journalctl -u miren -n 100", Note: "read recent logs"},
		}
	}
	if env.local() {
		return []ui.Action{
			{Command: "miren server container status", Note: "check the server container"},
		}
	}
	return []ui.Action{
		{Command: "miren cluster list", Note: "confirm the address"},
	}
}

// firewallActions only suggests a firewall command when the firewall in
// question is on this machine and we know which tool manages it.
func firewallActions(env *doctorEnv) []ui.Action {
	port := apiPortOf(env.cluster.Hostname)

	if env.local() && runtime.GOOS == "linux" {
		return []ui.Action{
			{Command: fmt.Sprintf("sudo ufw allow %s/udp", port), Note: "if this host uses ufw"},
			{Command: fmt.Sprintf("sudo firewall-cmd --add-port=%s/udp", port), Note: "if it uses firewalld"},
		}
	}

	// Not our host, so we can't know what manages its firewall. Point at the
	// probe rather than inventing a command: Action.Command is meant to be
	// runnable, and prose dressed up as a command is worse than a plain note.
	return []ui.Action{
		{Command: fmt.Sprintf("nc -zu %s %s", hostOf(env.cluster.Hostname), port), Note: "check UDP reachability yourself"},
	}
}

// hostOf strips any port from a cluster address.
func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func apiPortOf(addr string) string {
	if p := portOf(withDefaultAPIPort(addr)); p != "" {
		return p
	}
	return doctorAPIPort
}
