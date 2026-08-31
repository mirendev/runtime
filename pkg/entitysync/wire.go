package entitysync

import (
	"encoding/json"

	"miren.dev/runtime/pkg/entity"
)

const (
	Version1 uint = 1

	TypeSnapshotBegin    = "entity.snapshot.begin"
	TypeSnapshotBatch    = "entity.snapshot.batch"
	TypeSnapshotComplete = "entity.snapshot.complete"
	TypeChangeBatch      = "entity.change.batch"
	TypeAck              = "entity.ack"
)

type Offer struct {
	ExportSchemas []string `json:"export_schemas"`
	SourceEpoch   string   `json:"source_epoch"`
}

type Config struct {
	ExportSchema              string `json:"export_schema"`
	Cursor                    int64  `json:"cursor"`
	SnapshotRequired          bool   `json:"snapshot_required,omitempty"`
	SourceEpoch               string `json:"source_epoch"`
	ResnapshotAfterSeconds    int64  `json:"resnapshot_after_seconds,omitempty"`
	ResnapshotIntervalSeconds int64  `json:"resnapshot_interval_seconds,omitempty"`
}

type SnapshotBegin struct {
	SnapshotID         string `json:"snapshot_id"`
	SourceHead         int64  `json:"source_head"`
	ExportSchemaDigest string `json:"export_schema_digest"`
	SourceEpoch        string `json:"source_epoch"`
}

type SnapshotBatch struct {
	SnapshotID         string           `json:"snapshot_id"`
	ExportSchemaDigest string           `json:"export_schema_digest"`
	Entities           []*entity.Entity `json:"entities"`
}

type SnapshotComplete struct {
	MessageID    string           `json:"message_id"`
	SnapshotID   string           `json:"snapshot_id"`
	SourceHead   int64            `json:"source_head"`
	CountsByKind map[string]int64 `json:"counts_by_kind"`
}

type ChangeOp string

const (
	ChangePut    ChangeOp = "put"
	ChangeDelete ChangeOp = "delete"
)

type Change struct {
	Op       ChangeOp       `json:"op"`
	Revision int64          `json:"revision"`
	EntityID entity.Id      `json:"entity_id"`
	Kind     entity.Id      `json:"kind"`
	Entity   *entity.Entity `json:"entity,omitempty"`
}

type ChangeBatch struct {
	MessageID          string   `json:"message_id"`
	FromRevision       int64    `json:"from_revision"`
	ToRevision         int64    `json:"to_revision"`
	ExportSchemaDigest string   `json:"export_schema_digest"`
	Changes            []Change `json:"changes"`
	SourceEpoch        string   `json:"source_epoch"`
}

type Ack struct {
	MessageID string `json:"message_id"`
	Cursor    int64  `json:"cursor"`
	Error     string `json:"error,omitempty"`
}

func marshalOffer(digest, sourceEpoch string) json.RawMessage {
	raw, err := json.Marshal(Offer{ExportSchemas: []string{digest}, SourceEpoch: sourceEpoch})
	if err != nil {
		panic(err)
	}
	return raw
}
