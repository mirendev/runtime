package netdb

import (
	"database/sql"
	"fmt"
	"net"
	"net/netip"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"go4.org/netipx"
)

type NetDB struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

type Subnet struct {
	db  *sql.DB
	net netip.Prefix
	mu  sync.Mutex
}

func New(path string) (*NetDB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS ips (
			ip TEXT PRIMARY KEY,
			subnet TEXT,
			reserved INTEGER DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS subnets (
			cidr TEXT PRIMARY KEY,
			parent TEXT,
			reserved INTEGER DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS interfaces (
			name TEXT PRIMARY KEY,
			reserved INTEGER DEFAULT 1
		);
	`)
	if err != nil {
		return nil, err
	}

	return &NetDB{
		db:   db,
		path: path,
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
		db:  n.db,
		net: prefix,
	}, nil
}

func (s *Subnet) Router() netip.Prefix {
	return netip.PrefixFrom(s.net.Addr().Next(), s.net.Bits())
}

func (s *Subnet) Prefix() netip.Prefix {
	return s.net
}

func (s *Subnet) ReserveSubnet(bits int) (*Subnet, error) {
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
		_, err = tx.Exec("INSERT INTO subnets (cidr, parent, reserved) VALUES (?, ?, 1) ON CONFLICT(cidr) DO NOTHING",
			prefix.String(), s.net.String())
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
				db:  s.db,
				net: prefix,
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

	// Start from .2 (assuming .1 is gateway)
	ip := s.net.Addr().Next().Next()

	tx, err := s.db.Begin()
	if err != nil {
		return netip.Prefix{}, err
	}
	defer tx.Rollback()

	for {
		if !s.net.Contains(ip) {
			return netip.Prefix{}, net.InvalidAddrError("no available IPs in subnet")
		}

		// Try to insert with a unique constraint - if it fails, the IP was taken
		_, err = tx.Exec("INSERT INTO ips (ip, subnet, reserved) VALUES (?, ?, 1) ON CONFLICT(ip) DO NOTHING",
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

func (s *Subnet) Release(prefix netip.Prefix) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM ips WHERE ip = ?", prefix.Addr().String())
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
