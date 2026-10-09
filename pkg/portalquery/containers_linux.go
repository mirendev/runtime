//go:build linux

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

func readContainers(ctx context.Context, name string) ([]ContainerInfo, error) {
	return dockerContainers(ctx, "/var/run/docker.sock", name)
}

func dockerContainers(ctx context.Context, socket, name string) ([]ContainerInfo, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Docker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query Docker: %s", resp.Status)
	}
	var rows []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		State string   `json:"State"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode Docker containers: %w", err)
	}
	result := make([]ContainerInfo, 0)
	for _, row := range rows {
		label := row.ID
		if len(row.Names) != 0 {
			label = strings.TrimPrefix(row.Names[0], "/")
		}
		if processNameMatches(name, label) {
			result = append(result, ContainerInfo{ID: row.ID, Name: label, Image: row.Image, State: row.State})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
