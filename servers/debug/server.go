package debug

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
)

type Server struct {
	Log      *slog.Logger
	DataPath string
	EAC      *entityserver_v1alpha.EntityAccessClient
}

var _ debug_v1alpha.IPAlloc = &Server{}

func NewServer(log *slog.Logger, dataPath string, eac *entityserver_v1alpha.EntityAccessClient) *Server {
	return &Server{
		Log:      log.With("module", "debug"),
		DataPath: dataPath,
		EAC:      eac,
	}
}

func (s *Server) openDB() (*sql.DB, error) {
	dbPath := filepath.Join(s.DataPath, "net.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open netdb at %s: %w", dbPath, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to netdb at %s: %w", dbPath, err)
	}
	return db, nil
}

func (s *Server) ListLeases(ctx context.Context, state *debug_v1alpha.IPAllocListLeases) error {
	args := state.Args()
	results := state.Results()

	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	query := "SELECT ip, subnet, reserved, released_at FROM ips"
	var conditions []string
	var queryArgs []any

	if args.HasSubnet() && args.Subnet() != "" {
		conditions = append(conditions, "subnet = ?")
		queryArgs = append(queryArgs, args.Subnet())
	}
	if args.ReservedOnly() {
		conditions = append(conditions, "reserved = 1")
	}
	if args.ReleasedOnly() {
		conditions = append(conditions, "reserved = 0")
	}
	if args.StuckOnly() {
		conditions = append(conditions, "reserved = 1 AND released_at IS NULL")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY subnet, ip"

	rows, err := db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to query IPs: %w", err)
	}
	defer rows.Close()

	var leases []*debug_v1alpha.IPLease
	for rows.Next() {
		var ip, subnet string
		var reserved int
		var releasedAt sql.NullInt64

		if err := rows.Scan(&ip, &subnet, &reserved, &releasedAt); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		lease := &debug_v1alpha.IPLease{}
		lease.SetIp(ip)
		lease.SetSubnet(subnet)
		lease.SetReserved(reserved == 1)
		if releasedAt.Valid {
			lease.SetReleasedAt(standard.ToTimestamp(time.Unix(releasedAt.Int64, 0)))
		}
		leases = append(leases, lease)
	}

	results.SetLeases(leases)
	return nil
}

func (s *Server) Status(ctx context.Context, state *debug_v1alpha.IPAllocStatus) error {
	results := state.Results()

	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT
			subnet,
			COUNT(*) as total,
			SUM(CASE WHEN reserved = 1 THEN 1 ELSE 0 END) as reserved,
			SUM(CASE WHEN reserved = 0 THEN 1 ELSE 0 END) as released,
			SUM(CASE WHEN reserved = 1 AND released_at IS NULL THEN 1 ELSE 0 END) as stuck
		FROM ips
		GROUP BY subnet
		ORDER BY subnet
	`)
	if err != nil {
		return fmt.Errorf("failed to query stats: %w", err)
	}
	defer rows.Close()

	var subnets []*debug_v1alpha.SubnetStatus
	for rows.Next() {
		var subnet string
		var total, reserved, released, stuck int32

		if err := rows.Scan(&subnet, &total, &reserved, &released, &stuck); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		status := &debug_v1alpha.SubnetStatus{}
		status.SetSubnet(subnet)
		status.SetTotal(total)
		status.SetReserved(reserved)
		status.SetReleased(released)
		status.SetStuck(stuck)

		// Calculate capacity
		if prefix, err := netip.ParsePrefix(subnet); err == nil {
			status.SetCapacity(int32(calculateCapacity(prefix)))
		}

		subnets = append(subnets, status)
	}

	results.SetSubnets(subnets)
	return nil
}

func (s *Server) ReleaseIP(ctx context.Context, state *debug_v1alpha.IPAllocReleaseIP) error {
	args := state.Args()
	results := state.Results()

	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := db.ExecContext(ctx,
		"UPDATE ips SET reserved = 0, released_at = ? WHERE ip = ? AND reserved = 1",
		time.Now().Unix(), args.Ip())
	if err != nil {
		return fmt.Errorf("failed to release IP: %w", err)
	}

	affected, _ := result.RowsAffected()
	results.SetReleased(affected > 0)
	return nil
}

func (s *Server) ReleaseSubnet(ctx context.Context, state *debug_v1alpha.IPAllocReleaseSubnet) error {
	args := state.Args()
	results := state.Results()

	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := db.ExecContext(ctx,
		"UPDATE ips SET reserved = 0, released_at = ? WHERE subnet = ? AND reserved = 1 AND released_at IS NULL",
		time.Now().Unix(), args.Subnet())
	if err != nil {
		return fmt.Errorf("failed to release subnet IPs: %w", err)
	}

	affected, _ := result.RowsAffected()
	results.SetCount(int32(affected))
	return nil
}

func (s *Server) ReleaseAll(ctx context.Context, state *debug_v1alpha.IPAllocReleaseAll) error {
	results := state.Results()

	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := db.ExecContext(ctx,
		"UPDATE ips SET reserved = 0, released_at = ? WHERE reserved = 1 AND released_at IS NULL",
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to release all IPs: %w", err)
	}

	affected, _ := result.RowsAffected()
	results.SetCount(int32(affected))
	return nil
}

func (s *Server) Gc(ctx context.Context, state *debug_v1alpha.IPAllocGc) error {
	args := state.Args()
	results := state.Results()

	// Get all sandbox IPs from entity store
	sandboxRef := entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox)
	sandboxes, err := s.EAC.List(ctx, sandboxRef)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Build set of IPs that are in use by sandboxes
	liveIPs := make(map[string]bool)
	for _, sbEnt := range sandboxes.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(sbEnt.Entity())

		for _, net := range sb.Network {
			addr := net.Address
			if strings.Contains(addr, "/") {
				if prefix, err := netip.ParsePrefix(addr); err == nil {
					addr = prefix.Addr().String()
				}
			}
			liveIPs[addr] = true
		}
	}

	// Query reserved IPs from netdb
	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	query := "SELECT ip FROM ips WHERE reserved = 1"
	var queryArgs []any
	if args.HasSubnet() && args.Subnet() != "" {
		query += " AND subnet = ?"
		queryArgs = append(queryArgs, args.Subnet())
	}

	rows, err := db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to query IPs: %w", err)
	}
	defer rows.Close()

	var orphanedIPs []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		if !liveIPs[ip] {
			orphanedIPs = append(orphanedIPs, ip)
		}
	}

	results.SetOrphanedIps(orphanedIPs)

	if args.DryRun() || len(orphanedIPs) == 0 {
		results.SetReleasedCount(0)
		return nil
	}

	// Release orphaned IPs
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "UPDATE ips SET reserved = 0, released_at = ? WHERE ip = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, ip := range orphanedIPs {
		if _, err := stmt.ExecContext(ctx, now, ip); err != nil {
			return fmt.Errorf("failed to release IP %s: %w", ip, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	results.SetReleasedCount(int32(len(orphanedIPs)))
	return nil
}

func calculateCapacity(prefix netip.Prefix) int {
	bits := prefix.Bits()
	if prefix.Addr().Is4() {
		hostBits := 32 - bits
		if hostBits <= 0 {
			return 0
		}
		total := 1 << hostBits
		return total - 3 // subtract network, broadcast, gateway
	}
	hostBits := 128 - bits
	if hostBits > 16 {
		hostBits = 16
	}
	return (1 << hostBits) - 1
}
