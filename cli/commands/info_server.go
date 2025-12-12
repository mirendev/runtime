package commands

import (
	"errors"
	"fmt"
	"net"
	"time"

	"miren.dev/runtime/clientconfig"
)

// InfoServer shows server health and connectivity details
func InfoServer(ctx *Context, opts struct {
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
		connected = true
	}

	status := "not connected"
	statusStyle := infoRed
	if connected {
		status = "connected"
		statusStyle = infoGreen
	}

	// Check port availability
	host := cluster.Hostname
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	ports := []struct {
		port  int
		proto string
		desc  string
	}{
		{8443, "udp", "API - CLI and RPC"},
		{443, "tcp", "HTTPS - application traffic"},
		{80, "tcp", "HTTP - redirects and ACME"},
	}

	ctx.Printf("%s\n", infoBold.Render("Server"))
	ctx.Printf("%s\n", infoGray.Render("======"))
	ctx.Printf("%s   %s\n", infoLabel.Render("Cluster:"), clusterName)
	ctx.Printf("%s   %s\n", infoLabel.Render("Address:"), cluster.Hostname)
	ctx.Printf("%s    %s\n", infoLabel.Render("Status:"), statusStyle.Render(status))

	ctx.Printf("\n%s\n", infoLabel.Render("Ports:"))
	for _, p := range ports {
		open := checkPortProto(host, p.port, p.proto)
		var indicator, portStatus string
		if open {
			indicator = infoGreen.Render("[✓]")
			portStatus = "open"
		} else {
			indicator = infoRed.Render("[✗]")
			portStatus = "closed"
		}
		protoLabel := infoGray.Render(fmt.Sprintf("/%s", p.proto))
		ctx.Printf("  %s %d%s  %-6s %s\n", indicator, p.port, protoLabel, portStatus, infoGray.Render(p.desc))
	}

	return nil
}

func checkPortProto(host string, port int, proto string) bool {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout(proto, address, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
