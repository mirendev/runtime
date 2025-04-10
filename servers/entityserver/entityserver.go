package entityserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/fxamacker/cbor/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/pkg/entity"
	etypes "miren.dev/runtime/pkg/entity/types"
)

type EntityServer struct {
	Log   *slog.Logger
	Store entity.Store
}

var _ v1alpha.EntityAccess = (*EntityServer)(nil)

func (e *EntityServer) Get(ctx context.Context, req *v1alpha.EntityAccessGet) error {
	args := req.Args()

	if !args.HasId() {
		return fmt.Errorf("missing required field: id")
	}

	entity, err := e.Store.GetEntity(ctx, entity.Id(args.Id()))
	if err != nil {
		return fmt.Errorf("failed to get entity: %w", err)
	}

	var rpcEntity v1alpha.Entity
	rpcEntity.SetId(entity.ID.String())
	rpcEntity.SetCreatedAt(entity.CreatedAt)
	rpcEntity.SetUpdatedAt(entity.UpdatedAt)
	rpcEntity.SetRevision(entity.Revision)
	rpcEntity.SetAttrs(entity.Attrs)

	req.Results().SetEntity(&rpcEntity)

	return nil
}

func (e *EntityServer) Put(ctx context.Context, req *v1alpha.EntityAccessPut) error {
	args := req.Args()

	if !args.HasEntity() {
		return fmt.Errorf("missing required field: entity")
	}

	rpcE := args.Entity()

	attrs := rpcE.Attrs()
	if len(attrs) == 0 {
		return fmt.Errorf("missing required field: attrs")
	}

	results := req.Results()

	if rpcE.HasId() {
		// TODO: handle updated_at
		re, err := e.Store.UpdateEntity(ctx, entity.Id(rpcE.Id()), attrs)
		if err != nil {
			return fmt.Errorf("failed to create entity: %w", err)
		}

		e.Log.Debug("updated entity", "id", re.ID)
		results.SetRevision(re.Revision)
		results.SetId(re.ID.String())
	} else {
		// TODO: handle created_at and updated_at fileds
		re, err := e.Store.CreateEntity(ctx, attrs)
		if err != nil {
			return fmt.Errorf("failed to create entity: %w", err)
		}

		e.Log.Debug("created entity", "id", re.ID)
		results.SetRevision(re.Revision)
		results.SetId(re.ID.String())
	}

	return nil
}

func (e *EntityServer) Delete(ctx context.Context, req *v1alpha.EntityAccessDelete) error {
	args := req.Args()

	if !args.HasId() {
		return fmt.Errorf("missing required field: id")
	}

	id := args.Id()

	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}

	return e.Store.DeleteEntity(ctx, entity.Id(args.Id()))
}

func (e *EntityServer) WatchIndex(ctx context.Context, req *v1alpha.EntityAccessWatchIndex) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	if !args.HasValues() {
		return fmt.Errorf("missing required field: values")
	}

	send := args.Values()

	ch, err := e.Store.WatchIndex(ctx, args.Index())
	if err != nil {
		return fmt.Errorf("failed to watch index: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case watchevent, ok := <-ch:
			if !ok {
				return nil
			}

			for _, event := range watchevent.Events {
				var (
					eventType int
					read      bool
				)

				switch {
				case event.IsCreate():
					eventType = 1
					read = true
				case event.IsModify():
					eventType = 2
					read = true
				case event.Type == clientv3.EventTypeDelete:
					eventType = 3
				default:
					continue
				}

				var op v1alpha.EntityOp
				op.SetOperation(int64(eventType))

				if read {
					op.SetEntityId(string(event.Kv.Value))
					en, err := e.Store.GetEntity(ctx, entity.Id(event.Kv.Value))
					if err != nil {
						e.Log.Error("failed to get entity for event", "error", err, "id", event.Kv.Value)
						continue
					}

					if event.PrevKv != nil {
						op.SetPrevious(event.PrevKv.ModRevision)
					}

					var rpcEntity v1alpha.Entity
					rpcEntity.SetId(en.ID.String())
					rpcEntity.SetCreatedAt(en.CreatedAt)
					rpcEntity.SetUpdatedAt(en.UpdatedAt)
					rpcEntity.SetRevision(en.Revision)
					rpcEntity.SetAttrs(en.Attrs)

					op.SetEntity(&rpcEntity)
				} else if event.PrevKv != nil {
					op.SetEntityId(string(event.PrevKv.Value))
					op.SetPrevious(event.PrevKv.ModRevision)
				}

				_, err = send.Send(ctx, &op)
				if err != nil {
					e.Log.Error("failed to send event", "error", err)
				}
			}
		}
	}
}

