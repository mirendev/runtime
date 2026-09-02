package commands

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// skipAddresses contains addresses that should be skipped when trying to connect
var skipAddresses = map[string]bool{
	"0.0.0.0":               true,
	"127.0.0.1":             true,
	"::1":                   true,
	"localhost":             true,
	"localhost.localdomain": true,
}

type clusterAddOpts struct {
	FormatOptions
	Identity     string `short:"i" long:"identity" description:"Name of the identity to use (optional - will use the only one if single)"`
	Cluster      string `short:"c" long:"cluster" description:"Name of the cluster to add, looked up in Miren Cloud unless --address is given (optional - will list available)"`
	Address      string `short:"a" long:"address" description:"Address/hostname of the cluster (optional - will use from selected cluster)"`
	As           string `long:"as" description:"Local name to store the cluster under, when it should differ from its name in Miren Cloud"`
	Organization string `long:"organization" description:"Organization the named cluster belongs to, for when the same name exists in more than one"`
	Force        bool   `short:"f" long:"force" description:"Overwrite existing cluster configuration"`
	ViaCloud     bool   `long:"via-cloud" description:"Reach the cluster through Miren Cloud instead of dialing it, for a cluster this machine has no route to"`
}

func ClusterAdd(ctx *Context, opts clusterAddOpts) error {
	// Adding a cluster narrates itself as it goes, and in JSON mode stdout
	// belongs to the result document alone.
	if opts.IsJSON() {
		defer ctx.ProgressToStderr()()
	}

	added, err := addCluster(ctx, addClusterOptions{
		identityName: opts.Identity,
		clusterName:  opts.Cluster,
		address:      opts.Address,
		localName:    opts.As,
		organization: opts.Organization,
		force:        opts.Force,
		viaCloud:     opts.ViaCloud,
		jsonOutput:   opts.IsJSON(),
	})

	if !opts.IsJSON() {
		return err
	}

	return reportClusterAdd(ctx, added, err)
}

// AddClusterInteractive prompts the user to select and add a cluster interactively.
// It auto-selects the identity if only one is available.
// Returns nil if a cluster was successfully added.
func AddClusterInteractive(ctx *Context) error {
	_, err := addCluster(ctx, addClusterOptions{})
	return err
}

type addClusterOptions struct {
	identityName string
	clusterName  string
	address      string
	force        bool

	// localName overrides the name the cluster is stored under locally. The
	// picker asks for one; naming a cluster on the command line skips that
	// prompt, so this is how the same choice is made without it.
	localName string

	// organization narrows the cloud lookup when clusterName exists in more
	// than one of them.
	organization string

	// viaCloud writes an entry that reaches the cluster through Miren Cloud.
	// It only makes sense in discovery mode: routing through cloud needs the
	// cluster's XID, and asking cloud which clusters you have is the only way
	// to learn it.
	viaCloud bool

	// jsonOutput means the caller wants a document, not a conversation. The
	// steps that would put a question on the screen — the cluster picker, the
	// overwrite prompt — become errors it can act on instead, even when there
	// is a terminal attached and asking would have worked.
	jsonOutput bool
}

// canFallBackToCloud reports whether a cluster that would not answer a direct
// dial can be reached through cloud instead.
//
// The question is answered by trying it, not by asking cloud whether the
// cluster is online — see cloudRouteWorks for why those are different claims.
// A failure to reach it is not distinguished from an answer of no, because both
// produce the same decision: leave the user with the direct-connection error
// they were going to get anyway, rather than a confusing one about routing.
func canFallBackToCloud(
	ctx *Context,
	config *clientconfig.Config,
	identityName string,
	identity *clientconfig.IdentityConfig,
	cluster *ClusterResponse,
) bool {
	return cloudRouteProbe(ctx, config, identityName, identity, cluster)
}

// announceClusterAdd says which cluster is being added, under what local name,
// and how it will be reached. Shared so the three ways of getting here cannot
// describe themselves differently, and so a path that ends up routed cannot
// report an address it is not going to use.
func announceClusterAdd(ctx *Context, remoteName, localName, how string) {
	if localName != remoteName {
		ctx.Info("Adding cluster '%s' as '%s' (%s)", remoteName, localName, how)
		return
	}
	ctx.Info("Adding cluster '%s' (%s)", remoteName, how)
}

