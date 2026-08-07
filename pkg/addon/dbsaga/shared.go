package dbsaga

import (
	"context"
	"fmt"
	"strings"

	"miren.dev/runtime/pkg/addon"
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
	ServerID entity.Id
}

type IncrementAssociationCountOut struct {
	Incremented bool
}

func IncrementAssociationCount(ctx context.Context, in IncrementAssociationCountIn) (IncrementAssociationCountOut, error) {
	sc := saga.Get[ServerCounter](ctx)

	count, rev, err := sc.GetAssociationCount(ctx, in.ServerID)
	if err != nil {
		return IncrementAssociationCountOut{}, fmt.Errorf("getting server for count increment: %w", err)
	}

	if err := sc.PatchAssociationCount(ctx, in.ServerID, rev, count+1); err != nil {
		return IncrementAssociationCountOut{}, fmt.Errorf("updating association count: %w", err)
	}

	return IncrementAssociationCountOut{Incremented: true}, nil
}

func UndoIncrementAssociationCount(ctx context.Context, in IncrementAssociationCountIn, out IncrementAssociationCountOut) error {
	if !out.Incremented {
		return nil
	}

	sc := saga.Get[ServerCounter](ctx)

	count, rev, err := sc.GetAssociationCount(ctx, in.ServerID)
	if err != nil {
		return err
	}

	newCount := max(count-1, 0)
	return sc.PatchAssociationCount(ctx, in.ServerID, rev, newCount)
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

	count, rev, err := sc.GetAssociationCount(ctx, in.SharedServerRef)
	if err != nil {
		return DecrementAssociationCountOut{}, fmt.Errorf("getting server: %w", err)
	}

	if count <= 0 {
		return DecrementAssociationCountOut{RemainingCount: 0}, nil
	}

	newCount := count - 1
	if err := sc.PatchAssociationCount(ctx, in.SharedServerRef, rev, newCount); err != nil {
		return DecrementAssociationCountOut{}, fmt.Errorf("updating association count: %w", err)
	}

	return DecrementAssociationCountOut{RemainingCount: newCount}, nil
}

func UndoDecrementAssociationCount(ctx context.Context, in DecrementAssociationCountIn, out DecrementAssociationCountOut) error {
	return nil
}
