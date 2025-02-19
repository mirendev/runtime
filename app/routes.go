package app

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/pkg/errors"
)

func (a *AppAccess) LoadApplicationForHost(ctx context.Context, host string) (*AppConfig, string, error) {
	var (
		app AppConfig
	)

	err := a.DB.QueryRow(ctx,
		`SELECT app.id, app.xid, app.name, app.created_at, app.updated_at, r.host
		   FROM http_routes r, applications app
		  WHERE (host = $1 OR host = '*')
		    AND r.application_id = app.id
		  ORDER BY CASE WHEN host = $1 THEN 0 ELSE 1 END
		  LIMIT 1`, host).Scan(
		&app.Id, &app.Xid, &app.Name, &app.CreatedAt, &app.UpdatedAt, &host,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}

	return &app, host, nil
}

func (a *AppAccess) SetApplicationHost(ctx context.Context, ac *AppConfig, host string) error {
	cur, _, err := a.LoadApplicationForHost(ctx, host)
	if err != nil {
		return err
	}

	if cur != nil {
		if cur.Id == ac.Id {
			return nil
		}

		return errors.New("host already in use")
	}

	_, err = a.DB.Exec(ctx,
		`INSERT INTO http_routes (application_id, host)
		 VALUES ($1, $2)`,
		ac.Id, host,
	)
	return err
}
