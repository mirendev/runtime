package saga_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
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
	ID                entity.Id  `json:"id"`
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
	schema.RegisterEncodedSchema("dev.miren.saga", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x94]\xce\xd5 \x10\x86\xb7\xa1ƟD\x8d\xdeո\"\xc2\xe9\f|\xe3i\a2@Ӯ\xc1U\x18?]\xa2\xd7\x06h\xce\x0f\xd2\xe3M\x03\xef\xcc\xfb0P\x86g`=#\x03.\xc3L\x82<\x04m5\x9e\x89!\xfcX_\xdc\xcb_\xb2\\F\xbf\x8b+4\xe1\xab\xf5\x8f\x017k\xe2\x86k\f\xe1\x04\xe1\xfb\xcf\x13\xc1\xfa\xb6\xe3\x1eFA\x1d\x11\x94\x8ee\x85o7\xf3\xb8y\x84H3\x16\xf7\x87\x9e\x1b\xd0\x10S$\xc7*\xbb\vµb\xe6\x98\x10\x85\xd8\x16ҧ\xff\x90\x16\x94@\x8e\vL:z\xe6\x8dı\xc0^\xf6`(\xe2\xa4\xf8\xb1\x0e\xdb\x12>v]+\x8e\xa9\xec}\xcc\xeb\x85\x02\xf0\xff\xa8\x99\x85\xa7-b8>\x97j\xcaE;\x01\xac\xa5\xb8Vl@\xef{\xa0\xb2w=)b\x9fb\xad\x88\x1b\xad\xc1|\xeea\xbc\x16䨮\x15\x10\xd4\v\xd5\vd\xe0\x89\xe0\xb8(\xc1\xd1-(\x9b\n\xa3\xf3\xf5\xa7s\xa3\xdd\x1c\xf8s\xe6\xbc\xeaqB\xd41\xd5M\x99}\\\xee\x1cr\x9a\xcf\xf9\xa3\x16=%\f\xbf\x8c\xd14!\xac\xaf[J1\r5j=2\x10\xdb\xf5M?k\x0f[I\xcc\x0f\xd2\xf6\xb0M\f\xeeA\xda\x1e\xa6\xd1\xcd~\u0088\xb0\xbe\xeb'^\x12\x8e{0y\xb8\xeb\xc1\x9b\xf9\xa5\a\xed~\xfb\xed\xf2UO\xfeIO^hֲ\xa9\xdc\xfe\x901m\xc69<9\x89\xaa\xbe,%\xe3\xf8y\xf9\v\x00\x00\xff\xff\x01\x00\x00\xff\xff\x9f+\xaaǕ\x04\x00\x00"))
}
