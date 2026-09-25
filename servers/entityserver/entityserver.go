package entityserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
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
	sc *entity.SchemaCache
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
		sc:    sc,
	}, nil
}

var _ entityserver_v1alpha.EntityAccess = (*EntityServer)(nil)

func (e *EntityServer) Get(ctx context.Context, req *entityserver_v1alpha.EntityAccessGet) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	ent, err := e.resolveEntity(ctx, args.Id())
	if err != nil {
		return err
	}

	var rpcEntity entityserver_v1alpha.Entity
	rpcEntity.SetId(ent.Id().String())
	rpcEntity.SetCreatedAt(ent.GetCreatedAt().UnixMilli())
	rpcEntity.SetUpdatedAt(ent.GetUpdatedAt().UnixMilli())
	rpcEntity.SetRevision(ent.GetRevision())
	rpcEntity.SetAttrs(ent.Attrs())

	req.Results().SetEntity(&rpcEntity)

	return nil
}

// isNotFound reports whether err means the entity is absent, as opposed to the
// store being unable to answer.
func isNotFound(err error) bool {
	return errors.Is(err, entity.ErrEntityNotFound) || errors.Is(err, cond.ErrNotFound{})
}

// resolveEntity looks an entity up by full id, falling back to the short-id
// index. Operators type the short id the CLI printed at them, so every
// id-taking read path needs this, not just Get.
func (e *EntityServer) resolveEntity(ctx context.Context, id string) (*entity.Entity, error) {
	ent, err := e.Store.GetEntity(ctx, entity.Id(id))
	if err == nil {
		return ent, nil
	}

	// Only a genuine miss is worth a second guess. GetEntity also reports
	// transport failures and undecodable records, and turning those into
	// "not found" tells an operator their entity is gone when the truth is
	// that etcd is unreachable or the record is corrupt.
	if !isNotFound(err) {
		return nil, err
	}

	etcdStore, ok := e.Store.(*entity.EtcdStore)
	if !ok {
		return nil, cond.NotFound("entity", id)
	}

	if resolvedId, idxErr := etcdStore.GetOneIndex(ctx, entity.String(entity.DBShortId, id)); idxErr == nil {
		if ent, err = e.Store.GetEntity(ctx, resolvedId); err == nil {
			return ent, nil
		}
	}

	// A kind-prefixed short id like "sandbox/3sA": try the bare suffix.
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		bare := id[idx+1:]
		if resolvedId, idxErr := etcdStore.GetOneIndex(ctx, entity.String(entity.DBShortId, bare)); idxErr == nil {
			if ent, err = e.Store.GetEntity(ctx, resolvedId); err == nil {
				return ent, nil
			}
		}
	}

	return nil, cond.NotFound("entity", id)
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
		rpcEntity.SetAttrs(en.Attrs())

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

			var eventType int

			switch event.Type {
			case entity.EntityOpCreate:
				eventType = 1
			case entity.EntityOpUpdate, entity.EntityOpStated:
				eventType = 2
			case entity.EntityOpDelete:
				eventType = 3
			default:
				continue
			}

			var op entityserver_v1alpha.EntityOp
			op.SetOperation(int64(eventType))

			if event.Entity != nil {
				en = event.Entity
				var rpcEntity entityserver_v1alpha.Entity
				rpcEntity.SetId(en.Id().String())
				rpcEntity.SetCreatedAt(en.GetCreatedAt().UnixMilli())
				rpcEntity.SetUpdatedAt(en.GetUpdatedAt().UnixMilli())
				rpcEntity.SetRevision(en.GetRevision())
				rpcEntity.SetAttrs(en.Attrs())

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

	attrs := rpcE.Attrs()
	if len(attrs) == 0 {
		return fmt.Errorf("missing required field: attrs")
	}

	results := req.Results()

	var opts []entity.EntityOption

	if rpcE.HasId() {
		// If the entity has a revision, then make sure that we're updating that specific entity.
		if rev := rpcE.Revision(); rev > 0 {
			opts = append(opts, entity.WithFromRevision(rev))
		}

		re, err := e.Store.UpdateEntity(ctx, entity.Id(rpcE.Id()), entity.New(attrs), opts...)
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

	re, err := e.Store.CreateEntity(ctx, entity.New(attrs), opts...)
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

	entity, err := e.Store.CreateEntity(ctx, entity.New(attrs))
	if err != nil {
		return err
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

	ent, err := e.Store.ReplaceEntity(ctx, entity.New(attrs), opts...)
	if err != nil {
		return err
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

	ent, err := e.Store.PatchEntity(ctx, entity.New(attrs), opts...)
	if err != nil {
		return err
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

	ent, created, err := e.Store.EnsureEntity(ctx, entity.New(attrs))
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
		re, err := e.Store.UpdateEntity(ctx, entity.Id(rpcE.Id()), entity.New(attrs), opts...)
		if err != nil {
			if !errors.Is(err, entity.ErrNotFound) {
				return fmt.Errorf("failed to create entity: %w", err)
			}
		} else {
			results.SetRevision(re.GetRevision())
			results.SetId(re.Id().String())
		}
	} else {
		re, err := e.Store.CreateEntity(ctx, entity.New(attrs), opts...)
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

	// Progress watermarks and compaction signals are only meaningful to a client
	// that is resuming from a revision. A from-now client (fromRev==0) predates
	// these op types and can never fall behind the compaction point, so we never
	// forward them to it — preserving backward-compatible behavior for callers
	// that aren't revision-aware (e.g. the legacy activator/controller watches).
	fromRev := args.FromRevision()

	ch, err := e.Store.WatchIndex(ctx, args.Index(), fromRev)
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
				// A compacted start revision is recoverable for a resuming client:
				// tell it to re-list and resume from a fresh snapshot rather than
				// erroring. A from-now client can never fall behind the compaction
				// point, so fall through to the legacy error path for it.
				if watchevent.CompactRevision > 0 && fromRev > 0 {
					var op entityserver_v1alpha.EntityOp
					op.SetOperation(int64(entityserver_v1alpha.EntityOperationCompacted))
					op.SetRevision(watchevent.CompactRevision)
					if _, err := send.Send(ctx, &op); err != nil {
						if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
							e.Log.Error("failed to send compacted signal", "error", err)
						}
					}
					return nil
				}
				if err := watchevent.Err(); err != nil {
					return fmt.Errorf("watch canceled with error: %w", err)
				}
				return fmt.Errorf("watch canceled")
			}

			// Progress notifications carry no events; forward the observed revision
			// as a watermark so a resuming client can advance its cursor while idle.
			// IsProgressNotify deliberately excludes etcd's initial Created
			// confirmation: that response reports the current revision but arrives
			// before the historical backlog replays, so forwarding it would advance
			// the cursor past events the client hasn't seen yet.
			if watchevent.IsProgressNotify() {
				if fromRev > 0 {
					var op entityserver_v1alpha.EntityOp
					op.SetOperation(int64(entityserver_v1alpha.EntityOperationProgress))
					op.SetRevision(watchevent.Header.Revision)
					if _, err := send.Send(ctx, &op); err != nil {
						if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
							e.Log.Error("failed to send progress watermark", "error", err)
						}
						return nil
					}
				}
				continue
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
				// ModRevision is the revision of this change for both puts and
				// deletes; the client uses it to advance its resume cursor.
				op.SetRevision(event.Kv.ModRevision)

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
					rpcEntity.SetAttrs(en.Attrs())

					op.SetEntity(&rpcEntity)
				} else if event.PrevKv != nil {
					entityId := entity.Id(event.PrevKv.Value)
					op.SetEntityId(string(entityId))
					op.SetPrevious(event.PrevKv.ModRevision)

					// Try to fetch the entity data for the delete event. When only
					// an index entry is dropped (e.g. a session lease expiring) the
					// entity itself may still exist, so try a current read first.
					// When the entity was deleted via DeleteEntity, the index entry
					// and the entity key are removed together in one atomic txn, so
					// the entity is already gone at this event's revision; read it at
					// the prior revision to recover what was deleted.
					en, err := e.Store.GetEntity(ctx, entityId)
					if err != nil {
						en, err = e.Store.GetEntityAtRevision(ctx, entityId, event.Kv.ModRevision-1)
					}
					if err != nil {
						e.Log.Error("failed to get entity for delete event", "error", err, "id", entityId)
					} else {
						var rpcEntity entityserver_v1alpha.Entity
						rpcEntity.SetId(en.Id().String())
						rpcEntity.SetCreatedAt(en.GetCreatedAt().UnixMilli())
						rpcEntity.SetUpdatedAt(en.GetUpdatedAt().UnixMilli())
						rpcEntity.SetRevision(en.GetRevision())
						rpcEntity.SetAttrs(en.Attrs())

						op.SetEntity(&rpcEntity)
					}
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

// listIds resolves an index to the entity ids it holds. Session indexes are
// keyed differently from ordinary attribute indexes, so both list paths go
// through here to keep that distinction in one place.
func (e *EntityServer) listIds(ctx context.Context, index entity.Attr) ([]entity.Id, int64, error) {
	if index.ID == entity.AttrSession {
		data, err := base58.Decode(index.Value.String())
		if err != nil {
			return nil, 0, fmt.Errorf("invalid session id: %w", err)
		}

		ids, err := e.Store.ListSessionEntities(ctx, data)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to list entities: %w", err)
		}

		return ids, 0, nil
	}

	ids, listRev, err := e.Store.ListIndexRevision(ctx, index)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list entities: %w", err)
	}

	return ids, listRev, nil
}

func (e *EntityServer) List(ctx context.Context, req *entityserver_v1alpha.EntityAccessList) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	index := args.Index()

	ids, listRev, err := e.listIds(ctx, index)
	if err != nil {
		return err
	}

	// Use batch retrieval for better performance
	entities, err := e.Store.GetEntities(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to get entities: %w", err)
	}

	var ret []*entityserver_v1alpha.Entity
	for i, entity := range entities {
		if entity == nil {
			e.Log.Error("entity in index but not in store, skipping",
				"id", ids[i],
				"index", index)
			continue
		}

		var rpcEntity entityserver_v1alpha.Entity
		rpcEntity.SetId(entity.Id().String())
		rpcEntity.SetCreatedAt(entity.GetCreatedAt().UnixMilli())
		rpcEntity.SetUpdatedAt(entity.GetUpdatedAt().UnixMilli())
		rpcEntity.SetRevision(entity.GetRevision())
		rpcEntity.SetAttrs(entity.Attrs())

		ret = append(ret, &rpcEntity)
	}

	req.Results().SetValues(ret)
	req.Results().SetRevision(listRev)

	return nil
}

