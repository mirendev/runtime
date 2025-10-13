package entityserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/meta/meta_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	etypes "miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/model"
)

type EntityServer struct {
	Log   *slog.Logger
	Store entity.Store

	tf *model.TextFormatter
}

func NewEntityServer(log *slog.Logger, store entity.Store) (*EntityServer, error) {
	sc, err := entity.NewSchemaCache(store)
	if err != nil {
		return nil, err
	}

	tf, err := model.NewTextFormatter(sc)
	if err != nil {
		return nil, err
	}

	return &EntityServer{
		Log:   log.With("module", "entityserver"),
		Store: store,
		tf:    tf,
	}, nil
}

var _ entityserver_v1alpha.EntityAccess = (*EntityServer)(nil)

func (e *EntityServer) Get(ctx context.Context, req *entityserver_v1alpha.EntityAccessGet) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	entity, err := e.Store.GetEntity(ctx, entity.Id(args.Id()))
	if err != nil {
		return cond.NotFound("entity", args.Id())
	}

	var rpcEntity entityserver_v1alpha.Entity
	rpcEntity.SetId(entity.Id().String())
	rpcEntity.SetCreatedAt(entity.GetCreatedAt().UnixMilli())
	rpcEntity.SetUpdatedAt(entity.GetUpdatedAt().UnixMilli())
	rpcEntity.SetRevision(entity.GetRevision())
	rpcEntity.SetAttrs(entity.Attrs)

	req.Results().SetEntity(&rpcEntity)

	return nil
}

func (e *EntityServer) WatchEntity(ctx context.Context, req *entityserver_v1alpha.EntityAccessWatchEntity) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	send := args.Updates()

	ch, err := e.Store.WatchEntity(ctx, entity.Id(args.Id()))
	if err != nil {
		return fmt.Errorf("failed to watch index: %w", err)
	}

	// Send the current value of the entity so that there is no race condition
	en, err := e.Store.GetEntity(ctx, entity.Id(args.Id()))
	if err == nil {
		var rpcEntity entityserver_v1alpha.Entity
		rpcEntity.SetId(en.Id().String())
		rpcEntity.SetCreatedAt(en.GetCreatedAt().UnixMilli())
		rpcEntity.SetUpdatedAt(en.GetUpdatedAt().UnixMilli())
		rpcEntity.SetRevision(en.GetRevision())
		rpcEntity.SetAttrs(en.Attrs)

		var op entityserver_v1alpha.EntityOp
		op.SetOperation(1)
		op.SetEntity(&rpcEntity)

		_, err = send.Send(ctx, &op)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
				e.Log.Error("failed to send event", "error", err)
			}
			return nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}

			var (
				eventType int
				read      bool
			)

			switch event.Type {
			case entity.EntityOpCreate:
				eventType = 1
				read = true
			case entity.EntityOpUpdate, entity.EntityOpStated:
				eventType = 2
				read = true
			case entity.EntityOpDelete:
				eventType = 3
			default:
				continue
			}

			var op entityserver_v1alpha.EntityOp
			op.SetOperation(int64(eventType))

			if read {
				en = event.Entity
				var rpcEntity entityserver_v1alpha.Entity
				rpcEntity.SetId(en.Id().String())
				rpcEntity.SetCreatedAt(en.GetCreatedAt().UnixMilli())
				rpcEntity.SetUpdatedAt(en.GetUpdatedAt().UnixMilli())
				rpcEntity.SetRevision(en.GetRevision())
				rpcEntity.SetAttrs(en.Attrs)

				op.SetEntity(&rpcEntity)
			}

			_, err = send.Send(ctx, &op)
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
					e.Log.Error("failed to send event", "error", err)
				}
				return nil
			}
		}
	}
}

