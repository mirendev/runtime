package diskio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// CloudVolumeRegistrar creates a volume in miren.cloud for a local disk, so the
// backup paths have something to address.
//
// Without this a volume only exists on the node, and every cloud call carries a
// local id the cloud has never heard of.
type CloudVolumeRegistrar interface {
	// EnsureVolume registers the volume and returns its cloud identifier. It is
	// safe to call repeatedly: the same local volume always resolves to the
	// same cloud volume.
	EnsureVolume(ctx context.Context, req RegisterVolumeRequest) (string, error)
}

// RegisterVolumeRequest describes a local volume to register.
type RegisterVolumeRequest struct {
	// LocalVolumeID is the node's identifier for the volume. It seeds the
	// cloud volume's name and UUID, which is what makes registration
	// idempotent across retries.
	LocalVolumeID string
	// ClusterID scopes the name, so two clusters in one organization that each
	// hold a disk called "data" do not land on the same cloud volume and
	// interleave their backups.
	ClusterID string
	// DisplayName is the operator-facing disk name, recorded as metadata.
	DisplayName string
	SizeBytes   int64
	Filesystem  string
}

// cloudVolumeRegistrar talks to /api/v1/disk/volumes.
type cloudVolumeRegistrar struct {
	log        *slog.Logger
	baseURL    string
	authClient *cloudauth.AuthClient
	client     *http.Client
}

// NewCloudVolumeRegistrar creates a registrar against the given cloud.
func NewCloudVolumeRegistrar(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) CloudVolumeRegistrar {
	return &cloudVolumeRegistrar{
		log:        log.With("module", "cloud-volume-registrar"),
		baseURL:    baseURL,
		authClient: authClient,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

type createVolumeRequestJSON struct {
	Name         string            `json:"name"`
	UUID         string            `json:"uuid"`
	DeclaredSize int64             `json:"declared_size"`
	DataFormat   string            `json:"data_format,omitempty"`
	AppFormat    string            `json:"app_format,omitempty"`
	Segments     []string          `json:"segments"`
	BlockMap     blockMapJSON      `json:"block_map"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type blockMapJSON struct {
	Format string `json:"format"`
	Data   string `json:"data"`
}

type createVolumeResponseJSON struct {
	VolumeID string `json:"volume_id"`
}

func (r *cloudVolumeRegistrar) EnsureVolume(ctx context.Context, req RegisterVolumeRequest) (string, error) {
	if req.LocalVolumeID == "" {
		return "", fmt.Errorf("local volume ID is required")
	}
	if req.SizeBytes <= 0 {
		return "", fmt.Errorf("volume size must be positive, got %d", req.SizeBytes)
	}

	apiURL, err := url.JoinPath(r.baseURL, "api/v1/disk/volumes")
	if err != nil {
		return "", fmt.Errorf("failed to construct volume URL: %w", err)
	}

	// The cloud treats CreateVolume as idempotent by name and enforces a
	// globally unique UUID, so both have to be stable functions of the local
	// volume rather than anything generated per call.
	body, err := json.Marshal(createVolumeRequestJSON{
		Name:         cloudVolumeName(req.ClusterID, req.LocalVolumeID),
		UUID:         deterministicVolumeUUID(req.ClusterID, req.LocalVolumeID),
		DeclaredSize: req.SizeBytes,
		AppFormat:    req.Filesystem,
		Segments:     []string{},
		BlockMap:     blockMapJSON{Format: "none", Data: ""},
		Metadata: map[string]string{
			"cluster_id":      req.ClusterID,
			"local_volume_id": req.LocalVolumeID,
			"disk_name":       req.DisplayName,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal volume request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create volume request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	token, err := r.authClient.Authenticate(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to register volume: %w", err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registering volume failed with status %d: %s", resp.StatusCode, string(payload))
	}

	var created createVolumeResponseJSON
	if err := json.Unmarshal(payload, &created); err != nil {
		return "", fmt.Errorf("failed to decode volume response: %w", err)
	}
	if created.VolumeID == "" {
		return "", fmt.Errorf("cloud returned no volume id")
	}

	r.log.Info("registered volume with miren.cloud",
		"local_volume_id", req.LocalVolumeID,
		"cloud_volume_id", created.VolumeID,
	)
	return created.VolumeID, nil
}

// cloudVolumeName scopes a volume's name to its cluster. The cloud resolves
// CreateVolume by name within an organization, so an unscoped name would let
// two clusters share one volume and interleave their backups.
func cloudVolumeName(clusterID, localVolumeID string) string {
	if clusterID == "" {
		return localVolumeID
	}
	return clusterID + "-" + localVolumeID
}

// deterministicVolumeUUID derives a stable UUID from the cluster and local
// volume. The cloud enforces a globally unique UUID, so a fresh one per attempt
// would turn a retry into a constraint violation.
func deterministicVolumeUUID(clusterID, localVolumeID string) string {
	sum := sha256.Sum256([]byte("miren-disk-volume:" + clusterID + ":" + localVolumeID))

	var raw [16]byte
	copy(raw[:], sum[:16])
	// Stamp version 4 and the RFC 4122 variant so the value parses as a UUID
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
