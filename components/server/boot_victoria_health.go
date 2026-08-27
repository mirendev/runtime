//go:build linux

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func waitForVictoriaHealth(ctx context.Context, name, address string) error {
	baseURL := strings.TrimRight(address, "/")
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	healthURL := baseURL + "/health"
	readyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("creating %s readiness request: %w", name, err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			err = fmt.Errorf("unexpected status %s", resp.Status)
		}
		lastErr = err

		select {
		case <-readyCtx.Done():
			return fmt.Errorf("%s did not become healthy at %s: %w", name, healthURL, lastErr)
		case <-time.After(250 * time.Millisecond):
		}
	}
}
