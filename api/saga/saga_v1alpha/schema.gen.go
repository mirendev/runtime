package saga_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusUndoing   ExecutionStatus = "undoing"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

const (
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
	ID                entity.Id       `json:"id"`
	CreatedAt         time.Time       `cbor:"created_at,omitempty" json:"created_at"`
	DefinitionName    string          `cbor:"definition_name,omitempty" json:"definition_name,omitempty"`
	DefinitionVersion int64           `cbor:"definition_version,omitempty" json:"definition_version,omitempty"`
	Error             string          `cbor:"error,omitempty" json:"error,omitempty"`
	ExecutedActions   []byte          `cbor:"executed_actions,omitempty" json:"executed_actions,omitempty"`
	ExecutionOrder    []byte          `cbor:"execution_order,omitempty" json:"execution_order,omitempty"`
	InitialInputs     []byte          `cbor:"initial_inputs,omitempty" json:"initial_inputs,omitempty"`
	ParentExecutionId entity.Id       `cbor:"parent_execution_id,omitempty" json:"parent_execution_id,omitempty"`
	RecoveryScope     string          `cbor:"recovery_scope,omitempty" json:"recovery_scope,omitempty"`
	Status            ExecutionStatus `cbor:"status,omitempty" json:"status,omitempty"`
	UpdatedAt         time.Time       `cbor:"updated_at,omitempty" json:"updated_at"`
}

type SagaStatus = ExecutionStatus

const (
	PENDING   ExecutionStatus = ExecutionStatusPending
	RUNNING   ExecutionStatus = ExecutionStatusRunning
	UNDOING   ExecutionStatus = ExecutionStatusUndoing
	COMPLETED ExecutionStatus = ExecutionStatusCompleted
	FAILED    ExecutionStatus = ExecutionStatusFailed
)

var SagaStatusFromId = map[entity.Id]ExecutionStatus{SagaStatusPendingId: ExecutionStatusPending, SagaStatusRunningId: ExecutionStatusRunning, SagaStatusUndoingId: ExecutionStatusUndoing, SagaStatusCompletedId: ExecutionStatusCompleted, SagaStatusFailedId: ExecutionStatusFailed}
var SagaStatusToId = map[ExecutionStatus]entity.Id{ExecutionStatusPending: SagaStatusPendingId, ExecutionStatusRunning: SagaStatusRunningId, ExecutionStatusUndoing: SagaStatusUndoingId, ExecutionStatusCompleted: SagaStatusCompletedId, ExecutionStatusFailed: SagaStatusFailedId}

func (o *Saga) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
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
		o.Status = SagaStatusFromId[a.Value.Id()]
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
	if a, ok := SagaStatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(SagaStatusId, a))
	}
	if !entity.Empty(o.UpdatedAt) {
		attrs = append(attrs, entity.Time(SagaUpdatedAtId, o.UpdatedAt))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindSaga))
	return
}

func (o *Saga) Empty() bool {
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
	schema.RegisterEncodedSchema("dev.miren.saga", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x94]\xae\x14!\x10\x85\xb7\xa1ƟD\x8d\xbe\x8dqE\x1d\x86*\xb8\xe54\x05)\xe8N\xcf\x02|r\x15ƿ\x1d\xfal(:\xf7\xcee\x98\xebK\x87>u\xceGA7\xfc\x046\x01\x19p=\x04\x12\xe4C6\xde\xe0\x89\x18\xf2\xf7\xed\xd9c\xf9S\x95u\xf4[S\xb9+?D\xff:\x88\xc1\x10w\\\xe7\bg\xc8\xdf~\x1c\t\xb6׃\xf4\xc1\n\x9a\x820\x99\xa23|\xb9x/\xe7\x84P(\xa0\xa6ߍҀ\x8e\x98\nE\x9ejZ\x11\xb1\x17+\xc7\xe5\"\xc4^I\x1f\xfeCZQ2EV\x98\f\xf4ʳ\xc4Ea\xcfG0\x14\x89\xa2ylþ\x85\xf7\xc3Ԇvѵ\xdb:_V@\xbaR+\v\x8f\xe7\x82\xf9\xf6\xbe\xb4Pm:\n`k%\xf6b\az;\x02\xe9\xda\xcd<\x11\xa7\xa5\xb4\x8e\xb8\xd3:\xcc\xc7\x11&\x19A.\xd3C\a\x04\xed\x87\x1a\x15*\xf0Hp\xbb)A\x1bW\x94\xf3\x94mL\xed\xa3s\xa7]l\xf8\x9f\xcay1\xe2\xe4bʒ\x01y\tW_\xa4\x8a\x17۸[\xebTn\x1f\xeb\xefYm\xa7\xfa\x98V3/\x98\x7f9ghF\xd8^\xf6\x13j\xe8Ъ>!\x03\xb1\xdf^\x8d]{\xd9\xcb\xc2\xfc\x84m/\xfb\x85!>a\xdb\xcbdcH3\x16\x84\xed\xcd\xd8xo\x98uE\x01\xc3\x11%\x7f\xf5];\xfe\n\xb8/:h\f\xd9\xc6j\xb7\x82\xee\xf6\xb9_\x12<:\xf7\x17\xef\xf7\xe7\xde\xef'ί\x9f͜\xee̜\x84\x82\x91\xf3T\xaf\x1c\xa8\x98\xdeq\xcawQ\xca\xd4n3uܾ\xd2\xfe\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff\x173\xb7\x9a\t\x05\x00\x00"))
}
