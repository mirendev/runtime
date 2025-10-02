package lsvd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/units"
)

type DiskAPISegmentAccess struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
}

func NewDiskAPISegmentAccess(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) *DiskAPISegmentAccess {
	return &DiskAPISegmentAccess{
		log:        log.With("module", "cloud-disk"),
		baseURL:    baseURL,
		authClient: authClient,
		client:     &http.Client{},
	}
}

// API request/response types based on the OpenAPI spec
type SegmentUploadRequest struct {
	LsvdID   string `json:"lsvd_id"`
	VolumeID string `json:"volume_id,omitempty"`
}

type SegmentUploadResponse struct {
	SegmentID    string    `json:"segment_id"`
	UploadURL    string    `json:"upload_url"`
	CompletedURL string    `json:"completed_url"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type CompleteUploadRequest struct {
	MD5         string `json:"md5"`
	CRC32C      string `json:"crc32c"`
	Size        int64  `json:"size"`
	BlockLayout string `json:"block_layout,omitempty"`
	VolumeID    string `json:"volume_id,omitempty"`
}

type CompleteSegmentUploadResponse struct {
	SegmentID string `json:"segment_id"`
	Status    string `json:"status"`
}

type SegmentDownloadResponse struct {
	DownloadURL string    `json:"download_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Size        int64     `json:"size"`
	MD5         string    `json:"md5"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// Volume API types
type CreateVolumeRequest struct {
	Name         string         `json:"name"`
	UUID         string         `json:"uuid"`
	DeclaredSize int64          `json:"declared_size"`
	DataFormat   string         `json:"data_format,omitempty"`
	AppFormat    string         `json:"app_format,omitempty"`
	Segments     []string       `json:"segments"`
	BlockMap     map[string]any `json:"block_map"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type VolumeResponse struct {
	VolumeID  string `json:"volume_id"`
	VersionID string `json:"version_id"`
}

type ListVolumesResponse struct {
	Volumes    []VolumeInfoResponse `json:"volumes"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type VolumeInfoResponse struct {
	VolumeID     string    `json:"volume_id"`
	Name         string    `json:"name"`
	UUID         string    `json:"uuid"`
	DeclaredSize int64     `json:"declared_size"`
	DataFormat   string    `json:"data_format,omitempty"`
	AppFormat    string    `json:"app_format,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DeleteSegmentResponse struct {
	LsvdID string `json:"lsvd_id"`
	Status string `json:"status"`
}

func (d *DiskAPISegmentAccess) InitContainer(ctx context.Context) error {
	// The disk API manages storage container initialization on the server side,
	// so no client-side initialization is needed
	return nil
}

func (d *DiskAPISegmentAccess) InitVolume(ctx context.Context, vol *VolumeInfo) error {
	if err := vol.Normalize(); err != nil {
		return fmt.Errorf("failed to normalize volume info: %w", err)
	}

	req := CreateVolumeRequest{
		Name:         vol.Name,
		UUID:         vol.UUID,
		DeclaredSize: int64(vol.Size),
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal create volume request: %w", err)
	}

	apiURL, err := url.JoinPath(d.baseURL, "api/v1/disk/volumes")
	if err != nil {
		return fmt.Errorf("failed to construct volume URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create volume request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Get fresh auth token
	token, err := d.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to create volume: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create volume failed with status %d: %s", resp.StatusCode, string(body))
	}

	var volumeResp VolumeResponse
	if err := json.NewDecoder(resp.Body).Decode(&volumeResp); err != nil {
		return fmt.Errorf("failed to decode volume response: %w", err)
	}

	d.log.Info("volume created", "volume_id", volumeResp.VolumeID, "version_id", volumeResp.VersionID)
	return nil
}

func (d *DiskAPISegmentAccess) ListVolumes(ctx context.Context) ([]string, error) {
	apiURL, err := url.JoinPath(d.baseURL, "api/v1/disk/volumes")
	if err != nil {
		return nil, fmt.Errorf("failed to construct volumes URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list volumes request: %w", err)
	}

	// Get fresh auth token
	token, err := d.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list volumes failed with status %d: %s", resp.StatusCode, string(body))
	}

	var listResp ListVolumesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode list volumes response: %w", err)
	}

	var volumes []string
	for _, vol := range listResp.Volumes {
		volumes = append(volumes, vol.Name)
	}

	return volumes, nil
}

