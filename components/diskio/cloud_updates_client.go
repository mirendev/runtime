package diskio

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
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

	// KindLBDCheckpoint completion atomically activates its recovery coverage.
	KindLBDCheckpoint UpdateKind = "lbd_checkpoint"

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
	// Checkpoint binds checkpoint activation to the uploaded bytes and log head.
	// Completion must acknowledge it exactly; ordinary upload success is not proof.
	Checkpoint *CheckpointPublication
}

// UpdateInfo describes one update the cloud is holding.
type UpdateInfo struct {
	UpdateID      string `json:"update_id"`
	Kind          string `json:"kind"`
	OrderingKey   string `json:"ordering_key"`
	EndKey        string `json:"end_key,omitempty"`
	Size          int64  `json:"size"`
	SnapshotName  string `json:"snapshot_name,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	PreviousLabel string `json:"previous_label,omitempty"`
	FirstSequence uint64 `json:"first_sequence,omitempty"`
	EntryCount    uint64 `json:"entry_count,omitempty"`
	Boundary      bool   `json:"boundary,omitempty"`
}

// maxUpdateListPages caps a cursor walk. The server pages at 100 by default, so
// this allows a very long stream while still terminating if paging misbehaves.
const maxUpdateListPages = 1000

// ListOptions filters a volume's update stream.
//
// After is exclusive and must be paired with a Kind: ordering keys are only
// comparable within one kind, so the cloud refuses the pair otherwise.
type ListOptions struct {
	Kind  UpdateKind
	After string
	Until string

	// Descending returns newest first instead of replay order. Paired with
	// Limit 1 it answers "what is the most recent update" in one round trip,
	// rather than paging a volume's whole history to keep the last row.
	Descending bool

	// Limit caps how many updates come back. Zero walks every page, which is
	// what replay wants; a positive value stops as soon as it has enough.
	Limit int

	// Protected recovery uses the same cursor walk, bound to one restore session.
	restoreSession string
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
	// payloads sends PUT bodies. It has no overall timeout: the stall timer
	// bounds a transfer that stops, so a large one on a slow link can finish.
	payloads  *http.Client
	preparing chan struct{}
}

// NewCloudUpdatesClient creates a client for the cloud's volume update API.
func NewCloudUpdatesClient(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient) CloudUpdatesClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Preparation holds at most uploadConcurrency connections, which leaves
	// one for each volume's publisher so control requests never queue behind
	// payload transfers and spend their deadline waiting for a connection.
	transport.MaxIdleConnsPerHost = uploadConcurrency + logVolumeConcurrency
	transport.MaxConnsPerHost = uploadConcurrency + logVolumeConcurrency
	return &cloudUpdatesClient{
		log:        log.With("module", "cloud-updates"),
		baseURL:    baseURL,
		authClient: authClient,
		preparing:  make(chan struct{}, uploadConcurrency),
		// Generous: a loop image can be gigabytes.
		client:   &http.Client{Timeout: 30 * time.Minute, Transport: transport},
		payloads: &http.Client{Transport: transport},
	}
}

type beginUploadRequestJSON struct {
	Kind            string `json:"kind"`
	OrderingKey     string `json:"ordering_key"`
	EndKey          string `json:"end_key,omitempty"`
	SnapshotName    string `json:"snapshot_name,omitempty"`
	LeaseNonce      string `json:"lease_nonce,omitempty"`
	MD5             string `json:"md5,omitempty"`
	CRC32C          string `json:"crc32c,omitempty"`
	Size            int64  `json:"size,omitempty"`
	RecoveryVersion int    `json:"recovery_version,omitempty"`
	// StreamID lets cloud refuse a conflicting checkpoint stream before any
	// bytes are sent. It is set only for lbd_checkpoint.
	StreamID string `json:"stream_id,omitempty"`
}

type beginUploadResponseJSON struct {
	UpdateID        string `json:"update_id"`
	UploadURL       string `json:"upload_url"`
	CompleteURL     string `json:"complete_url"`
	SupportsPrepare bool   `json:"supports_prepare"`
	AlreadyActive   bool   `json:"already_active"`
	Prepared        bool   `json:"prepared"`
	Kind            string `json:"kind"`
	OrderingKey     string `json:"ordering_key"`
	MD5             string `json:"md5"`
	CRC32C          string `json:"crc32c"`
	Size            int64  `json:"size"`
	Publication     *struct {
		Version    int `json:"version"`
		MaxEntries int `json:"max_entries"`
	} `json:"prepared_prefix_publication"`
	RecoveryVersion int `json:"recovery_version,omitempty"`
}

type completeUploadRequestJSON struct {
	MD5         string                 `json:"md5"`
	CRC32C      string                 `json:"crc32c"`
	Size        int64                  `json:"size"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
	LeaseNonce  string                 `json:"lease_nonce,omitempty"`
	PrepareOnly bool                   `json:"prepare_only,omitempty"`
	Checkpoint  *CheckpointPublication `json:"checkpoint,omitempty"`
}

