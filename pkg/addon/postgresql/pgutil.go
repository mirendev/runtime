package postgresql

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	defaultPostgresDB   = "postgres"
	defaultPostgresUser = "postgres"
)

// quoteIdentifier safely quotes a PostgreSQL identifier using pgx.
func quoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// quoteLiteral safely quotes a PostgreSQL string literal.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// connectPostgres establishes a connection to a PostgreSQL server.
func connectPostgres(ctx context.Context, host string, port int, user, password, database string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parsing default pgx config: %w", err)
	}

	cfg.Host = host
	cfg.Port = uint16(port)
	cfg.User = user
	cfg.Password = password
	cfg.Database = database

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres at %s:%d: %w", host, port, err)
	}

	return conn, nil
}

// connectAsSuperuser connects to the default postgres database as the superuser.
func connectAsSuperuser(ctx context.Context, host string, password string) (*pgx.Conn, error) {
	return connectPostgres(ctx, host, postgresPort, defaultPostgresUser, password, defaultPostgresDB)
}

// connectAsSuperuserTrying connects as the superuser, trying each candidate
// password until one authenticates. During a superuser rotation the credential
// used to connect is the very thing being changed, so a retry after a crash may
// find the engine on either the old or the new password; supplying both lets the
// rotation converge regardless of where the previous attempt stopped.
func connectAsSuperuserTrying(ctx context.Context, host string, passwords ...string) (*pgx.Conn, error) {
	var lastErr error
	tried := false
	for _, pw := range passwords {
		if pw == "" {
			continue
		}
		tried = true
		conn, err := connectAsSuperuser(ctx, host, pw)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if !tried {
		return nil, fmt.Errorf("no superuser password candidates to try")
	}
	return nil, lastErr
}

func createPostgresUser(ctx context.Context, conn *pgx.Conn, username, password string) error {
	sql := fmt.Sprintf("CREATE USER %s WITH PASSWORD %s",
		quoteIdentifier(username), quoteLiteral(password))

	_, err := conn.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("creating user %s: %w", username, err)
	}

	return nil
}

// alterPostgresUserPassword changes a role's password. ALTER USER is an alias
// for ALTER ROLE, so this works for both a per-app user and the superuser role.
// Existing connections stay authenticated; only new connections need the new
// password.
func alterPostgresUserPassword(ctx context.Context, conn *pgx.Conn, username, password string) error {
	sql := fmt.Sprintf("ALTER USER %s WITH PASSWORD %s",
		quoteIdentifier(username), quoteLiteral(password))

	_, err := conn.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("altering password for user %s: %w", username, err)
	}

	return nil
}

func createPostgresDatabase(ctx context.Context, conn *pgx.Conn, dbname, owner string) error {
	sql := fmt.Sprintf("CREATE DATABASE %s OWNER %s",
		quoteIdentifier(dbname), quoteIdentifier(owner))

	_, err := conn.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("creating database %s: %w", dbname, err)
	}

	return nil
}

func dropPostgresDatabase(ctx context.Context, conn *pgx.Conn, dbname string) error {
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbname))

	_, err := conn.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("dropping database %s: %w", dbname, err)
	}

	return nil
}

func dropPostgresUser(ctx context.Context, conn *pgx.Conn, username string) error {
	sql := fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdentifier(username))

	_, err := conn.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("dropping user %s: %w", username, err)
	}

	return nil
}

func terminatePostgresConnections(ctx context.Context, conn *pgx.Conn, dbname string) error {
	_, err := conn.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		dbname)
	if err != nil {
		return fmt.Errorf("terminating connections to %s: %w", dbname, err)
	}

	return nil
}
