package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
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
	if !isInteractive() {
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

	// Create list items
	items := make([]list.Item, 0, len(clusters))
	for i, cluster := range clusters {
		if len(cluster.APIAddresses) == 0 {
			continue // Skip clusters without API addresses
		}

		// Build multi-line description with all details
		var descLines []string

		// Organization
		descLines = append(descLines, fmt.Sprintf("Organization: %s", cluster.OrganizationName))

		// Description if available
		if cluster.Description != "" {
			descLines = append(descLines, fmt.Sprintf("Description: %s", cluster.Description))
		}

		// API Addresses - show all of them
		if len(cluster.APIAddresses) > 0 {
			if len(cluster.APIAddresses) == 1 {
				descLines = append(descLines, fmt.Sprintf("Address: %s", cluster.APIAddresses[0]))
			} else {
				descLines = append(descLines, fmt.Sprintf("Addresses (%d):", len(cluster.APIAddresses)))
				for j, addr := range cluster.APIAddresses {
					if j < 3 { // Show first 3 addresses
						descLines = append(descLines, fmt.Sprintf("  • %s", addr))
					}
				}
				if len(cluster.APIAddresses) > 3 {
					descLines = append(descLines, fmt.Sprintf("  • ... and %d more", len(cluster.APIAddresses)-3))
				}
			}
		}

		// Certificate fingerprint if available
		if cluster.CACertFingerprint != "" {
			// Show first and last 8 chars of fingerprint for readability
			fp := cluster.CACertFingerprint
			if len(fp) > 20 {
				fp = fmt.Sprintf("%s...%s", fp[:8], fp[len(fp)-8:])
			}
			descLines = append(descLines, fmt.Sprintf("Certificate: %s", fp))
		}

		// Tags if available
		if len(cluster.Tags) > 0 {
			var tagStrs []string
			for k, v := range cluster.Tags {
				tagStrs = append(tagStrs, fmt.Sprintf("%s=%v", k, v))
				if len(tagStrs) >= 3 {
					break // Only show first 3 tags
				}
			}
			descLines = append(descLines, fmt.Sprintf("Tags: %s", strings.Join(tagStrs, ", ")))
		}

		items = append(items, listItem{
			title: cluster.Name,
			desc:  strings.Join(descLines, "\n"),
			index: i, // Store the cluster index for direct lookup
		})
	}

	// Create the list model
	const defaultWidth = 100 // Increased width for more detail
	const listHeight = 20    // Increased height since items are taller

	delegate := &customDelegate{}
	l := list.New(items, delegate, defaultWidth, listHeight)
	l.Title = "Select a cluster to bind"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.Styles.Title = listTitleStyle
	l.Styles.HelpStyle = modalHelpStyle

	m := clusterListModel{
		list:     l,
		clusters: clusters,
		selected: -1,
		state:    "selecting",
	}

	// Run the interactive selection
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return nil, "", fmt.Errorf("failed to run cluster selection: %w", err)
	}

	model := result.(clusterListModel)
	if model.cancelled {
		return nil, "", fmt.Errorf("cluster selection cancelled")
	}

	if model.selected < 0 || model.selected >= len(clusters) {
		return nil, "", fmt.Errorf("invalid selection")
	}

	// Return both the selected cluster and the local name
	return &clusters[model.selected], model.localName, nil
}

type listItem struct {
	title, desc string
	index       int // Store the original cluster index for direct lookup
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

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

	// List styles
	listTitleStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Padding(0, 0, 1, 0)

	listItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(primaryColor).
				Background(lipgloss.Color("236")).
				Bold(true)

	listItemTitleStyle = lipgloss.NewStyle().
				Foreground(primaryColor)

	listItemDescStyle = lipgloss.NewStyle().
				Foreground(secondaryColor)
)

// customDelegate implements list.ItemDelegate with our consistent styling
type customDelegate struct{}