// Preparation does the slow storage work without exposing an update to replay.
// Only CompleteUpload publishes; callers must invoke it in ordering-key order.
type stagedUpdatesClient interface {
	PrepareUpload(context.Context, string, UploadRequest, io.ReadSeeker, int64) (*preparedUpload, error)
	CompleteUpload(context.Context, *preparedUpload) (string, error)
}

type preparedUpload struct {
	updateID    string
	completeURL string
	completion  completeUploadRequestJSON
	volumeID    string
	kind        UpdateKind
	orderingKey string
	active      bool
	batchLimit  int
}

type publicationBoundary struct {
	UpdateID    string `json:"update_id"`
	OrderingKey string `json:"ordering_key"`
}

type prefixUpdatesClient interface {
	PublishPrepared(context.Context, *publicationBoundary, []*preparedUpload) (*publicationBoundary, error)
}

var errPreparationUnsupported = errors.New("cloud does not support ordered upload preparation")

// Old clouds lack the recovery extensions. Callers keep the legacy behavior
// rather than failing, which is safe only while the cloud retains every log.
var (
	errCheckpointUnsupported = errors.New("cloud does not support accelerator checkpoints")
	errRecoveryUnsupported   = errors.New("cloud does not support protected accelerator recovery")
	// errCheckpointStreamConflict means the cloud already has another stream
	// for the volume. Retrying the same local stream can never succeed.
	errCheckpointStreamConflict = errors.New("cloud refused the checkpoint's stream")
	// errCheckpointClaimRefused is any other 409 at checkpoint begin, such as a
	// duplicate key or a differing payload. The staged claim cannot succeed,
	// but the volume's stream may still be fine.
	errCheckpointClaimRefused = errors.New("cloud refused the checkpoint claim")
)

// streamConflict reports a checkpoint stream refusal. Only the explicit code
// counts, at begin and completion alike: a plain 409 also covers duplicate
// keys, constraint violations and differing payloads or metadata, and treating
// those as a conflict would stop checkpoints for the volume for good.
func streamConflict(err error, atBegin bool) error {
	httpErr, ok := errors.AsType[*cloudUpdateHTTPError](err)
	switch {
	case !ok || httpErr.status != http.StatusConflict:
		return err
	case httpErr.code == "stream_conflict":
		return fmt.Errorf("%w: %v", errCheckpointStreamConflict, err)
	case atBegin:
		return fmt.Errorf("%w: %v", errCheckpointClaimRefused, err)
	}
	return err
}

// unsupportedStatus reports a refusal of an unknown route, or with
// unknownKind also of an unknown update kind.
func unsupportedStatus(err error, unknownKind bool) bool {
	httpErr, ok := errors.AsType[*cloudUpdateHTTPError](err)
	return ok && (httpErr.status == http.StatusNotFound || httpErr.status == http.StatusNotImplemented ||
		(unknownKind && httpErr.status == http.StatusBadRequest))
}

