package entityserver_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/entity"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type entityData struct {
	Id        *string        `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Revision  *int64         `cbor:"1,keyasint,omitempty" json:"revision,omitempty"`
	CreatedAt *int64         `cbor:"2,keyasint,omitempty" json:"created_at,omitempty"`
	UpdatedAt *int64         `cbor:"3,keyasint,omitempty" json:"updated_at,omitempty"`
	Attrs     *[]entity.Attr `cbor:"4,keyasint,omitempty" json:"attrs,omitempty"`
}

type Entity struct {
	data entityData
}

func (v *Entity) HasId() bool {
	return v.data.Id != nil
}

func (v *Entity) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *Entity) SetId(id string) {
	v.data.Id = &id
}

func (v *Entity) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *Entity) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v *Entity) SetRevision(revision int64) {
	v.data.Revision = &revision
}

func (v *Entity) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *Entity) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *Entity) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *Entity) HasUpdatedAt() bool {
	return v.data.UpdatedAt != nil
}

func (v *Entity) UpdatedAt() int64 {
	if v.data.UpdatedAt == nil {
		return 0
	}
	return *v.data.UpdatedAt
}

func (v *Entity) SetUpdatedAt(updated_at int64) {
	v.data.UpdatedAt = &updated_at
}

func (v *Entity) HasAttrs() bool {
	return v.data.Attrs != nil
}

func (v *Entity) Attrs() []entity.Attr {
	if v.data.Attrs == nil {
		return nil
	}
	return *v.data.Attrs
}

func (v *Entity) SetAttrs(attrs []entity.Attr) {
	x := slices.Clone(attrs)
	v.data.Attrs = &x
}

func (v *Entity) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Entity) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Entity) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Entity) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityOpData struct {
	Entity    *Entity `cbor:"0,keyasint,omitempty" json:"entity,omitempty"`
	Previous  *int64  `cbor:"1,keyasint,omitempty" json:"previous,omitempty"`
	Operation *int64  `cbor:"2,keyasint,omitempty" json:"operation,omitempty"`
	EntityId  *string `cbor:"3,keyasint,omitempty" json:"entity_id,omitempty"`
}

type EntityOp struct {
	data entityOpData
}

func (v *EntityOp) HasEntity() bool {
	return v.data.Entity != nil
}

func (v *EntityOp) Entity() *Entity {
	return v.data.Entity
}

func (v *EntityOp) SetEntity(entity *Entity) {
	v.data.Entity = entity
}

func (v *EntityOp) HasPrevious() bool {
	return v.data.Previous != nil
}

func (v *EntityOp) Previous() int64 {
	if v.data.Previous == nil {
		return 0
	}
	return *v.data.Previous
}

func (v *EntityOp) SetPrevious(previous int64) {
	v.data.Previous = &previous
}

func (v *EntityOp) HasOperation() bool {
	return v.data.Operation != nil
}

func (v *EntityOp) Operation() int64 {
	if v.data.Operation == nil {
		return 0
	}
	return *v.data.Operation
}

func (v *EntityOp) SetOperation(operation int64) {
	v.data.Operation = &operation
}

func (v *EntityOp) HasEntityId() bool {
	return v.data.EntityId != nil
}

func (v *EntityOp) EntityId() string {
	if v.data.EntityId == nil {
		return ""
	}
	return *v.data.EntityId
}

func (v *EntityOp) SetEntityId(entity_id string) {
	v.data.EntityId = &entity_id
}

func (v *EntityOp) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityOp) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityOp) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityOp) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type parsedFileData struct {
	Format   *string    `cbor:"0,keyasint,omitempty" json:"format,omitempty"`
	Entities *[]*Entity `cbor:"1,keyasint,omitempty" json:"entities,omitempty"`
}

type ParsedFile struct {
	data parsedFileData
}

func (v *ParsedFile) HasFormat() bool {
	return v.data.Format != nil
}

func (v *ParsedFile) Format() string {
	if v.data.Format == nil {
		return ""
	}
	return *v.data.Format
}

func (v *ParsedFile) SetFormat(format string) {
	v.data.Format = &format
}

func (v *ParsedFile) HasEntities() bool {
	return v.data.Entities != nil
}

func (v *ParsedFile) Entities() []*Entity {
	if v.data.Entities == nil {
		return nil
	}
	return *v.data.Entities
}

func (v *ParsedFile) SetEntities(entities []*Entity) {
	x := slices.Clone(entities)
	v.data.Entities = &x
}

func (v *ParsedFile) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ParsedFile) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ParsedFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ParsedFile) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvArgsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type StreamRecvArgs struct {
	call *rpc.Call
	data streamRecvArgsData
}

func (v *StreamRecvArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *StreamRecvArgs) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *StreamRecvArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type StreamRecvResults struct {
	call *rpc.Call
	data streamRecvResultsData
}

func (v *StreamRecvResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *StreamRecvResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type StreamRecv struct {
	*rpc.Call
	args    StreamRecvArgs
	results StreamRecvResults
}

func (t *StreamRecv) Args() *StreamRecvArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *StreamRecv) Results() *StreamRecvResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Stream interface {
	Recv(ctx context.Context, state *StreamRecv) error
}

type reexportStream struct {
	client *rpc.Client
}

func (_ reexportStream) Recv(ctx context.Context, state *StreamRecv) error {
	panic("not implemented")
}

func (t reexportStream) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptStream(t Stream) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "recv",
			InterfaceName: "Stream",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Recv(ctx, &StreamRecv{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type StreamClient struct {
	*rpc.Client
}

func (c StreamClient) Export() Stream {
	return reexportStream{client: c.Client}
}

type StreamClientRecvResults struct {
	client *rpc.Client
	data   streamRecvResultsData
}

func (v *StreamClientRecvResults) HasData() bool {
	return v.data.Data != nil
}

func (v *StreamClientRecvResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v StreamClient) Recv(ctx context.Context, count int32) (*StreamClientRecvResults, error) {
	args := StreamRecvArgs{}
	args.data.Count = &count

	var ret streamRecvResultsData

	err := v.Client.Call(ctx, "recv", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &StreamClientRecvResults{client: v.Client, data: ret}, nil
}

type entityAccessGetArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type EntityAccessGetArgs struct {
	call *rpc.Call
	data entityAccessGetArgsData
}

func (v *EntityAccessGetArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *EntityAccessGetArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *EntityAccessGetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessGetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessGetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessGetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessGetResultsData struct {
	Entity *Entity `cbor:"0,keyasint,omitempty" json:"entity,omitempty"`
}

type EntityAccessGetResults struct {
	call *rpc.Call
	data entityAccessGetResultsData
}

func (v *EntityAccessGetResults) SetEntity(entity *Entity) {
	v.data.Entity = entity
}

func (v *EntityAccessGetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessGetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessGetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessGetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessPutArgsData struct {
	Entity *Entity `cbor:"0,keyasint,omitempty" json:"entity,omitempty"`
}

type EntityAccessPutArgs struct {
	call *rpc.Call
	data entityAccessPutArgsData
}

func (v *EntityAccessPutArgs) HasEntity() bool {
	return v.data.Entity != nil
}

func (v *EntityAccessPutArgs) Entity() *Entity {
	return v.data.Entity
}

func (v *EntityAccessPutArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessPutArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessPutArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessPutArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessPutResultsData struct {
	Revision *int64  `cbor:"0,keyasint,omitempty" json:"revision,omitempty"`
	Id       *string `cbor:"1,keyasint,omitempty" json:"id,omitempty"`
}

type EntityAccessPutResults struct {
	call *rpc.Call
	data entityAccessPutResultsData
}

func (v *EntityAccessPutResults) SetRevision(revision int64) {
	v.data.Revision = &revision
}

func (v *EntityAccessPutResults) SetId(id string) {
	v.data.Id = &id
}

func (v *EntityAccessPutResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessPutResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessPutResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessPutResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessDeleteArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type EntityAccessDeleteArgs struct {
	call *rpc.Call
	data entityAccessDeleteArgsData
}

func (v *EntityAccessDeleteArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *EntityAccessDeleteArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *EntityAccessDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessDeleteResultsData struct {
	Revision *int64 `cbor:"0,keyasint,omitempty" json:"revision,omitempty"`
}

type EntityAccessDeleteResults struct {
	call *rpc.Call
	data entityAccessDeleteResultsData
}

func (v *EntityAccessDeleteResults) SetRevision(revision int64) {
	v.data.Revision = &revision
}

func (v *EntityAccessDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessWatchIndexArgsData struct {
	Index  *entity.Attr    `cbor:"0,keyasint,omitempty" json:"index,omitempty"`
	Values *rpc.Capability `cbor:"1,keyasint,omitempty" json:"values,omitempty"`
}

type EntityAccessWatchIndexArgs struct {
	call *rpc.Call
	data entityAccessWatchIndexArgsData
}

func (v *EntityAccessWatchIndexArgs) HasIndex() bool {
	return v.data.Index != nil
}

func (v *EntityAccessWatchIndexArgs) Index() entity.Attr {
	return *v.data.Index
}

func (v *EntityAccessWatchIndexArgs) HasValues() bool {
	return v.data.Values != nil
}

func (v *EntityAccessWatchIndexArgs) Values() *stream.SendStreamClient[*EntityOp] {
	if v.data.Values == nil {
		return nil
	}
	return &stream.SendStreamClient[*EntityOp]{Client: v.call.NewClient(v.data.Values)}
}

func (v *EntityAccessWatchIndexArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessWatchIndexArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessWatchIndexArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessWatchIndexArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessWatchIndexResultsData struct{}

type EntityAccessWatchIndexResults struct {
	call *rpc.Call
	data entityAccessWatchIndexResultsData
}

func (v *EntityAccessWatchIndexResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessWatchIndexResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessWatchIndexResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessWatchIndexResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessWatchEntityArgsData struct {
	Id      *string         `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Updates *rpc.Capability `cbor:"1,keyasint,omitempty" json:"updates,omitempty"`
}

