package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
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

// formatAddressWithGrayPort formats an address with the port portion grayed out
func formatAddressWithGrayPort(address string) string {
	// Check for IPv6 format with port
	if strings.Contains(address, "]:") {
		parts := strings.Split(address, "]:")
		if len(parts) == 2 {
			grayPort := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("]:" + parts[1])
			return parts[0] + grayPort
		}
	} else if strings.Contains(address, ":") {
		// IPv4 or hostname with port
		lastColon := strings.LastIndex(address, ":")
		if lastColon > 0 {
			host := address[:lastColon]
			port := address[lastColon:]
			grayPort := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(port)
			return host + grayPort
		}
	}
	return address
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
	for i := 0; i < len(sorted); i++ {
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
	// Handle both host:port and plain host formats
	if strings.Contains(address, "]:") {
		// IPv6 with port [::1]:8443
		end := strings.Index(address, "]")
		if end > 0 {
			return address[1:end]
		}
	} else if strings.Contains(address, ":") {
		// IPv4 or hostname with port
		parts := strings.Split(address, ":")
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return address
}

func isLocalAddress(host string) bool {
	return host == "127.0.0.1" ||
		host == "0.0.0.0" ||
		host == "localhost" ||
		host == "::1" ||
		strings.HasPrefix(host, "127.")
}

func isPrivateAddress(host string) bool {
	// Check for RFC1918 private addresses
	if strings.HasPrefix(host, "10.") {
		return true
	}
	if strings.HasPrefix(host, "192.168.") {
		return true
	}
	// Check for 172.16.0.0/12 range
	if strings.HasPrefix(host, "172.") {
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			// Second octet should be 16-31 for private range
			if second := parts[1]; len(second) > 0 {
				if second >= "16" && second <= "31" {
					return true
				}
			}
		}
	}
	// Also consider link-local addresses as private
	if strings.HasPrefix(host, "169.254.") {
		return true
	}
	return false
}

// fetchAvailableClusters queries the identity server for available clusters
func fetchAvailableClusters(ctx *Context, identity *clientconfig.IdentityConfig) ([]ClusterResponse, error) {
	if identity.Type != "keypair" {
		return nil, fmt.Errorf("cluster listing is only supported for keypair identities")
	}

	// Get the issuer URL
	issuerURL := identity.Issuer
	if issuerURL == "" {
		return nil, fmt.Errorf("identity has no issuer configured")
	}

	// Load the private key
	keyPair, err := cloudauth.LoadKeyPairFromPEM(identity.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load private key: %w", err)
	}

	// Get JWT token
	token, err := clientconfig.AuthenticateWithKey(ctx, issuerURL, keyPair)
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

// selectClusterFromList presents an interactive list of clusters for selection and prompts for local name
// Returns the selected cluster and the local name to use
func selectClusterFromList(ctx *Context, clusters []ClusterResponse) (*ClusterResponse, string, error) {
	// Check if we can run interactive mode
	if !ui.IsInteractive() {
		// Non-interactive mode - list clusters and exit
		ctx.Printf("Available clusters:\n\n")
		clusterNum := 1
		for _, cluster := range clusters {
			if len(cluster.APIAddresses) == 0 {
				continue // Skip clusters without API addresses
			}

			ctx.Printf("%d. Cluster: %s\n", clusterNum, cluster.Name)
			ctx.Printf("   Organization: %s\n", cluster.OrganizationName)
			if cluster.Description != "" {
				ctx.Printf("   Description: %s\n", cluster.Description)
			}
			ctx.Printf("   API Addresses:\n")
			for _, addr := range cluster.APIAddresses {
				ctx.Printf("     - %s\n", addr)
			}
			if cluster.CACertFingerprint != "" {
				ctx.Printf("   Certificate Fingerprint: %s\n", cluster.CACertFingerprint)
			}
			ctx.Printf("\n")
			clusterNum++
		}
		ctx.Printf("Re-run with --cluster and --address flags to select a specific cluster\n")
		return nil, "", fmt.Errorf("interactive mode not available")
	}

	// Create table picker items
	items := make([]ui.PickerItem, 0, len(clusters))
	clusterMap := make(map[string]*ClusterResponse)

	for i, cluster := range clusters {
		if len(cluster.APIAddresses) == 0 {
			continue // Skip clusters without API addresses
		}

		// Sort addresses to put localhost/0.0.0.0 last
		addresses := sortAddresses(cluster.APIAddresses)

		// Format primary address with grayed port
		address := formatAddressWithGrayPort(addresses[0])
		if len(addresses) > 1 {
			address = fmt.Sprintf("%s (+%d)", address, len(addresses)-1)
		}

		// Create table item
		itemID := fmt.Sprintf("cluster_%d", i)
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

	// Run the table picker
	selected, err := ui.RunPicker(items,
		ui.WithTitle("Select a cluster to bind:"),
		ui.WithHeaders([]string{"NAME", "ORGANIZATION", "ADDRESS"}),
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

// Define consistent styles for both list and modal
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