func (e *EntityServer) List(ctx context.Context, req *v1alpha.EntityAccessList) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	ids, err := e.Store.ListIndex(ctx, args.Index())
	if err != nil {
		return fmt.Errorf("failed to list entities: %w", err)
	}

	var ret []*v1alpha.Entity

	for _, id := range ids {

		entity, err := e.Store.GetEntity(ctx, entity.Id(id))
		if err != nil {
			return fmt.Errorf("failed to get entity: %w", err)
		}

		var rpcEntity v1alpha.Entity
		rpcEntity.SetId(entity.ID.String())
		rpcEntity.SetCreatedAt(entity.CreatedAt)
		rpcEntity.SetUpdatedAt(entity.UpdatedAt)
		rpcEntity.SetRevision(entity.Revision)
		rpcEntity.SetAttrs(entity.Attrs)

		ret = append(ret, &rpcEntity)
	}

	req.Results().SetValues(ret)

	return nil
}

func (e *EntityServer) MakeAttr(ctx context.Context, req *v1alpha.EntityAccessMakeAttr) error {
	args := req.Args()

	if !args.HasId() {
		return fmt.Errorf("missing required field: name")
	}

	if !args.HasValue() {
		return fmt.Errorf("missing required field: value")
	}

	id := entity.Id(args.Id())

	schema, err := e.Store.GetAttributeSchema(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	var value entity.Value

	switch schema.Type {
	case entity.TypeStr:
		value = entity.StringValue(args.Value())

	case entity.TypeInt:
		i, err := strconv.ParseInt(args.Value(), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer value: %w", err)
		}

		value = entity.IntValue(int(i))

	case entity.TypeFloat:
		f, err := strconv.ParseFloat(args.Value(), 64)
		if err != nil {
			return fmt.Errorf("invalid float value: %w", err)
		}
		value = entity.Float64Value(f)

	case entity.TypeBool:
		b, err := strconv.ParseBool(args.Value())
		if err != nil {
			return fmt.Errorf("invalid boolean value: %w", err)
		}
		value = entity.BoolValue(b)

	case entity.TypeRef:
		value = entity.RefValue(entity.Id(args.Value()))

	case entity.TypeTime:
		// Try RFC3339 first
		t, err := time.Parse(time.RFC3339, args.Value())
		if err != nil {
			// Try RFC3339Nano
			t, err = time.Parse(time.RFC3339Nano, args.Value())
			if err != nil {
				return fmt.Errorf("invalid time value, must be RFC3339 or RFC3339Nano format: %w", err)
			}
		}
		value = entity.TimeValue(t)

	case entity.TypeDuration:
		d, err := time.ParseDuration(args.Value())
		if err != nil {
			return fmt.Errorf("invalid duration value: %w", err)
		}
		value = entity.DurationValue(d)

	case entity.TypeKeyword:
		if !entity.ValidKeyword(args.Value()) {
			return fmt.Errorf("invalid keyword value: %s", args.Value())
		}
		value = entity.KeywordValue(etypes.Keyword(args.Value()))

	case entity.TypeBytes:
		b, err := base64.StdEncoding.DecodeString(args.Value())
		if err != nil {
			return fmt.Errorf("invalid base64 encoded bytes: %w", err)
		}
		value = entity.BytesValue(b)

	case entity.TypeLabel:
		parts := strings.SplitN(args.Value(), "=", 2)
		if len(parts) == 1 {
			value = entity.LabelValue(parts[0], "")
		} else {
			value = entity.LabelValue(parts[0], parts[1])
		}

	case entity.TypeEnum:
		value = entity.RefValue(id)

		// Look up the enum value in the schema
		if !slices.ContainsFunc(schema.EnumValues, func(v entity.Value) bool {
			return v.Equal(value)
		}) {
			return fmt.Errorf("invalid enum value: %s", args.Value())
		}

	default:
		return fmt.Errorf("unsupported attribute type: %s", schema.Type)
	}

	req.Results().SetAttr(&entity.Attr{ID: id, Value: value})

	return nil
}

func (e *EntityServer) LookupKind(ctx context.Context, req *v1alpha.EntityAccessLookupKind) error {
	args := req.Args()

	if !args.HasKind() {
		return fmt.Errorf("missing required field: kind")
	}

	ids, err := e.Store.ListIndex(ctx, entity.Keyword(entity.SchemaKind, args.Kind()))
	if err != nil {
		return fmt.Errorf("failed to lookup kind '%s': %w", args.Kind(), err)
	}

	switch {
	case len(ids) == 0:
		return fmt.Errorf("kind '%s' not found", args.Kind())
	case len(ids) > 1:
		return fmt.Errorf("kind '%s' is ambiguous, %d schemas found", args.Kind(), len(ids))
	}

	schema, err := e.Store.GetEntity(ctx, ids[0])
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	sa, ok := schema.Get(entity.Schema)
	if !ok {
		return fmt.Errorf("corrupt missing schema")
	}

	var es entity.EncodedDomain

	gr, err := gzip.NewReader(bytes.NewReader(sa.Value.Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gr.Close()

	err = cbor.NewDecoder(gr).Decode(&es)
	if err != nil {
		return fmt.Errorf("failed to decode schema: %w", err)
	}

	if _, ok := es.Kinds[args.Kind()]; ok {
		attr := entity.Keyword(entity.EntityKind, args.Kind())
		req.Results().SetAttr(&attr)
		return nil
	}

	spew.Dump(es.ShortKinds)

	if kind, ok := es.ShortKinds[args.Kind()]; ok {
		attr := entity.Keyword(entity.EntityKind, kind)
		req.Results().SetAttr(&attr)
		return nil
	}

	return fmt.Errorf("kind '%s' not found", args.Kind())
}

func (e *EntityServer) Parse(ctx context.Context, req *v1alpha.EntityAccessParse) error {
	args := req.Args()

	data := args.Data()

	var x any

	err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&x)
	if err != nil {
		return err
	}

	m, ok := x.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid entity format")
	}

	kind, ok := m["kind"].(string)
	if !ok {
		return fmt.Errorf("missing kind")
	}

	version, ok := m["version"].(string)
	if !ok {
		return fmt.Errorf("missing version")
	}

	domain, kind, ok := strings.Cut(kind, "/")
	if !ok {
		return fmt.Errorf("invalid kind format")
	}

	schema, err := e.Store.GetEntity(ctx, entity.Id("schema."+domain+"/"+version))
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	esch, ok := schema.Get(entity.Schema)
	if !ok {
		return fmt.Errorf("missing schema")
	}

	var ed entity.EncodedDomain
	gr, err := gzip.NewReader(bytes.NewReader(esch.Value.Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gr.Close()

	err = cbor.NewDecoder(gr).Decode(&ed)
	if err != nil {
		return fmt.Errorf("failed to decode schema: %w", err)
	}

	es := ed.Kinds[kind]
	if es == nil {
		return fmt.Errorf("unknown kind: %s", kind)
	}

	attrs, err := entity.NaturalDecode(x, es)
	if err != nil {
		return fmt.Errorf("failed to decode entity: %w", err)
	}

	var rpcEntity v1alpha.Entity
	rpcEntity.SetAttrs(attrs)

	req.Results().SetEntity(&rpcEntity)
	return nil
}

func (e *EntityServer) Format(ctx context.Context, req *v1alpha.EntityAccessFormat) error {
	args := req.Args()

	ent := args.Entity().Entity()

	sid, ok := ent.Get(entity.EntitySchema)
	if !ok {
		req.Results().SetData([]byte(spew.Sdump(ent)))
		return nil
	}

	schema, err := e.Store.GetEntity(ctx, sid.Value.Id())
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	esch, ok := schema.Get(entity.Schema)
	if !ok {
		return fmt.Errorf("missing schema")
	}

	var ed entity.EncodedDomain
	gr, err := gzip.NewReader(bytes.NewReader(esch.Value.Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gr.Close()

	err = cbor.NewDecoder(gr).Decode(&ed)
	if err != nil {
		return fmt.Errorf("failed to decode schema: %w", err)
	}

	var results []any

	for _, kind := range ent.GetAll(entity.EntityKind) {
		es, ok := ed.Kinds[string(kind.Value.Keyword())]
		if !ok {
			continue
		}
		m, err := entity.NaturalEncode(ent, es)
		if err != nil {
			return fmt.Errorf("failed to encode entity: %w", err)
		}
		results = append(results, m)
	}

	var n yaml.Node
	err = n.Encode(map[string]any{
		"attrs": ent.Attrs,
	})

	var n2 yaml.Node
	err = n2.Encode(map[string]any{
		"kinds": results,
	})

	n2.Content = append(n2.Content, n.Content...)

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	err = enc.Encode(&n2)
	if err != nil {
		return fmt.Errorf("failed to encode entity: %w", err)
	}

	req.Results().SetData(buf.Bytes())

	return nil
}