// ListPage reads a bounded page of entities from an index.
func (e *EntityServer) ListPage(ctx context.Context, req *entityserver_v1alpha.EntityAccessListPage) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	page, err := e.entityPage(ctx, args.Index(), args.Cursor(), args.Limit())
	if err != nil {
		return err
	}

	ret := make([]*entityserver_v1alpha.Entity, 0, len(page.entities))
	for _, ent := range page.entities {
		var rpcEntity entityserver_v1alpha.Entity
		rpcEntity.SetId(ent.Id().String())
		rpcEntity.SetCreatedAt(ent.GetCreatedAt().UnixMilli())
		rpcEntity.SetUpdatedAt(ent.GetUpdatedAt().UnixMilli())
		rpcEntity.SetRevision(ent.GetRevision())
		rpcEntity.SetAttrs(ent.Attrs())

		ret = append(ret, &rpcEntity)
	}

	req.Results().SetValues(ret)
	req.Results().SetCursor(encodeCursor(page.cursor))
	req.Results().SetTotal(page.total)
	req.Results().SetRevision(page.revision)

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
		// Atoi parses at the platform's int width, so out-of-range values are
		// rejected rather than silently truncated on 32-bit builds.
		i, err := strconv.Atoi(args.Value())
		if err != nil {
			return fmt.Errorf("invalid integer value: %w", err)
		}

		value = entity.IntValue(i)

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
		rpcEntity.SetAttrs(ent.Attrs())
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

