package diskio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
)

// errSequenceGap means the cloud refused an lbd_log because its entries do
// not continue the volume's published sequence: an lbd log write was lost.
// The refusal is final, so retrying the same log can never succeed. Recovery
// is manual; see "Sequence gaps" in accelerator_recovery.md.
var errSequenceGap = errors.New("cloud refused a log over a sequence gap")

// sequenceGapHold is how long a volume's uploads wait after a sequence_gap
// refusal before trying once more, in case an operator has recovered it.
const sequenceGapHold = time.Hour

// uploadBlockedSequenceGap is the volume entity's upload_blocked value while
// the cloud refuses its logs over a sequence gap.
const uploadBlockedSequenceGap = "sequence_gap"

func isSequenceGap(err error) bool {
	httpErr, ok := errors.AsType[*cloudUpdateHTTPError](err)
	return ok && httpErr.status == http.StatusConflict && httpErr.code == "sequence_gap"
}

// SetMetricsWriter publishes the uploader's counters; nil disables them.
func (u *CloudSegmentUploader) SetMetricsWriter(w metrics.PointWriter) {
	u.metrics = newDiskMetrics(w)
}

// SetEAC lets the uploader mark a volume whose uploads the cloud refuses, so
// miren doctor reports it.
func (u *CloudSegmentUploader) SetEAC(eac *entityserver_v1alpha.EntityAccessClient) {
	u.eac = eac
}

// heldForSequenceGap returns an error while volumeID's uploads are held after
// a sequence_gap refusal.
func (u *CloudSegmentUploader) heldForSequenceGap(volumeID string) error {
	u.gapMu.Lock()
	defer u.gapMu.Unlock()
	if until, ok := u.gapUntil[volumeID]; ok && time.Now().Before(until) {
		return fmt.Errorf("%w: uploads for volume %s are held until %s", errSequenceGap, volumeID, until.Format(time.RFC3339))
	}
	return nil
}

// noteUploadOutcome holds a volume's uploads after a sequence_gap refusal and
// releases them, and its doctor signal, after a later upload succeeds.
func (u *CloudSegmentUploader) noteUploadOutcome(ctx context.Context, volumeID string, err error) error {
	if err != nil && !isSequenceGap(err) {
		return err
	}
	u.gapMu.Lock()
	if u.gapUntil == nil {
		u.gapUntil, u.gapClear = map[string]time.Time{}, map[string]bool{}
	}
	if err == nil {
		// Clear once per process: a restart forgets the hold, not the entity.
		_, held := u.gapUntil[volumeID]
		clear := held || !u.gapClear[volumeID]
		delete(u.gapUntil, volumeID)
		u.gapClear[volumeID] = true
		u.gapMu.Unlock()
		if clear {
			u.markUploadBlocked(ctx, volumeID, "")
		}
		return nil
	}
	u.gapUntil[volumeID] = time.Now().Add(sequenceGapHold)
	u.gapClear[volumeID] = false
	u.gapMu.Unlock()

	var leaseID string
	if reader, ok := u.leases.(interface {
		ActiveLeaseID(context.Context, string) (string, error)
	}); ok {
		leaseID, _ = reader.ActiveLeaseID(ctx, volumeID)
	}
	u.log.Error("cloud refused this volume's log over an lbd sequence gap; its uploads stall until it is recovered by hand",
		"volume_id", volumeID, "lease_id", leaseID, "retry_after", sequenceGapHold, "error", err)
	u.metrics.inc(ctx, "miren_disk_lbd_log_sequence_gap_refusals_total", map[string]string{"volume_id": volumeID, "lease_id": leaseID})
	u.markUploadBlocked(ctx, volumeID, uploadBlockedSequenceGap)
	return fmt.Errorf("%w: %w", errSequenceGap, err)
}

// markUploadBlocked records why the cloud refuses volumeID's uploads on its
// disk_volume entity, or clears it. Failure only warns: the log already
// names the problem, and the next outcome writes the signal again.
func (u *CloudSegmentUploader) markUploadBlocked(ctx context.Context, volumeID, reason string) {
	if u.eac == nil || u.state == nil {
		return
	}
	for _, v := range u.state.ListVolumes() {
		if v.CloudVolumeId != volumeID {
			continue
		}
		if _, err := u.eac.Patch(ctx, []entity.Attr{
			entity.Ref(entity.DBId, entity.Id(v.EntityId)),
			entity.String(storage_v1alpha.DiskVolumeUploadBlockedId, reason),
		}, 0); err != nil {
			u.log.Warn("failed to record the volume's upload state", "volume_id", volumeID, "error", err)
		}
	}
}