// findClusterByName picks the cluster the caller named out of the list cloud
// returned, standing in for the interactive picker when the name is already
// known.
//
// Cluster names are unique within an organization but not across them, so an
// ambiguous name is refused rather than guessed at: binding the wrong
// production cluster to the right local name is a worse outcome than being
// asked for --organization. Matching is exact first and case-insensitive only
// as a fallback, so a name that is exactly right can never lose to one that
// merely differs in case.
func findClusterByName(clusters []ClusterResponse, name, organization string) (*ClusterResponse, error) {
	candidates, err := clustersInOrganization(clusters, organization)
	if err != nil {
		return nil, err
	}

	matches := matchClusterName(candidates, func(clusterName string) bool { return clusterName == name })
	if len(matches) == 0 {
		matches = matchClusterName(candidates, func(clusterName string) bool { return strings.EqualFold(clusterName, name) })
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, codedErrorf(codeClusterNotFound, "no cluster named %q. Available clusters: %s",
			name, describeClusters(candidates))
	default:
		return nil, codedErrorf(codeAmbiguousCluster, "%d clusters are named %q (%s). Pick one with --organization",
			len(matches), name, describeClusters(derefClusters(matches)))
	}
}

// clustersInOrganization narrows a cluster list to one organization, named
// either by its display name or by its id. An organization that matches nothing
// is an error rather than an empty list, because the overwhelmingly likely
// cause is a misspelled name, and an empty list reads as "you have no clusters
// there" — a different and more alarming claim.
//
// Shared by the commands that take --organization so the flag means the same
// thing whether it is narrowing a lookup or a listing.
func clustersInOrganization(clusters []ClusterResponse, organization string) ([]ClusterResponse, error) {
	if organization == "" {
		return clusters, nil
	}

	var matched []ClusterResponse
	for _, cluster := range clusters {
		if strings.EqualFold(cluster.OrganizationName, organization) || cluster.OrganizationXID == organization {
			matched = append(matched, cluster)
		}
	}

	if len(matched) == 0 {
		return nil, codedErrorf(codeUnknownOrganization, "no clusters in organization %q. Your clusters are in: %s",
			organization, organizationNames(clusters))
	}

	return matched, nil
}

func matchClusterName(clusters []ClusterResponse, match func(name string) bool) []*ClusterResponse {
	var matches []*ClusterResponse
	for i := range clusters {
		if match(clusters[i].Name) {
			matches = append(matches, &clusters[i])
		}
	}
	return matches
}

func derefClusters(clusters []*ClusterResponse) []ClusterResponse {
	out := make([]ClusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, *cluster)
	}
	return out
}

// describeClusters renders clusters as "name (organization)" so an error about
// the wrong name is actionable without running a second command.
func describeClusters(clusters []ClusterResponse) string {
	described := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		described = append(described, fmt.Sprintf("%s (%s)", cluster.Name, cluster.OrganizationName))
	}
	slices.Sort(described)
	return strings.Join(described, ", ")
}

