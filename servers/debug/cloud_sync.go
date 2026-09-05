package debug

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/pkg/entitysync"
	"miren.dev/runtime/pkg/rpc/standard"
)

func (s *Server) GetStatus(_ context.Context, state *debug_v1alpha.CloudSyncGetStatus) error {
	if s.CloudSync == nil {
		return errors.New("cloud sync diagnostics not available")
	}

	state.Results().SetReport(cloudSyncReport(s.CloudSync.SnapshotStatus()))
	return nil
}

func cloudSyncReport(status entitysync.Status) *debug_v1alpha.CloudSyncReport {
	report := &debug_v1alpha.CloudSyncReport{}
	report.SetState(cloudSyncState(status))
	report.SetSummary(cloudSyncSummary(status))

	facts := make([]*debug_v1alpha.CloudSyncFact, 0, 16)
	addFact := func(name, value string) {
		if value == "" {
			return
		}
		fact := &debug_v1alpha.CloudSyncFact{}
		fact.SetName(name)
		fact.SetValue(value)
		facts = append(facts, fact)
	}
	addFact("uplink_state", status.UplinkState)
	addFact("session_id", status.SessionID)
	if status.HandshakeVersion != 0 {
		addFact("handshake_version", strconv.FormatUint(uint64(status.HandshakeVersion), 10))
	}
	addFact("capability_state", status.CapabilityState)
	if status.CapabilityVersion != 0 {
		addFact("capability_version", strconv.FormatUint(uint64(status.CapabilityVersion), 10))
	}
	addFact("preparation_state", status.PreparationState)
	addFact("preparation_detail", status.PreparationDetail)
	addFact("source_epoch", status.SourceEpoch)
	addFact("schema_digest", status.SchemaDigest)
	addFact("wait_reason", status.WaitReason)
	addFact("cloud_committed_cursor", strconv.FormatInt(status.CloudCursor, 10))
	if status.NextWatchRevision != 0 {
		addFact("next_watched_revision", strconv.FormatInt(status.NextWatchRevision, 10))
	}
	if !status.RetryAt.IsZero() {
		addFact("retry_at", status.RetryAt.Format(time.RFC3339))
	}
	if snapshot := status.Snapshot; snapshot != nil {
		addFact("snapshot_id", snapshot.ID)
		addFact("snapshot_head_revision", strconv.FormatInt(snapshot.HeadRevision, 10))
		addFact("snapshot_next_revision", strconv.FormatInt(snapshot.NextRevision, 10))
		addFact("snapshot_pages_sent", strconv.FormatInt(snapshot.PagesSent, 10))
		addFact("snapshot_entities_sent", strconv.FormatInt(snapshot.EntitiesSent, 10))
		for _, kind := range slices.Sorted(maps.Keys(snapshot.CountsByKind)) {
			addFact("snapshot_count."+kind, strconv.FormatInt(snapshot.CountsByKind[kind], 10))
		}
	}
	report.SetFacts(facts)

	events := make([]*debug_v1alpha.CloudSyncEvent, 0, 2)
	if status.LastAcknowledgment != nil {
		events = append(events, cloudSyncEvent("acknowledgment", status.LastAcknowledgment))
	}
	if status.LastError != nil {
		events = append(events, cloudSyncEvent("error", status.LastError))
	}
	report.SetEvents(events)
	return report
}

func cloudSyncState(status entitysync.Status) string {
	if status.UplinkState == "disabled" || status.CapabilityState == "disabled" {
		return "disabled"
	}
	if status.Mode == "" {
		return "unknown"
	}
	return status.Mode
}

func cloudSyncSummary(status entitysync.Status) string {
	state := cloudSyncState(status)
	switch state {
	case "disabled":
		return cloudSyncSummaryWithReason("entity sync is disabled", status.WaitReason)
	case "waiting":
		return cloudSyncSummaryWithReason("entity sync is waiting", status.WaitReason)
	case "retrying":
		return cloudSyncSummaryWithReason("entity sync will retry", status.WaitReason)
	case "snapshotting":
		if status.Snapshot != nil && status.Snapshot.ID != "" {
			return "sending entity snapshot " + status.Snapshot.ID
		}
		return "sending a full entity snapshot"
	case "watching":
		return "watching for entity changes"
	default:
		return "entity sync state is " + state
	}
}

func cloudSyncSummaryWithReason(summary, reason string) string {
	if reason == "" {
		return summary
	}
	return summary + ": " + reason
}

func cloudSyncEvent(kind string, event *entitysync.Event) *debug_v1alpha.CloudSyncEvent {
	rpcEvent := &debug_v1alpha.CloudSyncEvent{}
	rpcEvent.SetKind(kind)
	if !event.At.IsZero() {
		rpcEvent.SetAt(standard.ToTimestamp(event.At))
	}
	var parts []string
	if event.Stage != "" {
		parts = append(parts, event.Stage)
	}
	if event.Message != "" {
		parts = append(parts, event.Message)
	}
	if event.MessageID != "" {
		parts = append(parts, "message "+event.MessageID)
	}
	if event.Cursor != 0 {
		parts = append(parts, fmt.Sprintf("cursor %d", event.Cursor))
	}
	rpcEvent.SetSummary(strings.Join(parts, ", "))
	return rpcEvent
}
