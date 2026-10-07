package saga_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	SagaBlockedOnId         = entity.Id("dev.miren.saga/saga.blocked_on")
	SagaBlockedReasonId     = entity.Id("dev.miren.saga/saga.blocked_reason")
	SagaCreatedAtId         = entity.Id("dev.miren.saga/saga.created_at")
	SagaDefinitionNameId    = entity.Id("dev.miren.saga/saga.definition_name")
	SagaDefinitionVersionId = entity.Id("dev.miren.saga/saga.definition_version")
	SagaErrorId             = entity.Id("dev.miren.saga/saga.error")
	SagaExecutedActionsId   = entity.Id("dev.miren.saga/saga.executed_actions")
	SagaExecutionOrderId    = entity.Id("dev.miren.saga/saga.execution_order")
	SagaInitialInputsId     = entity.Id("dev.miren.saga/saga.initial_inputs")
	SagaParentExecutionIdId = entity.Id("dev.miren.saga/saga.parent_execution_id")
	SagaRecoveryScopeId     = entity.Id("dev.miren.saga/saga.recovery_scope")
	SagaStatusId            = entity.Id("dev.miren.saga/saga.status")
	SagaStatusPendingId     = entity.Id("dev.miren.saga/status.pending")
	SagaStatusRunningId     = entity.Id("dev.miren.saga/status.running")
	SagaStatusUndoingId     = entity.Id("dev.miren.saga/status.undoing")
	SagaStatusCompletedId   = entity.Id("dev.miren.saga/status.completed")
	SagaStatusFailedId      = entity.Id("dev.miren.saga/status.failed")
	SagaUpdatedAtId         = entity.Id("dev.miren.saga/saga.updated_at")
)

type Saga struct {
	ID                entity.Id  `json:"id"`
	BlockedOn         string     `cbor:"blocked_on,omitempty" json:"blocked_on,omitempty"`
	BlockedReason     string     `cbor:"blocked_reason,omitempty" json:"blocked_reason,omitempty"`
	CreatedAt         time.Time  `cbor:"created_at,omitempty" json:"created_at"`
	DefinitionName    string     `cbor:"definition_name,omitempty" json:"definition_name,omitempty"`
	DefinitionVersion int64      `cbor:"definition_version,omitempty" json:"definition_version,omitempty"`
	Error             string     `cbor:"error,omitempty" json:"error,omitempty"`
	ExecutedActions   []byte     `cbor:"executed_actions,omitempty" json:"executed_actions,omitempty"`
	ExecutionOrder    []byte     `cbor:"execution_order,omitempty" json:"execution_order,omitempty"`
	InitialInputs     []byte     `cbor:"initial_inputs,omitempty" json:"initial_inputs,omitempty"`
	ParentExecutionId entity.Id  `cbor:"parent_execution_id,omitempty" json:"parent_execution_id,omitempty"`
	RecoveryScope     string     `cbor:"recovery_scope,omitempty" json:"recovery_scope,omitempty"`
	Status            SagaStatus `cbor:"status,omitempty" json:"status,omitempty"`
	UpdatedAt         time.Time  `cbor:"updated_at,omitempty" json:"updated_at"`
}

type SagaStatus string

const (
	PENDING   SagaStatus = "status.pending"
	RUNNING   SagaStatus = "status.running"
	UNDOING   SagaStatus = "status.undoing"
	COMPLETED SagaStatus = "status.completed"
	FAILED    SagaStatus = "status.failed"
)

var sagastatusFromId = map[entity.Id]SagaStatus{SagaStatusPendingId: PENDING, SagaStatusRunningId: RUNNING, SagaStatusUndoingId: UNDOING, SagaStatusCompletedId: COMPLETED, SagaStatusFailedId: FAILED}
var sagastatusToId = map[SagaStatus]entity.Id{PENDING: SagaStatusPendingId, RUNNING: SagaStatusRunningId, UNDOING: SagaStatusUndoingId, COMPLETED: SagaStatusCompletedId, FAILED: SagaStatusFailedId}