// organizationNames lists the organizations the given clusters belong to, once
// each, for the error about an organization that matched nothing.
func organizationNames(clusters []ClusterResponse) string {
	var names []string
	for _, cluster := range clusters {
		if !slices.Contains(names, cluster.OrganizationName) {
			names = append(names, cluster.OrganizationName)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func addCluster(ctx *Context, opts addClusterOptions) (*addedCluster, error) {
	identityName, clusterName, address, force := opts.identityName, opts.clusterName, opts.address, opts.force

	if opts.viaCloud && address != "" {
		return nil, codedErrorf(codeInvalidFlags, "--via-cloud and --address are mutually exclusive: routing through cloud is for a cluster you have no address for")
	}
	if opts.localName != "" && address != "" {
		return nil, codedErrorf(codeInvalidFlags, "--as and --address are mutually exclusive: with --address nothing is looked up in cloud, so --cluster is already the local name")
	}
	if opts.organization != "" && address != "" {
		return nil, codedErrorf(codeInvalidFlags, "--organization and --address are mutually exclusive: with --address nothing is looked up in cloud")
	}
	if opts.localName != "" && clusterName == "" {
		return nil, codedErrorf(codeInvalidFlags, "--as needs --cluster to say which cluster to rename; the picker asks for a local name itself")
	}
	// Silently ignoring it would be worse: it reads as a filter that was
	// applied, and the picker shows every cluster either way.
	if opts.organization != "" && clusterName == "" {
		return nil, codedErrorf(codeInvalidFlags, "--organization only narrows the lookup for --cluster; the picker already shows which organization each cluster is in")
	}
	// Checked here with the other flag combinations rather than where the
	// address is used: an address with nothing to call it is wrong on its face,
	// and leaving it until after identity resolution meant reporting whatever
	// that found instead — "multiple identities available" for a command whose
	// real problem was a missing --cluster.
	if address != "" && clusterName == "" {
		return nil, codedErrorf(codeInvalidFlags, "--address needs --cluster to say what to call the cluster locally; drop --address to look one up in Miren Cloud instead")
	}
	// The picker cannot answer to a caller that wanted a document, so say what
	// to do instead of opening a list nobody is watching.
	if opts.jsonOutput && clusterName == "" && address == "" {
		return nil, codedErrorf(codeInteractiveRequired,
			"choosing a cluster from a list needs a person; name one with --cluster (run 'miren cluster available' to see them) or give --cluster and --address")
	}
	// Load the main config to check if the identity exists
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return nil, codedErrorf(codeConfigLoadFailed, "failed to load configuration: %w", err)
	}

	// Detect manual mode: an address was given, so the cluster is dialed
	// directly and cloud is never asked anything. Naming a cluster without an
	// address is a cloud lookup like the picker, and needs an identity for the
	// same reason.
	manualMode := address != ""

	// In manual mode, identity is optional (a CI runner authenticates with an
	// OIDC token from its environment and has none configured).
	// In discovery mode, identity is required to fetch available clusters.
	if !manualMode {
		// Discovery mode — identity is required
		identityName, err = pickCloudIdentity(ctx, mainConfig, identityName,
			", or use --cluster and --address to add a cluster directly")
		if err != nil {
			return nil, err
		}
	} else if identityName != "" {
		// Manual mode with explicit --identity: validate it exists
		if mainConfig == nil || !mainConfig.HasIdentities() {
			return nil, codedErrorf(codeIdentityNotFound, "identity %q not found: no identities configured", identityName)
		}
	} else if mainConfig != nil {
		// Manual mode with no --identity. Optional isn't the same as unwanted:
		// someone who has run `miren login` has an identity sitting right there,
		// and leaving it off produces a cluster entry with no way to authenticate
		// at all — which used to surface much later, and much less helpfully, as a
		// certificate parse error on the first command that needed the cluster.
		// Discovery mode already adopts a lone identity; match it.
		switch names := mainConfig.GetIdentityNames(); len(names) {
		case 0:
			// Nothing configured, so this really is an ambient-auth setup.
		case 1:
			identityName = names[0]
			ctx.Info("Using identity '%s' (only one available)", identityName)
		default:
			return nil, codedErrorf(codeMultipleIdentities, "multiple identities available, please specify one with --identity: %s", strings.Join(names, ", "))
		}
	}

	// Look up identity if one was specified (skip if manual mode with no identity)
	var identity *clientconfig.IdentityConfig
	if identityName != "" {
		identity, err = lookupIdentity(mainConfig, identityName)
		if err != nil {
			return nil, err
		}
	}

	// With no address to dial, the cluster comes from cloud — either named on
	// the command line or chosen from the picker. Both land on the same
	// selected cluster and then share every decision about how to reach it.
	var clusterCert *clusterCertificate
	var allAddresses []string
	var clusterXID string

	// What the cluster is called in cloud, and whose it is. Empty in manual
	// mode, where cloud is never asked and neither is knowable.
	var cloudName, organizationName string

	if address == "" {
		ctx.Info("Fetching available clusters from identity server...")

		clusters, err := fetchAvailableClusters(ctx, mainConfig, identityName, identity)
		if err != nil {
			return nil, codedErrorf(codeCloudRequestFailed, "failed to fetch available clusters: %w", err)
		}

		if len(clusters) == 0 {
			return nil, codedErrorf(codeNoClusters, "no clusters available for your account")
		}

		var (
			selectedCluster *ClusterResponse
			localName       string
			// Which of the undialable clusters cloud can still reach. Skipped
			// when the caller already said --via-cloud, since the answer would
			// change nothing.
			cloudRoutable map[string]bool
		)

		if clusterName != "" {
			selectedCluster, err = findClusterByName(clusters, clusterName, opts.organization)
			if err != nil {
				return nil, err
			}

			localName = clusterName
			if opts.localName != "" {
				localName = opts.localName
			}

			// Only the one cluster is asked about, since no list is being
			// rendered and the other answers would go unused.
			if !opts.viaCloud {
				cloudRoutable = cloudRoutableClusters(ctx, mainConfig, identityName, identity, []ClusterResponse{*selectedCluster})
			}

			// The picker greys out a cluster with nothing to dial that cloud
			// cannot reach either. Named, there is no row to grey out, and
			// falling through would probe localhost as a last resort — which
			// on a dev machine can pin whatever is listening there under a
			// remote cluster's name.
			if !opts.viaCloud && !selectedCluster.HasReachableAddress() && !cloudRoutable[selectedCluster.XID] {
				return nil, codedErrorf(codeClusterUnreachable, "%s has %s, and cloud cannot reach it either; to connect: %s",
					selectedCluster.Name, unreachableAddressNote, unreachableAddressHelp)
			}
		} else {
			// Asked before the picker so a cluster that works is offered as
			// one, rather than greyed out for advertising no address it was
			// never going to have.
			if !opts.viaCloud {
				cloudRoutable = cloudRoutableClusters(ctx, mainConfig, identityName, identity, clusters)
			}

			// Present cluster selection to user and get local name
			selectedCluster, localName, err = selectClusterFromList(ctx, clusters, cloudRoutable)
			if err != nil {
				return nil, err
			}
		}

		clusterName = localName
		clusterXID = selectedCluster.XID
		cloudName = selectedCluster.Name
		organizationName = selectedCluster.OrganizationName

		// A cluster nothing can dial, which cloud reports it can reach. Routing
		// through cloud is not a fallback here so much as the only way it was
		// ever going to work, so it is chosen without asking — but it is still
		// confirmed by using the route, because presence in cloud does not
		// establish that the cluster will answer over it.
		if !opts.viaCloud && cloudRoutable[selectedCluster.XID] {
			if !canFallBackToCloud(ctx, mainConfig, identityName, identity, selectedCluster) {
				return nil, codedErrorf(codeClusterUnreachable,
					"%s advertises no address this machine can dial, and did not answer through cloud either; "+
						"if its runtime predates cloud-routed RPC, upgrade it or add the cluster from a network that can reach it",
					selectedCluster.Name)
			}
			opts.viaCloud = true
			ctx.Info("%s advertises no address this machine can dial, and cloud can reach it.", selectedCluster.Name)
		}

		if opts.viaCloud {
			// No address, no probe, no certificate. Every one of those describes
			// dialing the cluster, which is the thing this entry exists to avoid
			// — and probing would just fail slowly before writing the entry that
			// was going to work.
			announceClusterAdd(ctx, selectedCluster.Name, localName, "routed through Miren Cloud")
		} else {
			// Store all available addresses
			allAddresses = selectedCluster.APIAddresses

			// Try to connect to the cluster
			workingAddress, cert, err := tryConnectToCluster(ctx, selectedCluster, true)
			if err != nil {
				// It advertised addresses and none of them answered, which is
				// the other shape of unreachable: a cluster on a network this
				// machine is not on. Cloud may still hold a link to it, and if
				// it does, the entry that works is the routed one.
				if !canFallBackToCloud(ctx, mainConfig, identityName, identity, selectedCluster) {
					return nil, codedErrorf(codeClusterUnreachable, "%w", err)
				}

				ctx.Info("Could not reach %s directly, but cloud has a link to it.", selectedCluster.Name)

				// The address and certificate belong to a route that does not
				// work from here. Keeping either would write an entry that
				// looks dialable and is not.
				opts.viaCloud = true
				allAddresses = nil

				announceClusterAdd(ctx, selectedCluster.Name, localName, "routed through Miren Cloud")
			} else {
				clusterCert = cert
				address = workingAddress

				announceClusterAdd(ctx, selectedCluster.Name, localName, "connected to "+workingAddress)
			}
		}
	} else {
		// Manual mode - address was specified directly
		ctx.Info("Connecting to %s to extract TLS certificate...", address)

		// Extract the TLS certificate from the server
		cert, err := extractTLSCertificate(ctx, address)
		if err != nil {
			return nil, codedErrorf(codeClusterUnreachable, "failed to extract TLS certificate: %w", err)
		}
		clusterCert = cert
		ctx.Completed("Successfully extracted TLS certificate (fingerprint: %s)", cert.Fingerprint)
	}

	// Create the cluster configuration
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname:     address,
		AllAddresses: allAddresses,
		Identity:     identityName,
		XID:          clusterXID,
		ViaCloud:     opts.viaCloud,
	}

	// A cloud reached over plain HTTP is a development one, and routing through
	// it means putting this identity's token on an unencrypted socket. The
	// entry has to say so or it will not connect at all — and writing an entry
	// that cannot work, or one that quietly ships a credential in the clear
	// without recording that anywhere, are both worse than saying it out loud
	// here and putting the admission in the file.
	if opts.viaCloud && identity != nil && strings.HasPrefix(identity.Issuer, "http://") {
		clusterConfig.Insecure = true
		ctx.Warn("%s is not encrypted, so commands to this cluster will send your credentials in the clear.", identity.Issuer)
		ctx.Warn("Recorded as insecure: true on the cluster. Use an https cloud for anything but local development.")
	}

	if clusterCert != nil {
		clusterConfig.CACert = clusterCert.CAPEM
	}

	applyVerificationName(ctx, clusterConfig, address, clusterCert)

	// Load or create the main client config
	mainConfig, err = clientconfig.LoadConfig()
	if err != nil {
		// If no config exists, create a new one
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return nil, codedErrorf(codeConfigLoadFailed, "failed to load client config: %w", err)
		}
	}

	// Check if the leaf config already exists (by trying to get the cluster)
	if mainConfig.HasCluster(clusterName) && !force {
		// A caller wanting a document gets the decision handed back rather than
		// a prompt, even at a terminal: overwriting somebody's cluster entry is
		// not a call to make on their behalf because nobody was watching.
		if ui.IsInteractive() && !opts.jsonOutput {
			// Prompt user to choose: overwrite or cancel
			items := []ui.PickerItem{
				ui.SimplePickerItem{Text: "Overwrite existing configuration"},
				ui.SimplePickerItem{Text: "Cancel"},
			}

			title := fmt.Sprintf("Cluster %q already exists", clusterName)
			selected, err := ui.RunPicker(items, ui.WithTitle(title))
			if err != nil {
				return nil, codedErrorf(codeInteractiveRequired, "failed to run picker: %w", err)
			}
			if selected == nil || selected.ID() == "Cancel" {
				return nil, codedErrorf(codeCancelled, "cancelled")
			}
			// User chose to overwrite, continue
		} else {
			return nil, codedErrorf(codeClusterExists, "cluster configuration %q already exists. Use --force to overwrite", clusterName)
		}
	}

	// Create the cluster config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: clusterConfig,
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/{cluster}.yaml)
	mainConfig.SetLeafConfig(clusterName, leafConfigData)

	if mainConfig.GetClusterCount() == 1 {
		// If this is the first cluster, set it as active
		if err := mainConfig.SetActiveCluster(clusterName); err != nil {
			return nil, codedErrorf(codeConfigWriteFailed, "failed to set active cluster: %w", err)
		}
		ctx.Info("Setting %q as the active cluster", clusterName)
	}

	// Save the main config (which will also save the leaf config)
	if err := mainConfig.Save(); err != nil {
		return nil, codedErrorf(codeConfigWriteFailed, "failed to save cluster configuration: %w", err)
	}

	if opts.viaCloud {
		ctx.Completed("Successfully added cluster %q with identity %q, routed through Miren Cloud", clusterName, identityName)
		ctx.Info("Commands reach it over the connection it holds open to cloud, so it needs no address here.")
	} else if identityName != "" {
		ctx.Completed("Successfully added cluster %q with identity %q at %s", clusterName, identityName, address)
	} else {
		ctx.Completed("Successfully added cluster %q at %s", clusterName, address)
		ctx.Info("No identity is attached, so this cluster authenticates with whatever the environment supplies.")
		ctx.Info("Outside CI that's usually not what you want. Run 'miren login' and add it again.")
	}
	ctx.Info("Configuration saved to clientconfig.d/%s.yaml", clusterName)

	// If there's no active cluster set, suggest setting this one
	if mainConfig != nil && mainConfig.ActiveCluster() == "" {
		ctx.Info("")
		ctx.Info("Tip: Set this as your active cluster with:")
		ctx.Info("  miren cluster switch %s", clusterName)
	}

	added := &addedCluster{
		Name:       clusterName,
		XID:        clusterXID,
		Address:    address,
		ViaCloud:   opts.viaCloud,
		Identity:   identityName,
		Insecure:   clusterConfig.Insecure,
		Active:     mainConfig.ActiveCluster() == clusterName,
		ConfigFile: mainConfig.GetClusterSource(clusterName),
	}
	if cloudName != "" && cloudName != clusterName {
		added.CloudName = cloudName
	}
	added.Organization = organizationName

	return added, nil
}