type cloudUpdateHTTPError struct {
	status     int
	code       string
	message    string
	retryAfter time.Duration
}

func (e *cloudUpdateHTTPError) Error() string { return e.message }

func updateHTTPError(resp *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	apiURL := *resp.Request.URL
	apiURL.RawQuery, apiURL.Fragment = "", ""
	var delay time.Duration
	if seconds, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
		// Cap before conversion so an invalid header cannot overflow Duration.
		delay = time.Duration(min(seconds, 86400)) * time.Second
	} else if until, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		delay = max(0, time.Until(until))
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(payload, &body)
	return &cloudUpdateHTTPError{status: resp.StatusCode, code: body.Code, retryAfter: delay,
		message: fmt.Sprintf("%s %s failed with status %d: %s", resp.Request.Method, apiURL.Redacted(), resp.StatusCode, payload)}
}

type listUpdatesResponseJSON struct {
	Updates    []UpdateInfo      `json:"updates"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Recovery   *RecoveryManifest `json:"recovery,omitempty"`
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

// invalidateOnUnauthorized drops the cached token when cloud rejects it, so the
// caller's next retry authenticates again. Without this a token cloud no longer
// accepts, such as after a signing-key rotation, is reused until it expires and
// every disk upload, lease and registration fails in the meantime.
func invalidateOnUnauthorized(authClient *cloudauth.AuthClient, resp *http.Response) {
	if resp.StatusCode == http.StatusUnauthorized && authClient != nil {
		authClient.InvalidateToken()
	}
}

// jsonRequestTimeout bounds each small control request. Payload bytes travel
// only through the PUT, which is bounded by uploadStallTimeout instead.
const jsonRequestTimeout = 2 * time.Minute

// uploadStallTimeout cancels a PUT that stops sending; a variable for tests.
var uploadStallTimeout = 2 * time.Minute

// progressReader reports each successful read so a stall timer can be reset.
type progressReader struct {
	r        io.Reader
	progress func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.progress()
	}
	return n, err
}

// doJSON sends a request and decodes a JSON response body into out.
func (c *cloudUpdatesClient) doJSON(ctx context.Context, method, apiURL string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, jsonRequestTimeout)
	defer cancel()
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
	started := time.Now()
	defer func() {
		c.log.Debug("cloud update request", "method", method, "path", req.URL.Path, "duration", time.Since(started))
	}()
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
	invalidateOnUnauthorized(c.authClient, resp)

	if resp.StatusCode != http.StatusOK {
		return updateHTTPError(resp)
	}

	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

func (c *cloudUpdatesClient) Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error) {
	prepared, err := c.uploadPayload(ctx, volumeID, req, body, size, false)
	if err != nil {
		return "", err
	}
	return c.CompleteUpload(ctx, prepared)
}

func (c *cloudUpdatesClient) PrepareUpload(ctx context.Context, volumeID string, req UploadRequest, body io.ReadSeeker, size int64) (*preparedUpload, error) {
	select {
	case c.preparing <- struct{}{}:
		defer func() { <-c.preparing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.uploadPayload(ctx, volumeID, req, body, size, true)
}

func (c *cloudUpdatesClient) PublishPrepared(ctx context.Context, head *publicationBoundary, prepared []*preparedUpload) (*publicationBoundary, error) {
	first := prepared[0]
	type entry struct {
		publicationBoundary
		MD5      string         `json:"md5"`
		CRC32C   string         `json:"crc32c"`
		Size     int64          `json:"size"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	request := struct {
		Kind         UpdateKind           `json:"kind"`
		LeaseNonce   string               `json:"lease_nonce"`
		ExpectedHead *publicationBoundary `json:"expected_head"`
		Entries      []entry              `json:"entries"`
	}{Kind: first.kind, LeaseNonce: first.completion.LeaseNonce, ExpectedHead: head}
	for _, p := range prepared {
		request.Entries = append(request.Entries, entry{
			publicationBoundary: publicationBoundary{p.updateID, p.orderingKey},
			MD5:                 p.completion.MD5, CRC32C: p.completion.CRC32C, Size: p.completion.Size, Metadata: p.completion.Metadata,
		})
	}
	var response struct {
		Version  int                   `json:"version"`
		Entries  []publicationBoundary `json:"entries"`
		Terminal publicationBoundary   `json:"terminal"`
	}
	apiURL, err := c.updatesPath(first.volumeID, "publish-prepared")
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(ctx, "POST", apiURL, request, &response); err != nil {
		return nil, err
	}
	if response.Version != 1 || len(response.Entries) != len(prepared) {
		return nil, fmt.Errorf("cloud did not acknowledge the publication manifest")
	}
	for i, p := range prepared {
		if response.Entries[i] != (publicationBoundary{p.updateID, p.orderingKey}) {
			return nil, fmt.Errorf("cloud acknowledged a different publication identity")
		}
	}
	if response.Terminal != response.Entries[len(prepared)-1] {
		return nil, fmt.Errorf("cloud acknowledged a different publication boundary")
	}
	c.log.Debug("updates uploaded", "volume_id", first.volumeID, "count", len(prepared), "ordering_key", response.Terminal.OrderingKey)
	return &response.Terminal, nil
}

