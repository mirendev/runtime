package commands

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"miren.dev/runtime/clientconfig"
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
	client, err := ctx.RPCClient("entities")
	if err == nil && client != nil {
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
