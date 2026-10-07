//go:build linux

package query

import (
	"bufio"
	"context"
	"debug/elf"
	"debug/gosym"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type inspectedSymbol struct {
	name, module  string
	address, size uint64
	fileOffset    *uint64
}

func inspectSymbols(ctx context.Context, r SymbolRequest) (SymbolResult, error) {
	switch r.Target {
	case "kernel":
		return inspectKernel(ctx, r)
	case "binary":
		syms, err := readELFSymbols(r.Path)
		if err != nil {
			return SymbolResult{}, err
		}
		return resultFromSymbols(ctx, r, syms), nil
	default:
		return inspectProcess(ctx, r)
	}
}

func readELFSymbols(path string) ([]inspectedSymbol, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return elfFunctionSymbols(f, path)
}

func elfFunctionSymbols(f *elf.File, path string) ([]inspectedSymbol, error) {
	var raw []elf.Symbol
	if s, e := f.Symbols(); e == nil {
		raw = append(raw, s...)
	} else if !errors.Is(e, elf.ErrNoSymbols) {
		return nil, e
	}
	if s, e := f.DynamicSymbols(); e == nil {
		raw = append(raw, s...)
	} else if !errors.Is(e, elf.ErrNoSymbols) {
		return nil, e
	}
	// Stripped Go executables retain runtime function metadata even when
	// their ELF symbol table is removed (including ordinary go test builds).
	if f.Section(".symtab") == nil {
		pcln, text := f.Section(".gopclntab"), f.Section(".text")
		if pcln == nil {
			pcln = f.Section(".data.rel.ro.gopclntab")
		}
		if pcln != nil && text != nil {
			data, err := pcln.Data()
			if err != nil {
				return nil, err
			}
			table, err := gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
			if err != nil {
				return nil, err
			}
			for _, function := range table.Funcs {
				raw = append(raw, elf.Symbol{Name: function.Name, Value: function.Entry, Size: function.End - function.Entry, Info: byte(elf.STT_FUNC), Section: 1})
			}
		}
	}
	seen := map[string]bool{}
	out := make([]inspectedSymbol, 0, len(raw))
	module := filepath.Base(path)
	for _, s := range raw {
		if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Name == "" || s.Section == elf.SHN_UNDEF {
			continue
		}
		key := fmt.Sprintf("%x/%x/%s", s.Value, s.Size, s.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		is := inspectedSymbol{name: s.Name, module: module, address: s.Value, size: s.Size}
		for _, p := range f.Progs {
			if p.Type == elf.PT_LOAD && s.Value >= p.Vaddr && s.Value-p.Vaddr < p.Filesz {
				v := p.Off + s.Value - p.Vaddr
				is.fileOffset = &v
				break
			}
		}
		out = append(out, is)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].address != out[j].address {
			return out[i].address < out[j].address
		}
		return out[i].name < out[j].name
	})
	return out, nil
}

func resultFromSymbols(ctx context.Context, r SymbolRequest, syms []inspectedSymbol) SymbolResult {
	res := SymbolResult{}
	if r.Name != "" {
		for _, s := range syms {
			if ctx.Err() != nil {
				break
			}
			if symbolNameMatches(r.Name, s.name) {
				res.Symbols = append(res.Symbols, record(s))
				if len(res.Symbols) >= r.effectiveLimit() {
					break
				}
			}
		}
		return res
	}
	for _, a := range r.Addresses {
		fr := SymbolFrame{Address: symbolHex(a), Error: "no containing function symbol"}
		if s := containing(syms, a); s != nil {
			fr.Name = s.name
			fr.Module = s.module
			fr.Offset = a - s.address
			fr.Error = ""
		}
		res.Frames = append(res.Frames, fr)
	}
	return res
}

func containing(s []inspectedSymbol, a uint64) *inspectedSymbol {
	for i := sort.Search(len(s), func(i int) bool { return s[i].address > a }) - 1; i >= 0; i-- {
		x := &s[i]
		if x.size > 0 && a-x.address < x.size {
			return x
		}
	}
	return nil
}
func record(s inspectedSymbol) SymbolRecord {
	return SymbolRecord{Address: symbolHex(s.address), Name: s.name, Module: s.module, Size: s.size, FileOffset: s.fileOffset}
}

func inspectKernel(ctx context.Context, r SymbolRequest) (SymbolResult, error) {
	all, err := readKernelSymbols(ctx)
	if err != nil {
		return SymbolResult{}, err
	}
	return resultFromSymbols(ctx, r, all), nil
}