func (d customDelegate) Height() int {
	// Calculate height based on typical content (title + multi-line desc)
	return 7 // Increased to accommodate more detail
}
func (d customDelegate) Spacing() int                            { return 1 }
func (d customDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d customDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(listItem)
	if !ok {
		return
	}

	// Render title with bold styling
	title := listItemTitleStyle.Bold(true).Render(i.title)

	// Render description lines with proper indentation
	descLines := strings.Split(i.desc, "\n")
	var styledDesc []string
	for _, line := range descLines {
		styledDesc = append(styledDesc, listItemDescStyle.Render(line))
	}
	desc := strings.Join(styledDesc, "\n")

	// Combine title and description
	content := lipgloss.JoinVertical(lipgloss.Left, title, desc)

	// Apply selection styling
	if index == m.Index() {
		// Add selection indicator and background
		lines := strings.Split(content, "\n")
		var selected []string
		for idx, line := range lines {
			if idx == 0 {
				// Add arrow to title line
				selected = append(selected, selectedItemStyle.Render("▸ "+line))
			} else {
				// Indent other lines under selection
				selected = append(selected, selectedItemStyle.Render("  "+line))
			}
		}
		content = strings.Join(selected, "\n")
	} else {
		// Regular item with padding
		lines := strings.Split(content, "\n")
		var padded []string
		for _, line := range lines {
			padded = append(padded, listItemStyle.Render(line))
		}
		content = strings.Join(padded, "\n")
	}

	fmt.Fprint(w, content)
}

type clusterListModel struct {
	list         list.Model
	textInput    textinput.Model
	clusters     []ClusterResponse
	selected     int
	cancelled    bool
	state        string // "selecting" or "naming"
	localName    string // The final chosen local name
	windowWidth  int
	windowHeight int
}

func (m clusterListModel) Init() tea.Cmd {
	return nil
}

func (m clusterListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size updates in any state
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 2)
	}

	// Handle different states
	if m.state == "naming" {
		// In naming state, handle text input
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c":
				m.cancelled = true
				return m, tea.Quit
			case "esc":
				// Go back to selection
				m.state = "selecting"
				return m, nil
			case "enter":
				value := m.textInput.Value()
				if value == "" {
					// Use placeholder if empty
					value = m.textInput.Placeholder
				}
				// Validate the name
				if strings.ContainsAny(value, "/\\:*?\"<>|") {
					// Show error but stay in naming mode
					// We'll handle this in the view
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

	// In selecting state, handle list selection
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if i, ok := m.list.SelectedItem().(listItem); ok {
				// Use the stored index for direct cluster lookup
				m.selected = i.index
				cluster := m.clusters[i.index]
				// Switch to naming mode
				m.state = "naming"
				// Initialize text input with cluster name
				m.textInput = textinput.New()
				m.textInput.Placeholder = cluster.Name
				m.textInput.SetValue(cluster.Name)
				m.textInput.Focus()
				m.textInput.CharLimit = 100
				m.textInput.Width = 50
				m.textInput.Prompt = "Local name: "
				return m, textinput.Blink
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m clusterListModel) View() string {
	if m.state == "naming" {
		// Create the modal content
		var modalContent strings.Builder

		// Title
		title := "Choose Local Name"
		modalContent.WriteString(modalTitleStyle.Render(title))
		modalContent.WriteString("\n\n")

		// Show selected cluster info
		if m.selected >= 0 && m.selected < len(m.clusters) {
			cluster := m.clusters[m.selected]
			info := fmt.Sprintf("Cluster: %s\nOrganization: %s", cluster.Name, cluster.OrganizationName)
			if cluster.Description != "" {
				info += fmt.Sprintf("\nDescription: %s", cluster.Description)
			}
			modalContent.WriteString(modalSubtitleStyle.Render(info))
			modalContent.WriteString("\n\n")
		}

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
		modalContent.WriteString(modalHelpStyle.Render("Enter: confirm • Esc: back • Ctrl+C: cancel"))

		// Apply modal styling
		modal := modalStyle.Render(modalContent.String())

		// If window size is not set yet, just return the modal
		if m.windowWidth == 0 || m.windowHeight == 0 {
			return modal
		}

		// Create the overlay effect
		// Place the modal centered on the screen with a subtle background
		return lipgloss.Place(
			m.windowWidth,
			m.windowHeight,
			lipgloss.Center,
			lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(lipgloss.Color("236")),
		)
	}

	// In selecting state, show the list
	return m.list.View()
}

// isInteractive checks if we're in an interactive terminal
func isInteractive() bool {
	if os.Getenv("CI") != "" {
		return false
	}

	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

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
