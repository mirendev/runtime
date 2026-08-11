package diskio

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// UpdateKind names a producer format in the cloud's volume update stream.
//
// Every kind shares the same transport and the same ordering model: each update
// carries an ordering key that sorts lexicographically against its siblings of
// the same kind. What differs is the shape of that key.
type UpdateKind string

const (
	// KindLBDLog is a log segment written by the lbd kernel module in
	// accelerator mode. Its ordering key is the TAI64N label from the segment
	// filename, disk.<label>.log.
	KindLBDLog UpdateKind = "lbd_log"

	// KindLoopImage is a compressed snapshot of a universal-mode volume's
	// backing image. Its ordering key is a zero-padded 16-hex snapshot
	// sequence.
	KindLoopImage UpdateKind = "loop_image"
)

// UploadRequest describes one update to be uploaded.
type UploadRequest struct {
	Kind        UpdateKind
	OrderingKey string
	// EndKey bounds range-valued kinds. Neither kind here uses it; the cloud
	// rejects it for lbd_log and loop_image.
	EndKey string
	// SnapshotName names a restore point, pinning the update against cleanup.
	// It must be unique per volume among live updates.
	SnapshotName string
	// Metadata is kind-specific payload detail. The cloud writes it to a
	// sidecar object beside the payload rather than indexing it.
	Metadata map[string]any
	// LeaseNonce is required when the volume has an active lease.
	LeaseNonce string
}

// UpdateInfo describes one update the cloud is holding.
type UpdateInfo struct {
	UpdateID     string `json:"update_id"`
	Kind         string `json:"kind"`
	OrderingKey  string `json:"ordering_key"`
	EndKey       string `json:"end_key,omitempty"`
	Size         int64  `json:"size"`
	SnapshotName string `json:"snapshot_name,omitempty"`
}

// ListOptions filters a volume's update stream.
//
// After is exclusive and must be paired with a Kind: ordering keys are only
// comparable within one kind, so the cloud refuses the pair otherwise.
type ListOptions struct {
	Kind  UpdateKind
	After string
	Until string
}

// CloudUpdatesClient is the runtime's view of the cloud's volume update API.
type CloudUpdatesClient interface {
	// Upload sends one update and returns the cloud's ID for it. size must be
	// the exact byte length of body.
	Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error)

	// List returns a volume's active updates in replay order.
	List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error)

	// Download opens an update's payload. The caller closes it.
	Download(ctx context.Context, volumeID, updateID string) (io.ReadCloser, error)
}

// cloudUpdatesClient talks to /api/v1/disk/volumes/{volumeId}/updates.
type cloudUpdatesClient struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
}

// NewCloudUpdatesClient creates a client for the cloud's volume update API.
func NewCloudUpdatesClient(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) CloudUpdatesClient {
	return &cloudUpdatesClient{
		log:        log.With("module", "cloud-updates"),
		baseURL:    baseURL,
		authClient: authClient,
		// Generous: a loop image can be gigabytes.
		client: &http.Client{Timeout: 30 * time.Minute},
	}
}

type beginUploadRequestJSON struct {
	Kind         string `json:"kind"`
	OrderingKey  string `json:"ordering_key"`
	EndKey       string `json:"end_key,omitempty"`
	SnapshotName string `json:"snapshot_name,omitempty"`
	LeaseNonce   string `json:"lease_nonce,omitempty"`
}

type beginUploadResponseJSON struct {
	UpdateID    string `json:"update_id"`
	UploadURL   string `json:"upload_url"`
	CompleteURL string `json:"complete_url"`
}