func (o *Saga) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(SagaBlockedOnId); ok && a.Value.Kind() == entity.KindString {
		o.BlockedOn = a.Value.String()
	}
	if a, ok := e.Get(SagaBlockedReasonId); ok && a.Value.Kind() == entity.KindString {
		o.BlockedReason = a.Value.String()
	}
	if a, ok := e.Get(SagaCreatedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.CreatedAt = a.Value.Time()
	}
	if a, ok := e.Get(SagaDefinitionNameId); ok && a.Value.Kind() == entity.KindString {
		o.DefinitionName = a.Value.String()
	}
	if a, ok := e.Get(SagaDefinitionVersionId); ok && a.Value.Kind() == entity.KindInt64 {
		o.DefinitionVersion = a.Value.Int64()
	}
	if a, ok := e.Get(SagaErrorId); ok && a.Value.Kind() == entity.KindString {
		o.Error = a.Value.String()
	}
	if a, ok := e.Get(SagaExecutedActionsId); ok && a.Value.Kind() == entity.KindBytes {
		o.ExecutedActions = a.Value.Bytes()
	}
	if a, ok := e.Get(SagaExecutionOrderId); ok && a.Value.Kind() == entity.KindBytes {
		o.ExecutionOrder = a.Value.Bytes()
	}
	if a, ok := e.Get(SagaInitialInputsId); ok && a.Value.Kind() == entity.KindBytes {
		o.InitialInputs = a.Value.Bytes()
	}
	if a, ok := e.Get(SagaParentExecutionIdId); ok && a.Value.Kind() == entity.KindId {
		o.ParentExecutionId = a.Value.Id()
	}
	if a, ok := e.Get(SagaRecoveryScopeId); ok && a.Value.Kind() == entity.KindString {
		o.RecoveryScope = a.Value.String()
	}
	if a, ok := e.Get(SagaStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = sagastatusFromId[a.Value.Id()]
	}
	if a, ok := e.Get(SagaUpdatedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.UpdatedAt = a.Value.Time()
	}
}

func (o *Saga) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindSaga)
}

func (o *Saga) ShortKind() string {
	return "saga"
}

func (o *Saga) Kind() entity.Id {
	return KindSaga
}

func (o *Saga) EntityId() entity.Id {
	return o.ID
}

func (o *Saga) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.BlockedOn) {
		attrs = append(attrs, entity.String(SagaBlockedOnId, o.BlockedOn))
	}
	if !entity.Empty(o.BlockedReason) {
		attrs = append(attrs, entity.String(SagaBlockedReasonId, o.BlockedReason))
	}
	if !entity.Empty(o.CreatedAt) {
		attrs = append(attrs, entity.Time(SagaCreatedAtId, o.CreatedAt))
	}
	if !entity.Empty(o.DefinitionName) {
		attrs = append(attrs, entity.String(SagaDefinitionNameId, o.DefinitionName))
	}
	if !entity.Empty(o.DefinitionVersion) {
		attrs = append(attrs, entity.Int64(SagaDefinitionVersionId, o.DefinitionVersion))
	}
	if !entity.Empty(o.Error) {
		attrs = append(attrs, entity.String(SagaErrorId, o.Error))
	}
	if len(o.ExecutedActions) > 0 {
		attrs = append(attrs, entity.Bytes(SagaExecutedActionsId, o.ExecutedActions))
	}
	if len(o.ExecutionOrder) > 0 {
		attrs = append(attrs, entity.Bytes(SagaExecutionOrderId, o.ExecutionOrder))
	}
	if len(o.InitialInputs) > 0 {
		attrs = append(attrs, entity.Bytes(SagaInitialInputsId, o.InitialInputs))
	}
	if !entity.Empty(o.ParentExecutionId) {
		attrs = append(attrs, entity.Ref(SagaParentExecutionIdId, o.ParentExecutionId))
	}
	if !entity.Empty(o.RecoveryScope) {
		attrs = append(attrs, entity.String(SagaRecoveryScopeId, o.RecoveryScope))
	}
	if a, ok := sagastatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(SagaStatusId, a))
	}
	if !entity.Empty(o.UpdatedAt) {
		attrs = append(attrs, entity.Time(SagaUpdatedAtId, o.UpdatedAt))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSaga))
	return
}

func (o *Saga) Empty() bool {
	if !entity.Empty(o.BlockedOn) {
		return false
	}
	if !entity.Empty(o.BlockedReason) {
		return false
	}
	if !entity.Empty(o.CreatedAt) {
		return false
	}
	if !entity.Empty(o.DefinitionName) {
		return false
	}
	if !entity.Empty(o.DefinitionVersion) {
		return false
	}
	if !entity.Empty(o.Error) {
		return false
	}
	if len(o.ExecutedActions) != 0 {
		return false
	}
	if len(o.ExecutionOrder) != 0 {
		return false
	}
	if len(o.InitialInputs) != 0 {
		return false
	}
	if !entity.Empty(o.ParentExecutionId) {
		return false
	}
	if !entity.Empty(o.RecoveryScope) {
		return false
	}
	if o.Status != "" {
		return false
	}
	if !entity.Empty(o.UpdatedAt) {
		return false
	}
	return true
}