// GetDocument renders a single entity into its presentation form.
func (e *EntityServer) GetDocument(ctx context.Context, req *entityserver_v1alpha.EntityAccessGetDocument) error {
	args := req.Args()

	if !args.HasId() {
		return cond.ValidationFailure("missing-field", "id")
	}

	ent, err := e.resolveEntity(ctx, args.Id())
	if err != nil {
		return err
	}

	doc := model.BuildDocument(ctx, e.sc, ent, model.Options{
		MaxValueLen: int(args.MaxValueLen()),
	})

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to encode document: %w", err)
	}

	req.Results().SetDocument(data)

	return nil
}

// MaxPageLimit bounds one response. Without it a caller asking for everything
// makes the server hold every entity in the index, its rendered document, and
// the marshalled array all at once, which is the opposite of the property
// paging exists to give. A caller that wants the whole index still gets it, one
// page and one cursor at a time.
//
// A var rather than a const so a test can shrink it and exercise multi-page
// walks without materialising hundreds of entities.
var MaxPageLimit int64 = 500

// ListDocuments renders a bounded page of entities.
//
// Paging happens in the store, so the entities past the limit are never read at
// all and a page costs the same whether the index holds ten entries or a
// million.
func (e *EntityServer) ListDocuments(ctx context.Context, req *entityserver_v1alpha.EntityAccessListDocuments) error {
	args := req.Args()

	if !args.HasIndex() {
		return fmt.Errorf("missing required field: index")
	}

	page, err := e.entityPage(ctx, args.Index(), args.Cursor(), args.Limit())
	if err != nil {
		return err
	}

	opts := model.Options{MaxValueLen: int(args.MaxValueLen())}

	// Non-nil so an empty page marshals as [] rather than null, which spares
	// every JSON consumer a null check.
	docs := make([]*model.Document, 0, len(page.entities))
	for _, ent := range page.entities {
		docs = append(docs, model.BuildDocument(ctx, e.sc, ent, opts))
	}

	data, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("failed to encode documents: %w", err)
	}

	req.Results().SetDocuments(data)
	req.Results().SetCursor(encodeCursor(page.cursor))
	req.Results().SetTotal(page.total)

	return nil
}

