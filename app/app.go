package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AppAccess struct {
	DB *pgxpool.Pool

	tx pgx.Tx
}

func (a *AppAccess) UseTx(tx pgx.Tx) {
	a.tx = tx
}

type AppConfig struct {
	Id        uint64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
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
			"SELECT id, name, created_at, updated_at FROM applications WHERE name = $1", name,
		).Scan(&app.Id, &app.Name, &app.CreatedAt, &app.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}

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
			"INSERT INTO applications (name, created_at, updated_at) VALUES ($1, $2, $3)",
			app.Name, app.CreatedAt, app.UpdatedAt,
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
			"SELECT id, name, created_at, updated_at FROM applications",
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
