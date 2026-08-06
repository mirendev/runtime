package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// ClusterResponse represents a cluster returned from the API
type ClusterResponse struct {
	XID               string                 `json:"xid"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	Tags              map[string]interface{} `json:"tags"`
	APIAddresses      []string               `json:"api_addresses,omitempty"`
	CACertFingerprint string                 `json:"ca_cert_fingerprint,omitempty"`
	OrganizationXID   string                 `json:"organization_xid"`
	OrganizationName  string                 `json:"organization_name"`
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

// buildClusterPickerItems turns the fetched clusters into picker rows. Every
// cluster gets a row: reachable ones show their primary address (sorted so a
// public address wins) and are selectable; unreachable ones (no advertised
// address) show the reason inline and are marked disabled so the picker greys
// them out and blocks selection. It returns the rows, a map from row ID back to
// the source cluster, the set of disabled row IDs, and the count of selectable
// (reachable) clusters. Kept pure so the classification is unit-testable
// without standing up a TUI. See MIR-1316.
func buildClusterPickerItems(clusters []ClusterResponse) (items []ui.PickerItem, clusterMap map[string]*ClusterResponse, disabled map[string]bool, reachableCount int) {
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
func selectClusterFromList(ctx *Context, clusters []ClusterResponse) (*ClusterResponse, string, error) {
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
			} else {
				ctx.Printf("   Status: %s — %s\n", unreachableAddressNote, unreachableAddressHelp)
			}
			ctx.Printf("\n")
		}
		ctx.Printf("Re-run with --cluster and --address flags to select a specific cluster\n")
		return nil, "", fmt.Errorf("interactive mode not available")
	}

	// Build the picker rows. Clusters with no reachable address are listed too,
	// but disabled (greyed out, not selectable) with the reason inline, rather
	// than hidden — a silently missing cluster reads as an auth/org problem and
	// sends users chasing the wrong thing. See MIR-1316.
	items, clusterMap, disabled, reachableCount := buildClusterPickerItems(clusters)

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
// and returns the working address and CA certificate. It tries all provided addresses
// in parallel and optionally falls back to localhost if all addresses fail.
func tryConnectToCluster(ctx *Context, cluster *ClusterResponse, tryLocalhost bool) (workingAddress string, caCert string, err error) {
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
		return "", "", fmt.Errorf("no valid addresses available for cluster %s", cluster.Name)
	}

	ctx.Info("Trying to connect to cluster addresses...")

	// Result struct for each connection attempt
	type connResult struct {
		addr        string
		cert        string
		fingerprint string
		err         error
	}

	// Try all addresses in parallel
	resultChan := make(chan connResult, len(addressesToTry))
	var wg sync.WaitGroup

	for _, addr := range addressesToTry {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()

			cert, fingerprint, err := extractTLSCertificate(ctx, address)
			resultChan <- connResult{
				addr:        address,
				cert:        cert,
				fingerprint: fingerprint,
				err:         err,
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
			if !strings.EqualFold(cluster.CACertFingerprint, result.fingerprint) {
				ctx.Warn("Certificate fingerprint mismatch for %s", result.addr)
				ctx.Warn("Expected: %s", cluster.CACertFingerprint)
				ctx.Warn("Actual:   %s", result.fingerprint)
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

				cert, fingerprint, err := extractTLSCertificate(ctx, address)
				localResultChan <- connResult{
					addr:        address,
					cert:        cert,
					fingerprint: fingerprint,
					err:         err,
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
				if !strings.EqualFold(cluster.CACertFingerprint, result.fingerprint) {
					ctx.Warn("Certificate fingerprint mismatch for %s", result.addr)
					ctx.Warn("Expected: %s", cluster.CACertFingerprint)
					ctx.Warn("Actual:   %s", result.fingerprint)
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
		return "", "", fmt.Errorf("failed to connect to any cluster address: %w", lastErr)
	}
	return "", "", fmt.Errorf("no addresses available for cluster %s", cluster.Name)
}