func (o *Saga) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("blocked_on", "dev.miren.saga/saga.blocked_on", schema.Doc("The nested execution whose refusal is blocking this one, re-checked before this one is driven again"))
	sb.String("blocked_reason", "dev.miren.saga/saga.blocked_reason", schema.Doc("Why the running binary refused to resume this execution; empty when nothing is blocking it"))
	sb.Time("created_at", "dev.miren.saga/saga.created_at", schema.Doc("When the execution was created"))
	sb.String("definition_name", "dev.miren.saga/saga.definition_name", schema.Doc("The name of the registered saga definition"), schema.Indexed)
	sb.Int64("definition_version", "dev.miren.saga/saga.definition_version", schema.Doc("The version of the definition when this execution started"))
	sb.String("error", "dev.miren.saga/saga.error", schema.Doc("Error message if the saga failed"))
	sb.Bytes("executed_actions", "dev.miren.saga/saga.executed_actions", schema.Doc("JSON-encoded map of action name to ActionResult"))
	sb.Bytes("execution_order", "dev.miren.saga/saga.execution_order", schema.Doc("JSON-encoded array of action names in execution order"))
	sb.Bytes("initial_inputs", "dev.miren.saga/saga.initial_inputs", schema.Doc("JSON-encoded initial inputs for the saga"))
	sb.Ref("parent_execution_id", "dev.miren.saga/saga.parent_execution_id", schema.Doc("Reference to the parent saga execution (set for child/nested sagas)"), schema.Indexed)
	sb.String("recovery_scope", "dev.miren.saga/saga.recovery_scope", schema.Doc("Stable identity of the executor allowed to recover this execution"))
	sb.Singleton("dev.miren.saga/status.pending")
	sb.Singleton("dev.miren.saga/status.running")
	sb.Singleton("dev.miren.saga/status.undoing")
	sb.Singleton("dev.miren.saga/status.completed")
	sb.Singleton("dev.miren.saga/status.failed")
	sb.Ref("status", "dev.miren.saga/saga.status", schema.Doc("Current execution status"), schema.Indexed, schema.Choices(SagaStatusPendingId, SagaStatusRunningId, SagaStatusUndoingId, SagaStatusCompletedId, SagaStatusFailedId))
	sb.Time("updated_at", "dev.miren.saga/saga.updated_at", schema.Doc("When the execution last changed state"))
}

var (
	KindSaga = entity.Id("dev.miren.saga/kind.saga")
	Schema   = entity.Id("dev.miren.saga/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.saga", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Saga{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.saga", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x94]\xae\x14!\x10\x85\xb7\xa1ƟD\x8d\xbe\x8dqE\x84\xa1\nn9tA\n\xe8\xf4,\xc2E\x18\xaf.\xd1g\x03t\xee\x9dAf|\xe9\xc0\xa9:_\x1f\xba\x81G`\xbd \x03\xae\x87\x85\x04\xf9\x90\xb4\xd3x\"\x86\xf4c{q-\x7f\xa9r\x1b\xfdn\xae4\x94\x9f\xad\x7f,\x84E\x13\x0f\\k\t=\xa4\xef?\x8f\x04\xdbۉ\xfbp\xf4\xc1\x9c\x10T\xe0\xf6\x86o\x17\xf3|\x8ehS\x16b\xd7\xfc\xef\xef\xf9\x05u\xda\x19<h#g\x9a\xc3\bꌠt\xee9.\xe6\xd5\x0f\x99\x16l\xee\x0f37\xa0%\xa6L\x81Uu7D\x18\xc51ǧ\xff\x90V\x94D\xfb\x9ad\xa2W\x9e!\xce\r\xf6r\x06C\x91 ͏}8F\xf88umhJ[\xbb\xa9\xefK\r\x10\xffQ+\v\x8f\xe7\x8c\xe9\xf6w\xe9\xa6\x1a:\b`\x8f\x12Fq\x00M\x7fs[\xbb\xf6\x8a8\x96\xdc\x13\xf1\xa0\r\x98\xcf3LԂ\x9c\xd5s\x02\x82\xbe\xb1g\x85\n<\x12\xdc\x0e%h\u008arVɄ\xd8\x7f:\x0f\xda\xc5\a\x7f\xac\x9cW3N\xca:\x97\xbe(\xbb\x8f۞C.˩>Ԫ}\xc1\xf4\xcbZM\x1ea{=R\x9a\xe9Ы.\"\x03\xb1\xdb\xde̻\xf6\xb2\x93\xc2|\xa7m/\xbb\xc2\x10\xee\xb4\xede2a\x89\x1e3\xc2\xf6n\xde\xf8\xd4p\xfb\f\x96\bWg\xf0b\xfet\x06ݾ\xfb\xdd\xfaU\xfb\xf8\xa0}\x14Z\xb4\x9cU\xbd\x86\xa0bƎSz\b\x92U\xbf\xe1Z\xc7\xedk\xee/\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff,ؗ\x93\x1d\x05\x00\x00"))
}
