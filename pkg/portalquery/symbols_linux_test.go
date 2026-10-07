//go:build linux

package query

import (
	"bufio"
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSymbolMappingsIgnoreHeapButRetainExecutableIdentity(t *testing.T) {
	text := procMap{start: 0x1000, end: 0x2000, perms: "r-xp", path: "/bin/app", inode: 12}
	maps := []procMap{
		{start: 0x3000, end: 0x4000, perms: "rw-p", path: "[heap]"},
		text,
		{start: 0x5000, end: 0x6000, perms: "r-xp", path: "[vdso]"},
	}
	got := symbolMappings(maps)
	if len(got) != 1 || got[0] != text {
		t.Fatalf("executable mapping identity was lost: %+v", got)
	}
}

func TestContainingSymbolRequiresActualRangeAndHandlesAliases(t *testing.T) {
	syms := []inspectedSymbol{
		{name: "alias_a", address: 0x100, size: 8},
		{name: "alias_b", address: 0x100, size: 8},
		{name: "next", address: 0x200, size: 4},
	}
	if got := containing(syms, 0x107); got == nil || got.address != 0x100 {
		t.Fatalf("got %#v", got)
	}
	if got := containing(syms, 0x108); got != nil {
		t.Fatalf("gap incorrectly resolved to %#v", got)
	}
	if got := containing(syms, 0x1ff); got != nil {
		t.Fatalf("nearest symbol incorrectly used: %#v", got)
	}
	result := resultFromSymbols(context.Background(), SymbolRequest{Target: "binary", Name: "alias*"}, syms)
	if len(result.Symbols) != 2 {
		t.Fatalf("aliases were collapsed: %#v", result.Symbols)
	}
	if got := containing([]inspectedSymbol{{name: "stripped", address: 0x100}}, 0x100); got != nil {
		t.Fatalf("zero-sized/stripped symbol incorrectly treated as a range: %#v", got)
	}
}

func TestBinarySymbolsHaveLinkAddressesAndFileOffsets(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	syms, err := readELFSymbols(path)
	if err != nil {
		t.Fatal(err)
	}
	var found *inspectedSymbol
	for i := range syms {
		if syms[i].name == "miren.dev/runtime/pkg/portalquery.TestBinarySymbolsHaveLinkAddressesAndFileOffsets" {
			found = &syms[i]
			break
		}
	}
	if found == nil {
		t.Skip("test executable has no Go ELF symbol table")
	}
	if found.fileOffset == nil {
		t.Fatal("function in a load segment has no file offset")
	}
	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	valid := false
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && found.address >= p.Vaddr && found.address-p.Vaddr < p.Filesz && *found.fileOffset == p.Off+found.address-p.Vaddr {
			valid = true
		}
	}
	if !valid {
		t.Fatalf("address %#x has incorrect file offset %#x", found.address, *found.fileOffset)
	}
	result, err := InspectSymbols(context.Background(), SymbolRequest{Target: "binary", Path: path, Addresses: []uint64{found.address, found.address + found.size}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Frames[0].Name != found.name {
		t.Fatalf("resolved %q", result.Frames[0].Name)
	}
	if found.size > 0 && result.Frames[1].Name == found.name {
		t.Fatal("exclusive function end resolved to the preceding function")
	}
}

func TestMapBiasUsesMappingFileOffset(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, p := range f.Progs {
		page := uint64(os.Getpagesize())
		if p.Type == elf.PT_LOAD && p.Memsz > 0 && p.Off/page*page != 0 {
			off := p.Off / page * page
			v := p.Vaddr / page * page
			m := &procMap{start: 0x70000000 + v, offset: off}
			got, err := mapBias(f, m)
			if err != nil {
				t.Fatal(err)
			}
			if got != 0x70000000 {
				t.Fatalf("bias %#x", got)
			}
			return
		}
	}
	t.Fatal("no load segment")
}

func TestUnresolvedMappingMetadataUsesMapRelativeFileOffset(t *testing.T) {
	fr := SymbolFrame{Address: "0x91ab", Error: "no containing function symbol"}
	setMappingFallback(&fr, 0x91ab, &procMap{start: 0x8123, end: 0xa123, offset: 0x2345, path: "/opt/full path/app (deleted)"})
	if fr.Module != "/opt/full path/app (deleted)" || fr.FileOffset == nil || *fr.FileOffset != 0x33cd || fr.Name != "" {
		t.Fatalf("incorrect mapping fallback: %+v", fr)
	}
	anon := SymbolFrame{}
	setMappingFallback(&anon, 0x5555, &procMap{start: 0x5000, end: 0x6000})
	if anon.Module != "" || anon.FileOffset != nil {
		t.Fatalf("anonymous mapping acquired file metadata: %+v", anon)
	}
}

func requireMatchingProcessELFIdentity(t *testing.T) {
	t.Helper()
	address := uint64(reflect.ValueOf(requireMatchingProcessELFIdentity).Pointer())
	// Overlay filesystems can expose a different inode through procfs than
	// stat. The resolver deliberately refuses to trust that mismatched ELF.
	var stat unix.Stat_t
	if err := unix.Stat("/proc/self/exe", &stat); err != nil {
		t.Fatal(err)
	}
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(maps), "\n") {
		var start, end, offset, inode uint64
		var perms, dev string
		if n, _ := fmt.Sscanf(line, "%x-%x %s %x %s %d", &start, &end, &perms, &offset, &dev, &inode); n != 6 || address < start || address >= end {
			continue
		}
		if inode != stat.Ino || dev != fmt.Sprintf("%02x:%02x", unix.Major(uint64(stat.Dev)), unix.Minor(uint64(stat.Dev))) {
			t.Skip("requires matching procfs and filesystem executable identities")
		}
		break
	}
}

func TestProcessSymbolsResolveCurrentFunction(t *testing.T) {
	requireMatchingProcessELFIdentity(t)
	address := uint64(reflect.ValueOf(TestProcessSymbolsResolveCurrentFunction).Pointer())
	result, err := InspectSymbols(context.Background(), SymbolRequest{Target: "process", PID: uint32(os.Getpid()), Addresses: []uint64{address, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Frames[0].Name != "miren.dev/runtime/pkg/portalquery.TestProcessSymbolsResolveCurrentFunction" || result.Frames[0].Offset != 0 || result.Frames[1].Error == "" || result.Frames[1].Address != "0x1" {
		t.Fatalf("incorrect process mapping/ASLR resolution: %+v", result)
	}
	result, err = InspectSymbols(context.Background(), SymbolRequest{Target: "process", PID: uint32(os.Getpid()), Name: "miren.dev/runtime/pkg/portalquery.TestProcessSymbolsResolveCurrentFunction"})
	if err != nil || len(result.Symbols) != 1 || result.Symbols[0].Address != symbolHex(address) {
		t.Fatalf("Go process name search: %+v, %v", result, err)
	}
}

func TestStrippedProcessAddressFallsBackToMappedFile(t *testing.T) {
	requireMatchingProcessELFIdentity(t)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler required for stripped ELF fixture")
	}
	strip, err := exec.LookPath("strip")
	if err != nil {
		t.Skip("strip required for stripped ELF fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stripped-app")
	source := filepath.Join(dir, "app.c")
	if err := os.WriteFile(source, []byte(`#include <stdio.h>
static __attribute__((noinline)) int hidden_portal_function(void) { return 7; }
int main(void) {
    printf("%p\n", (void *)hidden_portal_function);
    fflush(stdout);
    return getchar() == EOF ? 0 : hidden_portal_function();
}`), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(ctx, cc, "-O0", "-fPIE", "-pie", source, "-o", path).CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v: %s", err, output)
	}
	if output, err := exec.CommandContext(ctx, strip, "--strip-all", path).CombinedOutput(); err != nil {
		t.Fatalf("strip fixture: %v: %s", err, output)
	}
	child := exec.CommandContext(ctx, path)
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); child.Wait() }()
	var addressText string
	if _, err := fmt.Fscan(bufio.NewReader(stdout), &addressText); err != nil {
		t.Fatal(err)
	}
	address, err := strconv.ParseUint(addressText, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(child.Process.Pid)
	_, maps, err := readMaps(pid)
	if err != nil {
		t.Fatal(err)
	}
	var mapping *procMap
	for i := range maps {
		if address >= maps[i].start && address < maps[i].end {
			mapping = &maps[i]
			break
		}
	}
	if mapping == nil {
		t.Fatal("fixture function has no process mapping")
	}
	wantOffset := mapping.offset + address - mapping.start
	result, err := InspectSymbols(ctx, SymbolRequest{Target: "process", PID: pid, Addresses: []uint64{address}})
	if err != nil {
		t.Fatal(err)
	}
	frame := result.Frames[0]
	if frame.Address != symbolHex(address) || frame.Name != "" || frame.Module != path || frame.FileOffset == nil || *frame.FileOffset != wantOffset || frame.Error != "no containing function symbol" {
		t.Fatalf("incorrect stripped mapping fallback: %+v; mapping %+v", frame, mapping)
	}
}

func TestProcessSymbolsSearchExecutableAndLibrary(t *testing.T) {
	requireMatchingProcessELFIdentity(t)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler required for PIE/shared-library fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	for name, source := range map[string]string{
		"lib.c": `int portal_sym_library(void) { return 7; }`,
		"app.c": `#include <stdio.h>
int portal_sym_library(void);
int portal_sym_executable(void) { return 3; }
int main(void) {
    printf("%p %p\n", (void *)portal_sym_executable, (void *)portal_sym_library);
    fflush(stdout);
    return getchar() == EOF ? 0 : 1;
}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"-shared", "-fPIC", "lib.c", "-o", "libportaltest.so"},
		{"-fPIE", "-pie", "app.c", "-L.", "-Wl,-rpath," + dir, "-lportaltest", "-o", "app"},
	} {
		build := exec.CommandContext(ctx, cc, args...)
		build.Dir = dir
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v: %s", err, output)
		}
	}
	child := exec.CommandContext(ctx, filepath.Join(dir, "app"))
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); child.Wait() }()
	var executableAddress, libraryAddress string
	if _, err := fmt.Fscan(bufio.NewReader(stdout), &executableAddress, &libraryAddress); err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct{ address, module string }{
		"portal_sym_executable": {executableAddress, "app"},
		"portal_sym_library":    {libraryAddress, "libportaltest.so"},
	}
	pid := uint32(child.Process.Pid)
	for _, search := range []struct {
		name  string
		limit int
		count int
	}{
		{"portal_sym_*", 0, 2},
		{"portal_sym_library", 0, 1},
		{"*executable", 0, 1},
		{"portal_sym_*", 1, 1},
		{"does_not_exist", 0, 0},
	} {
		result, err := InspectSymbols(ctx, SymbolRequest{Target: "process", PID: pid, Name: search.name, Limit: search.limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Symbols) != search.count || len(result.Frames) != 0 {
			t.Fatalf("search %+v: %+v", search, result)
		}
		seen := map[string]bool{}
		for _, symbol := range result.Symbols {
			want, ok := expected[symbol.Name]
			address, err := strconv.ParseUint(symbol.Address, 0, 64)
			wantAddress, _ := strconv.ParseUint(want.address, 0, 64)
			if !ok || err != nil || address != wantAddress || symbol.Module != want.module || symbol.FileOffset == nil || seen[symbol.Name] {
				t.Fatalf("incorrect or duplicate runtime symbol: %+v, want %+v", symbol, want)
			}
			seen[symbol.Name] = true
		}
	}
}