type completeUploadRequestJSON struct {
	MD5        string         `json:"md5"`
	CRC32C     string         `json:"crc32c"`
	Size       int64          `json:"size"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	LeaseNonce string         `json:"lease_nonce,omitempty"`
}

type listUpdatesResponseJSON struct {
	Updates    []UpdateInfo `json:"updates"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type downloadResponseJSON struct {
	DownloadURL string `json:"download_url"`
}

// updatesPath builds the endpoint prefix for one volume's update stream.
func (c *cloudUpdatesClient) updatesPath(volumeID string, suffix ...string) (string, error) {
	parts := append([]string{c.baseURL, "api/v1/disk/volumes", volumeID, "updates"}, suffix...)
	return url.JoinPath(parts[0], parts[1:]...)
}

// authorize attaches a fresh bearer token.
func (c *cloudUpdatesClient) authorize(ctx context.Context, req *http.Request) error {
	token, err := c.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// doJSON sends a request and decodes a JSON response body into out.
func (c *cloudUpdatesClient) doJSON(ctx context.Context, method, apiURL string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.authorize(ctx, req); err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s failed with status %d: %s", method, apiURL, resp.StatusCode, string(payload))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

func (c *cloudUpdatesClient) Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error) {
	if req.Kind == "" {
		return "", fmt.Errorf("update kind is required")
	}
	if req.OrderingKey == "" {
		return "", fmt.Errorf("ordering key is required for kind %q", req.Kind)
	}
	if size <= 0 {
		return "", fmt.Errorf("size must be positive, got %d", size)
	}

	// Step 1: claim the ordering key and get somewhere to put the bytes
	beginURL, err := c.updatesPath(volumeID, "upload")
	if err != nil {
		return "", fmt.Errorf("failed to construct upload URL: %w", err)
	}

	var begun beginUploadResponseJSON
	err = c.doJSON(ctx, "POST", beginURL, beginUploadRequestJSON{
		Kind:         string(req.Kind),
		OrderingKey:  req.OrderingKey,
		EndKey:       req.EndKey,
		SnapshotName: req.SnapshotName,
		LeaseNonce:   req.LeaseNonce,
	}, &begun)
	if err != nil {
		return "", err
	}

	// Step 2: PUT the payload, hashing as it streams past
	md5h := md5.New()
	crch := crc32.New(crc32cTable)

	uploadReq, err := http.NewRequestWithContext(ctx, "PUT", begun.UploadURL,
		io.TeeReader(body, io.MultiWriter(md5h, crch)))
	if err != nil {
		return "", fmt.Errorf("failed to create data upload request: %w", err)
	}
	uploadReq.ContentLength = size

	uploadResp, err := c.client.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("failed to upload data: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode < 200 || uploadResp.StatusCode >= 300 {
		payload, _ := io.ReadAll(uploadResp.Body)
		return "", fmt.Errorf("data upload failed with status %d: %s", uploadResp.StatusCode, string(payload))
	}

	// Step 3: report what we sent so the cloud can verify it
	completeURL, err := c.resolveCompleteURL(begun.CompleteURL)
	if err != nil {
		return "", err
	}

	err = c.doJSON(ctx, "POST", completeURL, completeUploadRequestJSON{
		MD5:        encodeHash(md5h),
		CRC32C:     encodeHash(crch),
		Size:       size,
		Metadata:   req.Metadata,
		LeaseNonce: req.LeaseNonce,
	}, nil)
	if err != nil {
		return "", err
	}

	c.log.Info("update uploaded",
		"update_id", begun.UpdateID,
		"volume_id", volumeID,
		"kind", req.Kind,
		"ordering_key", req.OrderingKey,
		"size", size,
	)
	return begun.UpdateID, nil
}

func (c *cloudUpdatesClient) List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error) {
	// The cloud refuses after/until without a kind, since ordering keys only
	// compare within one. Catch it here with a clearer message.
	if (opts.After != "" || opts.Until != "") && opts.Kind == "" {
		return nil, fmt.Errorf("listing with after or until requires a kind")
	}

	baseURL, err := c.updatesPath(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to construct list URL: %w", err)
	}

	query := url.Values{}
	if opts.Kind != "" {
		query.Set("kind", string(opts.Kind))
	}
	if opts.After != "" {
		query.Set("after", opts.After)
	}
	if opts.Until != "" {
		query.Set("until", opts.Until)
	}

	// Walk every page: callers want the whole matching set, and the cursor is
	// an implementation detail of the transport.
	var all []UpdateInfo
	for {
		pageURL := baseURL
		if encoded := query.Encode(); encoded != "" {
			pageURL += "?" + encoded
		}

		var page listUpdatesResponseJSON
		if err := c.doJSON(ctx, "GET", pageURL, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Updates...)

		if page.NextCursor == "" {
			return all, nil
		}
		query.Set("cursor", page.NextCursor)
	}
}

func (c *cloudUpdatesClient) Download(ctx context.Context, volumeID, updateID string) (io.ReadCloser, error) {
	apiURL, err := c.updatesPath(volumeID, updateID, "download")
	if err != nil {
		return nil, fmt.Errorf("failed to construct download URL: %w", err)
	}

	var resolved downloadResponseJSON
	if err := c.doJSON(ctx, "GET", apiURL, nil, &resolved); err != nil {
		return nil, err
	}

	// The presigned URL carries its own authorization, so no bearer token here
	dataReq, err := http.NewRequestWithContext(ctx, "GET", resolved.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	dataResp, err := c.client.Do(dataReq)
	if err != nil {
		return nil, fmt.Errorf("failed to download update: %w", err)
	}
	if dataResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(dataResp.Body)
		dataResp.Body.Close()
		return nil, fmt.Errorf("update download failed with status %d: %s", dataResp.StatusCode, string(payload))
	}

	return dataResp.Body, nil
}

// resolveCompleteURL turns the cloud's completion URL into an absolute one,
// refusing to follow it somewhere other than the configured cloud.
func (c *cloudUpdatesClient) resolveCompleteURL(completeURL string) (string, error) {
	parsed, err := url.Parse(completeURL)
	if err == nil && parsed.IsAbs() {
		base, berr := url.Parse(c.baseURL)
		if berr != nil {
			return "", fmt.Errorf("failed to parse base URL: %w", berr)
		}
		if parsed.Scheme != base.Scheme || parsed.Host != base.Host {
			return "", fmt.Errorf("complete URL origin %s://%s does not match base URL origin %s://%s",
				parsed.Scheme, parsed.Host, base.Scheme, base.Host)
		}
		return completeURL, nil
	}

	joined, err := url.JoinPath(c.baseURL, completeURL)
	if err != nil {
		return "", fmt.Errorf("failed to construct complete URL: %w", err)
	}
	return joined, nil
}

// encodeHash renders a hash's digest the way the cloud compares it.
func encodeHash(h hash.Hash) string {
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