func (c *cloudUpdatesClient) CompleteUpload(ctx context.Context, prepared *preparedUpload) (string, error) {
	var response struct {
		Checkpoint *CheckpointPublication `json:"checkpoint"`
	}
	var out any
	if prepared.completion.Checkpoint != nil {
		out = &response
	}
	if err := c.doJSON(ctx, "POST", prepared.completeURL, prepared.completion, out); err != nil {
		if prepared.completion.Checkpoint != nil {
			return "", streamConflict(err, false)
		}
		return "", err
	}
	if prepared.completion.Checkpoint != nil && (response.Checkpoint == nil || *response.Checkpoint != *prepared.completion.Checkpoint) {
		return "", fmt.Errorf("cloud did not authenticate the exact checkpoint activation")
	}
	c.log.Info("update uploaded", "update_id", prepared.updateID, "volume_id", prepared.volumeID,
		"kind", prepared.kind, "ordering_key", prepared.orderingKey, "size", prepared.completion.Size)
	return prepared.updateID, nil
}

func (c *cloudUpdatesClient) uploadPayload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64, prepareOnly bool) (*preparedUpload, error) {
	if req.Kind == "" {
		return nil, fmt.Errorf("update kind is required")
	}
	if req.OrderingKey == "" {
		return nil, fmt.Errorf("ordering key is required for kind %q", req.Kind)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size must be positive, got %d", size)
	}

	// Bind the claim before sending bytes. The cloud can then recover the same
	// identity after a lost begin response, failed PUT, or process restart.
	md5h := md5.New()
	crch := crc32.New(crc32cTable)
	claim := beginUploadRequestJSON{
		Kind: string(req.Kind), OrderingKey: req.OrderingKey, EndKey: req.EndKey,
		SnapshotName: req.SnapshotName, LeaseNonce: req.LeaseNonce,
	}
	if req.Kind == KindLBDCheckpoint {
		if req.Checkpoint == nil || req.Checkpoint.Version != AcceleratorRecoveryVersion || req.Checkpoint.VolumeID != volumeID || req.LeaseNonce == "" {
			return nil, fmt.Errorf("checkpoint upload requires recovery coverage and an owned lease")
		}
		claim.RecoveryVersion = AcceleratorRecoveryVersion
		claim.StreamID = req.Checkpoint.StreamID
	} else if req.Checkpoint != nil {
		return nil, fmt.Errorf("checkpoint coverage requires lbd_checkpoint kind")
	}
	if seeker, ok := body.(io.ReadSeeker); ok {
		position, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(io.MultiWriter(md5h, crch), seeker)
		if err != nil {
			return nil, err
		}
		if n != size {
			return nil, fmt.Errorf("payload size %d does not match declared size %d", n, size)
		}
		if _, err := seeker.Seek(position, io.SeekStart); err != nil {
			return nil, err
		}
		claim.MD5, claim.CRC32C, claim.Size = encodeHash(md5h), encodeHash(crch), size
		md5h.Reset()
		crch.Reset()
	} else if prepareOnly {
		return nil, fmt.Errorf("preparation requires a rewindable payload")
	}

	beginURL, err := c.updatesPath(volumeID, "upload")
	if err != nil {
		return nil, fmt.Errorf("failed to construct upload URL: %w", err)
	}

	var begun beginUploadResponseJSON
	err = c.doJSON(ctx, "POST", beginURL, claim, &begun)
	if req.Checkpoint != nil && unsupportedStatus(err, true) {
		return nil, fmt.Errorf("%w: %v", errCheckpointUnsupported, err)
	}
	if req.Checkpoint != nil && err != nil {
		return nil, streamConflict(err, true)
	}
	if err != nil {
		return nil, err
	}
	// Old servers ignore prepare_only. Refuse BEFORE the completion request,
	// otherwise a concurrent preparation could publish a replay-visible hole.
	if prepareOnly && !begun.SupportsPrepare {
		return nil, errPreparationUnsupported
	}
	if req.Checkpoint != nil && begun.RecoveryVersion != AcceleratorRecoveryVersion {
		return nil, errCheckpointUnsupported
	}
	if begun.UpdateID == "" {
		return nil, fmt.Errorf("cloud returned an empty upload identity")
	}
	if begun.CompleteURL == "" {
		begun.CompleteURL, err = c.updatesPath(volumeID, begun.UpdateID, "complete")
		if err != nil {
			return nil, err
		}
	}
	completeURL, err := c.resolveCompleteURL(begun.CompleteURL)
	if err != nil {
		return nil, err
	}
	prepared := &preparedUpload{updateID: begun.UpdateID, completeURL: completeURL,
		volumeID: volumeID, kind: req.Kind, orderingKey: req.OrderingKey, active: begun.AlreadyActive,
		completion: completeUploadRequestJSON{MD5: claim.MD5, CRC32C: claim.CRC32C, Size: size,
			Metadata: req.Metadata, LeaseNonce: req.LeaseNonce}}
	if begun.Publication != nil && begun.Publication.Version == 1 && req.Kind == KindLBDLog && req.LeaseNonce != "" {
		prepared.batchLimit = min(uploadWindow, begun.Publication.MaxEntries)
	}
	if req.Checkpoint != nil {
		checkpoint := *req.Checkpoint
		checkpoint.Base.UpdateID = begun.UpdateID
		prepared.completion.Checkpoint = &checkpoint
	}
	// Prepared bytes are immutable and have no upload URL. Reuse the bound
	// identity, but still authenticate publication through CompleteUpload.
	if begun.AlreadyActive || begun.Prepared {
		if claim.MD5 == "" || begun.Kind != string(req.Kind) || begun.OrderingKey != req.OrderingKey ||
			begun.MD5 != claim.MD5 || begun.CRC32C != claim.CRC32C || begun.Size != size {
			return nil, fmt.Errorf("existing update does not match local payload")
		}
		return prepared, nil
	}

	// Bound a stalled transfer, not a large or slow one: a fixed deadline
	// would fail the same oversized segment forever and wedge its volume.
	putCtx, cancelPut := context.WithCancel(ctx)
	defer cancelPut()
	stall := time.AfterFunc(uploadStallTimeout, cancelPut)
	defer stall.Stop()
	uploadReq, err := http.NewRequestWithContext(putCtx, "PUT", begun.UploadURL,
		&progressReader{r: io.TeeReader(body, io.MultiWriter(md5h, crch)), progress: func() { stall.Reset(uploadStallTimeout) }})
	if err != nil {
		return nil, fmt.Errorf("failed to create data upload request: %w", err)
	}
	uploadReq.ContentLength = size

	uploadResp, err := c.payloads.Do(uploadReq)
	if err != nil {
		// url.Error renders the presigned URL, signature included.
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}
		return nil, fmt.Errorf("failed to upload data: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode < 200 || uploadResp.StatusCode >= 300 {
		return nil, updateHTTPError(uploadResp)
	}
	if _, err := io.Copy(io.Discard, uploadResp.Body); err != nil {
		return nil, err
	}
	uploadResp.Body.Close()

	prepared.completion.MD5, prepared.completion.CRC32C = encodeHash(md5h), encodeHash(crch)
	if claim.MD5 != "" && (claim.MD5 != prepared.completion.MD5 || claim.CRC32C != prepared.completion.CRC32C) {
		return nil, fmt.Errorf("payload changed during upload")
	}
	if prepareOnly {
		completion := prepared.completion
		completion.PrepareOnly = true
		// A dedicated endpoint fails closed on an old replica; an old /complete
		// handler could ignore prepare_only and accidentally publish out of order.
		prepareURL, err := c.updatesPath(volumeID, begun.UpdateID, "prepare")
		if err != nil {
			return nil, err
		}
		var response struct {
			Prepared bool   `json:"prepared"`
			UpdateID string `json:"update_id"`
		}
		if err := c.doJSON(ctx, "POST", prepareURL, completion, &response); err != nil {
			return nil, err
		}
		if !response.Prepared || response.UpdateID != begun.UpdateID {
			return nil, fmt.Errorf("cloud did not confirm preparation")
		}
	}
	return prepared, nil
}

