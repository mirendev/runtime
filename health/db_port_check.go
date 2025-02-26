package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"miren.dev/runtime/health/portreg"
)

type PostgresPortCheck struct{}

var _ = portreg.Register("postgres", &PostgresPortCheck{})

func (p *PostgresPortCheck) CheckPort(ctx context.Context, log *slog.Logger, host string, port int) (bool, error) {
	log.Debug("checking postgres port", "host", host, "port", port)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), time.Second)
	if err != nil {
		log.Debug("failed to connect to postgres", "error", err)
		return false, nil
	}

	defer conn.Close()

	fe := pgproto3.NewFrontend(conn, conn)

	fe.Send(&pgproto3.StartupMessage{
		ProtocolVersion: pgproto3.ProtocolVersionNumber,
		Parameters: map[string]string{
			"user":     "postgres",
			"database": "postgres",
		},
	})

	err = fe.Flush()
	if err != nil {
		log.Debug("failed to send startup message", "error", err)
		return false, err
	}

	log.Debug("sent startup message, waiting on recieve")
	msg, err := fe.Receive()
	if err != nil {
		log.Debug("failed to receive postgres message", "error", err)
		return false, err
	}

	switch m := msg.(type) {
	case *pgproto3.ErrorResponse:
		const StartupInProgress = "57P03"

		if m.Code == StartupInProgress {
			log.Debug("postgres is still starting up")
			return false, nil
		}
	default:
		log.Debug("received non-startup error, treating as success")
	}

	// Anything else means we got a structured response that means the server
	// is up and ready.

	return true, nil
}
