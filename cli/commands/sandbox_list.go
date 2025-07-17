package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

func SandboxList(ctx *Context, opts struct {
	Status string `short:"s" long:"status" description:"Filter by status (pending, not_ready, running, stopped, dead)"`
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Get the sandbox kind attribute
	kindRes, err := eac.LookupKind(ctx, "sandbox")
	if err != nil {
		return err
	}

	// List all sandboxes
	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	if len(res.Values()) == 0 {
		ctx.Printf("No sandboxes found\n")
		return nil
	}

	// Create a tabwriter for formatted output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "ID\tSTATUS\tVERSION\tCONTAINERS\n")

	for _, e := range res.Values() {
		// Decode the sandbox entity
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(e.Entity())

		// Get status string
		status := string(sandbox.Status)
		if status == "" {
			status = "unknown"
		}

		// Filter by status if specified
		if opts.Status != "" && status != "status."+opts.Status {
			continue
		}

		// Get version string
		version := sandbox.Version.String()
		if version == "" {
			version = "-"
		}

		// Count containers
		containerCount := len(sandbox.Container)

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
			sandbox.ID.String(),
			status,
			version,
			containerCount,
		)
	}

	return w.Flush()
}