func (d *DiskAPISegmentAccess) RemoveSegment(ctx context.Context, seg SegmentId) error {
	apiURL, err := url.JoinPath(d.baseURL, "api/v1/disk/segments", seg.String())
	if err != nil {
		return fmt.Errorf("failed to construct segment URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete segment request: %w", err)
	}

	// Get fresh auth token
	token, err := d.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete segment failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deleteResp DeleteSegmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return fmt.Errorf("failed to decode delete segment response: %w", err)
	}

	d.log.Info("segment deleted", "lsvd_id", deleteResp.LsvdID, "status", deleteResp.Status)
	return nil
}

func (d *DiskAPISegmentAccess) OpenVolume(ctx context.Context, vol string) (Volume, error) {
	return &DiskAPIVolume{
		access: d,
		name:   vol,
	}, nil
}

func (d *DiskAPISegmentAccess) GetVolumeInfo(ctx context.Context, vol string) (*VolumeInfo, error) {
	// Use the direct API endpoint to get volume info
	apiURL, err := url.JoinPath(d.baseURL, "api/v1/disk/volumes", vol)
	if err != nil {
		return nil, fmt.Errorf("failed to construct volume URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get volume request: %w", err)
	}

	// Get fresh auth token
	token, err := d.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("volume %s not found", vol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read volume response body: %w", err)
	}

	spew.Dump(body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get volume failed with status %d: %s", resp.StatusCode, string(body))
	}

	var volInfo VolumeInfoResponse
	if err := json.Unmarshal(body, &volInfo); err != nil {
		return nil, fmt.Errorf("failed to decode volume response: %w", err)
	}

	return &VolumeInfo{
		Name: volInfo.Name,
		Size: units.Bytes(volInfo.DeclaredSize),
		UUID: volInfo.UUID,
	}, nil
}

var _ SegmentAccess = (*DiskAPISegmentAccess)(nil)

type DiskAPIVolume struct {
	access *DiskAPISegmentAccess
	name   string
}

func (v *DiskAPIVolume) Info(ctx context.Context) (*VolumeInfo, error) {
	// Use the parent access GetVolumeInfo which properly fetches full volume info
	return v.access.GetVolumeInfo(ctx, v.name)
}

func (v *DiskAPIVolume) ListSegments(ctx context.Context) ([]SegmentId, error) {
	// Use the new direct endpoint for latest volume segments - returns 302 redirect
	apiURL, err := url.JoinPath(v.access.baseURL, "api/v1/disk/volumes", v.name, "latest/segments")
	if err != nil {
		return nil, fmt.Errorf("failed to construct segments URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create segments request: %w", err)
	}

	// Get fresh auth token
	token, err := v.access.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// Don't follow redirects automatically - we need to get the Location header
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get segments URL: %w", err)
	}
	defer resp.Body.Close()

	// API always returns 302 redirect with Location header
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("expected 302 redirect, got status %d: %s", resp.StatusCode, string(body))
	}

	segmentsURL := resp.Header.Get("Location")
	if segmentsURL == "" {
		return nil, fmt.Errorf("redirect response missing Location header")
	}

	// Fetch the actual segments list from the presigned URL
	segReq, err := http.NewRequestWithContext(ctx, "GET", segmentsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create segments fetch request: %w", err)
	}

	segResp, err := v.access.client.Do(segReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch segments: %w", err)
	}
	defer segResp.Body.Close()

	if segResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(segResp.Body)
		return nil, fmt.Errorf("failed to fetch segments: status %d, body: %s", segResp.StatusCode, string(body))
	}

	// Read and parse the segments list
	data, err := io.ReadAll(segResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read segments: %w", err)
	}

	// Try JSON array format first
	var segmentIDs []string
	if err := json.Unmarshal(data, &segmentIDs); err == nil {
		segments := make([]SegmentId, 0, len(segmentIDs))
		for _, id := range segmentIDs {
			seg, err := ParseSegment(id)
			if err != nil {
				v.access.log.Warn("failed to parse segment ID", "id", id, "error", err)
				continue
			}
			segments = append(segments, seg)
		}
		return segments, nil
	}

	// Fall back to binary format
	return ReadSegmentList(bytes.NewReader(data))
}

