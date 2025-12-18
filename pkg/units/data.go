package units

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type (
	Bytes     int64
	KiloBytes int64
	MegaBytes int64
	GigaBytes int64
)

func (b Bytes) Bytes() Bytes {
	return b
}

func (b Bytes) KiloBytes() KiloBytes {
	return KiloBytes(b / 1024)
}

func (b Bytes) MegaBytes() MegaBytes {
	return MegaBytes(b / 1024 / 1024)
}

func (b Bytes) GigaBytes() GigaBytes {
	return GigaBytes(b / 1024 / 1024 / 1024)
}

func (k KiloBytes) Bytes() Bytes {
	return Bytes(k * 1024)
}

func (k KiloBytes) MegaBytes() MegaBytes {
	return MegaBytes(k / 1024)
}

func (k KiloBytes) GigaBytes() GigaBytes {
	return GigaBytes(k / 1024 / 1024)
}

func (m MegaBytes) Bytes() Bytes {
	return Bytes(m * 1024 * 1024)
}

func (m MegaBytes) KiloBytes() KiloBytes {
	return KiloBytes(m * 1024)
}

func (m MegaBytes) GigaBytes() GigaBytes {
	return GigaBytes(m / 1024)
}

func (g GigaBytes) Bytes() Bytes {
	return Bytes(g * 1024 * 1024 * 1024)
}

func (g GigaBytes) KiloBytes() KiloBytes {
	return KiloBytes(g * 1024 * 1024)
}

func (g GigaBytes) MegaBytes() MegaBytes {
	return MegaBytes(g * 1024)
}

func (b Bytes) Short() string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(b)/1024/1024)
	default:
		return fmt.Sprintf("%.1fGB", float64(b)/1024/1024/1024)
	}
}

func (k KiloBytes) Short() string {
	return Bytes(k * 1024).Short()
}

func (m MegaBytes) Short() string {
	return Bytes(m * 1024 * 1024).Short()
}

func (g GigaBytes) Short() string {
	return Bytes(g * 1024 * 1024 * 1024).Short()
}

func (b Bytes) String() string {
	return fmt.Sprintf("%dB", b)
}

func (k KiloBytes) String() string {
	return fmt.Sprintf("%dKB", k)
}

func (m MegaBytes) String() string {
	return fmt.Sprintf("%dMB", m)
}

func (g GigaBytes) String() string {
	return fmt.Sprintf("%dGB", g)
}

type Data interface {
	Bytes() Bytes
	Short() string
	String() string
}

func (b Bytes) Int64() int64 {
	return int64(b)
}

func (k KiloBytes) Int64() int64 {
	return int64(k)
}

func (m MegaBytes) Int64() int64 {
	return int64(m)
}

func (g GigaBytes) Int64() int64 {
	return int64(g)
}

const (
	kilo = 1000
	mega = kilo * 1000
	giga = mega * 1000
	tera = giga * 1000
	peta = tera * 1000
)

var sizeSuffix = map[string]int{
	"k":  kilo,
	"kb": kilo,
	"kB": kilo,
	"K":  kilo,
	"Kb": kilo,
	"KB": kilo,
	"m":  mega,
	"mb": mega,
	"mB": mega,
	"M":  mega,
	"Mb": mega,
	"MB": mega,
	"g":  giga,
	"gb": giga,
	"gB": giga,
	"G":  giga,
	"Gb": giga,
	"GB": giga,
	"t":  tera,
	"tb": tera,
	"tB": tera,
	"T":  tera,
	"Tb": tera,
	"TB": tera,
	"p":  peta,
	"pb": peta,
	"pB": peta,
	"P":  peta,
	"Pb": peta,
	"PB": peta,
}

func ParseData(str string) (Data, error) {
	str = strings.TrimSpace(str)

	// Handle numeric-only input
	if val, err := strconv.ParseInt(str, 10, 64); err == nil {
		return Bytes(val), nil
	}

	for suf, factor := range sizeSuffix {
		if strings.HasSuffix(str, suf) {
			base, err := strconv.ParseInt(str[:len(str)-len(suf)], 10, 64)
			if err != nil {
				return nil, errors.Wrapf(err, "parsing size")
			}

			return Bytes(base * int64(factor)), nil
		}
	}

	return nil, errors.New("unable to parse data size: unknown format")
}

// ParseDuration parses a duration string with support for days (d) and weeks (w)
// in addition to Go's standard duration units (ns, us, ms, s, m, h).
// Examples: "7d", "2w", "24h", "168h", "30m"
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration string")
	}

	// Try standard Go duration first (e.g., "24h", "30m")
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse extended format with days/weeks (e.g., "7d", "2w")
	var value float64
	var unit string
	_, err := fmt.Sscanf(s, "%f%s", &value, &unit)
	if err != nil {
		return 0, errors.Wrapf(err, "parsing duration %q", s)
	}

	switch strings.ToLower(unit) {
	case "d":
		return time.Duration(value * float64(24*time.Hour)), nil
	case "w":
		return time.Duration(value * float64(7*24*time.Hour)), nil
	default:
		return 0, errors.Errorf("unknown duration unit %q in %q", unit, s)
	}
}
