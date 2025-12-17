package commands

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// DoctorServer shows server health and connectivity details
func DoctorServer(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		if errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Printf("No clusters configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
			return nil
		}
		return err
	}

	clusterName := cfg.ActiveCluster()
	if opts.Cluster != "" {
		clusterName = opts.Cluster
	}

	cluster, err := cfg.GetCluster(clusterName)
	if err != nil {
		return err
	}

	// Check connectivity
	connected := false
	var connErr error
	client, connErr := ctx.RPCClient("entities")
	if connErr == nil && client != nil {
		defer client.Close()
		connected = true
	}

	status := "not connected"
	statusStyle := infoRed
	if connected {
		status = "connected"
		statusStyle = infoGreen
	}

	// Get host for HTTP checks
	host := cluster.Hostname
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	ctx.Printf("%s\n", infoBold.Render("Server"))
	ctx.Printf("%s\n", infoGray.Render("======"))
	ctx.Printf("%s   %s\n", infoLabel.Render("Cluster:"), clusterName)
	ctx.Printf("%s   %s\n", infoLabel.Render("Address:"), cluster.Hostname)
	ctx.Printf("%s    %s\n", infoLabel.Render("Status:"), statusStyle.Render(status))

	// Check HTTP endpoints
	ctx.Printf("\n%s\n", infoLabel.Render("Endpoints:"))

	// Check HTTPS (port 443)
	httpsStatus, httpsDetail := checkHTTPS(host)
	printEndpointStatus(ctx, "HTTPS", httpsStatus, httpsDetail)

	// Check HTTP (port 80)
	httpStatus, httpDetail := checkHTTP(host)
	printEndpointStatus(ctx, "HTTP", httpStatus, httpDetail)

	// Interactive prompts when not connected
	if !connected && ui.IsInteractive() {
		isLocal := isLocalCluster(cluster.Hostname)
		// Check for various connection failure messages
		connErrStr := ""
		if connErr != nil {
			connErrStr = connErr.Error()
		}
		isConnectionFailed := connErr != nil && (strings.Contains(connErrStr, "connection refused") ||
			strings.Contains(connErrStr, "timeout") ||
			strings.Contains(connErrStr, "no recent network activity"))

		ctx.Printf("\n")

		if isLocal && isConnectionFailed {
			// Local server not running - offer to start it
			ctx.Printf("Server isn't running. Start it? [Y/n] ")
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response == "" || response == "y" || response == "yes" {
				ctx.Printf("\n%s", infoGray.Render("Starting miren server..."))

				// Try systemctl first
				cmd := exec.Command("sudo", "systemctl", "start", "miren")
				if err := cmd.Run(); err != nil {
					// Systemd not available, try starting manually
					ctx.Printf("\n%s", infoGray.Render("systemctl not available, starting manually..."))
					cmd = exec.Command("sudo", "/var/lib/miren/release/miren", "server", "-vv", "--address=0.0.0.0:8443", "--serve-tls")
					cmd.Start()
				}

				// Wait for server to start
				ctx.Printf(" waiting for server")
				for i := 0; i < 10; i++ {
					time.Sleep(1 * time.Second)
					fmt.Print(".")
				}

				// Verify it started
				verifyClient, verifyErr := ctx.RPCClient("entities")
				if verifyErr == nil && verifyClient != nil {
					verifyClient.Close()
					ctx.Printf("\n%s\n", infoGreen.Render("✓ Server started"))
					ctx.Printf("%s    %s\n", infoLabel.Render("Status:"), infoGreen.Render("connected"))
				} else {
					ctx.Printf("\n%s\n", infoRed.Render("✗ Server failed to start"))
					ctx.Printf("%s\n", infoGray.Render("Check logs with: journalctl -u miren -f"))
				}
			}
		} else if !isLocal {
			// Remote server - offer to retry
			ctx.Printf("Cannot reach server. Retry connection? [Y/n] ")
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response == "" || response == "y" || response == "yes" {
				ctx.Printf("\n%s\n", infoGray.Render("Retrying..."))
				retryClient, retryErr := ctx.RPCClient("entities")
				if retryErr == nil && retryClient != nil {
					retryClient.Close()
					ctx.Printf("%s\n", infoGreen.Render("✓ Connected"))
					ctx.Printf("%s    %s\n", infoLabel.Render("Status:"), infoGreen.Render("connected"))
				} else {
					ctx.Printf("%s\n\n", infoRed.Render("✗ Still cannot connect"))

					// Offer to try a different server
					ctx.Printf("Try a different server? [Y/n] ")
					fmt.Scanln(&response)
					response = strings.TrimSpace(strings.ToLower(response))
					if response == "" || response == "y" || response == "yes" {
						// Build picker items from configured clusters
						var items []ui.PickerItem
						cfg.IterateClusters(func(name string, c *clientconfig.ClusterConfig) error {
							if name != clusterName { // Skip the current failing one
								items = append(items, ui.SimplePickerItem{
									Text: fmt.Sprintf("%-15s %s", name, c.Hostname),
								})
							}
							return nil
						})
						items = append(items, ui.SimplePickerItem{Text: "[back]"})

						if len(items) > 1 { // More than just [back]
							ctx.Printf("\n")
							selected, pickerErr := ui.RunPicker(items, ui.WithTitle("Select a server:"))
							if pickerErr == nil && selected != nil && selected.ID() != "[back]" {
								// Extract cluster name from selection
								parts := strings.Fields(selected.ID())
								if len(parts) > 0 {
									newClusterName := parts[0]
									ctx.Printf("\n%s\n", infoGray.Render("To connect to "+newClusterName+", run:"))
									ctx.Printf("  miren doctor server -C %s\n", newClusterName)
								}
							}
						} else {
							ctx.Printf("%s\n", infoGray.Render("No other clusters configured"))
						}
					}
				}
			}
		}
	}

	return nil
}

func printEndpointStatus(ctx *Context, name string, ok bool, detail string) {
	var indicator string
	if ok {
		indicator = infoGreen.Render("[✓]")
	} else {
		indicator = infoRed.Render("[✗]")
	}
	ctx.Printf("  %s %-6s %s\n", indicator, name, infoGray.Render(detail))
}

// checkHTTPS verifies HTTPS connectivity to the server
func checkHTTPS(host string) (bool, string) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Accept self-signed certs for diagnostics
			},
		},
	}

	url := fmt.Sprintf("https://%s/", host)
	resp, err := client.Get(url)
	if err != nil {
		return false, describeHTTPError(err)
	}
	defer resp.Body.Close()

	return true, fmt.Sprintf("responding (%d)", resp.StatusCode)
}

func describeHTTPError(err error) string {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	// Check for connection refused
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*net.OpError); ok {
			return sysErr.Err.Error()
		}
		return opErr.Err.Error()
	}
	return err.Error()
}

// checkHTTP verifies HTTP connectivity (typically redirects to HTTPS)
func checkHTTP(host string) (bool, string) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	url := fmt.Sprintf("http://%s/", host)
	resp, err := client.Get(url)
	if err != nil {
		return false, describeHTTPError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return true, fmt.Sprintf("redirecting (%d)", resp.StatusCode)
	}
	return true, fmt.Sprintf("responding (%d)", resp.StatusCode)
}
