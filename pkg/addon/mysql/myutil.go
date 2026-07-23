package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"miren.dev/runtime/pkg/addon"
)

const (
	defaultMysqlDB   = "mysql"
	defaultMysqlUser = "root"
)

// maxMysqlIdentLen is the maximum length of a MySQL username or database name.
const maxMysqlIdentLen = 32

func sanitizeIdentifier(name string) string {
	return addon.SanitizeIdentifier(name, maxMysqlIdentLen)
}

// quoteIdentifier wraps a MySQL identifier in backticks, escaping any
// embedded backticks by doubling them.
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func connectMysql(ctx context.Context, host string, port int, user, password, database string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = database
	cfg.TLSConfig = "false"

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("opening mysql connection to %s:%d: %w", host, port, err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to mysql at %s:%d: %w", host, port, err)
	}

	return db, nil
}

func connectAsRoot(ctx context.Context, host string, password string) (*sql.DB, error) {
	return connectMysql(ctx, host, mysqlPort, defaultMysqlUser, password, defaultMysqlDB)
}

// connectAsRootTrying connects as root, trying each candidate password until one
// authenticates. During a root rotation the credential used to connect is the
// very thing being changed, so a retry after a crash may find the engine on
// either the old or the new password; supplying both lets the rotation converge
// regardless of where the previous attempt stopped.
func connectAsRootTrying(ctx context.Context, host string, passwords ...string) (*sql.DB, error) {
	var lastErr error
	tried := false
	for _, pw := range passwords {
		if pw == "" {
			continue
		}
		tried = true
		db, err := connectAsRoot(ctx, host, pw)
		if err == nil {
			return db, nil
		}
		lastErr = err
	}
	if !tried {
		return nil, fmt.Errorf("no root password candidates to try")
	}
	return nil, lastErr
}

// alterMysqlUserPassword changes a user's password. Works for both a per-app
// user and root (all accounts here are addressed at the '%' host). Existing
// connections stay authenticated; only new connections need the new password.
func alterMysqlUserPassword(ctx context.Context, db *sql.DB, username, password string) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf("ALTER USER %s@'%%' IDENTIFIED BY '%s'",
			quoteIdentifier(username), escapeMysqlString(password)))
	if err != nil {
		return fmt.Errorf("altering password for user %s: %w", username, err)
	}
	return nil
}

// escapeMysqlString escapes a value for use in a MySQL single-quoted string
// literal. Backslashes must be doubled first (MySQL treats \ as an escape
// character by default), then single quotes are doubled.
func escapeMysqlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

func createMysqlUser(ctx context.Context, db *sql.DB, username, password string) error {
	escaped := escapeMysqlString(password)
	_, err := db.ExecContext(ctx,
		fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'%%' IDENTIFIED BY '%s'",
			quoteIdentifier(username), escaped))
	if err != nil {
		return fmt.Errorf("creating user %s: %w", username, err)
	}
	// Ensure password is current even if user already existed from a prior saga attempt.
	_, err = db.ExecContext(ctx,
		fmt.Sprintf("ALTER USER %s@'%%' IDENTIFIED BY '%s'",
			quoteIdentifier(username), escaped))
	if err != nil {
		return fmt.Errorf("updating password for user %s: %w", username, err)
	}
	return nil
}

func createMysqlDatabase(ctx context.Context, db *sql.DB, dbname, owner string) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteIdentifier(dbname)))
	if err != nil {
		return fmt.Errorf("creating database %s: %w", dbname, err)
	}

	_, err = db.ExecContext(ctx,
		fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'",
			quoteIdentifier(dbname), quoteIdentifier(owner)))
	if err != nil {
		return fmt.Errorf("granting privileges on %s to %s: %w", dbname, owner, err)
	}

	_, err = db.ExecContext(ctx, "FLUSH PRIVILEGES")
	if err != nil {
		return fmt.Errorf("flushing privileges: %w", err)
	}

	return nil
}

func dropMysqlDatabase(ctx context.Context, db *sql.DB, dbname string) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbname)))
	if err != nil {
		return fmt.Errorf("dropping database %s: %w", dbname, err)
	}
	return nil
}

func dropMysqlUser(ctx context.Context, db *sql.DB, username string) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf("DROP USER IF EXISTS %s@'%%'", quoteIdentifier(username)))
	if err != nil {
		return fmt.Errorf("dropping user %s: %w", username, err)
	}
	return nil
}

func terminateMysqlConnections(ctx context.Context, db *sql.DB, dbname string) error {
	rows, err := db.QueryContext(ctx,
		"SELECT ID FROM INFORMATION_SCHEMA.PROCESSLIST WHERE DB = ? AND ID != CONNECTION_ID()", dbname)
	if err != nil {
		return fmt.Errorf("listing connections to %s: %w", dbname, err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scanning process id: %w", err)
		}
		_, _ = db.ExecContext(ctx, fmt.Sprintf("KILL %d", id))
	}

	return rows.Err()
}
