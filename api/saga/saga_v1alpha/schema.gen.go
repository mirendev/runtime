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
	CreatedAt         time.Time  `cbor:"created_at,omitempty" json:"created_at,omitempty"`
	DefinitionName    string     `cbor:"definition_name,omitempty" json:"definition_name,omitempty"`
	DefinitionVersion int64      `cbor:"definition_version,omitempty" json:"definition_version,omitempty"`
	Error             string     `cbor:"error,omitempty" json:"error,omitempty"`
	ExecutedActions   []byte     `cbor:"executed_actions,omitempty" json:"executed_actions,omitempty"`
	ExecutionOrder    []byte     `cbor:"execution_order,omitempty" json:"execution_order,omitempty"`
	InitialInputs     []byte     `cbor:"initial_inputs,omitempty" json:"initial_inputs,omitempty"`
	ParentExecutionId entity.Id  `cbor:"parent_execution_id,omitempty" json:"parent_execution_id,omitempty"`
	Status            SagaStatus `cbor:"status,omitempty" json:"status,omitempty"`
	UpdatedAt         time.Time  `cbor:"updated_at,omitempty" json:"updated_at,omitempty"`
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
	schema.RegisterEncodedSchema("dev.miren.saga", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x84\x94[n\xdb0\x10E\xb7\xd1\x16}\x00m\xd1\xfe)Ȋ\x04Z3\xa4'\x96\x86\x04\x1f\x82\xbc\x85\xec\"\x88\x93%\xe6;\xe0P\xf0\x83\xa1\x9d\x1f\x83\xbas\xef\xe1\xd0\xe0\xf0\x00\xac&d\xc0\xb9\x9b\xc8#wA\x19\x85;b\bO˗K\xf9.˲z\x95T\xa8ʧ\xe8\x9b\x06;)⊫5\xe1\b\xe1\xf1yC\xb0\xfcl\xa4\xbb\xc1\xa3\x8a\b\xbd\x8a\xb2\xc3\xc3\xd9w\xdc;\x84H\x13J\xfaO+\r\xa8\x89)\x92\xe5>\xa7\x05ak1st\x88\x9e\xd8\b\xe9\xdf'\xa4\x19} \xcb\x02\xf3\r=\xf3\x06\xe2(\xb0\xaf-\x18zo\xbd\xe4\xb1,\xeb\x16\xfe6S\v\x0eI\xce>\xe4\xfd\x82\x00\xdc\a5\xb3p\xb3\x8f\x18\xae\xff/%\x94\x9b\xb6\x1e\xb0\xb4bk\xb1\x02\xfdn\x81\xe4\xecj\xec\x89]\x8a\xa5#\xae\xb4\n\xf3\xbf\x85q\xca#\xc7\xfe\xd4\x01A\xb9P\xadB\x06n\b\x0e\x99\xf6\xadE\vQ\xc5T\x9a\xd1\xebZ\xee\nr\x9av\xf9\xa7\x9f\u05580\xbch\xadhDX\xbe\xd7\x14\tu\xa5j\x1c2\x10\x9b\xe5G۵\x96\x8dO\xcc7lk\xd9$\x06{ö\x96i\xb0\x93\x1b1\",\xbf\xdaƣ\xe1\xfa\xec$\a\x17\xb3s\xf6}\x9c\x1d\xb3\xdeZ3߫\xd1m\xd5\xe8<M\xca\xef\xfb<\xb6\x901\xb5c\x17\xb6\xd6Ǿ\xbc\b\xe2\xb8\xfe,\xbc\x03\x00\x00\xff\xff\x01\x00\x00\xff\xff\xb3r?\xd8M\x04\x00\x00"))
}