func readKernelSymbols(ctx context.Context) ([]inspectedSymbol, error) {
	f, e := os.Open("/proc/kallsyms")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var all []inspectedSymbol
	nonzero := false
	for sc.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p := strings.Fields(sc.Text())
		if len(p) < 3 {
			continue
		}
		a, e := strconv.ParseUint(p[0], 16, 64)
		if e != nil {
			continue
		}
		if a != 0 {
			nonzero = true
		}
		if a == 0 {
			continue
		}
		typ := p[1][0]
		if !strings.ContainsRune("tTwW", rune(typ)) {
			continue
		}
		m := "kernel"
		if len(p) > 3 {
			m = strings.Trim(p[3], "[]")
		}
		all = append(all, inspectedSymbol{name: p[2], module: m, address: a})
	}
	if e := sc.Err(); e != nil {
		return nil, e
	}
	if !nonzero {
		return nil, errors.New("kernel symbols are restricted: /proc/kallsyms exposes only zero addresses")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].address < all[j].address })
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[j].address > all[i].address {
				all[i].size = all[j].address - all[i].address
				break
			}
		}
	}
	return all, nil
}

type procMap struct {
	start, end, offset uint64
	perms, dev, path   string
	inode              uint64
}

func readMaps(pid uint32) (string, []procMap, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if e != nil {
		return "", nil, e
	}
	var ms []procMap
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		rr := strings.Split(f[0], "-")
		if len(rr) != 2 {
			continue
		}
		a, _ := strconv.ParseUint(rr[0], 16, 64)
		z, _ := strconv.ParseUint(rr[1], 16, 64)
		o, _ := strconv.ParseUint(f[2], 16, 64)
		ino, _ := strconv.ParseUint(f[4], 10, 64)
		p := ""
		if len(f) > 5 {
			p = strings.Join(f[5:], " ")
		}
		ms = append(ms, procMap{a, z, o, f[1], f[3], p, ino})
	}
	return string(b), ms, sc.Err()
}
func procStart(pid uint32) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	i := strings.LastIndex(string(b), ")")
	if i < 0 {
		return "", errors.New("invalid process stat")
	}
	f := strings.Fields(string(b)[i+1:])
	if len(f) < 20 {
		return "", errors.New("invalid process stat")
	}
	return f[19], nil
}

