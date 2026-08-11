package diskio

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
	"time"

	"miren.dev/lbd"
	"miren.dev/runtime/pkg/cloudauth"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// CloudSegmentUploader implements LogSegmentUploader by uploading segments
// to miren.cloud using presigned URLs. It follows the same 3-step pattern
// as DiskAPISegmentAccess.NewSegment: request upload URL, PUT data, POST completion.
type CloudSegmentUploader struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
	state      *State
}

func NewCloudSegmentUploader(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient, state *State) *CloudSegmentUploader {
	return &CloudSegmentUploader{
		log:        log.With("module", "cloud-uploader"),
		baseURL:    baseURL,
		authClient: authClient,
		client:     &http.Client{Timeout: 60 * time.Second},
		state:      state,
	}
}

type logSegmentUploadRequest struct {
	VolumeID string `json:"volume_id"`
	// Label is the TAI64N label from the segment filename, disk.<label>.log.
	// The cloud stores it as the segment's ordering key and hands it back from
	// ListLogSegments, which is what replay sorts and horizon-compares on, so
	// it cannot be reconstructed server-side.
	Label string `json:"label"`
}

type logSegmentUploadResponse struct {
	SegmentID    string `json:"segment_id"`
	UploadURL    string `json:"upload_url"`
	CompletedURL string `json:"completed_url"`
}

type logSegmentCompleteRequest struct {
	MD5        string `json:"md5"`
	CRC32C     string `json:"crc32c"`
	Size       int64  `json:"size"`
	VolumeID   string `json:"volume_id"`
	LeaseNonce string `json:"lease_nonce,omitempty"`
}

// UploadSegment uploads a log segment file to the cloud and returns the cloud segment ID.
func (u *CloudSegmentUploader) UploadSegment(ctx context.Context, volumeID, segmentPath string) (string, error) {
	f, err := os.Open(segmentPath)
	if err != nil {
		return "", fmt.Errorf("failed to open segment file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat segment file: %w", err)
	}
	size := stat.Size()
	if size == 0 {
		return "", nil // skip empty files
	}

	label := lbd.LabelFromLogPath(segmentPath)
	if !isValidTAI64NLabel(label) {
		return "", fmt.Errorf("segment %q has no usable TAI64N label", segmentPath)
	}

	// Step 1: Request upload URL
	reqBody, err := json.Marshal(logSegmentUploadRequest{VolumeID: volumeID, Label: label})
	if err != nil {
		return "", fmt.Errorf("failed to marshal upload request: %w", err)
	}

	apiURL, err := url.JoinPath(u.baseURL, "api/v1/disk/log-segments/upload")
	if err != nil {
		return "", fmt.Errorf("failed to construct upload URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	token, err := u.authClient.Authenticate(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := u.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to request upload URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var uploadResp logSegmentUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", fmt.Errorf("failed to decode upload response: %w", err)
	}

	// Step 2: Upload the file data with hash computation
	md5h := md5.New()
	crch := crc32.New(crc32cTable)

	uploadReq, err := http.NewRequestWithContext(ctx, "PUT", uploadResp.UploadURL, io.TeeReader(f, io.MultiWriter(md5h, crch)))
	if err != nil {
		return "", fmt.Errorf("failed to create data upload request: %w", err)
	}
	uploadReq.ContentLength = size

	uploadHttpResp, err := u.client.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("failed to upload data: %w", err)
	}
	defer uploadHttpResp.Body.Close()

	if uploadHttpResp.StatusCode < 200 || uploadHttpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(uploadHttpResp.Body)
		return "", fmt.Errorf("data upload failed with status %d: %s", uploadHttpResp.StatusCode, string(body))
	}

	md5Hash := base64.StdEncoding.EncodeToString(md5h.Sum(nil))
	crc32cHash := base64.StdEncoding.EncodeToString(crch.Sum(nil))

	// Look up lease nonce for this volume from mount state
	var leaseNonce string
	if u.state != nil {
		for _, m := range u.state.ListMounts() {
			if m.VolumeId == volumeID && m.LeaseNonce != "" {
				leaseNonce = m.LeaseNonce
				break
			}
		}
	}

	// Step 3: Complete the upload
	completeReq := logSegmentCompleteRequest{
		MD5:        md5Hash,
		CRC32C:     crc32cHash,
		Size:       size,
		VolumeID:   volumeID,
		LeaseNonce: leaseNonce,
	}

	completeBody, err := json.Marshal(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal complete request: %w", err)
	}

	var completeURL string
	if parsedURL, parseErr := url.Parse(uploadResp.CompletedURL); parseErr == nil && parsedURL.IsAbs() {
		baseURL, _ := url.Parse(u.baseURL)
		if parsedURL.Scheme != baseURL.Scheme || parsedURL.Host != baseURL.Host {
			return "", fmt.Errorf("complete URL origin %s://%s does not match base URL origin %s://%s",
				parsedURL.Scheme, parsedURL.Host, baseURL.Scheme, baseURL.Host)
		}
		completeURL = uploadResp.CompletedURL
	} else {
		completeURL, err = url.JoinPath(u.baseURL, uploadResp.CompletedURL)
		if err != nil {
			return "", fmt.Errorf("failed to construct complete URL: %w", err)
		}
	}

	completeHttpReq, err := http.NewRequestWithContext(ctx, "POST", completeURL, bytes.NewReader(completeBody))
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}
	completeHttpReq.Header.Set("Content-Type", "application/json")

	token, err = u.authClient.Authenticate(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate for completion: %w", err)
	}
	completeHttpReq.Header.Set("Authorization", "Bearer "+token)

	completeHttpResp, err := u.client.Do(completeHttpReq)
	if err != nil {
		return "", fmt.Errorf("failed to complete upload: %w", err)
	}
	defer completeHttpResp.Body.Close()

	if completeHttpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(completeHttpResp.Body)
		return "", fmt.Errorf("complete upload failed with status %d: %s", completeHttpResp.StatusCode, string(body))
	}

	u.log.Info("log segment uploaded", "segment_id", uploadResp.SegmentID, "volume_id", volumeID, "size", size)
	return uploadResp.SegmentID, nil
}
