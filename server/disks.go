package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"miren.dev/runtime/api"
	"miren.dev/runtime/pkg/asm/autoreg"
)

type RPCDisks struct {
	DB    *pgxpool.Pool
	OrgId uint64 `asm:"org_id"`
}

var _ api.Disks = &RPCDisks{}

var _ = autoreg.Register[RPCDisks]()

func (r *RPCDisks) New(ctx context.Context, req *api.DisksNew) error {
	args := req.Args()

	var id string

	err := r.DB.QueryRow(ctx,
		`SELECT external_id
		 FROM disks
	   WHERE org_id = $1
	   AND name = $2`, r.OrgId, args.Name()).Scan(&id)
	if err == nil {
		return fmt.Errorf("disk with name %q already exists", args.Name())
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	err = r.DB.QueryRow(ctx,
		`INSERT INTO disks (org_id, name, size) RETURNING external_id`,
		r.OrgId, args.Name(), args.Capacity(),
	).Scan(&id)

	if err != nil {
		return err
	}

	req.Results().SetId(id)

	return nil
}

func (r *RPCDisks) List(ctx context.Context, req *api.DisksList) error {
	rows, err := r.DB.Query(ctx,
		`SELECT external_id, name, size
		 FROM disks
	   WHERE org_id = $1`, r.OrgId)
	if err != nil {
		return err
	}

	defer rows.Close()

	var disks []*api.DiskConfig

	for rows.Next() {
		var (
			id   string
			name string
			size int64
		)

		err = rows.Scan(&id, &name, &size)
		if err != nil {
			return err
		}

		var dc api.DiskConfig
		dc.SetId(id)
		dc.SetName(name)
		dc.SetCapacity(size)

		disks = append(disks, &dc)
	}

	req.Results().SetDisks(disks)
	return nil
}

func (r *RPCDisks) Delete(ctx context.Context, req *api.DisksDelete) error {
	args := req.Args()

	_, err := r.DB.Exec(ctx,
		`DELETE FROM disks
	   WHERE org_id = $1
	   AND external_id = $2`, r.OrgId, args.Id())
	if err != nil {
		return err
	}

	return nil
}

func (r *RPCDisks) GetById(ctx context.Context, req *api.DisksGetById) error {
	args := req.Args()

	var (
		id   string
		name string
		size int64
	)

	err := r.DB.QueryRow(ctx,
		`SELECT external_id, name, size
		 FROM disks
	   WHERE org_id = $1
	   AND external_id = $2`, r.OrgId, args.Id(),
	).Scan(&id, &name, &size)
	if err != nil {
		return err
	}

	var dc api.DiskConfig
	dc.SetId(id)
	dc.SetName(name)
	dc.SetCapacity(size)

	req.Results().SetConfig(&dc)

	return nil
}

func (r *RPCDisks) GetByName(ctx context.Context, req *api.DisksGetByName) error {
	args := req.Args()

	var (
		id   string
		name string
		size int64
	)

	err := r.DB.QueryRow(ctx,
		`SELECT external_id, name, size
		 FROM disks
	   WHERE org_id = $1
	   AND name = $2`, r.OrgId, args.Name(),
	).Scan(&id, &name, &size)
	if err != nil {
		return err
	}

	var dc api.DiskConfig
	dc.SetId(id)
	dc.SetName(name)
	dc.SetCapacity(size)

	req.Results().SetConfig(&dc)

	return nil
}
