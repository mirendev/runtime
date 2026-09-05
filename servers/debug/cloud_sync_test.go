package debug

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/pkg/entitysync"
)

func TestGetStatusWithoutCloudSyncDiagnostics(t *testing.T) {
	server := &Server{}

	err := server.GetStatus(context.Background(), &debug_v1alpha.CloudSyncGetStatus{})

	require.EqualError(t, err, "cloud sync diagnostics not available")
}

func TestCloudSyncReportKeepsProtocolDetailsInExtensibleFacts(t *testing.T) {
	at := time.Date(2026, time.September, 4, 12, 30, 0, 0, time.UTC)
	report := cloudSyncReport(entitysync.Status{
		UplinkState:       "connected",
		SessionID:         "session-1",
		HandshakeVersion:  1,
		CapabilityState:   "selected",
		CapabilityVersion: 1,
		PreparationState:  "ready",
		SourceEpoch:       "epoch-1",
		SchemaDigest:      "sha256:test",
		Mode:              "snapshotting",
		CloudCursor:       40,
		Snapshot: &entitysync.SnapshotProgress{
			ID: "snapshot-1", HeadRevision: 60, NextRevision: 61,
			PagesSent: 2, EntitiesSent: 3, CountsByKind: map[string]int64{"app": 3},
		},
		LastAcknowledgment: &entitysync.Event{
			At: at, Stage: "snapshot-page", MessageID: "message-1", Cursor: 40,
		},
	})

	require.Equal(t, "snapshotting", report.State())
	require.Equal(t, "sending entity snapshot snapshot-1", report.Summary())
	facts := make(map[string]string, len(report.Facts()))
	for _, fact := range report.Facts() {
		facts[fact.Name()] = fact.Value()
	}
	require.Equal(t, "session-1", facts["session_id"])
	require.Equal(t, "40", facts["cloud_committed_cursor"])
	require.Equal(t, "3", facts["snapshot_count.app"])
	require.Len(t, report.Events(), 1)
	require.Equal(t, "acknowledgment", report.Events()[0].Kind())
	require.Equal(t, "snapshot-page, message message-1, cursor 40", report.Events()[0].Summary())
}
