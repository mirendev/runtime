package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// ClusterResponse represents a cluster returned from the API
type ClusterResponse struct {
	XID               string         `json:"xid"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Tags              map[string]any `json:"tags"`
	APIAddresses      []string       `json:"api_addresses,omitempty"`
	CACertFingerprint string         `json:"ca_cert_fingerprint,omitempty"`
	OrganizationXID   string         `json:"organization_xid"`
	OrganizationName  string         `json:"organization_name"`
}

// hasReachableAddress reports whether the cloud advertised at least one API
// address for this cluster. A cluster with none can't be dialed by any client,
// so it would otherwise be silently hidden from `miren cluster add`. The most
// common cause is a firewalled inbound port: miren connects over QUIC (UDP
// 8443), and when the cloud's netcheck can't reach that port it drops the
// discovered public IP, leaving the cluster advertising nothing. See MIR-1316.
func (c ClusterResponse) hasReachableAddress() bool {
	return len(c.APIAddresses) > 0
}

const (
	// unreachableAddressNote is the short, inline note shown in listings next to
	// a cluster that advertised no reachable API address.
	unreachableAddressNote = "no reachable address"

	// unreachableAddressHelp is the one-line remediation shown alongside that
	// note. It names UDP 8443 specifically (miren dials over QUIC, so the UDP
	// port is what's necessary and sufficient, not TCP) and points at the
	// on-host diagnostic that reproduces the exact decision.
	unreachableAddressHelp = "open UDP 8443 (QUIC) on the host or set additional_ips, then restart miren; run 'miren debug advertise' on the host to see why"

	// viaCloudNote replaces that note for a cluster nothing can dial but cloud
	// can still reach. Not a degraded state to remediate: it is a working
	// cluster on a network that does not accept inbound connections, which is a
	// reasonable way to run one.
	viaCloudNote = "via Miren Cloud"
)

// printUnreachableClustersHelp prints a warning header, a bulleted list of the
// given clusters, and the standard remediation guidance. Shared between the
// cluster-add picker and login's auto-config so the two messages can't drift.
func printUnreachableClustersHelp(ctx *Context, header string, clusters []ClusterResponse) {
	ctx.Warn(header)
	for _, cluster := range clusters {
		ctx.Info("  • %s (%s)", cluster.Name, cluster.OrganizationName)
	}
	ctx.Info("")
	ctx.Info("To connect: %s", unreachableAddressHelp)
}

// formatAddressWithGrayPort formats an address with the port portion grayed out
func formatAddressWithGrayPort(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// No port or invalid format, return as-is
		return address
	}

	// Gray out the port portion
	grayStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	// Check if host needs brackets (IPv6)
	if strings.Contains(host, ":") {
		// IPv6 address - reconstruct with brackets
		grayPort := grayStyle.Render("]:" + port)
		return "[" + host + grayPort
	}

	// IPv4 or hostname
	grayPort := grayStyle.Render(":" + port)
	return host + grayPort
}

// sortAddresses sorts addresses to prioritize public/routable addresses over localhost/0.0.0.0
func sortAddresses(addresses []string) []string {
	if len(addresses) <= 1 {
		return addresses
	}

	// Copy to avoid modifying original
	sorted := make([]string, len(addresses))
	copy(sorted, addresses)

	// Sort with custom logic
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			// Check if addresses should be swapped
			if shouldSwapAddresses(sorted[i], sorted[j]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// shouldSwapAddresses returns true if addr1 should come after addr2
func shouldSwapAddresses(addr1, addr2 string) bool {
	// Extract host part from address
	host1 := extractHost(addr1)
	host2 := extractHost(addr2)

	// Check address types
	local1 := isLocalAddress(host1)
	local2 := isLocalAddress(host2)
	private1 := isPrivateAddress(host1)
	private2 := isPrivateAddress(host2)

	// Priority order: public > private > local
	// If one is local and the other isn't, local goes last
	if local1 && !local2 {
		return true
	}
	if !local1 && local2 {
		return false
	}

	// Both are local or both are not local
	// If one is private and the other is public, private goes after
	if private1 && !private2 {
		return true
	}

	return false
}

func extractHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// No port or invalid format, return as-is
		return address
	}
	return host
}

func isLocalAddress(host string) bool {
	// Handle localhost hostname
	if host == "localhost" {
		return true
	}

	// Parse as IP address
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	// Check for loopback (127.0.0.0/8 or ::1)
	if ip.IsLoopback() {
		return true
	}

	// Check for unspecified addresses (0.0.0.0 or ::)
	if ip.IsUnspecified() {
		return true
	}

	return false
}

func isPrivateAddress(host string) bool {
	// Parse as IP address
	ip := net.ParseIP(host)
	if ip == nil {
		// Not a valid IP, could be a hostname
		return false
	}

	// Use the built-in IsPrivate method (available in Go 1.17+)
	// This checks for:
	// - 10.0.0.0/8 (RFC1918)
	// - 172.16.0.0/12 (RFC1918)
	// - 192.168.0.0/16 (RFC1918)
	// - 169.254.0.0/16 (link-local)
	// - fc00::/7 (IPv6 unique local)
	// - fe80::/10 (IPv6 link-local)
	return ip.IsPrivate()
}

// fetchAvailableClusters queries the identity server for available clusters.
// identityName may be "" for an anonymous in-memory identity (e.g. during login
// before it has been named), in which case token refreshes are not persisted.
func fetchAvailableClusters(ctx *Context, config *clientconfig.Config, identityName string, identity *clientconfig.IdentityConfig) ([]ClusterResponse, error) {
	if identity.Type != clientconfig.IdentityKeypair && identity.Type != clientconfig.IdentityToken {
		return nil, fmt.Errorf("cluster listing is only supported for keypair and token identities")
	}

	// Get the issuer URL
	issuerURL := identity.Issuer
	if issuerURL == "" {
		return nil, fmt.Errorf("identity has no issuer configured")
	}

	// Get JWT token
	token, err := config.TokenForIdentity(ctx, identityName, identity, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Make request to fetch clusters
	clustersURL, err := url.JoinPath(issuerURL, "/api/v1/users/clusters")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", clustersURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clusters: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	// Define response structure
	var response struct {
		Clusters []ClusterResponse `json:"clusters"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Clusters, nil
}

