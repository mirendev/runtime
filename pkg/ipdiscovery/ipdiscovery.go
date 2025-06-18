package ipdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Discovery holds information about discovered IP addresses
type Discovery struct {
	PublicIP  string    `json:"public_ip"`
	Addresses []Address `json:"addresses"`
}

// Address represents an IP address associated with a network interface
type Address struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	Network   string `json:"network"`
	IsIPv6    bool   `json:"is_ipv6"`
}

// PublicIPResponse represents the response from ifconfig.co/json
type PublicIPResponse struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	City    string `json:"city"`
}

// Discover gathers all local interface addresses and the public IP
func Discover(ctx context.Context, log *slog.Logger) (*Discovery, error) {
	discovery := &Discovery{
		Addresses: []Address{},
	}

	// Get local interface addresses
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces: %w", err)
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			var network string

			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
				network = v.String()
			case *net.IPAddr:
				ip = v.IP
				network = v.String()
			default:
				continue
			}

			// Skip loopback addresses if desired
			if ip.IsLoopback() {
				continue
			}

			address := Address{
				Interface: iface.Name,
				IP:        ip.String(),
				Network:   network,
				IsIPv6:    ip.To4() == nil,
			}

			discovery.Addresses = append(discovery.Addresses, address)
		}
	}

	// Get public IP
	publicIP, err := getPublicIP(ctx)
	if err != nil {
		// Don't fail the entire discovery if we can't get public IP
		log.Warn("Failed to get public IP", "error", err)
		discovery.PublicIP = ""
	} else {
		discovery.PublicIP = publicIP
	}

	return discovery, nil
}

// getPublicIP fetches the public IP address from ifconfig.co
func getPublicIP(ctx context.Context) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://ifconfig.co/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent to avoid being blocked
	req.Header.Set("User-Agent", "miren-runtime/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public IP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result PublicIPResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.IP, nil
}

// DiscoverWithTimeout is a convenience function that adds a timeout to Discover
func DiscoverWithTimeout(timeout time.Duration, log *slog.Logger) (*Discovery, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Discover(ctx, log)
}