// normalizeAddress handles robust address normalization for various formats:
// - Strips optional scheme prefixes (https:// or http://)
// - Handles IPv6 literals correctly (bracketed and unbracketed)
// - Adds default port 8443 when no port is present
// Returns normalized address and host for SNI (with brackets stripped for IPv6)
func normalizeAddress(address string) (normalizedAddr, sniHost string, err error) {
	// Strip scheme if present
	addr := address
	if after, ok := strings.CutPrefix(addr, "https://"); ok {
		addr = after
	} else if after, ok := strings.CutPrefix(addr, "http://"); ok {
		addr = after
	}

	// Handle IPv6 literals and port logic
	if strings.Contains(addr, "]") {
		// Bracketed IPv6 format [::1]:8443 or [::1]
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// No port specified, assume it's just [::1]
			if strings.HasSuffix(addr, "]") {
				normalizedAddr = addr + ":8443"
				sniHost = strings.Trim(addr, "[]")
				return normalizedAddr, sniHost, nil
			}
			return "", "", fmt.Errorf("invalid IPv6 address format: %w", err)
		}
		normalizedAddr = addr
		sniHost = strings.Trim(host, "[]")
		return normalizedAddr, sniHost, nil
	} else if strings.Count(addr, ":") > 1 && !strings.Contains(addr, "[") {
		// Unbracketed IPv6 like ::1 or 2001:db8::1
		// Need to wrap in brackets and add port
		normalizedAddr = "[" + addr + "]:8443"
		sniHost = addr
		return normalizedAddr, sniHost, nil
	} else {
		// IPv4 or hostname
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// No port specified
			normalizedAddr = addr + ":8443"
			sniHost = addr
			return normalizedAddr, sniHost, nil
		}
		normalizedAddr = addr
		sniHost = host
		return normalizedAddr, sniHost, nil
	}
}

