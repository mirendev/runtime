package netdb

import (
	"database/sql"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go4.org/netipx"
)

type NetDB struct {
	db   *sql.DB
	path string
	mu   sync.Mutex

	cooldownDur time.Duration
}

// IPLease represents an IP lease record from the database.
type IPLease struct {
	IP         string
	Subnet     string
	Reserved   bool
	ReleasedAt *time.Time
}

// SubnetStatus contains utilization statistics for a subnet.
type SubnetStatus struct {
	Subnet   string
	Total    int
	Reserved int
	Released int
	Capacity int
}

// LeaseFilter specifies filters for listing IP leases.
type LeaseFilter struct {
	Subnet       string
	ReservedOnly bool
	ReleasedOnly bool
}

type Subnet struct {
	netdb *NetDB
	db    *sql.DB
	net   netip.Prefix
	mu    sync.Mutex
}

func New(path string) (*NetDB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open netdb database: %w", err)
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		return nil, err
	}

	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS ips (
			ip TEXT PRIMARY KEY,
			subnet TEXT,
			reserved INTEGER DEFAULT 1,
			released_at INTEGER DEFAULT NULL
		);
		CREATE TABLE IF NOT EXISTS subnets (
			cidr TEXT PRIMARY KEY,
			parent TEXT,
			identifier TEXT,
			reserved INTEGER DEFAULT 1,
			UNIQUE(parent, identifier)
		);
		CREATE TABLE IF NOT EXISTS interfaces (
			name TEXT PRIMARY KEY,
			reserved INTEGER DEFAULT 1
		);
	`)
	if err != nil {
		return nil, err
	}

	db.Exec("ALTER TABLE ips ADD COLUMN released_at INTEGER DEFAULT NULL;")

	return &NetDB{
		db:          db,
		path:        path,
		cooldownDur: 30 * time.Minute,
	}, nil
}

func (n *NetDB) Close() error {
	return n.db.Close()
}

func (n *NetDB) Subnet(cidr string) (*Subnet, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}

	return &Subnet{
		netdb: n,
		db:    n.db,
		net:   prefix,
	}, nil
}

func (s *Subnet) Router() netip.Prefix {
	return netip.PrefixFrom(s.net.Addr().Next(), s.net.Bits())
}

func (s *Subnet) Prefix() netip.Prefix {
	return s.net
}

func (s *Subnet) ReserveSubnet(bits int, identifier string) (*Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if bits < s.net.Bits() {
		return nil, fmt.Errorf("requested subnet size %d is larger than parent subnet %d", bits, s.net.Bits())
	}

	// Start from first subnet
	addr := s.net.Addr()
	prefix := netip.PrefixFrom(addr, bits)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Check if we already have a reservation for this identifier
	var existingCIDR string
	err = tx.QueryRow("SELECT cidr FROM subnets WHERE parent = ? AND identifier = ?",
		s.net.String(), identifier).Scan(&existingCIDR)
	if err == nil {
		// Found existing reservation
		prefix, err := netip.ParsePrefix(existingCIDR)
		if err != nil {
			return nil, fmt.Errorf("invalid stored cidr: %w", err)
		}

		err = tx.Commit()
		if err != nil {
			return nil, err
		}

		return &Subnet{
			netdb: s.netdb,
			db:    s.db,
			net:   prefix,
		}, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	// No existing reservation found, proceed with allocation
	for {
		if !s.net.Contains(prefix.Addr()) {
			return nil, net.InvalidAddrError("no available subnets in parent subnet")
		}

		// Check if this subnet overlaps with any existing IP reservations
		var ipConflict bool
		err = tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM ips 
				WHERE ip >= ? AND ip <= ?
			)`,
			prefix.Addr().String(),
			lastIPInPrefix(prefix).String(),
		).Scan(&ipConflict)
		if err != nil {
			return nil, err
		}

		if ipConflict {
			prefix = nextSubnet(prefix)
			continue
		}

		// Check if this subnet overlaps with any existing subnet reservations
		var subnetConflict bool
		err = tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM subnets
				WHERE (
					? <= substr(cidr, 1, instr(cidr, '/') - 1) AND
					? >= substr(cidr, 1, instr(cidr, '/') - 1)
				) OR (
					substr(cidr, 1, instr(cidr, '/') - 1) <= ? AND
					substr(cidr, 1, instr(cidr, '/') - 1) >= ?
				)
			)`,
			prefix.Addr().String(), lastIPInPrefix(prefix).String(),
			prefix.Addr().String(), lastIPInPrefix(prefix).String(),
		).Scan(&subnetConflict)
		if err != nil {
			return nil, err
		}

		if subnetConflict {
			prefix = nextSubnet(prefix)
			continue
		}

		// Try to insert the subnet reservation
		_, err = tx.Exec("INSERT INTO subnets (cidr, parent, identifier, reserved) VALUES (?, ?, ?, 1) ON CONFLICT(cidr) DO NOTHING",
			prefix.String(), s.net.String(), identifier)
		if err != nil {
			return nil, err
		}

		// Check if we actually inserted (got the reservation)
		var count int
		err = tx.QueryRow("SELECT changes()").Scan(&count)
		if err != nil {
			return nil, err
		}

		if count > 0 {
			err = tx.Commit()
			if err != nil {
				return nil, err
			}

			return &Subnet{
				netdb: s.netdb,
				db:    s.db,
				net:   prefix,
			}, nil
		}

		prefix = nextSubnet(prefix)
	}
}

func nextSubnet(prefix netip.Prefix) netip.Prefix {
	return netip.PrefixFrom(netipx.PrefixLastIP(prefix).Next(), prefix.Bits())
}

func (s *Subnet) ReleaseSubnet(prefix netip.Prefix) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM subnets WHERE cidr = ?", prefix.String())
	return err
}

// Helper function to get the last IP in a prefix
func lastIPInPrefix(prefix netip.Prefix) netip.Addr {
	return netipx.PrefixLastIP(prefix)
}

func (s *Subnet) Reserve() (netip.Prefix, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return netip.Prefix{}, err
	}
	defer tx.Rollback()

	lastIP := netipx.PrefixLastIP(s.net)

	ts := time.Now()

	for _, dur := range []time.Duration{s.netdb.cooldownDur, 0} {
		// Start from .2 (assuming .1 is gateway)
		ip := s.net.Addr().Next().Next()

		for s.net.Contains(ip) && ip.Less(lastIP) {
			// Check if the IP is available and not recently released
			var releasedAt int64
			err = tx.QueryRow(`
				SELECT released_at FROM ips 
				WHERE ip = ? 
				AND released_at IS NOT NULL 
			`, ip.String()).Scan(&releasedAt)
			if err == nil {
				check := ts.Add(-dur).Unix()

				if releasedAt <= check {
					_, err := tx.Exec(`
						UPDATE ips
						SET reserved = 1, released_at = NULL
						WHERE ip = ?`, ip.String())
					if err != nil {
						return netip.Prefix{}, err
					}

					// We got the reservation
					err = tx.Commit()
					if err != nil {
						return netip.Prefix{}, err
					}

					return netip.PrefixFrom(ip, s.net.Bits()), nil
				}
				ip = ip.Next()
				continue
			}

			// Try to insert with a unique constraint - if it fails, the IP was taken
			_, err = tx.Exec(`
			INSERT INTO ips (ip, subnet, reserved, released_at) 
			VALUES (?, ?, 1, NULL) 
			ON CONFLICT(ip) DO NOTHING`,
				ip.String(), s.net.String())
			if err != nil {
				return netip.Prefix{}, err
			}

			// Check if we actually inserted (got the reservation)
			var count int
			err = tx.QueryRow("SELECT changes()").Scan(&count)
			if err != nil {
				return netip.Prefix{}, err
			}

			if count > 0 {
				// We got the reservation
				err = tx.Commit()
				if err != nil {
					return netip.Prefix{}, err
				}

				return netip.PrefixFrom(ip, s.net.Bits()), nil
			}

			ip = ip.Next()
		}
	}

	return netip.Prefix{}, net.InvalidAddrError("no available IPs in subnet")
}

func (s *Subnet) Release(prefix netip.Prefix) error {
	return s.ReleaseAddr(prefix.Addr())
}

func (s *Subnet) ReleaseAddr(addr netip.Addr) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE ips 
		SET reserved = 0, released_at = ? 
		WHERE ip = ?`,
		time.Now().Unix(), addr.String())
	return err
}