// resolvedPage is one page of entities that actually resolved, with the ids
// that did not already dropped and accounted for.
type resolvedPage struct {
	entities []*entity.Entity
	cursor   string
	total    int64
	revision int64
}

// entityPage reads one page of an index and the entities it names.
//
// Both listing methods reduce to this plus a transform, and keeping the read in
// one place is what stops them drifting: they differ in what they render, not
// in what they read or in how they account for an id the store could not
// resolve.
//
// Session indexes stay here rather than moving into the store, because reaching
// ListSessionEntities means base58-decoding a session id out of the attribute
// value, which is a wire concern. Those listings carry no revision to pin to.
// The index arrives already read rather than as the request, so callers have to
// check HasIndex before they get here: Index() dereferences the optional field,
// and Go evaluates the argument before the call, so a guard inside this
// function would be a panic that never got the chance to fire.
func (e *EntityServer) entityPage(
	ctx context.Context,
	index entity.Attr,
	rawCursor string,
	limit int64,
) (*resolvedPage, error) {
	cursor, err := decodeCursor(rawCursor)
	if err != nil {
		return nil, err
	}

	// Zero means "everything" to the caller, which the client turns into a walk
	// over successive pages rather than one enormous response.
	if limit <= 0 || limit > MaxPageLimit {
		limit = MaxPageLimit
	}

	if index.ID == entity.AttrSession {
		ids, next, total, err := e.pageSessionIds(ctx, index, cursor, limit)
		if err != nil {
			return nil, err
		}

		entities, err := e.Store.GetEntities(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("failed to get entities: %w", err)
		}

		return e.resolve(index, ids, entities, nil, next, total, 0), nil
	}

	page, err := e.Store.ListIndexEntitiesPage(ctx, index, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}

	return e.resolve(index, page.Ids, page.Entities, page.Undecodable, page.Cursor, page.Total, page.Revision), nil
}

// resolve drops the ids the store could not answer for and keeps the reported
// total honest about what the caller can actually see.
//
// Absent and unreadable both arrive as a nil entity and deserve different
// lines. An id the index still names but the store has dropped is stale-index
// work; a key that will not decode is a corrupt entity nobody should assume is
// gone.
func (e *EntityServer) resolve(
	index entity.Attr,
	ids []entity.Id,
	entities []*entity.Entity,
	undecodable map[entity.Id]bool,
	cursor string,
	total, revision int64,
) *resolvedPage {
	resolved := make([]*entity.Entity, 0, len(entities))

	for i, ent := range entities {
		if ent != nil {
			resolved = append(resolved, ent)
			continue
		}

		if undecodable[ids[i]] {
			e.Log.Error("entity in index cannot be decoded, skipping",
				"id", ids[i], "index", index)
		} else {
			e.Log.Error("entity in index but not in store, skipping",
				"id", ids[i], "index", index)
		}

		if total > 0 {
			total--
		}
	}

	return &resolvedPage{entities: resolved, cursor: cursor, total: total, revision: revision}
}

