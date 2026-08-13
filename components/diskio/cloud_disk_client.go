package diskio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// LogSegmentInfo describes a remote log segment with its cloud ID and TAI64N label.
type LogSegmentInfo struct {
	SegmentID string
	Label     string
}

// CloudDiskClient abstracts the cloud operations needed by the mount controller
// for volume lease management and log segment retrieval.
type CloudDiskClient interface {
	AcquireLease(ctx context.Context, volumeID string) (nonce string, err error)
	ReleaseLease(ctx context.Context, volumeID string, nonce string) error
	// ListLogSegments returns the volume's log segments whose TAI64N label
	// sorts after `after`. Passing the caller's replay horizon keeps the server
	// from walking, and returning, a volume's entire backup history. An empty
	// `after` asks for everything.
	ListLogSegments(ctx context.Context, volumeID string, after string) ([]LogSegmentInfo, error)
	DownloadLogSegment(ctx context.Context, volumeID, segmentID string) (io.ReadCloser, error)
}

// cloudDiskClient implements CloudDiskClient using the miren.cloud HTTP API.
//
// Lease operations are their own endpoints; log segments go through the generic
// volume update API as lbd_log updates.
type cloudDiskClient struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
	updates    CloudUpdatesClient
}

// NewCloudDiskClient creates a new CloudDiskClient.
func NewCloudDiskClient(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) CloudDiskClient {
	return NewCloudDiskClientWithUpdates(log, baseURL, authClient,
		NewCloudUpdatesClient(log, baseURL, authClient))
}

// NewCloudDiskClientWithUpdates builds a disk client over an existing updates
// client, so callers that already have one need not construct a second.
func NewCloudDiskClientWithUpdates(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient, updates CloudUpdatesClient) CloudDiskClient {
	return &cloudDiskClient{
		log:        log.With("module", "cloud-disk-client"),
		baseURL:    baseURL,
		authClient: authClient,
		client:     &http.Client{Timeout: 30 * time.Second},
		updates:    updates,
	}
}

type acquireLeaseRequest struct {
	Metadata map[string]any `json:"metadata,omitempty"`
}

type leaseResponse struct {
	LeaseID  string `json:"lease_id"`
	VolumeID string `json:"volume_id"`
	Nonce    string `json:"nonce"`
}

func (c *cloudDiskClient) AcquireLease(ctx context.Context, volumeID string) (string, error) {
	apiURL, err := url.JoinPath(c.baseURL, "api/v1/disk/volumes", volumeID, "lease")
	if err != nil {
		return "", fmt.Errorf("failed to construct lease URL: %w", err)
	}

	reqBody, err := json.Marshal(acquireLeaseRequest{})
	if err != nil {
		return "", fmt.Errorf("failed to marshal lease request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create lease request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	token, err := c.authClient.Authenticate(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to acquire lease: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		return "", fmt.Errorf("volume %s already has an active lease", volumeID)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("acquire lease failed with status %d: %s", resp.StatusCode, string(body))
	}

	var leaseResp leaseResponse
	if err := json.Unmarshal(body, &leaseResp); err != nil {
		return "", fmt.Errorf("failed to decode lease response: %w", err)
	}

	c.log.Info("acquired volume lease", "volume_id", volumeID, "lease_id", leaseResp.LeaseID)
	return leaseResp.Nonce, nil
}

func (c *cloudDiskClient) ReleaseLease(ctx context.Context, volumeID string, nonce string) error {
	apiURL, err := url.JoinPath(c.baseURL, "api/v1/disk/volumes", volumeID, "lease")
	if err != nil {
		return fmt.Errorf("failed to construct lease URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create release lease request: %w", err)
	}
	req.Header.Set("X-Lease-Nonce", nonce)

	token, err := c.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to release lease: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("release lease failed with status %d: %s", resp.StatusCode, string(body))
	}

	c.log.Info("released volume lease", "volume_id", volumeID)
	return nil
}

func (c *cloudDiskClient) ListLogSegments(ctx context.Context, volumeID string, after string) ([]LogSegmentInfo, error) {
	updates, err := c.updates.List(ctx, volumeID, ListOptions{
		Kind:  KindLBDLog,
		After: after,
	})
	if err != nil {
		return nil, fmt.Errorf("listing log segments: %w", err)
	}

	// An lbd_log update's ordering key is the segment's TAI64N label
	segments := make([]LogSegmentInfo, len(updates))
	for i, u := range updates {
		segments[i] = LogSegmentInfo{SegmentID: u.UpdateID, Label: u.OrderingKey}
	}
	return segments, nil
}

func (c *cloudDiskClient) DownloadLogSegment(ctx context.Context, volumeID, segmentID string) (io.ReadCloser, error) {
	return c.updates.Download(ctx, volumeID, segmentID)
}