func (e *EntityServer) Put(ctx context.Context, req *entityserver_v1alpha.EntityAccessPut) error {
	args := req.Args()

	if !args.HasEntity() {
		return fmt.Errorf("missing required field: entity")
	}

	rpcE := args.Entity()

	e.Log.Debug("starting put for entity", "id", rpcE.Id(), "revision", rpcE.Revision())

	attrs := rpcE.Attrs()
	if len(attrs) == 0 {
		return fmt.Errorf("missing required field: attrs")
	}

	results := req.Results()

	var opts []entity.EntityOption

	if rpcE.HasId() {
		e.Log.Debug("updating entity", "id", rpcE.Id(), "revision", rpcE.Revision())

		// If the entity has a revision, then make sure that we're updating that specific entity.
		if rev := rpcE.Revision(); rev > 0 {
			opts = append(opts, entity.WithFromRevision(rev))
		}

		re, err := e.Store.UpdateEntity(ctx, entity.Id(rpcE.Id()), attrs, opts...)
		if err != nil {
			if !errors.Is(err, cond.ErrNotFound{}) {
				// We got an error that _wasn't_ a not found error, so we should return it
				return fmt.Errorf("failed to update entity in put: %w", err)
			}
			// Otherwise we got a not found error, so we can fall through to create the entity
		} else {
			results.SetRevision(re.GetRevision())
			results.SetId(re.Id().String())
			return nil
		}
	}

	re, err := e.Store.CreateEntity(ctx, attrs, opts...)
	if err != nil {
		return fmt.Errorf("failed to create entity in put: %w", err)
	}

	results.SetRevision(re.GetRevision())
	results.SetId(re.Id().String())

	return nil
}

func (e *EntityServer) Create(ctx context.Context, req *entityserver_v1alpha.EntityAccessCreate) error {
	args := req.Args()

	attrs := args.Attrs()
	if len(attrs) == 0 {
		return cond.ValidationFailure("missing-field", "attrs")
	}

	entity, err := e.Store.CreateEntity(ctx, attrs)
	if err != nil {
		return fmt.Errorf("failed to create entity: %w", err)
	}

	results := req.Results()
	results.SetRevision(entity.GetRevision())
	results.SetId(entity.Id().String())

	return nil
}

func (e *EntityServer) Replace(ctx context.Context, req *entityserver_v1alpha.EntityAccessReplace) error {
	args := req.Args()

	attrs := args.Attrs()
	if len(attrs) == 0 {
		return cond.ValidationFailure("missing-field", "attrs")
	}

	// Extract ID from attrs to validate it's present
	var hasId bool
	for _, attr := range attrs {
		if attr.ID == entity.DBId {
			hasId = true
			break
		}
	}
	if !hasId {
		return cond.ValidationFailure("missing-field", "db/id attribute is required")
	}

	var opts []entity.EntityOption
	if args.HasRevision() && args.Revision() > 0 {
		opts = append(opts, entity.WithFromRevision(args.Revision()))
	}

	ent, err := e.Store.ReplaceEntity(ctx, attrs, opts...)
	if err != nil {
		return fmt.Errorf("failed to replace entity: %w", err)
	}

	results := req.Results()
	results.SetRevision(ent.GetRevision())
	results.SetId(ent.Id().String())

	return nil
}

func (e *EntityServer) Patch(ctx context.Context, req *entityserver_v1alpha.EntityAccessPatch) error {
	args := req.Args()

	attrs := args.Attrs()
	if len(attrs) == 0 {
		return cond.ValidationFailure("missing-field", "attrs")
	}

	// Extract ID from attrs to validate it's present
	var hasId bool
	for _, attr := range attrs {
		if attr.ID == entity.DBId {
			hasId = true
			break
		}
	}
	if !hasId {
		return cond.ValidationFailure("missing-field", "db/id attribute is required")
	}

	var opts []entity.EntityOption
	if args.HasRevision() && args.Revision() > 0 {
		opts = append(opts, entity.WithFromRevision(args.Revision()))
	}

	ent, err := e.Store.PatchEntity(ctx, attrs, opts...)
	if err != nil {
		return fmt.Errorf("failed to patch entity: %w", err)
	}

	results := req.Results()
	results.SetRevision(ent.GetRevision())
	results.SetId(ent.Id().String())

	return nil
}

func (e *EntityServer) Ensure(ctx context.Context, req *entityserver_v1alpha.EntityAccessEnsure) error {
	args := req.Args()

	attrs := args.Attrs()
	if len(attrs) == 0 {
		return cond.ValidationFailure("missing-field", "attrs")
	}

	// Extract ID from attrs to validate it's present
	var hasId bool
	for _, attr := range attrs {
		if attr.ID == entity.DBId {
			hasId = true
			break
		}
	}
	if !hasId {
		return cond.ValidationFailure("missing-field", "db/id attribute is required")
	}

	ent, created, err := e.Store.EnsureEntity(ctx, attrs)
	if err != nil {
		return fmt.Errorf("failed to ensure entity: %w", err)
	}

	results := req.Results()
	results.SetRevision(ent.GetRevision())
	results.SetId(ent.Id().String())
	results.SetCreated(created)

	return nil
}

