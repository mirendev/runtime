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

// unknownClusterError reports that a cluster was named but couldn't be
// resolved at all. -C accepts either a configured cluster name or an ad-hoc
// address, so this covers a typo in either form.
func (c *Context) unknownClusterError() error {
	named := c.requestedCluster
	if named == "" {
		// No -C, so the broken name came from the config's active cluster.
		return &ui.Diagnostic{
			Summary: "the active cluster couldn't be loaded",
			Detail: "Your configuration selects a cluster that can't be resolved. Nothing " +
				"was contacted, because connecting to a different cluster would " +
				"answer with the wrong cluster's data.",
			Actions: []ui.Action{
				{Command: "miren cluster list", Note: "see configured clusters"},
				{Command: "miren cluster switch <name>", Note: "pick a different one"},
			},
			Cause:     c.clusterErr,
			ShowCause: true,
		}
	}

	return &ui.Diagnostic{
		Summary: fmt.Sprintf("no cluster named %q", named),
		Detail: fmt.Sprintf("%q didn't match a configured cluster, and it isn't a reachable "+
			"address either. Nothing was contacted, because falling back to your "+
			"active cluster would answer with the wrong cluster's data.", named),
		Actions: []ui.Action{
			{Command: "miren cluster list", Note: "see configured clusters"},
		},
		Cause:     c.clusterErr,
		ShowCause: true,
	}
}

// unusableClusterError reports that the selected cluster's configuration can't
// be turned into a connection.
//
// Naming the file it came from matters here: these entries are spread across
// clientconfig.yaml and clientconfig.d/*.yaml, and "which file is this cluster
// even defined in" is the first thing you need in order to fix it.
func (c *Context) unusableClusterError(err error) error {
	d := &ui.Diagnostic{
		Summary: fmt.Sprintf("can't connect using the configuration for %s", c.clusterLabel()),
		Detail: "The cluster is configured, but its entry couldn't be turned into a " +
			"connection. Nothing was contacted, because falling back to a " +
			"different cluster would answer your question with the wrong " +
			"cluster's data.",
		Actions: []ui.Action{
			{Command: "miren cluster list", Note: "see how it's configured"},
			{Command: "miren login", Note: "if its credentials have expired"},
		},
		Cause:     err,
		ShowCause: true,
	}

	if c.ClientConfig != nil && c.ClusterName != "" {
		if src := c.ClientConfig.GetClusterSource(c.ClusterName); src != "" {
			d.Facts = []ui.Fact{{Label: "Defined in", Value: src}}
		}
	}

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
		// A CI token that verified but matched no binding is not an expired
		// session. Sending someone to 'miren login' when their credentials are
		// fine is the single most expensive wrong turn this error can cause,
		// which is why the server goes out of its way to name this case.
		if re.StatusCode == 401 && re.Code == rpc.AuthErrorOIDCBindingMismatch {
			return &ui.Diagnostic{
				Summary: fmt.Sprintf("no CI binding on %s accepts this token", cluster),
				Detail: "The token is valid and correctly signed, it just doesn't match any " +
					"binding configured on this cluster. Signing in again won't help, " +
					"because the credentials aren't the problem.",
				Facts: []ui.Fact{{Label: "Cluster reported", Value: re.Detail}},
				Causes: []string{
					"the binding names a different repository",
					"the repository was created, renamed, or transferred after 2026-07-15, and the binding still matches on the old subject format",
					"the workflow's event or ref isn't allowed by the binding",
				},
				Actions: []ui.Action{
					{Command: "miren auth ci list -a <app>", Note: "see the configured bindings"},
					{Command: "miren auth ci add --github OWNER/REPO -a <app>", Note: "replace a stale one"},
				},
				Cause: fmt.Errorf("%w: %w", ErrAccessDenied, re),
			}
		}

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