// clusterOnlineInCloud asks cloud whether it currently holds a link to a
// cluster, which is the precondition for relaying RPC to it.
//
// This is what separates "you cannot dial this cluster, and nothing else can
// either" from "you cannot dial it, but cloud can". The first is a dead end
// worth saying so about; the second is a cluster that works fine once the
// config says to route through cloud.
func clusterOnlineInCloud(
	ctx *Context,
	config *clientconfig.Config,
	identityName string,
	identity *clientconfig.IdentityConfig,
	clusterXID string,
) (bool, error) {
	if identity == nil || clusterXID == "" {
		return false, nil
	}

	issuerURL := identity.Issuer
	if issuerURL == "" {
		return false, fmt.Errorf("identity has no issuer configured")
	}

	token, err := config.TokenForIdentity(ctx, identityName, identity, issuerURL)
	if err != nil {
		return false, fmt.Errorf("failed to authenticate: %w", err)
	}

	onlineURL, err := url.JoinPath(issuerURL, "/api/v1/clusters/", clusterXID, "/online")
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", onlineURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	// Short, because this runs while somebody is waiting at a prompt and a slow
	// answer is worth no more than no answer: either way we fall back to
	// treating the cluster as unroutable.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("cloud returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var response struct {
		Online bool `json:"online"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Online, nil
}

const (
	// cloudProbeTimeout bounds the relayed probe. The failure it exists to
	// catch is silence, so without a deadline the probe inherits the very hang
	// it is there to prevent. Longer than the presence check, because this one
	// opens a session and makes a call rather than asking a question.
	cloudProbeTimeout = 20 * time.Second

	// cloudProbeObject is what the probe resolves. Any exposed object would do;
	// this one is the object nearly every command reaches for, so a cluster
	// that cannot produce it is not usable through cloud in any case.
	cloudProbeObject = "dev.miren.runtime/app"
)

// cloudRouteWorks reports whether a cloud-routed entry for this cluster would
// actually work, by building the entry that would be written and using it.
//
// Cloud reporting a cluster online is a weaker claim than it appears. It says a
// link exists; it does not say what is on the far end of that link will answer
// a relayed call. A cluster running a runtime from before the relay shipped is
// online and silent: it has no handler for the session, so nothing refuses the
// call and nothing answers it either. An entry written on the strength of
// presence alone would hang rather than fail.
//
// So the cloud route is probed the same way a direct address is — by using it,
// and believing the result rather than a claim about it. This costs a round
// trip at add time and settles the question for every command afterwards.
func cloudRouteWorks(
	ctx *Context,
	config *clientconfig.Config,
	identityName string,
	identity *clientconfig.IdentityConfig,
	cluster *ClusterResponse,
) bool {
	// The entry that would be written, not an approximation of it. Probing
	// anything else would leave the thing actually written untested.
	probe := &clientconfig.ClusterConfig{
		ViaCloud: true,
		XID:      cluster.XID,
		Identity: identityName,
	}
	if identity != nil && strings.HasPrefix(identity.Issuer, "http://") {
		probe.Insecure = true
	}

	probeCtx, cancel := context.WithTimeout(ctx, cloudProbeTimeout)
	defer cancel()

	state, err := probe.State(probeCtx, config, rpc.WithLogger(ctx.Log))
	if err != nil {
		ctx.Warn("Could not reach %s through cloud: %v", cluster.Name, err)
		return false
	}
	defer state.Close() //nolint:errcheck // a probe's teardown tells us nothing

	// Resolving the object is a round trip the cluster itself has to answer.
	// Cloud relays it and cannot satisfy it, which is what makes this evidence
	// about the cluster rather than about the link.
	if _, err := state.Client(cloudProbeObject); err != nil {
		ctx.Warn("Could not reach %s through cloud: %v", cluster.Name, err)
		return false
	}

	return true
}

// cloudRouteProbe is the check the fallback decision runs, as a variable so a
// test can substitute one. The real probe needs a live relayed session, which
// is the blackbox suite's job; what is worth testing here is the decision made
// from its answer. Tests in this package run sequentially, so swapping it is
// safe as long as none of them opts into t.Parallel.
var cloudRouteProbe = cloudRouteWorks

// cloudRoutableClusters reports which of the given clusters cannot be dialed
// directly but can be reached through cloud, keyed by XID.
//
// Only the ones advertising no address are asked about, which is both the case
// this exists for and a bound on how many requests a picker costs.
//
// An error on any single check is not fatal, since the cluster simply does not
// get offered as cloud-routable, which is what a "no" would have produced. It
// is still reported once at the end rather than swallowed: about to be told a
// cluster is unusable, it matters whether that is cloud's answer or cloud
// failing to give one.
func cloudRoutableClusters(
	ctx *Context,
	config *clientconfig.Config,
	identityName string,
	identity *clientconfig.IdentityConfig,
	clusters []ClusterResponse,
) map[string]bool {
	routable := make(map[string]bool)

	var (
		unchecked []string
		lastErr   error
	)

	// Concurrently, because these run while somebody waits at a prompt and the
	// checks do not depend on each other. Sequentially, an account with several
	// undialable clusters paid each one's timeout in turn before the picker
	// appeared — worst case tens of seconds of nothing on screen. The direct
	// path already probes its addresses this way.
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, cluster := range clusters {
		if cluster.hasReachableAddress() || cluster.XID == "" {
			continue
		}

		wg.Add(1)
		go func(cluster ClusterResponse) {
			defer wg.Done()

			online, err := clusterOnlineInCloud(ctx, config, identityName, identity, cluster.XID)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				unchecked = append(unchecked, cluster.Name)
				lastErr = err
				return
			}
			if online {
				routable[cluster.XID] = true
			}
		}(cluster)
	}

	wg.Wait()

	// Names are reported in a stable order rather than whichever request
	// happened to fail first, so the same failure reads the same way twice.
	sort.Strings(unchecked)

	if len(unchecked) > 0 {
		ctx.Warn("Could not ask cloud whether %s reachable through it (%v); treating as not reachable.",
			plural(unchecked), lastErr)
	}

	return routable
}

// plural renders a short list of names for a one-line message.
func plural(names []string) string {
	if len(names) == 1 {
		return fmt.Sprintf("%s is", names[0])
	}
	return fmt.Sprintf("%s are", strings.Join(names, ", "))
}

// buildClusterPickerItems turns the fetched clusters into picker rows. Every
// cluster gets a row: reachable ones show their primary address (sorted so a
// public address wins) and are selectable; unreachable ones (no advertised
// address) show the reason inline and are marked disabled so the picker greys
// them out and blocks selection. It returns the rows, a map from row ID back to
// the source cluster, the set of disabled row IDs, and the count of selectable
// (reachable) clusters. Kept pure so the classification is unit-testable
// without standing up a TUI. See MIR-1316.
func buildClusterPickerItems(clusters []ClusterResponse, cloudRoutable map[string]bool) (items []ui.PickerItem, clusterMap map[string]*ClusterResponse, disabled map[string]bool, reachableCount int) {
	items = make([]ui.PickerItem, 0, len(clusters))
	clusterMap = make(map[string]*ClusterResponse)
	disabled = make(map[string]bool)

	for i, cluster := range clusters {
		itemID := fmt.Sprintf("cluster_%d", i)

		var address string
		if cluster.hasReachableAddress() {
			reachableCount++
			// Sort addresses to put localhost/0.0.0.0 last, then format the
			// primary one with a grayed port.
			addresses := sortAddresses(cluster.APIAddresses)
			address = formatAddressWithGrayPort(addresses[0])
			if len(addresses) > 1 {
				address = fmt.Sprintf("%s (+%d)", address, len(addresses)-1)
			}
		} else if cloudRoutable[cluster.XID] {
			// Selectable, because it can be used: the entry will route through
			// cloud rather than dialing. Counted as reachable for the same
			// reason, since that count only exists to decide whether the picker
			// has anything to offer.
			reachableCount++
			address = viaCloudNote
		} else {
			address = unreachableAddressNote
			disabled[itemID] = true
		}

		items = append(items, ui.TablePickerItem{
			Columns: []string{
				cluster.Name,
				cluster.OrganizationName,
				address,
			},
			ItemID: itemID,
		})
		clusterMap[itemID] = &clusters[i]
	}

	return items, clusterMap, disabled, reachableCount
}

// selectClusterFromList presents an interactive list of clusters for selection and prompts for local name
// Returns the selected cluster and the local name to use
func selectClusterFromList(ctx *Context, clusters []ClusterResponse, cloudRoutable map[string]bool) (*ClusterResponse, string, error) {
	// Check if we can run interactive mode
	if !ui.IsInteractive() {
		// Non-interactive mode - list clusters and exit. We list clusters with no
		// reachable address too (annotated), rather than hiding them: a silently
		// missing cluster reads as an auth/org problem and sends users chasing
		// the wrong thing. See MIR-1316.
		ctx.Printf("Available clusters:\n\n")
		for clusterNum, cluster := range clusters {
			ctx.Printf("%d. Cluster: %s\n", clusterNum+1, cluster.Name)
			ctx.Printf("   Organization: %s\n", cluster.OrganizationName)
			if cluster.Description != "" {
				ctx.Printf("   Description: %s\n", cluster.Description)
			}
			if cluster.hasReachableAddress() {
				ctx.Printf("   API Addresses:\n")
				for _, addr := range cluster.APIAddresses {
					ctx.Printf("     - %s\n", addr)
				}
				if cluster.CACertFingerprint != "" {
					ctx.Printf("   Certificate Fingerprint: %s\n", cluster.CACertFingerprint)
				}
			} else if cloudRoutable[cluster.XID] {
				ctx.Printf("   Status: %s (no direct route, but cloud can reach it)\n", viaCloudNote)
			} else {
				ctx.Printf("   Status: %s — %s\n", unreachableAddressNote, unreachableAddressHelp)
			}
			ctx.Printf("\n")
		}
		ctx.Printf("Re-run with --cluster <name> to add one of these without the picker\n")
		ctx.Printf("(add --organization when the same name appears in more than one, or --address to dial a cluster directly)\n")
		return nil, "", fmt.Errorf("interactive mode not available")
	}

	// Build the picker rows. A cluster with no reachable address that cloud can
	// still reach is selectable and annotated, because it works. One nothing can
	// reach is listed but disabled, with the reason inline rather than hidden —
	// a silently missing cluster reads as an auth or org problem and sends users
	// chasing the wrong thing. See MIR-1316.
	items, clusterMap, disabled, reachableCount := buildClusterPickerItems(clusters, cloudRoutable)

	// If nothing is selectable, a picker is a dead end. Print the clusters with
	// their reason and return a clear error instead of trapping the user in a
	// list where Enter does nothing.
	if reachableCount == 0 {
		printUnreachableClustersHelp(ctx, "None of your clusters advertise a reachable address:", clusters)
		return nil, "", fmt.Errorf("no clusters with a reachable address")
	}

	// Run the table picker
	selected, err := ui.RunPicker(items,
		ui.WithTitle("Select a cluster to bind:"),
		ui.WithHeaders([]string{"NAME", "ORGANIZATION", "ADDRESS"}),
		ui.WithDisabledCheck(func(item ui.PickerItem) bool {
			return disabled[item.ID()]
		}, unreachableAddressHelp),
	)

	if err != nil {
		return nil, "", fmt.Errorf("failed to run cluster selection: %w", err)
	}

	if selected == nil {
		return nil, "", fmt.Errorf("cluster selection cancelled")
	}

	// Get the selected cluster
	selectedCluster := clusterMap[selected.ID()]
	if selectedCluster == nil {
		return nil, "", fmt.Errorf("invalid selection")
	}

	// Now prompt for local name using a text input modal
	localName, err := promptForLocalName(ctx, selectedCluster)
	if err != nil {
		return nil, "", err
	}

	// Return both the selected cluster and the local name
	return selectedCluster, localName, nil
}

// promptForLocalName prompts the user to enter a local name for the cluster
func promptForLocalName(ctx *Context, cluster *ClusterResponse) (string, error) {
	if !ui.IsInteractive() {
		// Non-interactive mode - use cluster name
		return cluster.Name, nil
	}

	// Create a text input model
	textInput := textinput.New()
	textInput.Placeholder = cluster.Name
	textInput.SetValue(cluster.Name)
	textInput.Focus()
	textInput.CharLimit = 100
	textInput.Width = 50
	textInput.Prompt = "Local name: "

	m := localNameModel{
		textInput: textInput,
		cluster:   cluster,
	}

	// Run the text input
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("failed to run name input: %w", err)
	}

	model := result.(localNameModel)
	if model.cancelled {
		return "", fmt.Errorf("name input cancelled")
	}

	return model.localName, nil
}

type localNameModel struct {
	textInput textinput.Model
	cluster   *ClusterResponse
	localName string
	cancelled bool
}

func (m localNameModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m localNameModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			value := m.textInput.Value()
			if value == "" {
				// Use placeholder if empty
				value = m.textInput.Placeholder
			}
			// Validate the name
			if strings.ContainsAny(value, "/\\:*?\"<>|") {
				// Invalid characters - don't accept
				return m, nil
			}
			m.localName = value
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m localNameModel) View() string {
	// Create the modal content
	var modalContent strings.Builder

	// Title
	title := "Choose Local Name"
	modalContent.WriteString(modalTitleStyle.Render(title))
	modalContent.WriteString("\n\n")

	// Show selected cluster info
	info := fmt.Sprintf("Cluster: %s\nOrganization: %s", m.cluster.Name, m.cluster.OrganizationName)
	if m.cluster.Description != "" {
		info += fmt.Sprintf("\nDescription: %s", m.cluster.Description)
	}
	modalContent.WriteString(modalSubtitleStyle.Render(info))
	modalContent.WriteString("\n\n")

	// Text input
	modalContent.WriteString(m.textInput.View())

	// Show validation error if needed
	value := m.textInput.Value()
	if value != "" && strings.ContainsAny(value, "/\\:*?\"<>|") {
		modalContent.WriteString("\n\n")
		modalContent.WriteString(modalErrorStyle.Render("⚠ Name contains illegal characters (/\\:*?\"<>|)"))
	}

	// Help text
	modalContent.WriteString("\n\n")
	modalContent.WriteString(modalHelpStyle.Render("Enter: confirm • Esc: cancel • Ctrl+C: cancel"))

	// Apply modal styling
	return modalStyle.Render(modalContent.String())
}

// Define consistent styles for both list and modal.
//
// These are intentionally NOT drawn from pkg/theme: the modal paints its own dark
// background (bgColor) and layers light text on top, so it reads correctly on any
// terminal. Swapping in adaptive foregrounds would flip them to dark tones on a
// light terminal while the box stayed dark, making the modal unreadable.
var (
	// Shared colors
	primaryColor   = lipgloss.Color("229") // Bright yellow-white for titles
	secondaryColor = lipgloss.Color("244") // Gray for descriptions
	accentColor    = lipgloss.Color("62")  // Blue-green for borders and selection
	bgColor        = lipgloss.Color("235") // Dark background
	errorColor     = lipgloss.Color("196") // Red for errors
	helpColor      = lipgloss.Color("241") // Dim gray for help text

	// Modal styles
	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(1, 2).
			Background(bgColor)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			MarginBottom(1)

	modalSubtitleStyle = lipgloss.NewStyle().
				Foreground(secondaryColor).
				MarginBottom(1)

	modalErrorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	modalHelpStyle = lipgloss.NewStyle().
			Foreground(helpColor).
			MarginTop(1)
)

// tryConnectToCluster attempts to connect to a cluster using its available addresses
// and returns the working address along with what its certificate told us. It tries
// all provided addresses in parallel and optionally falls back to localhost if all
// addresses fail.
func tryConnectToCluster(ctx *Context, cluster *ClusterResponse, tryLocalhost bool) (workingAddress string, cert *clusterCertificate, err error) {
	// Filter out addresses we should skip
	var addressesToTry []string
	for _, addr := range cluster.APIAddresses {
		_, sniHost, err := normalizeAddress(addr)
		if err != nil {
			ctx.Warn("Failed to parse address %s: %v", addr, err)
			continue
		}
		if !skipAddresses[sniHost] {
			addressesToTry = append(addressesToTry, addr)
		}
	}

	if len(addressesToTry) == 0 && !tryLocalhost {
		return "", nil, fmt.Errorf("no valid addresses available for cluster %s", cluster.Name)
	}

	ctx.Info("Trying to connect to cluster addresses...")

	// Result struct for each connection attempt
	type connResult struct {
		addr string
		cert *clusterCertificate
		err  error
	}

	// Try all addresses in parallel
	resultChan := make(chan connResult, len(addressesToTry))
	var wg sync.WaitGroup

	for _, addr := range addressesToTry {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()

			cert, err := extractTLSCertificate(ctx, address)
			resultChan <- connResult{
				addr: address,
				cert: cert,
				err:  err,
			}
		}(addr)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results and find the first successful connection
	var lastErr error
	var results []connResult
	for result := range resultChan {
		results = append(results, result)
	}

	// Process results - prefer successful connections
	for _, result := range results {
		if result.err != nil {
			ctx.Warn("Failed to connect to %s: %v", result.addr, result.err)
			lastErr = result.err
			continue
		}

		// Check fingerprint if we have an expected one
		if cluster.CACertFingerprint != "" {
			if !strings.EqualFold(cluster.CACertFingerprint, result.cert.Fingerprint) {
				ctx.Warn("Certificate fingerprint mismatch for %s", result.addr)
				ctx.Warn("Expected: %s", cluster.CACertFingerprint)
				ctx.Warn("Actual:   %s", result.cert.Fingerprint)
				lastErr = fmt.Errorf("certificate fingerprint verification failed for %s", result.addr)
				continue
			}
			ctx.Info("Certificate fingerprint verified for %s", result.addr)
		}

		// Successfully connected and verified
		ctx.Completed("Successfully connected to %s", result.addr)
		return result.addr, result.cert, nil
	}

	// If all normal addresses failed and tryLocalhost is true, try localhost as a fallback
	if tryLocalhost {
		ctx.Info("All cluster addresses failed, trying localhost as fallback...")

		// Try common localhost addresses with default port
		localhostAddresses := []string{
			"127.0.0.1:8443",
			"[::1]:8443",
		}

		// Try localhost addresses in parallel too
		localResultChan := make(chan connResult, len(localhostAddresses))
		var localWg sync.WaitGroup

		for _, addr := range localhostAddresses {
			localWg.Add(1)
			go func(address string) {
				defer localWg.Done()

				cert, err := extractTLSCertificate(ctx, address)
				localResultChan <- connResult{
					addr: address,
					cert: cert,
					err:  err,
				}
			}(addr)
		}

		// Close the channel when all goroutines are done
		go func() {
			localWg.Wait()
			close(localResultChan)
		}()

		// Process localhost results
		for result := range localResultChan {
			if result.err != nil {
				ctx.Info("Failed to connect to localhost %s: %v", result.addr, result.err)
				lastErr = result.err
				continue
			}

			// Check fingerprint if we have an expected one
			if cluster.CACertFingerprint != "" {
				if !strings.EqualFold(cluster.CACertFingerprint, result.cert.Fingerprint) {
					ctx.Warn("Certificate fingerprint mismatch for %s", result.addr)
					ctx.Warn("Expected: %s", cluster.CACertFingerprint)
					ctx.Warn("Actual:   %s", result.cert.Fingerprint)
					lastErr = fmt.Errorf("certificate fingerprint verification failed for %s", result.addr)
					continue
				}
				ctx.Info("Certificate fingerprint verified for %s", result.addr)
			}

			// Successfully connected and verified
			ctx.Completed("Successfully connected to localhost at %s", result.addr)
			return result.addr, result.cert, nil
		}
	}

	if lastErr != nil {
		return "", nil, fmt.Errorf("failed to connect to any cluster address: %w", lastErr)
	}
	return "", nil, fmt.Errorf("no addresses available for cluster %s", cluster.Name)
}
