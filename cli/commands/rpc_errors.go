package commands

import (
	"errors"
	"fmt"
	"net"
	"time"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// wrapRPCError turns a structured rpc.ResolveError into a human-facing
// diagnostic.
//
// pkg/rpc deliberately stops at classifying the failure and recording the facts
// (which capability, which address, how long we waited). The prose lives here
// because this is the layer that knows the cluster's name and the command the
// user actually typed — "miren route list" is a far better thing to put in an
// error than the capability "entities" it happens to resolve.
func (c *Context) wrapRPCError(err error) error {
	var re *rpc.ResolveError
	if !errors.As(err, &re) {
		return err
	}

	d := c.diagnoseResolve(re)
	if d == nil {
		return err
	}
	d.ShowCause = c.Verbose()
	return d
}

// clusterLabel names the cluster the way the user refers to it.
func (c *Context) clusterLabel() string {
	if c.ClusterName == "" {
		return "the cluster"
	}
	return fmt.Sprintf("cluster %q", c.ClusterName)
}

// commandLabel names what the user typed, falling back to the capability when
// the command name isn't available (library callers, tests).
func (c *Context) commandLabel(capability string) string {
	if c.CommandName == "" {
		return fmt.Sprintf("%q", capability)
	}
	return fmt.Sprintf("%q", "miren "+c.CommandName)
}

func (c *Context) diagnoseResolve(re *rpc.ResolveError) *ui.Diagnostic {
	cluster := c.clusterLabel()
	waited := re.Elapsed.Round(time.Second)

	switch re.Kind {
	case rpc.ResolveUnreachableError:
		causes := []string{"the server isn't running"}
		if port := portOf(re.Remote); port != "" {
			causes = append(causes, fmt.Sprintf("a firewall is blocking UDP port %s", port))
		} else {
			causes = append(causes, "a firewall is blocking the connection")
		}
		causes = append(causes, "the cluster's address has changed")

		return &ui.Diagnostic{
			Summary: fmt.Sprintf("couldn't reach %s at %s", cluster, re.Remote),
			Detail: fmt.Sprintf("Nothing answered after %s. The address resolved, so either "+
				"the server isn't running or something between here and there is "+
				"dropping the connection.", waited),
			Causes: causes,
			Actions: []ui.Action{
				{Command: "miren doctor", Note: "check what's reachable"},
				{Command: "miren cluster list", Note: "confirm the address"},
			},
			Cause: re,
		}

	case rpc.ResolveWentSilentError:
		return &ui.Diagnostic{
			Summary: fmt.Sprintf("lost the connection to %s at %s", cluster, re.Remote),
			Detail: fmt.Sprintf("Connected successfully, then it went quiet for %s partway "+
				"through the request. The server was reachable a moment earlier.", waited),
			Causes: []string{
				"the server crashed or restarted mid-request",
				"the server is overloaded and stopped responding",
				"the network path dropped",
			},
			Actions: []ui.Action{
				{Command: "miren doctor", Note: "check whether it's back"},
			},
			Cause: re,
		}

	case rpc.ResolveNoAnswerError:
		// Deliberately hedged. Our lookup deadline is shorter than the
		// transport's idle timeout, so when it fires we know the request was
		// accepted and nothing came back, but we cannot yet tell an
		// application-level hang from a server that froze outright — the
		// transport hasn't given up on the connection. Claiming the server is
		// "running and reachable" here would be wrong for a frozen server.
		// Disambiguating is doctor's job, which is why it leads the actions.
		return &ui.Diagnostic{
			Summary: fmt.Sprintf("%s never answered", cluster),
			Detail: fmt.Sprintf("Waited %s after %s accepted the request and nothing came back. "+
				"The connection was still open when we gave up, so the request "+
				"reached the server, but no reply followed.", waited, re.Remote),
			Causes: []string{
				"the server is still starting up",
				"the server is wedged or overloaded",
				"the server stopped responding after accepting the connection",
			},
			Actions: []ui.Action{
				{Command: "miren doctor", Note: "check whether it's still responding"},
				{Command: "miren version", Note: "compare CLI and cluster versions"},
			},
			Cause: re,
		}

	case rpc.ResolveLookupError:
		// "Currently" is load-bearing. A server that is still booting answers
		// this lookup saying it doesn't have the capability, because it hasn't
		// registered it yet — observed by running a command four seconds into
		// a server start. Blaming version skew outright would be a confident
		// wrong answer in what is probably the most common case of all.
		return &ui.Diagnostic{
			Summary: fmt.Sprintf("%s doesn't provide what %s needs", cluster, c.commandLabel(re.Name)),
			Detail: fmt.Sprintf("The cluster answered, but it isn't currently offering %q, "+
				"which this command needs.", re.Name),
			Causes: []string{
				"the server is still starting up and hasn't registered it yet",
				"the cluster is running an older miren than your CLI",
			},
			Actions: []ui.Action{
				{Command: "miren doctor", Note: "check whether the server is ready"},
				{Command: "miren version", Note: "compare CLI and cluster versions"},
			},
			Cause: re,
		}

	case rpc.ResolveStatusError:
		if re.StatusCode == 401 {
			return &ui.Diagnostic{
				Summary: fmt.Sprintf("access denied on %s", cluster),
				Detail: "You don't have permission to access this cluster. If you were " +
					"signed in previously, your session may have expired.",
				Actions: []ui.Action{
					{Command: "miren login", Note: "sign in again"},
					{Command: "miren whoami", Note: "check your current identity"},
				},
				// Both are reachable via errors.Is, so callers matching the
				// sentinel keep working while -v still shows the RPC detail.
				Cause: fmt.Errorf("%w: %w", ErrAccessDenied, re),
			}
		}

		return &ui.Diagnostic{
			Summary: fmt.Sprintf("%s returned an unexpected response (HTTP %d)", cluster, re.StatusCode),
			Detail: fmt.Sprintf("Asked %s for %q and got HTTP %d instead of a capability. That "+
				"usually means the address points at something that isn't a miren "+
				"server, or at a version that doesn't serve this endpoint.",
				re.Remote, re.Name, re.StatusCode),
			Actions: []ui.Action{
				{Command: "miren cluster list", Note: "confirm the address"},
				{Command: "miren version", Note: "compare CLI and cluster versions"},
			},
			Cause: re,
		}

	case rpc.ResolveDecodeError:
		return &ui.Diagnostic{
			Summary: fmt.Sprintf("couldn't understand the response from %s", cluster),
			Detail: fmt.Sprintf("%s answered the request but the response wasn't in the "+
				"expected format. The address may point at something that isn't a "+
				"miren server.", re.Remote),
			Actions: []ui.Action{
				{Command: "miren cluster list", Note: "confirm the address"},
			},
			Cause: re,
		}

	case rpc.ResolveHTTPError:
		// Unclassified transport failure. The underlying error is usually
		// specific enough to be worth showing as-is.
		return nil
	}

	return nil
}

// portOf extracts the port from a host:port address, returning "" when the
// address has no port to name.
func portOf(remote string) string {
	_, port, err := net.SplitHostPort(remote)
	if err != nil {
		return ""
	}
	return port
}