// encodeCursor hides the store key a cursor is made of. It is a resume point,
// not an identifier, and nobody should be tempted to construct or parse one.
func encodeCursor(key string) string {
	if key == "" {
		return ""
	}

	return base58.Encode([]byte(key))
}

func decodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}

	key, err := base58.Decode(cursor)
	if err != nil {
		return "", cond.ValidationFailure("invalid-field", "cursor")
	}

	return string(key), nil
}

// pageSessionIds reads one page of a session's entities.
//
// Session membership is not an attribute index, so the store cannot page it and
// this lists and slices instead. It is the only sort left on the paging path
// and the only place a page costs O(all matches), which is tolerable because a
// page is bounded by what one session bonded. Everything else pages in the
// store.
//
// Reaching the store here means base58-decoding a session id out of the
// attribute value, which is why this stays on the server rather than moving
// into ListIndexEntitiesPage with the rest of the paging.
func (e *EntityServer) pageSessionIds(
	ctx context.Context,
	index entity.Attr,
	cursor string,
	limit int64,
) (ids []entity.Id, next string, total int64, err error) {
	ids, _, err = e.listIds(ctx, index)
	if err != nil {
		return nil, "", 0, err
	}

	// The cursor has to be an entity id, since session membership has no
	// keyspace to resume from, and that needs a total order the store does not
	// provide.
	slices.Sort(ids)

	total = int64(len(ids))

	if cursor != "" {
		for len(ids) > 0 && string(ids[0]) <= cursor {
			ids = ids[1:]
		}
	}

	if limit > 0 && int64(len(ids)) > limit {
		next = string(ids[limit-1])
		ids = ids[:limit]
	}

	return ids, next, total, nil
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

	_, err = e.Store.CreateEntity(ctx, entity.New(
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

func (e *EntityServer) Reindex(ctx context.Context, req *entityserver_v1alpha.EntityAccessReindex) error {
	args := req.Args()
	dryRun := args.HasDryRun() && args.DryRun()

	e.Log.Info("starting entity reindex", "dry_run", dryRun)

	store, ok := e.Store.(*entity.EtcdStore)
	if !ok {
		return fmt.Errorf("reindex requires EtcdStore")
	}

	stats, err := store.Reindex(ctx, e.Log, entity.ReindexOptions{
		DryRun:       dryRun,
		CleanupStale: true,
	})
	if err != nil {
		return err
	}

	// Build response stats list
	results := req.Results()

	statsData := []struct {
		name  string
		value int64
	}{
		{"entities_processed", stats.EntitiesProcessed},
		{"indexes_rebuilt", stats.IndexesRebuilt},
		{"collection_entries_scanned", stats.CollectionEntriesScanned},
		{"stale_entries_found", stats.StaleEntriesFound},
		{"stale_entries_removed", stats.StaleEntriesRemoved},
	}

	var rpcStats []*entityserver_v1alpha.ReindexStat
	for _, data := range statsData {
		stat := &entityserver_v1alpha.ReindexStat{}
		stat.SetName(data.name)
		stat.SetValue(data.value)
		rpcStats = append(rpcStats, stat)
	}
	results.SetStats(rpcStats)

	return nil
}

func (e *EntityServer) GetAttributesByTag(ctx context.Context, req *entityserver_v1alpha.EntityAccessGetAttributesByTag) error {
	args := req.Args()
	tag := args.Tag()

	// Call the entity package function to get attributes by tag
	schemas, err := entity.GetAttributeSchemasByTag(ctx, e.Store, tag)
	if err != nil {
		return fmt.Errorf("failed to get attributes by tag: %w", err)
	}

	// Convert entity.AttributeSchema to RPC AttributeSchema
	var rpcSchemas []*entityserver_v1alpha.AttributeSchema
	for _, schema := range schemas {
		rpcSchema := &entityserver_v1alpha.AttributeSchema{}
		rpcSchema.SetId(string(schema.ID))
		rpcSchema.SetDoc(schema.Doc)
		rpcSchema.SetAttrType(string(schema.Type))
		rpcSchema.SetAllowMany(schema.AllowMany)
		rpcSchema.SetIndexed(schema.Index)
		rpcSchema.SetSession(schema.Session)
		if len(schema.Tags) > 0 {
			rpcSchema.SetTags(schema.Tags)
		}
		rpcSchemas = append(rpcSchemas, rpcSchema)
	}

	results := req.Results()
	results.SetSchemas(rpcSchemas)

	return nil
}