type EntityAccessWatchEntityArgs struct {
	call *rpc.Call
	data entityAccessWatchEntityArgsData
}

func (v *EntityAccessWatchEntityArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *EntityAccessWatchEntityArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *EntityAccessWatchEntityArgs) HasUpdates() bool {
	return v.data.Updates != nil
}

func (v *EntityAccessWatchEntityArgs) Updates() *stream.SendStreamClient[*EntityOp] {
	if v.data.Updates == nil {
		return nil
	}
	return &stream.SendStreamClient[*EntityOp]{Client: v.call.NewClient(v.data.Updates)}
}

func (v *EntityAccessWatchEntityArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessWatchEntityArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessWatchEntityArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessWatchEntityArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessWatchEntityResultsData struct{}

type EntityAccessWatchEntityResults struct {
	call *rpc.Call
	data entityAccessWatchEntityResultsData
}

func (v *EntityAccessWatchEntityResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessWatchEntityResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessWatchEntityResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessWatchEntityResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessListArgsData struct {
	Index *entity.Attr `cbor:"0,keyasint,omitempty" json:"index,omitempty"`
}

type EntityAccessListArgs struct {
	call *rpc.Call
	data entityAccessListArgsData
}

func (v *EntityAccessListArgs) HasIndex() bool {
	return v.data.Index != nil
}

func (v *EntityAccessListArgs) Index() entity.Attr {
	return *v.data.Index
}

func (v *EntityAccessListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessListResultsData struct {
	Values *[]*Entity `cbor:"0,keyasint,omitempty" json:"values,omitempty"`
}

type EntityAccessListResults struct {
	call *rpc.Call
	data entityAccessListResultsData
}

func (v *EntityAccessListResults) SetValues(values []*Entity) {
	x := slices.Clone(values)
	v.data.Values = &x
}

func (v *EntityAccessListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessMakeAttrArgsData struct {
	Id    *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Value *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type EntityAccessMakeAttrArgs struct {
	call *rpc.Call
	data entityAccessMakeAttrArgsData
}

func (v *EntityAccessMakeAttrArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *EntityAccessMakeAttrArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *EntityAccessMakeAttrArgs) HasValue() bool {
	return v.data.Value != nil
}

func (v *EntityAccessMakeAttrArgs) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *EntityAccessMakeAttrArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessMakeAttrArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessMakeAttrArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessMakeAttrArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessMakeAttrResultsData struct {
	Attr *entity.Attr `cbor:"0,keyasint,omitempty" json:"attr,omitempty"`
}

type EntityAccessMakeAttrResults struct {
	call *rpc.Call
	data entityAccessMakeAttrResultsData
}

func (v *EntityAccessMakeAttrResults) SetAttr(attr *entity.Attr) {
	v.data.Attr = attr
}

func (v *EntityAccessMakeAttrResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessMakeAttrResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessMakeAttrResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessMakeAttrResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessLookupKindArgsData struct {
	Kind *string `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
}

type EntityAccessLookupKindArgs struct {
	call *rpc.Call
	data entityAccessLookupKindArgsData
}

func (v *EntityAccessLookupKindArgs) HasKind() bool {
	return v.data.Kind != nil
}

func (v *EntityAccessLookupKindArgs) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *EntityAccessLookupKindArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessLookupKindArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessLookupKindArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessLookupKindArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessLookupKindResultsData struct {
	Attr *entity.Attr `cbor:"0,keyasint,omitempty" json:"attr,omitempty"`
}

type EntityAccessLookupKindResults struct {
	call *rpc.Call
	data entityAccessLookupKindResultsData
}

func (v *EntityAccessLookupKindResults) SetAttr(attr *entity.Attr) {
	v.data.Attr = attr
}

func (v *EntityAccessLookupKindResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessLookupKindResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessLookupKindResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessLookupKindResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessParseArgsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type EntityAccessParseArgs struct {
	call *rpc.Call
	data entityAccessParseArgsData
}

func (v *EntityAccessParseArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *EntityAccessParseArgs) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v *EntityAccessParseArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessParseArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessParseArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessParseArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessParseResultsData struct {
	File *ParsedFile `cbor:"0,keyasint,omitempty" json:"file,omitempty"`
}

type EntityAccessParseResults struct {
	call *rpc.Call
	data entityAccessParseResultsData
}

func (v *EntityAccessParseResults) SetFile(file *ParsedFile) {
	v.data.File = file
}

func (v *EntityAccessParseResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessParseResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessParseResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessParseResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessFormatArgsData struct {
	Entity *Entity `cbor:"0,keyasint,omitempty" json:"entity,omitempty"`
}

type EntityAccessFormatArgs struct {
	call *rpc.Call
	data entityAccessFormatArgsData
}

func (v *EntityAccessFormatArgs) HasEntity() bool {
	return v.data.Entity != nil
}

func (v *EntityAccessFormatArgs) Entity() *Entity {
	return v.data.Entity
}

func (v *EntityAccessFormatArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessFormatArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessFormatArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessFormatArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type entityAccessFormatResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type EntityAccessFormatResults struct {
	call *rpc.Call
	data entityAccessFormatResultsData
}

func (v *EntityAccessFormatResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *EntityAccessFormatResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EntityAccessFormatResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EntityAccessFormatResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EntityAccessFormatResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type EntityAccessGet struct {
	*rpc.Call
	args    EntityAccessGetArgs
	results EntityAccessGetResults
}

func (t *EntityAccessGet) Args() *EntityAccessGetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessGet) Results() *EntityAccessGetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessPut struct {
	*rpc.Call
	args    EntityAccessPutArgs
	results EntityAccessPutResults
}

func (t *EntityAccessPut) Args() *EntityAccessPutArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessPut) Results() *EntityAccessPutResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessDelete struct {
	*rpc.Call
	args    EntityAccessDeleteArgs
	results EntityAccessDeleteResults
}

func (t *EntityAccessDelete) Args() *EntityAccessDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessDelete) Results() *EntityAccessDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessWatchIndex struct {
	*rpc.Call
	args    EntityAccessWatchIndexArgs
	results EntityAccessWatchIndexResults
}

func (t *EntityAccessWatchIndex) Args() *EntityAccessWatchIndexArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessWatchIndex) Results() *EntityAccessWatchIndexResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessWatchEntity struct {
	*rpc.Call
	args    EntityAccessWatchEntityArgs
	results EntityAccessWatchEntityResults
}

func (t *EntityAccessWatchEntity) Args() *EntityAccessWatchEntityArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessWatchEntity) Results() *EntityAccessWatchEntityResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessList struct {
	*rpc.Call
	args    EntityAccessListArgs
	results EntityAccessListResults
}

func (t *EntityAccessList) Args() *EntityAccessListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessList) Results() *EntityAccessListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessMakeAttr struct {
	*rpc.Call
	args    EntityAccessMakeAttrArgs
	results EntityAccessMakeAttrResults
}

func (t *EntityAccessMakeAttr) Args() *EntityAccessMakeAttrArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessMakeAttr) Results() *EntityAccessMakeAttrResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessLookupKind struct {
	*rpc.Call
	args    EntityAccessLookupKindArgs
	results EntityAccessLookupKindResults
}

func (t *EntityAccessLookupKind) Args() *EntityAccessLookupKindArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessLookupKind) Results() *EntityAccessLookupKindResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessParse struct {
	*rpc.Call
	args    EntityAccessParseArgs
	results EntityAccessParseResults
}

func (t *EntityAccessParse) Args() *EntityAccessParseArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessParse) Results() *EntityAccessParseResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccessFormat struct {
	*rpc.Call
	args    EntityAccessFormatArgs
	results EntityAccessFormatResults
}

func (t *EntityAccessFormat) Args() *EntityAccessFormatArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EntityAccessFormat) Results() *EntityAccessFormatResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EntityAccess interface {
	Get(ctx context.Context, state *EntityAccessGet) error
	Put(ctx context.Context, state *EntityAccessPut) error
	Delete(ctx context.Context, state *EntityAccessDelete) error
	WatchIndex(ctx context.Context, state *EntityAccessWatchIndex) error
	WatchEntity(ctx context.Context, state *EntityAccessWatchEntity) error
	List(ctx context.Context, state *EntityAccessList) error
	MakeAttr(ctx context.Context, state *EntityAccessMakeAttr) error
	LookupKind(ctx context.Context, state *EntityAccessLookupKind) error
	Parse(ctx context.Context, state *EntityAccessParse) error
	Format(ctx context.Context, state *EntityAccessFormat) error
}

type reexportEntityAccess struct {
	client *rpc.Client
}

func (_ reexportEntityAccess) Get(ctx context.Context, state *EntityAccessGet) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) Put(ctx context.Context, state *EntityAccessPut) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) Delete(ctx context.Context, state *EntityAccessDelete) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) WatchIndex(ctx context.Context, state *EntityAccessWatchIndex) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) WatchEntity(ctx context.Context, state *EntityAccessWatchEntity) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) List(ctx context.Context, state *EntityAccessList) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) MakeAttr(ctx context.Context, state *EntityAccessMakeAttr) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) LookupKind(ctx context.Context, state *EntityAccessLookupKind) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) Parse(ctx context.Context, state *EntityAccessParse) error {
	panic("not implemented")
}

func (_ reexportEntityAccess) Format(ctx context.Context, state *EntityAccessFormat) error {
	panic("not implemented")
}

func (t reexportEntityAccess) CapabilityClient() *rpc.Client {
	return t.client
}

func AdaptEntityAccess(t EntityAccess) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "get",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Get(ctx, &EntityAccessGet{Call: call})
			},
		},
		{
			Name:          "put",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Put(ctx, &EntityAccessPut{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Delete(ctx, &EntityAccessDelete{Call: call})
			},
		},
		{
			Name:          "watch_index",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.WatchIndex(ctx, &EntityAccessWatchIndex{Call: call})
			},
		},
		{
			Name:          "watch_entity",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.WatchEntity(ctx, &EntityAccessWatchEntity{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.List(ctx, &EntityAccessList{Call: call})
			},
		},
		{
			Name:          "makeAttr",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.MakeAttr(ctx, &EntityAccessMakeAttr{Call: call})
			},
		},
		{
			Name:          "lookupKind",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.LookupKind(ctx, &EntityAccessLookupKind{Call: call})
			},
		},
		{
			Name:          "parse",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Parse(ctx, &EntityAccessParse{Call: call})
			},
		},
		{
			Name:          "format",
			InterfaceName: "EntityAccess",
			Index:         0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.Format(ctx, &EntityAccessFormat{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type EntityAccessClient struct {
	*rpc.Client
}

func (c EntityAccessClient) Export() EntityAccess {
	return reexportEntityAccess{client: c.Client}
}

type EntityAccessClientGetResults struct {
	client *rpc.Client
	data   entityAccessGetResultsData
}

func (v *EntityAccessClientGetResults) HasEntity() bool {
	return v.data.Entity != nil
}

func (v *EntityAccessClientGetResults) Entity() *Entity {
	return v.data.Entity
}

func (v EntityAccessClient) Get(ctx context.Context, id string) (*EntityAccessClientGetResults, error) {
	args := EntityAccessGetArgs{}
	args.data.Id = &id

	var ret entityAccessGetResultsData

	err := v.Client.Call(ctx, "get", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientGetResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientPutResults struct {
	client *rpc.Client
	data   entityAccessPutResultsData
}

func (v *EntityAccessClientPutResults) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *EntityAccessClientPutResults) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v *EntityAccessClientPutResults) HasId() bool {
	return v.data.Id != nil
}

func (v *EntityAccessClientPutResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v EntityAccessClient) Put(ctx context.Context, entity *Entity) (*EntityAccessClientPutResults, error) {
	args := EntityAccessPutArgs{}
	args.data.Entity = entity

	var ret entityAccessPutResultsData

	err := v.Client.Call(ctx, "put", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientPutResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientDeleteResults struct {
	client *rpc.Client
	data   entityAccessDeleteResultsData
}

func (v *EntityAccessClientDeleteResults) HasRevision() bool {
	return v.data.Revision != nil
}

func (v *EntityAccessClientDeleteResults) Revision() int64 {
	if v.data.Revision == nil {
		return 0
	}
	return *v.data.Revision
}

func (v EntityAccessClient) Delete(ctx context.Context, id string) (*EntityAccessClientDeleteResults, error) {
	args := EntityAccessDeleteArgs{}
	args.data.Id = &id

	var ret entityAccessDeleteResultsData

	err := v.Client.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientDeleteResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientWatchIndexResults struct {
	client *rpc.Client
	data   entityAccessWatchIndexResultsData
}

func (v EntityAccessClient) WatchIndex(ctx context.Context, index entity.Attr, values stream.SendStream[*EntityOp]) (*EntityAccessClientWatchIndexResults, error) {
	args := EntityAccessWatchIndexArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Index = &index
	{
		ic, oid, c := v.Client.NewInlineCapability(stream.AdaptSendStream[*EntityOp](values), values)
		args.data.Values = c
		caps[oid] = ic
	}

	var ret entityAccessWatchIndexResultsData

	err := v.Client.CallWithCaps(ctx, "watch_index", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientWatchIndexResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientWatchEntityResults struct {
	client *rpc.Client
	data   entityAccessWatchEntityResultsData
}

func (v EntityAccessClient) WatchEntity(ctx context.Context, id string, updates stream.SendStream[*EntityOp]) (*EntityAccessClientWatchEntityResults, error) {
	args := EntityAccessWatchEntityArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Id = &id
	{
		ic, oid, c := v.Client.NewInlineCapability(stream.AdaptSendStream[*EntityOp](updates), updates)
		args.data.Updates = c
		caps[oid] = ic
	}

	var ret entityAccessWatchEntityResultsData

	err := v.Client.CallWithCaps(ctx, "watch_entity", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientWatchEntityResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientListResults struct {
	client *rpc.Client
	data   entityAccessListResultsData
}

func (v *EntityAccessClientListResults) HasValues() bool {
	return v.data.Values != nil
}

func (v *EntityAccessClientListResults) Values() []*Entity {
	if v.data.Values == nil {
		return nil
	}
	return *v.data.Values
}

func (v EntityAccessClient) List(ctx context.Context, index entity.Attr) (*EntityAccessClientListResults, error) {
	args := EntityAccessListArgs{}
	args.data.Index = &index

	var ret entityAccessListResultsData

	err := v.Client.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientListResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientMakeAttrResults struct {
	client *rpc.Client
	data   entityAccessMakeAttrResultsData
}

func (v *EntityAccessClientMakeAttrResults) HasAttr() bool {
	return v.data.Attr != nil
}

func (v *EntityAccessClientMakeAttrResults) Attr() entity.Attr {
	return *v.data.Attr
}

func (v EntityAccessClient) MakeAttr(ctx context.Context, id string, value string) (*EntityAccessClientMakeAttrResults, error) {
	args := EntityAccessMakeAttrArgs{}
	args.data.Id = &id
	args.data.Value = &value

	var ret entityAccessMakeAttrResultsData

	err := v.Client.Call(ctx, "makeAttr", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientMakeAttrResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientLookupKindResults struct {
	client *rpc.Client
	data   entityAccessLookupKindResultsData
}

func (v *EntityAccessClientLookupKindResults) HasAttr() bool {
	return v.data.Attr != nil
}

func (v *EntityAccessClientLookupKindResults) Attr() entity.Attr {
	return *v.data.Attr
}

func (v EntityAccessClient) LookupKind(ctx context.Context, kind string) (*EntityAccessClientLookupKindResults, error) {
	args := EntityAccessLookupKindArgs{}
	args.data.Kind = &kind

	var ret entityAccessLookupKindResultsData

	err := v.Client.Call(ctx, "lookupKind", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientLookupKindResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientParseResults struct {
	client *rpc.Client
	data   entityAccessParseResultsData
}

func (v *EntityAccessClientParseResults) HasFile() bool {
	return v.data.File != nil
}

func (v *EntityAccessClientParseResults) File() *ParsedFile {
	return v.data.File
}

func (v EntityAccessClient) Parse(ctx context.Context, data []byte) (*EntityAccessClientParseResults, error) {
	args := EntityAccessParseArgs{}
	args.data.Data = &data

	var ret entityAccessParseResultsData

	err := v.Client.Call(ctx, "parse", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientParseResults{client: v.Client, data: ret}, nil
}

type EntityAccessClientFormatResults struct {
	client *rpc.Client
	data   entityAccessFormatResultsData
}

func (v *EntityAccessClientFormatResults) HasData() bool {
	return v.data.Data != nil
}

func (v *EntityAccessClientFormatResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v EntityAccessClient) Format(ctx context.Context, entity *Entity) (*EntityAccessClientFormatResults, error) {
	args := EntityAccessFormatArgs{}
	args.data.Entity = entity

	var ret entityAccessFormatResultsData

	err := v.Client.Call(ctx, "format", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EntityAccessClientFormatResults{client: v.Client, data: ret}, nil
}
