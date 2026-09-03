package commands

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// noDirectAddressNote describes a cluster advertising no address when nobody
// has asked cloud whether it can reach it. Deliberately not
// unreachableAddressNote: that one is a verdict, and without --check no verdict
// has been reached.
const noDirectAddressNote = "no direct address"

// availableCluster is what one cluster in Miren Cloud looks like to a script.
// Deliberately a shape of its own rather than ClusterResponse serialized
// directly, so the wire format cloud happens to use is not the CLI's contract.
type availableCluster struct {
	Name              string   `json:"name"`
	XID               string   `json:"xid"`
	Organization      string   `json:"organization"`
	OrganizationXID   string   `json:"organization_xid"`
	Description       string   `json:"description,omitempty"`
	APIAddresses      []string `json:"api_addresses,omitempty"`
	CACertFingerprint string   `json:"ca_cert_fingerprint,omitempty"`

	// Reachable reports that the cluster advertises at least one address to
	// dial. Known without asking cloud anything.
	Reachable bool `json:"reachable"`

	// ViaCloud reports whether cloud holds a link to a cluster that advertises
	// no address. Absent unless --check asked, because false and unasked are
	// different answers and a caller cannot tell them apart from a bare false.
	ViaCloud *bool `json:"via_cloud,omitempty"`

	// Added reports that this cluster is already in the local config, matched
	// by its id in cloud. A cluster added by --address carries no id, so it
	// cannot be matched this way and reads as not added.
	Added bool `json:"added"`

	// LocalName is the name it was added under, which is not necessarily its
	// name in cloud.
	LocalName string `json:"local_name,omitempty"`
}

type clusterAvailableOpts struct {
	FormatOptions
	Identity     string `short:"i" long:"identity" description:"Name of the identity to use (optional - will use the only one if single)"`
	Organization string `long:"organization" description:"Only list clusters in this organization"`
	Check        bool   `long:"check" description:"Ask cloud whether it can reach the clusters that advertise no address"`
}

// ClusterAvailable lists the clusters Miren Cloud has for this account, which
// is where the name `miren cluster add --cluster` wants comes from.
func ClusterAvailable(ctx *Context, opts clusterAvailableOpts) error {
	// Adopting an identity announces itself, and asking cloud about an
	// unreachable cluster can warn. Both are progress, and in JSON mode stdout
	// belongs to the document alone.
	if opts.IsJSON() {
		defer ctx.ProgressToStderr()()
	}

	config, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	identityName, err := pickCloudIdentity(ctx, config, opts.Identity, "")
	if err != nil {
		return err
	}

	identity, err := lookupIdentity(config, identityName)
	if err != nil {
		return err
	}

	clusters, err := fetchAvailableClusters(ctx, config, identityName, identity)
	if err != nil {
		return fmt.Errorf("failed to fetch available clusters: %w", err)
	}

	clusters, err = clustersInOrganization(clusters, opts.Organization)
	if err != nil {
		return err
	}

	// Only asked for when requested: it costs a round trip per address-less
	// cluster, and a listing should not make somebody wait for an answer they
	// did not ask for.
	var cloudRoutable map[string]bool
	if opts.Check {
		cloudRoutable = cloudRoutableClusters(ctx, config, identityName, identity, clusters)
	}

	added := addedClustersByXID(config)

	available := make([]availableCluster, 0, len(clusters))
	for _, cluster := range clusters {
		entry := availableCluster{
			Name:              cluster.Name,
			XID:               cluster.XID,
			Organization:      cluster.OrganizationName,
			OrganizationXID:   cluster.OrganizationXID,
			Description:       cluster.Description,
			APIAddresses:      cluster.APIAddresses,
			CACertFingerprint: cluster.CACertFingerprint,
			Reachable:         cluster.HasReachableAddress(),
		}

		if opts.Check && !cluster.HasReachableAddress() {
			routable := cloudRoutable[cluster.XID]
			entry.ViaCloud = &routable
		}

		if localName, ok := added[cluster.XID]; ok && cluster.XID != "" {
			entry.Added = true
			entry.LocalName = localName
		}

		available = append(available, entry)
	}

	// Cloud returns them in whatever order it likes. Sorting makes two runs
	// comparable, which matters more here than anywhere else: this output is
	// meant to be read by scripts.
	slices.SortFunc(available, func(a, b availableCluster) int {
		if byOrg := cmp.Compare(a.Organization, b.Organization); byOrg != 0 {
			return byOrg
		}
		return cmp.Compare(a.Name, b.Name)
	})

	if opts.IsJSON() {
		return PrintJSON(available)
	}

	if len(available) == 0 {
		ctx.Printf("No clusters available for your account\n")
		return nil
	}

	return renderAvailableClusters(ctx, available, opts.Check)
}

// addedClustersByXID maps each locally configured cluster's id in cloud to the
// name it was added under. Entries without an id — added by address rather than
// discovered — cannot be matched to a cloud cluster and are left out.
func addedClustersByXID(config *clientconfig.Config) map[string]string {
	byXID := make(map[string]string)
	if config == nil {
		return byXID
	}

	_ = config.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
		if cluster.XID != "" {
			byXID[cluster.XID] = name
		}
		return nil
	})

	return byXID
}

func renderAvailableClusters(ctx *Context, clusters []availableCluster, checked bool) error {
	muted := lipgloss.NewStyle().Foreground(theme.Muted)

	headers := []string{"NAME", "ORGANIZATION", "ADDRESS", "ADDED"}
	rows := make([]ui.Row, 0, len(clusters))

	var unchecked int
	for _, cluster := range clusters {
		var address string
		switch {
		case cluster.Reachable:
			addresses := sortAddresses(cluster.APIAddresses)
			address = formatAddressWithGrayPort(addresses[0])
			if len(addresses) > 1 {
				address = fmt.Sprintf("%s (+%d)", address, len(addresses)-1)
			}
		case cluster.ViaCloud == nil:
			unchecked++
			address = muted.Render(noDirectAddressNote)
		case *cluster.ViaCloud:
			address = muted.Render(viaCloudNote)
		default:
			address = muted.Render(unreachableAddressNote)
		}

		addedNote := muted.Render("-")
		if cluster.Added {
			addedNote = "yes"
			if cluster.LocalName != cluster.Name {
				addedNote = fmt.Sprintf("yes (as %s)", cluster.LocalName)
			}
		}

		rows = append(rows, ui.Row{cluster.Name, cluster.Organization, address, addedNote})
	}

	table := ui.NewTable(
		ui.WithColumns(ui.AutoSizeColumns(headers, rows, nil)),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())

	if unchecked > 0 && !checked {
		ctx.Printf("\n%d cluster(s) advertise no address. Re-run with --check to ask cloud whether it can reach them.\n", unchecked)
	}

	ctx.Printf("\nAdd one with: miren cluster add --cluster <name>\n")
	return nil
}