func (n *NetDB) ReserveInterface(prefix string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	tx, err := n.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Find the first available number by checking for gaps
	var nextNum int
	err = tx.QueryRow(`
		WITH RECURSIVE numbers(n) AS (
			SELECT 1
			UNION ALL
			SELECT n + 1
			FROM numbers
			WHERE n < (SELECT COALESCE(MAX(CAST(SUBSTR(name, ?) AS INTEGER)), 1) FROM interfaces WHERE name LIKE ? || '%')
		)
		SELECT n
		FROM numbers
		WHERE NOT EXISTS (
			SELECT 1
			FROM interfaces
			WHERE name = ? || n
		)
		LIMIT 1
	`, len(prefix)+1, prefix, prefix).Scan(&nextNum)
	if err != nil {
		if err == sql.ErrNoRows {
			// If no gaps found, use the next number after the highest
			err = tx.QueryRow(`
				SELECT COALESCE(MAX(CAST(SUBSTR(name, ?) AS INTEGER)), 0) + 1
				FROM interfaces 
				WHERE name LIKE ? || '%'
			`, len(prefix)+1, prefix).Scan(&nextNum)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	// Try to reserve the found number
	name := fmt.Sprintf("%s%d", prefix, nextNum)

	_, err = tx.Exec("INSERT INTO interfaces (name, reserved) VALUES (?, 1) ON CONFLICT(name) DO NOTHING",
		name)
	if err != nil {
		return "", err
	}

	var count int
	err = tx.QueryRow("SELECT changes()").Scan(&count)
	if err != nil {
		return "", err
	}

	if count > 0 {
		err = tx.Commit()
		if err != nil {
			return "", err
		}
		return name, nil
	}

	return "", fmt.Errorf("interface %s already reserved", name)
}

func (n *NetDB) ReleaseInterface(name string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	_, err := n.db.Exec("DELETE FROM interfaces WHERE name = ?", name)
	return err
}

// ListLeases returns IP leases matching the given filter.
func (n *NetDB) ListLeases(filter LeaseFilter) ([]IPLease, error) {
	query := "SELECT ip, subnet, reserved, released_at FROM ips"
	var conditions []string
	var args []any

	if filter.Subnet != "" {
		conditions = append(conditions, "subnet = ?")
		args = append(args, filter.Subnet)
	}
	if filter.ReservedOnly {
		conditions = append(conditions, "reserved = 1")
	}
	if filter.ReleasedOnly {
		conditions = append(conditions, "reserved = 0")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY subnet, ip"

	rows, err := n.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query leases: %w", err)
	}
	defer rows.Close()

	var leases []IPLease
	for rows.Next() {
		var ip, subnet string
		var reserved int
		var releasedAt sql.NullInt64

		if err := rows.Scan(&ip, &subnet, &reserved, &releasedAt); err != nil {
			return nil, fmt.Errorf("failed to scan lease: %w", err)
		}

		lease := IPLease{
			IP:       ip,
			Subnet:   subnet,
			Reserved: reserved == 1,
		}
		if releasedAt.Valid {
			t := time.Unix(releasedAt.Int64, 0)
			lease.ReleasedAt = &t
		}
		leases = append(leases, lease)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate leases: %w", err)
	}

	return leases, nil
}

// GetSubnetStatus returns utilization statistics for all subnets.
func (n *NetDB) GetSubnetStatus() ([]SubnetStatus, error) {
	rows, err := n.db.Query(`
		SELECT
			subnet,
			COUNT(*) as total,
			SUM(CASE WHEN reserved = 1 THEN 1 ELSE 0 END) as reserved,
			SUM(CASE WHEN reserved = 0 THEN 1 ELSE 0 END) as released
		FROM ips
		GROUP BY subnet
		ORDER BY subnet
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query subnet status: %w", err)
	}
	defer rows.Close()

	var statuses []SubnetStatus
	for rows.Next() {
		var status SubnetStatus
		if err := rows.Scan(&status.Subnet, &status.Total, &status.Reserved, &status.Released); err != nil {
			return nil, fmt.Errorf("failed to scan subnet status: %w", err)
		}

		if prefix, err := netip.ParsePrefix(status.Subnet); err == nil {
			status.Capacity = calculateCapacity(prefix)
		}

		statuses = append(statuses, status)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate subnet statuses: %w", err)
	}

	return statuses, nil
}

// GetReservedIPs returns all reserved IP addresses, optionally filtered by subnet.
func (n *NetDB) GetReservedIPs(subnet string) ([]string, error) {
	query := "SELECT ip FROM ips WHERE reserved = 1"
	var args []any
	if subnet != "" {
		query += " AND subnet = ?"
		args = append(args, subnet)
	}

	rows, err := n.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query reserved IPs: %w", err)
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, fmt.Errorf("failed to scan IP: %w", err)
		}
		ips = append(ips, ip)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reserved IPs: %w", err)
	}

	return ips, nil
}

// ForceReleaseIP releases a specific IP address regardless of its current state.
func (n *NetDB) ForceReleaseIP(ip string) (bool, error) {
	result, err := n.db.Exec(
		"UPDATE ips SET reserved = 0, released_at = ? WHERE ip = ? AND reserved = 1",
		time.Now().Unix(), ip)
	if err != nil {
		return false, fmt.Errorf("failed to release IP: %w", err)
	}

	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// ForceReleaseSubnet releases all reserved IPs in a subnet.
func (n *NetDB) ForceReleaseSubnet(subnet string) (int, error) {
	result, err := n.db.Exec(
		"UPDATE ips SET reserved = 0, released_at = ? WHERE subnet = ? AND reserved = 1",
		time.Now().Unix(), subnet)
	if err != nil {
		return 0, fmt.Errorf("failed to release subnet IPs: %w", err)
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// ForceReleaseAll releases all reserved IPs.
func (n *NetDB) ForceReleaseAll() (int, error) {
	result, err := n.db.Exec(
		"UPDATE ips SET reserved = 0, released_at = ? WHERE reserved = 1",
		time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("failed to release all IPs: %w", err)
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// ForceReleaseIPs releases a specific set of IPs in a single transaction.
func (n *NetDB) ForceReleaseIPs(ips []string) error {
	if len(ips) == 0 {
		return nil
	}

	tx, err := n.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE ips SET reserved = 0, released_at = ? WHERE ip = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, ip := range ips {
		if _, err := stmt.Exec(now, ip); err != nil {
			return fmt.Errorf("failed to release IP %s: %w", ip, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

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
