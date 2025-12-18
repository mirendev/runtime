package commands

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
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
			ctx.Printf("\n%s\n", infoLabel.Render("To add a cluster:"))
			ctx.Printf("  %s\n", infoGray.Render("miren cluster add"))
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

	// Show suggestions when not connected
	if !connected {
		isLocal := isLocalCluster(cluster.Hostname)
		connErrStr := ""
		if connErr != nil {
			connErrStr = connErr.Error()
		}

		ctx.Printf("\n")

		if isLocal {
			// Local server suggestions
			if strings.Contains(connErrStr, "connection refused") {
				ctx.Printf("%s\n", infoLabel.Render("The local server doesn't appear to be running."))
				ctx.Printf("\n%s\n", infoLabel.Render("To start the server:"))
				ctx.Printf("  %s\n", infoGray.Render("sudo systemctl start miren"))
				ctx.Printf("\n%s\n", infoLabel.Render("To check server logs:"))
				ctx.Printf("  %s\n", infoGray.Render("sudo journalctl -u miren -f"))
			} else if strings.Contains(connErrStr, "timeout") || strings.Contains(connErrStr, "no recent network activity") {
				ctx.Printf("%s\n", infoLabel.Render("Connection timed out. The server may be starting up."))
				ctx.Printf("\n%s\n", infoLabel.Render("To check server status:"))
				ctx.Printf("  %s\n", infoGray.Render("sudo systemctl status miren"))
			} else {
				ctx.Printf("%s %s\n", infoLabel.Render("Connection error:"), connErrStr)
				ctx.Printf("\n%s\n", infoLabel.Render("To check server status:"))
				ctx.Printf("  %s\n", infoGray.Render("sudo systemctl status miren"))
			}
		} else {
			// Remote server suggestions
			ctx.Printf("%s\n", infoLabel.Render("Cannot reach the remote server."))
			if connErr != nil {
				ctx.Printf("%s %s\n", infoGray.Render("Error:"), connErrStr)
			}
			ctx.Printf("\n%s\n", infoLabel.Render("Suggestions:"))
			ctx.Printf("  • Check if the server is running on the remote host\n")
			ctx.Printf("  • Verify network connectivity to %s\n", cluster.Hostname)
			ctx.Printf("  • Check firewall rules allow traffic on port 8443\n")

			// Show other available clusters
			var otherClusters []string
			cfg.IterateClusters(func(name string, _ *clientconfig.ClusterConfig) error {
				if name != clusterName {
					otherClusters = append(otherClusters, name)
				}
				return nil
			})

			if len(otherClusters) > 0 {
				ctx.Printf("\n%s\n", infoLabel.Render("To try a different cluster:"))
				ctx.Printf("  %s\n", infoGray.Render("miren cluster switch <name>"))
				ctx.Printf("\n%s\n", infoLabel.Render("Available clusters:"))
				for _, name := range otherClusters {
					ctx.Printf("  • %s\n", name)
				}
			}
		}
	}

	// Interactive mode - offer help options
	if ui.IsInteractive() {
		ctx.Printf("\n")
		items := []ui.PickerItem{
			ui.SimplePickerItem{Text: "How do I start a local server?"},
			ui.SimplePickerItem{Text: "How do I connect to a known remote server?"},
			ui.SimplePickerItem{Text: "How do I fix https/http connectivity?"},
			ui.SimplePickerItem{Text: "[done]"},
		}

		selected, err := ui.RunPicker(items, ui.WithTitle("Need help?"))
		if err != nil || selected == nil || selected.ID() == "[done]" {
			return nil
		}

		switch selected.ID() {
		case "How do I start a local server?":
			showStartLocalServerHelp(ctx)
		case "How do I connect to a known remote server?":
			showConnectRemoteServerHelp(ctx)
		case "How do I fix https/http connectivity?":
			showFixConnectivityHelp(ctx)
		}
	}

	return nil
}

func showStartLocalServerHelp(ctx *Context) {
	printHelpHeader(ctx, "Starting a local miren server")
	printCommand(ctx, "To install as a systemd service:", "sudo miren server install")
	printCommand(ctx, "If already installed as a systemd service:", "sudo systemctl start miren")
	printCommand(ctx, "To run manually (foreground):", "sudo miren server")
	ctx.Printf("%s\n", infoLabel.Render("To check server logs:"))
	ctx.Printf("  %s\n", infoGray.Render("sudo journalctl -u miren -f"))
	waitForEnter(ctx)
}

func showConnectRemoteServerHelp(ctx *Context) {
	printHelpHeader(ctx, "Connecting to a known remote server")
	printCommand(ctx, "Add a cluster from miren.cloud:", "miren cluster add")
	printCommand(ctx, "Add a cluster manually by address:", "miren cluster add -a <hostname:port>")
	ctx.Printf("%s\n", infoLabel.Render("Switch to a different cluster:"))
	ctx.Printf("  %s\n", infoGray.Render("miren cluster switch <name>"))
	waitForEnter(ctx)
}

func showFixConnectivityHelp(ctx *Context) {
	printHelpHeader(ctx, "Fixing https/http connectivity")
	printCommand(ctx, "Check if the server is running:", "sudo systemctl status miren")
	ctx.Printf("%s\n", infoLabel.Render("Check firewall rules:"))
	ctx.Printf("  %s\n", infoGray.Render("sudo ufw status"))
	ctx.Printf("  %s\n\n", infoGray.Render("sudo ufw allow 443/tcp"))
	printCommand(ctx, "Check if ports are listening:", "sudo lsof -i :443")
	ctx.Printf("%s\n", infoLabel.Render("Test connectivity:"))
	ctx.Printf("  %s\n", infoGray.Render("curl -k https://<hostname>/"))
	waitForEnter(ctx)
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
				InsecureSkipVerify: true,             // Accept self-signed certs for diagnostics
				MinVersion:         tls.VersionTLS12, // Enforce minimum TLS version
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
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
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
