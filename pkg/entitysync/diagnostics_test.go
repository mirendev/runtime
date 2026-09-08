package entitysync

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/uplink"
)

func TestDiagnosticsTracksNegotiatedSessionAndExporterProgress(t *testing.T) {
	diagnostics := NewDiagnostics("sha256:test")
	diagnostics.SetPreparation("ready", "source prepared")
	diagnostics.ObserveUplink(uplink.Status{State: "connected", Session: &uplink.Session{
		ID: "session-1", HandshakeVersion: 1,
		Capabilities: []uplink.CapabilitySelection{{Name: uplink.CapabilityEntitySync, Version: Version1}},
	}})
	diagnostics.setSource("epoch-1")
	diagnostics.setCursor(40)
	diagnostics.beginSnapshot("snapshot-1", 60, 61)
	diagnostics.addSnapshotPage(3, map[string]int64{"app": 3})
	diagnostics.setNextWatchRevision(65)
	diagnostics.acknowledge(TypeChangeBatch, Ack{MessageID: "message-1", Cursor: 64})

	status := diagnostics.SnapshotStatus()
	require.Equal(t, "connected", status.UplinkState)
	require.Equal(t, "session-1", status.SessionID)
	require.Equal(t, "selected", status.CapabilityState)
	require.Equal(t, uint(Version1), status.CapabilityVersion)
	require.Equal(t, "ready", status.PreparationState)
	require.Equal(t, "epoch-1", status.SourceEpoch)
	require.Equal(t, "sha256:test", status.SchemaDigest)
	require.Equal(t, int64(40), status.CloudCursor)
	require.Equal(t, int64(65), status.NextWatchRevision)
	require.Equal(t, int64(3), status.Snapshot.EntitiesSent)
	require.Equal(t, int64(3), status.Snapshot.CountsByKind["app"])
	require.Equal(t, "message-1", status.LastAcknowledgment.MessageID)

	diagnostics.finishSnapshot(64)
	status = diagnostics.SnapshotStatus()
	require.Equal(t, int64(64), status.CloudCursor)
	require.Nil(t, status.Snapshot)
}

func TestDiagnosticsSnapshotDoesNotAliasMutableState(t *testing.T) {
	diagnostics := NewDiagnostics("schema")
	diagnostics.beginSnapshot("snapshot-1", 10, 11)
	diagnostics.addSnapshotPage(1, map[string]int64{"app": 1})
	first := diagnostics.SnapshotStatus()
	first.Snapshot.CountsByKind["app"] = 99

	second := diagnostics.SnapshotStatus()
	require.Equal(t, int64(1), second.Snapshot.CountsByKind["app"])
}

func TestDiagnosticsRecordsWaitAndFailure(t *testing.T) {
	diagnostics := NewDiagnostics("schema")
	diagnostics.ObserveUplink(uplink.Status{State: "connected", Session: &uplink.Session{ID: "session-1"}})
	diagnostics.setMode("retrying", "start entity sync session")
	diagnostics.fail("watch", errors.New("compacted"))

	status := diagnostics.SnapshotStatus()
	require.Equal(t, "not-selected", status.CapabilityState)
	require.Equal(t, "retrying", status.Mode)
	require.Equal(t, "start entity sync session", status.WaitReason)
	require.Equal(t, "watch", status.LastError.Stage)
	require.Equal(t, "compacted", status.LastError.Message)
}

func TestDiagnosticsRecordsPreparationFailure(t *testing.T) {
	diagnostics := NewDiagnostics("schema")
	diagnostics.SetPreparationFailure("marker-backfill-retrying", "etcd unavailable")

	status := diagnostics.SnapshotStatus()
	require.Equal(t, "marker-backfill-retrying", status.PreparationState)
	require.Equal(t, "source-preparation", status.LastError.Stage)
	require.Equal(t, "etcd unavailable", status.LastError.Message)
}