// clusterCertificate is what probing a cluster's API endpoint tells us about
// its TLS configuration.
type clusterCertificate struct {
	// CAPEM is the certificate to pin, PEM encoded: the root of the presented
	// chain, or the certificate itself when it is self-signed.
	CAPEM string

	// Fingerprint is the SHA1 of the pinned certificate's DER bytes, hex encoded.
	Fingerprint string

	// Leaf is the certificate the server presented for itself. It carries the
	// SANs, which is what decides whether a given dial address can be verified.
	Leaf *x509.Certificate
}

// verificationName decides what name the cluster's certificate should be
// verified against when dialing with sniHost, and reports whether verification
// can succeed at all.
//
// Dialing by IP normally needs no help, because the API certificate carries
// every address the server discovered as an IP SAN, and those are the addresses
// cluster discovery hands out. A hostname is a different story: the certificate
// carries `localhost`, `api.miren`, and whatever the operator listed in
// `additional_names`, so a MagicDNS name or any other DNS record pointed at the
// cluster matches nothing and the handshake fails on a name mismatch. The same
// goes for an IP the server doesn't know it has, like one in front of a static
// NAT.
//
// api.miren is the way out. Every API certificate carries it precisely so that
// clients which cannot dial a SAN still verify against the cluster CA rather
// than skipping verification — it is how sandboxes reach the API over a bridge
// address that was leased after the certificate was issued. Borrowing it keeps
// a hostname dial fully verified instead of downgrading it.
func verificationName(sniHost string, leaf *x509.Certificate) (serverName string, verifiable bool) {
	if leaf == nil {
		return "", true
	}

	// Handles IP literals as well as names, matching against IP SANs.
	if leaf.VerifyHostname(sniHost) == nil {
		return "", true
	}

	if slices.Contains(leaf.DNSNames, clientconfig.APIServerName) {
		return clientconfig.APIServerName, true
	}

	return "", false
}

