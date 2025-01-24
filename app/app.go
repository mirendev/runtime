package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"
)

type AppAccess struct {
	DB *pgxpool.Pool

	OrgId uint64 `asm:"org_id"`

	tx pgx.Tx
}

func (a *AppAccess) UseTx(tx pgx.Tx) {
	a.tx = tx
}

type AppConfig struct {
	Id        uint64
	CreatedAt time.Time
	UpdatedAt time.Time

	OrgId uint64
	Name  string
}

func (a *AppAccess) inTx(ctx context.Context, f func(tx pgx.Tx) error) error {
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

func (a *AppAccess) LoadApp(ctx context.Context, name string) (*AppConfig, error) {
	var app AppConfig

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, name, created_at, updated_at 
       FROM applications 
       WHERE organization_id = $1
			   AND name = $2`, a.OrgId, name,
		).Scan(&app.Id, &app.Name, &app.CreatedAt, &app.UpdatedAt)
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load app %s", name)
	}

	app.OrgId = a.OrgId

	return &app, nil
}

func (a *AppAccess) CreateApp(ctx context.Context, app *AppConfig) error {
	now := time.Now()

	if app.CreatedAt.IsZero() {
		app.CreatedAt = now
	}

	if app.UpdatedAt.IsZero() {
		app.UpdatedAt = now
	}

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO applications (name, created_at, updated_at, organization_id) VALUES ($1, $2, $3, $4)",
			app.Name, app.CreatedAt, app.UpdatedAt, a.OrgId,
		)
		return err
	})

	return err
}

func (a *AppAccess) UpdateApp(ctx context.Context, app *AppConfig) error {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE applications SET name = $1, updated_at = $2 WHERE id = $3",
			app.Name, app.UpdatedAt, app.Id,
		)
		return err
	})
	return err
}

func (a *AppAccess) DeleteApp(ctx context.Context, id uint64) error {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"DELETE FROM applications WHERE id = $1", id,
		)
		return err
	})
	return err
}

func (a *AppAccess) ListApps(ctx context.Context) ([]*AppConfig, error) {
	var apps []*AppConfig

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, name, created_at, updated_at 
       FROM applications
			 WHERE organization_id = $1`,
			a.OrgId,
		)
		if err != nil {
			return err
		}

		for rows.Next() {
			var app AppConfig
			err = rows.Scan(&app.Id, &app.Name, &app.CreatedAt, &app.UpdatedAt)
			if err != nil {
				return err
			}
			app.OrgId = a.OrgId
			apps = append(apps, &app)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return apps, nil
}

type AppVersion struct {
	Id        uint64
	AppId     uint64
	Version   string
	CreatedAt time.Time
	UpdatedAt time.Time

	StaticDir sql.NullString

	App *AppConfig
}

func (av *AppVersion) ImageName() string {
	return av.App.Name + ":" + av.Version
}

func (a *AppAccess) CreateVersion(ctx context.Context, av *AppVersion) error {
	now := time.Now()

	if av.CreatedAt.IsZero() {
		av.CreatedAt = now
	}

	if av.UpdatedAt.IsZero() {
		av.UpdatedAt = now
	}

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO application_versions (application_id, version, created_at, updated_at, static_dir) VALUES ($1, $2, $3, $4, $5)",
			av.AppId, av.Version, av.CreatedAt, av.UpdatedAt, av.StaticDir,
		)
		return err
	})

	return err
}

func (a *AppAccess) DeleteVersion(ctx context.Context, av *AppVersion) error {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"DELETE FROM application_versions WHERE id = $1", av.Id,
		)
		return err
	})
	return err
}

func (a *AppAccess) LoadVersion(ctx context.Context, ac *AppConfig, version string) (*AppVersion, error) {
	var appVersion AppVersion

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT id, application_id, version, created_at, updated_at, static_dir FROM application_versions WHERE application_id = $1 AND version = $2", ac.Id, version,
		).Scan(&appVersion.Id, &appVersion.AppId, &appVersion.Version, &appVersion.CreatedAt, &appVersion.UpdatedAt, &appVersion.StaticDir)
	})
	if err != nil {
		return nil, err
	}

	appVersion.App = ac

	return &appVersion, nil
}

func (a *AppAccess) MostRecentVersion(ctx context.Context, ac *AppConfig) (*AppVersion, error) {
	var appVersion AppVersion

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT id, application_id, version, created_at, updated_at, static_dir FROM application_versions WHERE application_id = $1 ORDER BY created_at DESC LIMIT 1", ac.Id,
		).Scan(&appVersion.Id, &appVersion.AppId, &appVersion.Version, &appVersion.CreatedAt, &appVersion.UpdatedAt, &appVersion.StaticDir)
	})
	if err != nil {
		return nil, err
	}

	appVersion.App = ac

	return &appVersion, nil
}

func (a *AppAccess) ListVersions(ctx context.Context, ac *AppConfig) ([]*AppVersion, error) {
	var ret []*AppVersion

	err := a.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			"SELECT id, application_id, version, created_at, updated_at, static_dir FROM application_versions WHERE application_id = $1 ORDER BY created_at DESC", ac.Id,
		)
		if err != nil {
			return err
		}

		for rows.Next() {
			var appVersion AppVersion
			err = rows.Scan(&appVersion.Id, &appVersion.AppId, &appVersion.Version, &appVersion.CreatedAt, &appVersion.UpdatedAt, &appVersion.StaticDir)
			if err != nil {
				return err
			}

			appVersion.App = ac
			ret = append(ret, &appVersion)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return ret, nil
}
