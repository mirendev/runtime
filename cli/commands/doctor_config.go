package commands

import (
	"errors"
	"fmt"
	"net"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// isLocalCluster checks if a cluster hostname refers to the local machine
func isLocalCluster(hostname string) bool {
	host := hostname
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		host = h
	}
	return isLocalAddress(host)
}

// checkConfiguration reports whether we know which cluster to talk to.
//
// It deliberately doesn't render the cluster inventory: `miren cluster list`
// already does that, and doctor's job is to answer "is anything wrong", not to
// be a second, slightly different table.
func checkConfiguration(env *doctorEnv) checkResult {
	if env.configErr != nil && !errors.Is(env.configErr, clientconfig.ErrNoConfig) {
		return checkResult{
			Status:  checkFail,
			Summary: "unreadable",
			Problem: &ui.Diagnostic{
				Summary: "couldn't read your miren configuration",
				Detail:  env.configErr.Error(),
				Actions: []ui.Action{
					{Command: "miren cluster list", Note: "see what's configured"},
				},
				Cause: env.configErr,
			},
		}
	}

	if env.cfg == nil || env.clusterCount == 0 {
		return checkResult{
			Status:  checkFail,
			Summary: "no clusters configured",
			Problem: &ui.Diagnostic{
				Summary: "no clusters are configured",
				Detail:  "miren doesn't know which cluster to talk to yet.",
				Actions: []ui.Action{
					{Command: "miren login", Note: "authenticate with miren.cloud"},
					{Command: "miren cluster add", Note: "add a cluster manually"},
				},
			},
		}
	}

	// A cluster that was named but wouldn't load is a different problem from
	// not having picked one, and saying the latter would bury the reason.
	if env.cluster == nil && env.clusterErr != nil {
		named := env.requestedCluster
		if named == "" {
			named = "the active cluster"
		} else {
			named = fmt.Sprintf("%q", named)
		}

		return checkResult{
			Status:  checkFail,
			Summary: "selected cluster couldn't be loaded",
			Problem: &ui.Diagnostic{
				Summary: fmt.Sprintf("couldn't load %s", named),
				Detail: "The cluster is named but can't be resolved, so nothing else could " +
					"be checked against it.",
				Actions: []ui.Action{
					{Command: "miren cluster list", Note: "see configured clusters"},
					{Command: "miren cluster switch <name>", Note: "pick a different one"},
				},
				Cause:     env.clusterErr,
				ShowCause: true,
			},
		}
	}

	if env.cluster == nil {
		return checkResult{
			Status:  checkFail,
			Summary: fmt.Sprintf("%s configured, none selected", pluralClusters(env.clusterCount)),
			Problem: &ui.Diagnostic{
				Summary: "no cluster is selected",
				Detail:  "Clusters are configured, but none of them is active.",
				Actions: []ui.Action{
					{Command: "miren cluster switch <name>", Note: "pick one"},
					{Command: "miren cluster list", Note: "see what's available"},
				},
			},
		}
	}

	summary := fmt.Sprintf("%s, active: %s", pluralClusters(env.clusterCount), env.clusterName)
	return checkResult{Status: checkOK, Summary: summary}
}

func pluralClusters(n int) string {
	if n == 1 {
		return "1 cluster"
	}
	return fmt.Sprintf("%d clusters", n)
}