func inspectProcess(ctx context.Context, r SymbolRequest) (SymbolResult, error) {
	start, e := procStart(r.PID)
	if e != nil {
		return SymbolResult{}, e
	}
	_, processMaps, e := readMaps(r.PID)
	if e != nil {
		return SymbolResult{}, e
	}
	// symbolMappings filters in place; retain the complete snapshot for address
	// fallback metadata and the final process-mapping identity check.
	maps := symbolMappings(slices.Clone(processMaps))
	res := SymbolResult{Frames: make([]SymbolFrame, len(r.Addresses))}
	type mappedSymbols struct {
		symbols []inspectedSymbol
		bias    uint64
	}
	cache := map[string]mappedSymbols{}
	n := len(r.Addresses)
	if r.Name != "" {
		n = len(maps)
	}
	for i := 0; i < n; i++ {
		if e := ctx.Err(); e != nil {
			return SymbolResult{}, e
		}
		var a uint64
		var m *procMap
		if r.Name != "" {
			m = &maps[i]
		} else {
			a = r.Addresses[i]
			for j := range processMaps {
				if a >= processMaps[j].start && a < processMaps[j].end {
					m = &processMaps[j]
					break
				}
			}
		}
		fr := SymbolFrame{Address: symbolHex(a), Error: "address is not in an executable file mapping"}
		if m == nil {
			res.Frames[i] = fr
			continue
		}
		if r.Name == "" {
			setMappingFallback(&fr, a, m)
			if !strings.Contains(m.perms, "x") {
				fr.Error = "address is in a non-executable mapping; no containing function symbol"
				res.Frames[i] = fr
				continue
			}
			if m.path == "" {
				fr.Error = "address is in an anonymous executable mapping; no containing function symbol"
				res.Frames[i] = fr
				continue
			}
			if m.path[0] == '[' {
				fr.Error = "address is in a special executable mapping; no containing function symbol"
				res.Frames[i] = fr
				continue
			}
		}
		key := fmt.Sprintf("%s/%d/%s/%x/%x", m.dev, m.inode, m.path, m.start, m.offset)
		cached, ok := cache[key]
		syms, bias := cached.symbols, cached.bias
		if !ok {
			var er error
			syms, bias, er = readMappedSymbols(r.PID, m)
			if er != nil {
				if r.Name != "" {
					return SymbolResult{}, fmt.Errorf("inspect mapped symbols %s: %w", m.path, er)
				}
				fr.Error = er.Error()
				res.Frames[i] = fr
				continue
			}
			cache[key] = mappedSymbols{symbols: syms, bias: bias}
		}
		if r.Name != "" {
			for _, s := range syms {
				if e := ctx.Err(); e != nil {
					return SymbolResult{}, e
				}
				address := s.address + bias
				if address < s.address || address < m.start || address >= m.end || !symbolNameMatches(r.Name, s.name) {
					continue
				}
				s.address = address
				s.module = filepath.Base(cleanDeletedPath(m.path))
				res.Symbols = append(res.Symbols, record(s))
				if len(res.Symbols) >= r.effectiveLimit() {
					break
				}
			}
			if len(res.Symbols) >= r.effectiveLimit() {
				break
			}
			continue
		}
		if a >= bias {
			if s := containing(syms, a-bias); s != nil {
				fr.Name = s.name
				fr.Module = filepath.Base(cleanDeletedPath(m.path))
				fr.Offset = a - bias - s.address
				fr.FileOffset = nil
				fr.Error = ""
			} else {
				fr.Error = "no containing function symbol"
			}
		}
		res.Frames[i] = fr
	}
	end, e := procStart(r.PID)
	if e != nil || end != start {
		return SymbolResult{}, errors.New("process identity changed during symbol inspection")
	}
	_, current, e := readMaps(r.PID)
	// Allocations during symbol inspection can grow unrelated anonymous maps.
	// Verify only mappings that determine the requested result, plus PID lifetime.
	relevant := func(ms []procMap) []procMap {
		if r.Name != "" {
			return symbolMappings(ms)
		}
		return slices.DeleteFunc(ms, func(m procMap) bool {
			return !slices.ContainsFunc(r.Addresses, func(a uint64) bool { return a >= m.start && a < m.end })
		})
	}
	if e != nil || !slices.Equal(relevant(current), relevant(processMaps)) {
		return SymbolResult{}, errors.New("process mappings changed during symbol inspection")
	}
	return res, nil
}

// setMappingFallback records mapping metadata without implying that the PC was
// resolved to a function. FileOffset is meaningful only for file-backed maps.
func setMappingFallback(fr *SymbolFrame, address uint64, m *procMap) {
	if m.path == "" {
		return
	}
	fr.Module = m.path
	if m.path[0] != '[' {
		offset := m.offset + address - m.start
		fr.FileOffset = &offset
	}
}

func readMappedSymbols(pid uint32, m *procMap) ([]inspectedSymbol, uint64, error) {
	path := fmt.Sprintf("/proc/%d/map_files/%x-%x", pid, m.start, m.end)
	file, err := os.Open(path)
	if err != nil {
		path = fmt.Sprintf("/proc/%d/root%s", pid, cleanDeletedPath(m.path))
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("cannot open mapped ELF: %w", err)
	}
	defer file.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &stat); err != nil {
		return nil, 0, err
	}
	if stat.Ino != m.inode || fmt.Sprintf("%02x:%02x", unix.Major(uint64(stat.Dev)), unix.Minor(uint64(stat.Dev))) != m.dev {
		return nil, 0, errors.New("mapped ELF identity differs from process mapping")
	}
	ef, err := elf.NewFile(file)
	if err != nil {
		return nil, 0, err
	}
	bias, err := mapBias(ef, m)
	if err != nil {
		return nil, 0, err
	}
	syms, err := elfFunctionSymbols(ef, path)
	return syms, bias, err
}

func symbolMappings(maps []procMap) []procMap {
	return slices.DeleteFunc(maps, func(m procMap) bool {
		return !strings.Contains(m.perms, "x") || m.path == "" || m.path[0] == '['
	})
}

func mapBias(f *elf.File, m *procMap) (uint64, error) {
	page := uint64(os.Getpagesize())
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		off := p.Off / page * page
		v := p.Vaddr / page * page
		if m.offset >= off && m.offset < off+((p.Memsz+p.Off-off+page-1)/page*page) {
			mappedV := v + (m.offset - off)
			if m.start < mappedV {
				return 0, errors.New("invalid ELF load bias")
			}
			return m.start - mappedV, nil
		}
	}
	return 0, errors.New("mapping does not correspond to an ELF PT_LOAD segment")
}
