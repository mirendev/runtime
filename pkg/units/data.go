package units

import "fmt"

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
