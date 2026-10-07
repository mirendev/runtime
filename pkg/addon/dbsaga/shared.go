package dbsaga

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/saga"
)

// ServerCounter abstracts reading and patching the association count on a
// database server entity. Each provider implements this for its own entity
// type. Inject via saga.UsingAs[ServerCounter] and retrieve with
// saga.Get[ServerCounter](ctx).
type ServerCounter interface {
	GetAssociationCount(ctx context.Context, serverID entity.Id) (count int64, revision int64, err error)
	PatchAssociationCount(ctx context.Context, serverID entity.Id, revision int64, newCount int64) error
}

func changeAssociationCount(ctx context.Context, sc ServerCounter, serverID entity.Id, delta int64) (int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		count, rev, err := sc.GetAssociationCount(ctx, serverID)
		if err != nil {
			return 0, fmt.Errorf("getting server: %w", err)
		}
		next := max(count+delta, 0)
		if next == count {
			return next, nil
		}
		err = sc.PatchAssociationCount(ctx, serverID, rev, next)
		if errors.Is(err, cond.ErrConflict{}) {
			// Clone creation can overlap replacement cleanup on the same server.
			// Recompute from the current count rather than retrying a stale value.
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("updating association count: %w", err)
		}
		return next, nil
	}
}

// --- Shared Shared-Server Saga Actions ---

// WaitForSharedPool waits for the shared sandbox pool to be ready.

type WaitForSharedPoolIn struct {
	PoolID entity.Id
}

type WaitForSharedPoolOut struct {
	PoolReady bool
}

func WaitForSharedPool(ctx context.Context, in WaitForSharedPoolIn) (WaitForSharedPoolOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	cfg := saga.Get[*AddonConfig](ctx)

	if err := fw.WaitForPool(ctx, in.PoolID, cfg.ReadyTimeout); err != nil {
		return WaitForSharedPoolOut{}, fmt.Errorf("waiting for shared pool: %w", err)
	}

	return WaitForSharedPoolOut{PoolReady: true}, nil
}

func UndoWaitForSharedPool(ctx context.Context, in WaitForSharedPoolIn, out WaitForSharedPoolOut) error {
	return nil
}

// CreateSharedService creates a network Service for the shared pool.

type CreateSharedServiceIn struct{}

type CreateSharedServiceOut struct {
	ServiceID entity.Id
}

func CreateSharedService(ctx context.Context, in CreateSharedServiceIn) (CreateSharedServiceOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	cfg := saga.Get[*AddonConfig](ctx)

	labels := types.LabelSet(
		"addon", cfg.AddonName,
		"server", cfg.SharedServerName,
		"shared", "true",
	)

	suffix := strings.TrimPrefix(cfg.AddonName, "miren-")
	serviceName := cfg.SharedServerName + "-" + suffix
	svcID, err := fw.CreateService(ctx, serviceName, labels, cfg.Port)
	if err != nil {
		return CreateSharedServiceOut{}, fmt.Errorf("creating shared service: %w", err)
	}

	return CreateSharedServiceOut{ServiceID: svcID}, nil
}

func UndoCreateSharedService(ctx context.Context, in CreateSharedServiceIn, out CreateSharedServiceOut) error {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.DeleteService(ctx, out.ServiceID)
}

// WaitForSharedService waits for the service to receive an IP address.

type WaitForSharedServiceIn struct {
	ServiceID entity.Id
}

type WaitForSharedServiceOut struct {
	ServiceHost string
}

func WaitForSharedService(ctx context.Context, in WaitForSharedServiceIn) (WaitForSharedServiceOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	cfg := saga.Get[*AddonConfig](ctx)

	serviceHost, err := fw.WaitForServiceAddress(ctx, in.ServiceID, cfg.ReadyTimeout)
	if err != nil {
		return WaitForSharedServiceOut{}, fmt.Errorf("waiting for shared service address: %w", err)
	}

	return WaitForSharedServiceOut{ServiceHost: serviceHost}, nil
}

func UndoWaitForSharedService(ctx context.Context, in WaitForSharedServiceIn, out WaitForSharedServiceOut) error {
	return nil
}

// IncrementAssociationCount bumps the association count on a shared server.

type IncrementAssociationCountIn struct {
	ServerID        entity.Id
	DatabaseCreated bool `saga:"database_created,optional"`
}

type IncrementAssociationCountOut struct {
	Incremented bool
}

func IncrementAssociationCount(ctx context.Context, in IncrementAssociationCountIn) (IncrementAssociationCountOut, error) {
	sc := saga.Get[ServerCounter](ctx)

	if _, err := changeAssociationCount(ctx, sc, in.ServerID, 1); err != nil {
		return IncrementAssociationCountOut{}, err
	}

	return IncrementAssociationCountOut{Incremented: true}, nil
}

func UndoIncrementAssociationCount(ctx context.Context, in IncrementAssociationCountIn, out IncrementAssociationCountOut) error {
	if !out.Incremented {
		return nil
	}

	sc := saga.Get[ServerCounter](ctx)

	_, err := changeAssociationCount(ctx, sc, in.ServerID, -1)
	return err
}

// DecrementAssociationCount decreases the association count on a shared server.

type DecrementAssociationCountIn struct {
	SharedServerRef entity.Id
	DatabaseDropped bool
	UserDropped     bool
}

type DecrementAssociationCountOut struct {
	RemainingCount int64
}

func DecrementAssociationCount(ctx context.Context, in DecrementAssociationCountIn) (DecrementAssociationCountOut, error) {
	sc := saga.Get[ServerCounter](ctx)

	newCount, err := changeAssociationCount(ctx, sc, in.SharedServerRef, -1)
	if err != nil {
		return DecrementAssociationCountOut{}, err
	}

	return DecrementAssociationCountOut{RemainingCount: newCount}, nil
}

func UndoDecrementAssociationCount(ctx context.Context, in DecrementAssociationCountIn, out DecrementAssociationCountOut) error {
	return nil
}
