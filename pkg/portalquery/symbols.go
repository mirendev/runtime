package query

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	maxSymbolAddresses = 256
	defaultSymbolLimit = 256
	maxSymbolLimit     = 4096
)

// SymbolRequest describes a standalone symbol lookup. Name is exact, prefix*,
// or *suffix. A zero Limit uses 256.
type SymbolRequest struct {
	Target    string   `json:"target"`
	PID       uint32   `json:"pid,omitempty"`
	Path      string   `json:"path,omitempty"`
	Addresses []uint64 `json:"addresses,omitempty"`
	Name      string   `json:"name,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type SymbolResult struct {
	Frames  []SymbolFrame  `json:"frames,omitempty"`
	Symbols []SymbolRecord `json:"symbols,omitempty"`
}

type SymbolFrame struct {
	Address    string  `json:"address"`
	Name       string  `json:"name,omitempty"`
	Module     string  `json:"module,omitempty"`
	Offset     uint64  `json:"offset,omitempty"`
	FileOffset *uint64 `json:"file_offset,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type SymbolRecord struct {
	Address    string  `json:"address"`
	Name       string  `json:"name"`
	Module     string  `json:"module,omitempty"`
	Size       uint64  `json:"size,omitempty"`
	FileOffset *uint64 `json:"file_offset,omitempty"`
}

func (r SymbolRequest) Validate() error {
	if r.Target != "kernel" && r.Target != "process" && r.Target != "binary" {
		return errors.New("symbol target must be kernel, process, or binary")
	}
	if len(r.Addresses) > maxSymbolAddresses {
		return fmt.Errorf("at most %d symbol addresses are allowed", maxSymbolAddresses)
	}
	if len(r.Name) > 256 {
		return errors.New("symbol name exceeds 256 bytes")
	}
	if len(r.Path) > 4096 || (r.Target != "binary" && r.Path != "") || (r.Target != "process" && r.PID != 0) {
		return errors.New("symbol path is only for binary; PID is only for process")
	}
	if r.Name != "" && !validEdgeGlob(r.Name) {
		return errors.New("symbol name must be exact or contain one edge wildcard")
	}
	if r.Limit < 0 || r.Limit > maxSymbolLimit {
		return fmt.Errorf("symbol limit must be between 0 and %d", maxSymbolLimit)
	}
	if r.Target == "process" && r.PID == 0 {
		return errors.New("process symbol lookup requires a positive PID")
	}
	if (len(r.Addresses) == 0) == (r.Name == "") {
		return errors.New("symbol lookup requires exactly one of addresses or name")
	}
	if r.Target == "binary" && (r.Path == "" || !filepath.IsAbs(r.Path)) {
		return errors.New("binary symbol path must be absolute")
	}
	return nil
}

func (r SymbolRequest) effectiveLimit() int {
	if r.Limit == 0 {
		return defaultSymbolLimit
	}
	return r.Limit
}

func symbolHex(v uint64) string { return fmt.Sprintf("0x%x", v) }

func symbolNameMatches(pattern, name string) bool {
	return pattern != "" && processNameMatches(pattern, name)
}

// InspectSymbols resolves symbols without reading process or kernel memory.
func InspectSymbols(ctx context.Context, request SymbolRequest) (SymbolResult, error) {
	if err := request.Validate(); err != nil {
		return SymbolResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SymbolResult{}, err
	}
	result, err := inspectSymbols(ctx, request)
	if ctx.Err() != nil {
		return SymbolResult{}, ctx.Err()
	}
	return result, err
}

func cleanDeletedPath(path string) string {
	return strings.TrimSuffix(path, " (deleted)")
}
