package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/idgen"
)

var ctx = context.Background()

type DB struct {
	DB    *pgxpool.Pool
	OrgId uint64 `asm:"org_id"`

	tx pgx.Tx
}

var _ = autoreg.Register[DB]()

func (a *DB) UseTx(tx pgx.Tx) {
	a.tx = tx
}

func (a *DB) inTx(ctx context.Context, f func(tx pgx.Tx) error) error {
	if a.tx != nil {
		return f(a.tx)
	}

	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}

	err = f(tx)
	if err != nil {
		tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}

type AppAttachment struct {
	AppId uint64
	Name  string
}

type Instance struct {
	Id          uint64
	Xid         string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Addon       string
	Plan        string
	ContainerId string
	DiskIds     []uint64

	Apps []AppAttachment

	Config *InstanceConfig
}

// CreateInstance creates a new addon instance with the given parameters
func (db *DB) CreateInstance(instance *Instance) error {
	if instance.Xid == "" {
		instance.Xid = idgen.Gen("addon-")
	}

	data, err := json.Marshal(instance.Config)
	if err != nil {
		return fmt.Errorf("marshal instance config: %w", err)
	}

	return db.inTx(ctx, func(tx pgx.Tx) error {
		// Insert the addon instance
		err := tx.QueryRow(ctx, `
		INSERT INTO addon_instances 
		(organization_id, external_id, container_id, addon_name, addon_plan, config) 
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, db.OrgId, instance.Xid, instance.ContainerId,
			instance.Addon, instance.Plan, data,
		).Scan(&instance.Id, &instance.CreatedAt, &instance.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert addon instance: %w", err)
		}

		// Insert disk associations if any
		if len(instance.DiskIds) > 0 {
			for _, diskId := range instance.DiskIds {
				_, err = tx.Exec(ctx, `
				INSERT INTO addon_instance_disks 
				(addon_instance_id, disk_id) 
				VALUES ($1, $2)
			`, instance.Id, diskId)
				if err != nil {
					return fmt.Errorf("insert disk association: %w", err)
				}
			}
		}

		// Insert application attachment if AppId is set
		for _, app := range instance.Apps {
			_, err = tx.Exec(ctx, `
			INSERT INTO addon_instance_attachment 
			(addon_instance_id, application_id, name) 
			VALUES ($1, $2, $3)
		`, instance.Id, app.AppId, app.Name)
			if err != nil {
				return fmt.Errorf("insert application attachment: %w", err)
			}
		}

		return nil
	})
}

// GetInstanceByXid retrieves an addon instance by external ID
func (db *DB) GetInstanceByXid(xid string) (*Instance, error) {
	instance := &Instance{}

	err := db.inTx(ctx, func(tx pgx.Tx) error {
		var data []byte

		// Get the main instance data
		err := tx.QueryRow(ctx, `
		SELECT i.id, i.external_id, i.container_id,
					 i.addon_name, i.addon_plan, i.config,
		       i.created_at, i.updated_at
		FROM addon_instances i
		WHERE i.external_id = $1 AND i.organization_id = $2
	`, xid, db.OrgId).Scan(
			&instance.Id, &instance.Xid, &instance.ContainerId,
			&instance.Addon, &instance.Plan, &data,
			&instance.CreatedAt, &instance.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("query addon instance: %w", err)
		}

		// Unmarshal instance Config
		if err := json.Unmarshal(data, &instance.Config); err != nil {
			return fmt.Errorf("unmarshal instance config: %w", err)
		}

		// Get associated disk IDs
		rows, err := tx.Query(ctx, `
		SELECT disk_id 
		FROM addon_instance_disks 
		WHERE addon_instance_id = $1
	`, instance.Id)
		if err != nil {
			return fmt.Errorf("query disk associations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var diskId uint64
			if err := rows.Scan(&diskId); err != nil {
				return fmt.Errorf("scan disk id: %w", err)
			}
			instance.DiskIds = append(instance.DiskIds, diskId)
		}

		rows.Close()

		// Get associated application ID if any
		rows, err = tx.Query(ctx, `
		SELECT application_id, name 
		FROM addon_instance_attachment 
		WHERE addon_instance_id = $1
	`, instance.Id)

		defer rows.Close()

		for rows.Next() {
			var app AppAttachment

			if err := rows.Scan(&app.AppId, &app.Name); err != nil {
				return fmt.Errorf("scan application attachment: %w", err)
			}

			instance.Apps = append(instance.Apps, app)

		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// GetInstance retrieves an addon instance by ID
func (db *DB) GetInstance(id uint64) (*Instance, error) {
	instance := &Instance{}

	err := db.inTx(ctx, func(tx pgx.Tx) error {
		var data []byte

		// Get the main instance data
		err := tx.QueryRow(ctx, `
		SELECT i.id, i.external_id, i.container_id,
					 i.addon_name, i.addon_plan, i.config,
		       i.created_at, i.updated_at
		FROM addon_instances i
		WHERE i.id = $1
	`, id).Scan(
			&instance.Id, &instance.Xid, &instance.ContainerId,
			&instance.Addon, &instance.Plan, &data,
			&instance.CreatedAt, &instance.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("query addon instance: %w", err)
		}

		// Unmarshal instance Config
		if err := json.Unmarshal(data, &instance.Config); err != nil {
			return fmt.Errorf("unmarshal instance config: %w", err)
		}

		// Get associated disk IDs
		rows, err := tx.Query(ctx, `
		SELECT disk_id 
		FROM addon_instance_disks 
		WHERE addon_instance_id = $1
	`, id)
		if err != nil {
			return fmt.Errorf("query disk associations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var diskId uint64
			if err := rows.Scan(&diskId); err != nil {
				return fmt.Errorf("scan disk id: %w", err)
			}
			instance.DiskIds = append(instance.DiskIds, diskId)
		}

		rows.Close()

		// Get associated application ID if any
		rows, err = tx.Query(ctx, `
		SELECT application_id, name
		FROM addon_instance_attachment 
		WHERE addon_instance_id = $1
	`, id)
		if err != nil {
			return fmt.Errorf("query application attachment: %w", err)
		}

		defer rows.Close()

		for rows.Next() {
			var app AppAttachment

			if err := rows.Scan(&app.AppId, &app.Name); err != nil {
				return fmt.Errorf("scan application attachment: %w", err)
			}

			instance.Apps = append(instance.Apps, app)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// GetInstanceByAppAndName retrieves an addon instance by application ID and attachment name
func (db *DB) GetInstanceByAppAndName(appId uint64, name string) (*Instance, error) {
	instance := &Instance{}

	err := db.inTx(ctx, func(tx pgx.Tx) error {
		var data []byte

		var app AppAttachment
		app.AppId = appId

		// Get the main instance data joined with the attachment
		err := tx.QueryRow(ctx, `
		SELECT i.id, i.external_id, i.container_id,
					 i.addon_name, i.addon_plan, i.config,
		       i.created_at, i.updated_at, a.name
		FROM addon_instances i
		JOIN addon_instance_attachment a ON i.id = a.addon_instance_id
		WHERE a.application_id = $1 AND a.name = $2 AND i.organization_id = $3
	`, appId, name, db.OrgId).Scan(
			&instance.Id, &instance.Xid, &instance.ContainerId,
			&instance.Addon, &instance.Plan, &data,
			&instance.CreatedAt, &instance.UpdatedAt,
			&app.Name,
		)
		if err != nil {
			return fmt.Errorf("query addon instance: %w", err)
		}

		// Unmarshal instance Config
		if err := json.Unmarshal(data, &instance.Config); err != nil {
			return fmt.Errorf("unmarshal instance config: %w", err)
		}

		instance.Apps = append(instance.Apps, app)

		// Get associated disk IDs
		rows, err := tx.Query(ctx, `
		SELECT disk_id 
		FROM addon_instance_disks 
		WHERE addon_instance_id = $1
	`, instance.Id)
		if err != nil {
			return fmt.Errorf("query disk associations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var diskId uint64
			if err := rows.Scan(&diskId); err != nil {
				return fmt.Errorf("scan disk id: %w", err)
			}
			instance.DiskIds = append(instance.DiskIds, diskId)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// DeleteInstance removes an addon instance and all its associations
func (db *DB) DeleteInstance(id uint64) error {
	return db.inTx(ctx, func(tx pgx.Tx) error {
		// Delete application attachments
		_, err := tx.Exec(ctx, `
		DELETE FROM addon_instance_attachment 
		WHERE addon_instance_id = $1
	`, id)
		if err != nil {
			return fmt.Errorf("delete application attachments: %w", err)
		}

		// Delete disk associations
		_, err = tx.Exec(ctx, `
		DELETE FROM addon_instance_disks 
		WHERE addon_instance_id = $1
	`, id)
		if err != nil {
			return fmt.Errorf("delete disk associations: %w", err)
		}

		// Delete the instance itself
		result, err := tx.Exec(ctx, `
		DELETE FROM addon_instances 
		WHERE id = $1
	`, id)
		if err != nil {
			return fmt.Errorf("delete addon instance: %w", err)
		}

		if result.RowsAffected() == 0 {
			return fmt.Errorf("addon instance not found")
		}

		return nil
	})
}

// ListInstancesForApp retrieves all addon instances for the given application
func (db *DB) ListInstancesForApp(appId uint64) ([]*Instance, error) {
	var instances []*Instance

	err := db.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
		SELECT i.id, i.external_id, a.name, i.container_id,
			     i.addon_name, i.addon_plan, i.config,
		       i.created_at, i.updated_at
		FROM addon_instances i
		JOIN addon_instance_attachment a ON i.id = a.addon_instance_id
		WHERE a.application_id = $1
	`, appId)
		if err != nil {
			return fmt.Errorf("query addon instances: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var data string
			instance := &Instance{}

			var app AppAttachment
			if err := rows.Scan(
				&instance.Id, &instance.Xid, &app.Name, &instance.ContainerId,
				&instance.Addon, &instance.Plan, &data,
				&instance.CreatedAt, &instance.UpdatedAt,
			); err != nil {
				return fmt.Errorf("scan addon instance: %w", err)
			}

			// Unmarshal instance config
			if err := json.Unmarshal([]byte(data), &instance.Config); err != nil {
				return fmt.Errorf("unmarshal instance config: %w", err)
			}

			instance.Apps = append(instance.Apps, app)

			instances = append(instances, instance)
		}

		for _, instance := range instances {
			// Get associated disk IDs
			diskRows, err := tx.Query(ctx, `
			SELECT disk_id 
			FROM addon_instance_disks 
			WHERE addon_instance_id = $1
		`, instance.Id)
			if err != nil {
				return fmt.Errorf("query disk associations: %w", err)
			}
			defer diskRows.Close()

			for diskRows.Next() {
				var diskId uint64
				if err := diskRows.Scan(&diskId); err != nil {
					return fmt.Errorf("scan disk id: %w", err)
				}
				instance.DiskIds = append(instance.DiskIds, diskId)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return instances, nil
}
