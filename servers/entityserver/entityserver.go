package entityserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davecgh/go-spew/spew"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/pkg/entity"
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

	rpctypes := args.Entity()

	attrs := rpctypes.Attrs()
	if len(attrs) == 0 {
		return fmt.Errorf("missing required field: attrs")
	}

	// TODO: handle created_at and updated_at fileds
	re, err := e.Store.CreateEntity(ctx, attrs)
	if err != nil {
		return fmt.Errorf("failed to create entity: %w", err)
	}

	e.Log.Debug("created entity", "id", re.ID)

	req.Results().SetRevision(re.Revision)

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
						spew.Dump(event)
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
