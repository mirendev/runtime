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
type cloudDiskClient struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
}

// NewCloudDiskClient creates a new CloudDiskClient.
func NewCloudDiskClient(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) CloudDiskClient {
	return &cloudDiskClient{
		log:        log.With("module", "cloud-disk-client"),
		baseURL:    baseURL,
		authClient: authClient,
		client:     &http.Client{Timeout: 30 * time.Second},
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

type listLogSegmentsResponse struct {
	Segments []logSegmentInfoJSON `json:"segments"`
}

type logSegmentInfoJSON struct {
	SegmentID string `json:"segment_id"`
	Label     string `json:"label"`
}

func (c *cloudDiskClient) ListLogSegments(ctx context.Context, volumeID string, after string) ([]LogSegmentInfo, error) {
	apiURL, err := url.JoinPath(c.baseURL, "api/v1/disk/log-segments")
	if err != nil {
		return nil, fmt.Errorf("failed to construct log segments URL: %w", err)
	}

	query := url.Values{"volume_id": {volumeID}}
	if after != "" {
		query.Set("after", after)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list log segments request: %w", err)
	}

	token, err := c.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list log segments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list log segments failed with status %d: %s", resp.StatusCode, string(body))
	}

	var listResp listLogSegmentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode log segments response: %w", err)
	}

	segments := make([]LogSegmentInfo, len(listResp.Segments))
	for i, seg := range listResp.Segments {
		segments[i] = LogSegmentInfo(seg)
	}
	return segments, nil
}

type logSegmentDownloadResponse struct {
	DownloadURL string `json:"download_url"`
}

func (c *cloudDiskClient) DownloadLogSegment(ctx context.Context, volumeID, segmentID string) (io.ReadCloser, error) {
	apiURL, err := url.JoinPath(c.baseURL, "api/v1/disk/log-segments", segmentID, "download")
	if err != nil {
		return nil, fmt.Errorf("failed to construct download URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	token, err := c.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var downloadResp logSegmentDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}

	// Fetch the actual segment data from the presigned URL
	dataReq, err := http.NewRequestWithContext(ctx, "GET", downloadResp.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create data download request: %w", err)
	}

	dataResp, err := c.client.Do(dataReq)
	if err != nil {
		return nil, fmt.Errorf("failed to download segment data: %w", err)
	}

	if dataResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(dataResp.Body)
		dataResp.Body.Close()
		return nil, fmt.Errorf("segment data download failed with status %d: %s", dataResp.StatusCode, string(body))
	}

	return dataResp.Body, nil
}
