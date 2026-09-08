package entitysync

import (
	"maps"
	"sync"
	"time"

	"miren.dev/runtime/pkg/uplink"
)

// Event records the most recent acknowledgement or failure.
type Event struct {
	At        time.Time
	Stage     string
	Message   string
	MessageID string
	Cursor    int64
}

// SnapshotProgress describes the snapshot currently being transmitted.
type SnapshotProgress struct {
	ID           string
	HeadRevision int64
	NextRevision int64
	PagesSent    int64
	EntitiesSent int64
	CountsByKind map[string]int64
}

// Status is a point-in-time view of runtime entity sync.
type Status struct {
	UplinkState        string
	SessionID          string
	HandshakeVersion   uint
	CapabilityState    string
	CapabilityVersion  uint
	PreparationState   string
	PreparationDetail  string
	SourceEpoch        string
	SchemaDigest       string
	Mode               string
	WaitReason         string
	CloudCursor        int64
	NextWatchRevision  int64
	Snapshot           *SnapshotProgress
	LastAcknowledgment *Event
	LastError          *Event
	RetryAt            time.Time
}

// Diagnostics collects transient state for the local debug interface.
type Diagnostics struct {
	mu                 sync.RWMutex
	status             Status
	capabilityDisabled string
}

func NewDiagnostics(schemaDigest string) *Diagnostics {
	return &Diagnostics{status: Status{
		UplinkState:      "not-started",
		CapabilityState:  "not-negotiated",
		PreparationState: "not-started",
		SchemaDigest:     schemaDigest,
		Mode:             "waiting",
		WaitReason:       "uplink-not-connected",
	}}
}

func (d *Diagnostics) SnapshotStatus() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	status := d.status
	if status.Snapshot != nil {
		progress := *status.Snapshot
		progress.CountsByKind = maps.Clone(progress.CountsByKind)
		status.Snapshot = &progress
	}
	if status.LastAcknowledgment != nil {
		event := *status.LastAcknowledgment
		status.LastAcknowledgment = &event
	}
	if status.LastError != nil {
		event := *status.LastError
		status.LastError = &event
	}
	return status
}

func (d *Diagnostics) ObserveUplink(status uplink.Status) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.UplinkState = status.State
	d.status.RetryAt = status.RetryAt
	if status.LastError != "" {
		d.status.LastError = &Event{At: time.Now().UTC(), Stage: "uplink", Message: status.LastError}
	}
	if status.Session == nil {
		d.status.SessionID = ""
		d.status.HandshakeVersion = 0
		if d.capabilityDisabled != "" {
			d.status.CapabilityState = "disabled"
			d.status.Mode = "waiting"
			d.status.WaitReason = d.capabilityDisabled
		} else if status.State != "connected" {
			d.status.CapabilityState = "not-negotiated"
			d.status.CapabilityVersion = 0
			d.status.Mode = "waiting"
			d.status.WaitReason = "uplink-" + status.State
		}
		return
	}
	d.status.SessionID = status.Session.ID
	d.status.HandshakeVersion = status.Session.HandshakeVersion
	if d.capabilityDisabled != "" {
		d.status.CapabilityState = "disabled"
		d.status.CapabilityVersion = 0
		d.status.Mode = "waiting"
		d.status.WaitReason = d.capabilityDisabled
		return
	}
	selection, selected := status.Session.Capability(uplink.CapabilityEntitySync)
	if selected {
		d.status.CapabilityState = "selected"
		d.status.CapabilityVersion = selection.Version
	} else {
		d.status.CapabilityState = "not-selected"
		d.status.CapabilityVersion = 0
		d.status.Mode = "waiting"
		d.status.WaitReason = "capability-not-selected"
	}
}

func (d *Diagnostics) SetDisabled(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.UplinkState = "disabled"
	d.status.CapabilityState = "disabled"
	d.status.Mode = "waiting"
	d.status.WaitReason = reason
}

func (d *Diagnostics) SetCapabilityDisabled(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capabilityDisabled = reason
	d.status.CapabilityState = "disabled"
	d.status.Mode = "waiting"
	d.status.WaitReason = reason
}

func (d *Diagnostics) SetPreparation(state, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.PreparationState = state
	d.status.PreparationDetail = detail
}

func (d *Diagnostics) SetPreparationFailure(state, message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.PreparationState = state
	d.status.PreparationDetail = message
	d.status.LastError = &Event{At: time.Now().UTC(), Stage: "source-preparation", Message: message}
}

func (d *Diagnostics) setSource(sourceEpoch string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.SourceEpoch = sourceEpoch
}

func (d *Diagnostics) setMode(mode, waitReason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Mode = mode
	d.status.WaitReason = waitReason
}

func (d *Diagnostics) setCursor(cursor int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.CloudCursor = cursor
}

func (d *Diagnostics) setNextWatchRevision(revision int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.NextWatchRevision = revision
	if d.status.Snapshot != nil {
		d.status.Snapshot.NextRevision = revision
	}
}

func (d *Diagnostics) beginSnapshot(id string, head, next int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Snapshot = &SnapshotProgress{
		ID: id, HeadRevision: head, NextRevision: next, CountsByKind: make(map[string]int64),
	}
}

func (d *Diagnostics) addSnapshotPage(entities int, counts map[string]int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.Snapshot == nil {
		return
	}
	d.status.Snapshot.PagesSent++
	d.status.Snapshot.EntitiesSent += int64(entities)
	d.status.Snapshot.CountsByKind = maps.Clone(counts)
}

func (d *Diagnostics) finishSnapshot(cursor int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.CloudCursor = cursor
	d.status.Snapshot = nil
}

func (d *Diagnostics) abortSnapshot() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Snapshot = nil
}

func (d *Diagnostics) acknowledge(stage string, ack Ack) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.LastAcknowledgment = &Event{
		At: time.Now().UTC(), Stage: stage, MessageID: ack.MessageID, Cursor: ack.Cursor,
	}
}

func (d *Diagnostics) fail(stage string, err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.LastError = &Event{At: time.Now().UTC(), Stage: stage, Message: err.Error()}
}