func (e *EntityServer) PutSession(ctx context.Context, req *entityserver_v1alpha.EntityAccessPutSession) error {
	args := req.Args()

	if !args.HasEntity() {
		return fmt.Errorf("missing required field: entity")
	}

	rpcE := args.Entity()

	attrs := rpcE.Attrs()
	if len(attrs) == 0 {
		return fmt.Errorf("missing required field: attrs")
	}

	session := args.Session()
	if session == "" {
		return cond.ValidationFailure("missing-field", "session")
	}

	data, err := base58.Decode(session)
	if err != nil {
		return cond.ValidationFailure("invalid-field", "session")
	}

	results := req.Results()

	var opts []entity.EntityOption

	opts = append(opts, entity.WithSession(data))

	if rpcE.HasId() {
		re, err := e.Store.UpdateEntity(ctx, entity.Id(rpcE.Id()), attrs, opts...)
		if err != nil {
			if !errors.Is(err, entity.ErrNotFound) {
				return fmt.Errorf("failed to create entity: %w", err)
			}
		} else {
			results.SetRevision(re.GetRevision())
			results.SetId(re.Id().String())
		}
	} else {
		re, err := e.Store.CreateEntity(ctx, attrs, opts...)
		if err != nil {
			return fmt.Errorf("failed to create entity: %w", err)
		}

		results.SetRevision(re.GetRevision())
		results.SetId(re.Id().String())

	}
	return nil
}

func (e *EntityServer) Delete(ctx context.Context, req *entityserver_v1alpha.EntityAccessDelete) error {
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

func (e *EntityServer) WatchIndex(ctx context.Context, req *entityserver_v1alpha.EntityAccessWatchIndex) error {
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

			// Check if the watch was canceled or had an error
			if watchevent.Canceled {
				if err := watchevent.Err(); err != nil {
					return fmt.Errorf("watch canceled with error: %w", err)
				}
				return fmt.Errorf("watch canceled")
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

				var op entityserver_v1alpha.EntityOp
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

					var rpcEntity entityserver_v1alpha.Entity
					rpcEntity.SetId(en.Id().String())
					rpcEntity.SetCreatedAt(en.GetCreatedAt().UnixMilli())
					rpcEntity.SetUpdatedAt(en.GetUpdatedAt().UnixMilli())
					rpcEntity.SetRevision(en.GetRevision())
					rpcEntity.SetAttrs(en.Attrs)

					op.SetEntity(&rpcEntity)
				} else if event.PrevKv != nil {
					op.SetEntityId(string(event.PrevKv.Value))
					op.SetPrevious(event.PrevKv.ModRevision)
				}

				_, err = send.Send(ctx, &op)
				if err != nil {
					if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
						e.Log.Error("failed to send event", "error", err)
					}
					return nil
				}
			}
		}
	}
}

func (e *EntityServer) List(ctx context.Context, req *entityserver_v1alpha.EntityAccessList) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	var (
		ids []entity.Id
		err error
	)

	index := args.Index()

	if index.ID == entity.AttrSession {
		str := index.Value.String()

		data, decodeErr := base58.Decode(str)
		if decodeErr != nil {
			return fmt.Errorf("invalid session id: %w", decodeErr)
		}

		ids, err = e.Store.ListSessionEntities(ctx, data)
	} else {
		ids, err = e.Store.ListIndex(ctx, args.Index())
	}

	if err != nil {
		return fmt.Errorf("failed to list entities: %w", err)
	}

	// Use batch retrieval for better performance
	entities, err := e.Store.GetEntities(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get entities: %w", err)
	}

	var ret []*entityserver_v1alpha.Entity
	for i, entity := range entities {
		if entity == nil {
			return fmt.Errorf("entity not found: %s", ids[i])
		}

		var rpcEntity entityserver_v1alpha.Entity
		rpcEntity.SetId(entity.Id().String())
		rpcEntity.SetCreatedAt(entity.GetCreatedAt().UnixMilli())
		rpcEntity.SetUpdatedAt(entity.GetUpdatedAt().UnixMilli())
		rpcEntity.SetRevision(entity.GetRevision())
		rpcEntity.SetAttrs(entity.Attrs)

		ret = append(ret, &rpcEntity)
	}

	req.Results().SetValues(ret)

	return nil
}