// applyVerificationName sets the cluster's TLS server name when the dial
// address can't verify on its own, warning when nothing will.
//
// Both early returns are unreachable in practice and neither warns, which is
// deliberate: every caller reaches here only after extractTLSCertificate
// succeeded on this same address, so a nil certificate or an address that no
// longer parses would both mean something upstream broke its own contract.
// Warning about that would put an alarming and unactionable message in front of
// a user whose cluster is fine. The cost if it ever did happen is a config
// written without a TLSServerName, which fails later with a name mismatch.
func applyVerificationName(ctx *Context, cfg *clientconfig.ClusterConfig, address string, cert *clusterCertificate) {
	if cert == nil {
		return
	}

	_, sniHost, err := normalizeAddress(address)
	if err != nil {
		return
	}

	serverName, verifiable := verificationName(sniHost, cert.Leaf)
	switch {
	case !verifiable:
		ctx.Warn("The cluster's certificate doesn't cover %q, so connections to it will fail.", sniHost)
		ctx.Warn("Add it to additional_names in the server config and restart, or use an address the certificate already covers.")
	case serverName != "":
		cfg.TLSServerName = serverName
		ctx.Info("Verifying the cluster's certificate as %q, since it doesn't cover %q", serverName, sniHost)
	}
}

// extractTLSCertificate connects to the server via QUIC and inspects the
// certificate it presents.
func extractTLSCertificate(ctx context.Context, address string) (*clusterCertificate, error) {
	// Normalize the address with robust parsing
	normalizedAddr, sniHost, err := normalizeAddress(address)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize address: %w", err)
	}

	// Create a context with timeout
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create TLS config that accepts any certificate for now (we're extracting it)
	tlsConfig := &tls.Config{
		ServerName:         sniHost,                                   // Use properly stripped SNI host
		InsecureSkipVerify: true,                                      // We're extracting the cert, not verifying it yet
		NextProtos:         []string{"h3", "h3-29", "h3-28", "h3-27"}, // HTTP/3 ALPN with common variants
	}

	// Create QUIC config
	quicConfig := &quic.Config{
		InitialPacketSize:    rpc.InitialPacketSize,
		HandshakeIdleTimeout: 5 * time.Second,
		MaxIdleTimeout:       10 * time.Second,
	}

	// Try to establish a QUIC connection
	udpAddr, err := net.ResolveUDPAddr("udp", normalizedAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	// Try IPv6/dual-stack binding first, fallback to IPv4
	var udpConn *net.UDPConn
	udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err != nil {
		// Fallback to IPv4 if IPv6 fails
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return nil, fmt.Errorf("failed to create UDP socket: %w", err)
		}
	}
	defer udpConn.Close()

	transport := &quic.Transport{
		Conn: udpConn,
	}
	defer transport.Close()

	conn, err := transport.Dial(connCtx, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to establish QUIC connection: %w", err)
	}
	defer conn.CloseWithError(0, "done")

	// Get the TLS connection state
	connState := conn.ConnectionState().TLS

	// Extract the certificate chain
	if len(connState.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no certificates found in TLS handshake")
	}

	// Get the root CA certificate (usually the last in the chain)
	// But for self-signed certs, there might be only one
	var rootCert *x509.Certificate
	if len(connState.PeerCertificates) == 1 {
		// Self-signed certificate
		rootCert = connState.PeerCertificates[0]
	} else {
		// Take the last certificate in the chain as the root CA
		rootCert = connState.PeerCertificates[len(connState.PeerCertificates)-1]
	}

	// Calculate SHA1 fingerprint of the raw DER bytes
	sum := sha1.Sum(rootCert.Raw)
	fingerprint := hex.EncodeToString(sum[:])

	// Encode the certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: rootCert.Raw,
	})

	return &clusterCertificate{
		CAPEM:       string(certPEM),
		Fingerprint: fingerprint,
		Leaf:        connState.PeerCertificates[0],
	}, nil
}