func (c *cloudUpdatesClient) List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error) {
	return c.list(ctx, volumeID, opts, nil)
}

func (c *cloudUpdatesClient) list(ctx context.Context, volumeID string, opts ListOptions, recovery *RecoveryManifest) ([]UpdateInfo, error) {
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
	if opts.Descending {
		query.Set("order", "desc")
	}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if recovery != nil {
		query.Set("restore_session", opts.restoreSession)
		query.Set("recovery_version", strconv.Itoa(AcceleratorRecoveryVersion))
	}

	// Walk every page: callers want the whole matching set, and the cursor is
	// an implementation detail of the transport.
	// The server's cursor strictly advances, but a client that trusts that
	// without checking will spin forever against one that regresses, growing
	// `all` until it runs out of memory. Refuse to walk in circles.
	var all []UpdateInfo
	seen := make(map[string]struct{})

	for pages := 0; pages < maxUpdateListPages; pages++ {
		pageURL := baseURL
		if encoded := query.Encode(); encoded != "" {
			pageURL += "?" + encoded
		}

		var page listUpdatesResponseJSON
		if err := c.doJSON(ctx, "GET", pageURL, nil, &page); err != nil {
			return nil, err
		}
		if recovery != nil {
			if page.Recovery == nil || page.Recovery.SessionID != opts.restoreSession || page.Recovery.VolumeID != volumeID || page.Recovery.Version != AcceleratorRecoveryVersion {
				return nil, fmt.Errorf("cloud did not bind update listing to the restore selection")
			}
			if pages == 0 {
				*recovery = *page.Recovery
			} else if !reflect.DeepEqual(recovery, page.Recovery) {
				return nil, fmt.Errorf("recovery selection changed between update pages")
			}
		}
		all = append(all, page.Updates...)

		// A caller that asked for a bounded number stops as soon as it has
		// them, rather than following the cursor to the end of the history.
		if opts.Limit > 0 && len(all) >= opts.Limit {
			return all[:opts.Limit], nil
		}

		if page.NextCursor == "" {
			return all, nil
		}
		if _, repeated := seen[page.NextCursor]; repeated {
			return nil, fmt.Errorf("listing updates for volume %q: server repeated cursor %q",
				volumeID, page.NextCursor)
		}
		seen[page.NextCursor] = struct{}{}
		query.Set("cursor", page.NextCursor)
	}

	return nil, fmt.Errorf("listing updates for volume %q: more than %d pages",
		volumeID, maxUpdateListPages)
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

// Restore sessions are shared protection for immutable update selections. The
// cloud must implement these MIR-2183 semantics before enabling retention.
type restoreSession struct {
	Version   int       `json:"version"`
	VolumeID  string    `json:"volume_id"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// BeginRecovery selects and protects a base plus tail. A non-empty until
// selects the history as of that log label instead of the committed head.
func (c *cloudUpdatesClient) BeginRecovery(ctx context.Context, volume, nonce, until string) (*RecoveryManifest, error) {
	endpoint, err := c.updatesPath(volume, "restore-sessions")
	if err != nil {
		return nil, err
	}
	body := map[string]any{"version": AcceleratorRecoveryVersion, "lease_nonce": nonce, "kind": KindLBDCheckpoint}
	if until != "" {
		body["until"] = until
	}
	var session restoreSession
	if err := c.doJSON(ctx, "POST", endpoint, body, &session); err != nil {
		// A cloud without this route answers 405 rather than 404: the path
		// also matches DELETE /updates/{updateId}. A v1 cloud has the route.
		httpErr, _ := errors.AsType[*cloudUpdateHTTPError](err)
		if unsupportedStatus(err, false) || (httpErr != nil && httpErr.status == http.StatusMethodNotAllowed) {
			return nil, fmt.Errorf("%w: %v", errRecoveryUnsupported, err)
		}
		return nil, err
	}
	if session.Version != AcceleratorRecoveryVersion || session.VolumeID != volume || session.SessionID == "" || !session.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("cloud did not protect the selected updates")
	}
	var manifest RecoveryManifest
	updates, err := c.list(ctx, volume, ListOptions{Kind: KindLBDLog, restoreSession: session.SessionID}, &manifest)
	if err != nil {
		return nil, err
	}
	// "legacy": the volume never acknowledged a checkpoint stream, so cloud
	// retains its whole log and the caller replays it from the start.
	if manifest.Status == "unsupported" || manifest.Status == "legacy" {
		return nil, errRecoveryUnsupported
	}
	// No history at all, so there is no stream to name either. Callers that
	// expected history treat this as lost history, never as an empty volume.
	if manifest.Status == "never_backed_up" {
		return nil, errNeverBackedUp
	}
	if !manifest.ExpiresAt.Equal(session.ExpiresAt) {
		return nil, fmt.Errorf("update listing does not match restore protection")
	}
	// A cloud that ignored until would select the head, writes after it included.
	if manifest.Until != until {
		return nil, fmt.Errorf("cloud selected history until %q, not %q", manifest.Until, until)
	}
	for _, u := range updates {
		if u.Kind != string(KindLBDLog) {
			return nil, fmt.Errorf("non-log update in accelerator recovery tail")
		}
		manifest.Segments = append(manifest.Segments, RecoverySegment{
			RecoveryObject: RecoveryObject{UpdateID: u.UpdateID, Size: u.Size, SHA256: u.SHA256},
			Label:          u.OrderingKey, PreviousLabel: u.PreviousLabel, FirstSequence: u.FirstSequence, EntryCount: u.EntryCount,
			Boundary: u.Boundary,
		})
	}
	if err := manifest.validate(volume); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (c *cloudUpdatesClient) RenewRecovery(ctx context.Context, volume, session string) (time.Time, error) {
	endpoint, err := c.updatesPath(volume, "restore-sessions", session, "renew")
	if err != nil {
		return time.Time{}, err
	}
	var response restoreSession
	if err := c.doJSON(ctx, "POST", endpoint, map[string]any{"version": AcceleratorRecoveryVersion}, &response); err != nil {
		return time.Time{}, err
	}
	if response.Version != AcceleratorRecoveryVersion || response.VolumeID != volume || response.SessionID != session || !response.ExpiresAt.After(time.Now()) {
		return time.Time{}, fmt.Errorf("cloud did not renew the selected recovery session")
	}
	return response.ExpiresAt, nil
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