func (e *EntityServer) MakeAttr(ctx context.Context, req *entityserver_v1alpha.EntityAccessMakeAttr) error {
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

func (e *EntityServer) LookupKind(ctx context.Context, req *entityserver_v1alpha.EntityAccessLookupKind) error {
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
		attr := entity.Ref(entity.EntityKind, entity.Id(args.Kind()))
		req.Results().SetAttr(&attr)
		return nil
	}

	if kind, ok := es.ShortKinds[args.Kind()]; ok {
		attr := entity.Ref(entity.EntityKind, entity.Id(kind))
		req.Results().SetAttr(&attr)
		return nil
	}

	return fmt.Errorf("kind '%s' not found", args.Kind())
}

func (e *EntityServer) Parse(ctx context.Context, req *entityserver_v1alpha.EntityAccessParse) error {
	args := req.Args()

	data := args.Data()

	pf, err := e.tf.Parse(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to parse entity: %w", err)
	}

	var ents []*entityserver_v1alpha.Entity
	for _, ent := range pf.Entities {
		var rpcEntity entityserver_v1alpha.Entity
		rpcEntity.SetAttrs(ent.Attrs)
		if ent.Id() != "" {
			rpcEntity.SetId(ent.Id().String())
		}

		ents = append(ents, &rpcEntity)
	}

	var rpcPF entityserver_v1alpha.ParsedFile
	rpcPF.SetEntities(ents)
	rpcPF.SetFormat(pf.Format)

	req.Results().SetFile(&rpcPF)
	return nil
}

func (e *EntityServer) Format(ctx context.Context, req *entityserver_v1alpha.EntityAccessFormat) error {
	args := req.Args()

	ent := args.Entity().Entity()

	data, err := e.tf.Format(ctx, ent)
	if err != nil {
		return fmt.Errorf("failed to format entity: %w", err)
	}

	req.Results().SetData([]byte(data))
	return nil
}

func (e *EntityServer) CreateSession(ctx context.Context, req *entityserver_v1alpha.EntityAccessCreateSession) error {
	args := req.Args()

	if !args.HasTtl() {
		return cond.ValidationFailure("missing-field", "ttl")
	}

	ttl := args.Ttl()

	if ttl == 0 {
		return cond.ValidationFailure("invalid-field", "id")
	}

	id, err := e.Store.CreateSession(ctx, ttl)
	if err != nil {
		return err
	}

	nice := base58.Encode(id)

	var sess meta_v1alpha.Session
	sess.Usage = args.Usage()
	sess.UniqueId = nice

	_, err = e.Store.CreateEntity(ctx, entity.Attrs(
		entity.Ident, "session/"+nice,
		sess.Encode,
		(&core_v1alpha.Metadata{
			Name: "session/" + nice,
		}).Encode,
	), entity.BondToSession(id))
	if err != nil {
		e.Log.Error("failed to create session entity", "error", err)
	}

	req.Results().SetId(base58.Encode(id))

	return nil
}

// RevokeLease
func (e *EntityServer) RevokeSession(ctx context.Context, req *entityserver_v1alpha.EntityAccessRevokeSession) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	id := args.Id()

	if id == "" {
		return cond.ValidationFailure("invalid-field", "id")
	}

	data, err := base58.Decode(id)
	if err != nil {
		return cond.ValidationFailure("invalid-field", "id")
	}

	err = e.Store.RevokeSession(ctx, data)
	if err != nil {
		return err
	}

	return nil
}

// AssertLease keeps the lease alive
func (e *EntityServer) PingSession(ctx context.Context, req *entityserver_v1alpha.EntityAccessPingSession) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	id := args.Id()

	if id == "" {
		return cond.ValidationFailure("invalid-field", "id")
	}

	data, err := base58.Decode(id)
	if err != nil {
		return cond.ValidationFailure("invalid-field", "id")
	}

	err = e.Store.PingSession(ctx, data)
	if err != nil {
		return err
	}

	return nil
}