func (v *DiskAPIVolume) OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error) {
	apiURL, err := url.JoinPath(v.access.baseURL, "api/v1/disk/segments", seg.String(), "download")
	if err != nil {
		return nil, fmt.Errorf("failed to construct download URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	// Get fresh auth token
	token, err := v.access.authClient.Authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.access.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var downloadResp SegmentDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}

	return &DiskAPISegmentReader{
		ctx:         ctx,
		client:      v.access.client,
		downloadURL: downloadResp.DownloadURL,
		expiresAt:   downloadResp.ExpiresAt,
		size:        downloadResp.Size,
		md5:         downloadResp.MD5,
		log:         v.access.log,
		volume:      v,
		segmentID:   seg,
	}, nil
}

var crc32ctable = crc32.MakeTable(crc32.Castagnoli)

func (v *DiskAPIVolume) NewSegment(ctx context.Context, seg SegmentId, layout *SegmentLayout, data *os.File) error {
	if data == nil {
		return fmt.Errorf("data file is required")
	}

	size := int64(0)
	if stat, err := data.Stat(); err == nil {
		size = stat.Size()
	} else {
		return fmt.Errorf("failed to stat data file: %w", err)
	}

	if size == 0 {
		return fmt.Errorf("data file is empty")
	}

	// Step 1: Request upload URL
	req := SegmentUploadRequest{
		LsvdID:   seg.String(),
		VolumeID: v.name,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal upload request: %w", err)
	}

	apiURL, err := url.JoinPath(v.access.baseURL, "api/v1/disk/segments/upload")
	if err != nil {
		return fmt.Errorf("failed to construct upload URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Get fresh auth token
	token, err := v.access.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.access.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to request upload URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var uploadResp SegmentUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return fmt.Errorf("failed to decode upload response: %w", err)
	}

	// Step 2: Upload the file data
	data.Seek(0, io.SeekStart)

	// Calculate MD5 and size
	md5h := md5.New()
	crch := crc32.New(crc32ctable)

	// Upload using PUT to the presigned URL
	uploadReq, err := http.NewRequestWithContext(ctx, "PUT", uploadResp.UploadURL, io.TeeReader(data, io.MultiWriter(md5h, crch)))
	if err != nil {
		return fmt.Errorf("failed to create data upload request: %w", err)
	}

	uploadHttpResp, err := v.access.client.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("failed to upload data: %w", err)
	}
	defer uploadHttpResp.Body.Close()

	if uploadHttpResp.StatusCode < 200 || uploadHttpResp.StatusCode >= 300 {
		v.access.log.Error("data upload failed",
			"url", uploadResp.UploadURL,
			"segment_id", uploadResp.SegmentID,
			"expires_at", uploadResp.ExpiresAt,
			"size", size,
			"status", uploadHttpResp.StatusCode)

		// Try to read response body for more info
		body, _ := io.ReadAll(uploadHttpResp.Body)
		return fmt.Errorf("data upload failed with status %d: %s", uploadHttpResp.StatusCode, string(body))
	}

	// Calculate checksums AFTER the upload (when data has been read through TeeReader)
	md5Hash := base64.StdEncoding.EncodeToString(md5h.Sum(nil))
	crc32cHash := base64.StdEncoding.EncodeToString(crch.Sum(nil))

	// Step 3: Complete the upload
	completeReq := CompleteUploadRequest{
		MD5:      md5Hash,
		CRC32C:   crc32cHash,
		Size:     size,
		VolumeID: v.name,
	}

	completeBody, err := json.Marshal(completeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal complete request: %w", err)
	}

	// CompletedURL might be a full URL or a relative path
	var completeURL string
	if parsedURL, err := url.Parse(uploadResp.CompletedURL); err == nil && parsedURL.IsAbs() {
		// It's already a full URL, use it directly
		completeURL = uploadResp.CompletedURL
	} else {
		// It's a relative path, join it with the base URL
		completeURL, err = url.JoinPath(v.access.baseURL, uploadResp.CompletedURL)
		if err != nil {
			return fmt.Errorf("failed to construct complete URL: %w", err)
		}
	}

	completeHttpReq, err := http.NewRequestWithContext(ctx, "POST", completeURL, bytes.NewReader(completeBody))
	if err != nil {
		return fmt.Errorf("failed to create complete request: %w", err)
	}

	completeHttpReq.Header.Set("Content-Type", "application/json")

	// Get fresh auth token for completion request
	token, err = v.access.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate for completion: %w", err)
	}
	completeHttpReq.Header.Set("Authorization", "Bearer "+token)

	completeHttpResp, err := v.access.client.Do(completeHttpReq)
	if err != nil {
		return fmt.Errorf("failed to complete upload: %w", err)
	}
	defer completeHttpResp.Body.Close()

	if completeHttpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(completeHttpResp.Body)
		return fmt.Errorf("complete upload failed with status %d: %s", completeHttpResp.StatusCode, string(body))
	}

	v.access.log.Info("segment upload completed", "segment_id", uploadResp.SegmentID, "size", size)
	return nil
}

func (v *DiskAPIVolume) RemoveSegment(ctx context.Context, seg SegmentId) error {
	apiURL, err := url.JoinPath(v.access.baseURL, "api/v1/disk/volumes", v.name, "segments", seg.String())
	if err != nil {
		return fmt.Errorf("failed to construct segment URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete segment request: %w", err)
	}

	token, err := v.access.authClient.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.access.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete segment failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deleteResp DeleteSegmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return fmt.Errorf("failed to decode delete segment response: %w", err)
	}

	v.access.log.Info("segment deleted", "lsvd_id", deleteResp.LsvdID, "status", deleteResp.Status, "volume", v.name)
	return nil
}

var _ Volume = (*DiskAPIVolume)(nil)

type DiskAPISegmentReader struct {
	ctx         context.Context
	client      *http.Client
	downloadURL string
	expiresAt   time.Time
	size        int64
	md5         string
	log         *slog.Logger

	// Store info needed to re-request download URL
	volume    *DiskAPIVolume
	segmentID SegmentId
	mu        sync.Mutex // Protect concurrent access to downloadURL
}

// refreshDownloadURL requests a new download URL if the current one has expired
func (r *DiskAPISegmentReader) refreshDownloadURL() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if we need to refresh (with 5 minute buffer before expiry)
	if time.Now().Add(5 * time.Minute).Before(r.expiresAt) {
		return nil // URL is still valid
	}

	r.log.Info("refreshing expired download URL", "segment_id", r.segmentID.String(), "old_expires_at", r.expiresAt)

	// Re-request the download URL
	apiURL, err := url.JoinPath(r.volume.access.baseURL, "api/v1/disk/segments", r.segmentID.String(), "download")
	if err != nil {
		return fmt.Errorf("failed to construct download URL: %w", err)
	}

	req, err := http.NewRequestWithContext(r.ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	// Get fresh auth token
	token, err := r.volume.access.authClient.Authenticate(r.ctx)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := r.volume.access.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var downloadResp SegmentDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return fmt.Errorf("failed to decode download response: %w", err)
	}

	// Update the fields
	r.downloadURL = downloadResp.DownloadURL
	r.expiresAt = downloadResp.ExpiresAt
	r.log.Info("refreshed download URL", "segment_id", r.segmentID.String(), "new_expires_at", r.expiresAt)

	return nil
}

func (r *DiskAPISegmentReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.size {
		return 0, io.EOF
	}

	end := off + int64(len(p)) - 1
	if end >= r.size {
		end = r.size - 1
	}

	// Try the read, and retry once if it fails with an authorization error
	for attempt := range 2 {
		// Check and refresh URL if needed before making request
		if err := r.refreshDownloadURL(); err != nil {
			return 0, fmt.Errorf("failed to refresh download URL: %w", err)
		}

		rangeHeader := fmt.Sprintf("bytes=%d-%d", off, end)

		req, err := http.NewRequestWithContext(r.ctx, "GET", r.downloadURL, nil)
		if err != nil {
			return 0, fmt.Errorf("failed to create range request: %w", err)
		}

		req.Header.Set("Range", rangeHeader)

		resp, err := r.client.Do(req)
		if err != nil {
			return 0, fmt.Errorf("failed to make range request: %w", err)
		}
		defer resp.Body.Close()

		// Check if request succeeded
		if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK {
			n, err := io.ReadFull(resp.Body, p[:end-off+1])
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return n, fmt.Errorf("failed to read response body: %w", err)
			}
			return n, nil
		}

		// Check if we got an auth error that might indicate expired URL
		if attempt == 0 && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			r.log.Warn("download URL might be expired, forcing refresh", "status", resp.StatusCode)
			// Force refresh by setting expiry to past
			r.mu.Lock()
			r.expiresAt = time.Now().Add(-time.Minute)
			r.mu.Unlock()
			continue // Retry with refreshed URL
		}

		// Other error, don't retry
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("range request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return 0, fmt.Errorf("failed to read after retrying with refreshed URL")
}

func (r *DiskAPISegmentReader) Close() error {
	return nil
}

func (r *DiskAPISegmentReader) Layout(ctx context.Context) (*SegmentLayout, error) {
	// Layout retrieval not implemented in Disk API
	// The client will read the segment header to calculate the layout.
	// TODO cache the layout after reading once
	return nil, nil
}

var _ SegmentReader = (*DiskAPISegmentReader)(nil)
