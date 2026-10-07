//go:build linux

package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCgroupCollector(t *testing.T) {
	mount := t.TempDir()
	group := filepath.Join(mount, "system.slice", "web.service")
	if err := os.MkdirAll(group, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"cpu.stat": "usage_usec 1250000\nuser_usec 750000\nsystem_usec 500000\n",
		"cpu.max":  "250000 100000\n", "memory.current": "18446744073709551607\n",
		"memory.max": "18446744073709551615\n", "memory.stat": "file 30\nanon 20\nslab 7\n", "pids.current": "13\n",
	} {
		if err := os.WriteFile(filepath.Join(group, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cgroupSnapshot(context.Background(), mount, "/system.slice/web*")
	if err != nil || len(got) != 1 {
		t.Fatalf("cgroup collection: %+v, %v", got, err)
	}
	g := got[0]
	if g.Path != "/system.slice/web.service" || g.ID == "" || g.CPUSeconds == nil || *g.CPUSeconds != 1.25 || g.CPULimitCores == nil || *g.CPULimitCores != 2.5 ||
		g.MemoryBytes == nil || *g.MemoryBytes != ^uint64(0)-8 || g.MemoryLimitBytes == nil || *g.MemoryLimitBytes != ^uint64(0) ||
		g.MemoryAnonBytes == nil || *g.MemoryAnonBytes != 20 || g.MemoryFileBytes == nil || *g.MemoryFileBytes != 30 || g.PIDsCurrent == nil || *g.PIDsCurrent != 13 {
		t.Fatalf("incorrect cgroup accounting: %+v", g)
	}
	for name, data := range map[string]string{"cpu.max": "max 100000", "memory.max": "max", "memory.current": "0", "pids.current": "0"} {
		if err := os.WriteFile(filepath.Join(group, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err = cgroupSnapshot(context.Background(), mount, "*.service")
	if err != nil || len(got) != 1 || got[0].ID != g.ID || got[0].CPULimitCores != nil || got[0].MemoryLimitBytes != nil || got[0].MemoryBytes == nil || *got[0].MemoryBytes != 0 || *got[0].PIDsCurrent != 0 {
		t.Fatalf("unlimited/zero semantics or unstable identity: %+v, %v", got, err)
	}
	root, err := cgroupSnapshot(context.Background(), mount, "/")
	if err != nil || len(root) != 1 || root[0].CPUSeconds != nil || root[0].MemoryBytes != nil {
		t.Fatalf("absent controllers became metrics: %+v, %v", root, err)
	}
	if err := os.Rename(group, group+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(group, 0700); err != nil {
		t.Fatal(err)
	}
	got, err = cgroupSnapshot(context.Background(), mount, g.Path)
	if err != nil || len(got) != 1 || got[0].ID == g.ID {
		t.Fatalf("recreated cgroup identity reused: %+v, %v", got, err)
	}
	if err := os.Symlink(group+".old", filepath.Join(mount, "alias")); err != nil {
		t.Fatal(err)
	}
	if got, err := cgroupSnapshot(context.Background(), mount, "/alias"); err != nil || len(got) != 0 {
		t.Fatalf("followed directory symlink: %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cgroupSnapshot(ctx, mount, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cgroup collection ignored cancellation: %v", err)
	}
	if _, err := cgroupSnapshot(context.Background(), filepath.Join(mount, "missing"), ""); err == nil {
		t.Fatal("missing hierarchy became empty success")
	}
}

func TestCgroupMalformedMetrics(t *testing.T) {
	for _, tc := range []struct{ file, data string }{
		{"cpu.stat", "usage_usec -1"}, {"cpu.stat", "usage_usec"}, {"cpu.max", "10 0"}, {"cpu.max", "max nope"},
		{"memory.current", "18446744073709551616"}, {"memory.max", "-1"}, {"memory.stat", "anon nope"}, {"pids.current", ""},
	} {
		t.Run(tc.file+"/"+tc.data, func(t *testing.T) {
			mount := t.TempDir()
			if err := os.WriteFile(filepath.Join(mount, tc.file), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := cgroupSnapshot(context.Background(), mount, ""); err == nil || !strings.Contains(err.Error(), tc.file) {
				t.Fatalf("malformed %s became missing/zero metric: %v", tc.file, err)
			}
		})
	}
	mount := t.TempDir()
	outside := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(outside, []byte("usage_usec 42"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(mount, "cpu.stat")); err != nil {
		t.Fatal(err)
	}
	if _, err := cgroupSnapshot(context.Background(), mount, ""); err == nil {
		t.Fatal("metric symlink escaped directory")
	}
}

func TestCgroupIOStat(t *testing.T) {
	mount := t.TempDir()
	collect := func() CgroupInfo {
		t.Helper()
		got, err := cgroupSnapshot(context.Background(), mount, "")
		if err != nil || len(got) != 1 {
			t.Fatalf("collect io.stat: %+v, %v", got, err)
		}
		return got[0]
	}

	if got := collect(); got.IO != nil {
		t.Fatalf("missing io.stat was present: %+v", got.IO)
	}
	if err := os.WriteFile(filepath.Join(mount, "io.stat"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	io := collect().IO
	if io == nil || len(io.Devices) != 0 || io.ReadBytes == nil || *io.ReadBytes != 0 || io.WriteBytes == nil || *io.WriteBytes != 0 ||
		io.DiscardBytes == nil || *io.DiscardBytes != 0 || io.ReadIOs == nil || *io.ReadIOs != 0 || io.WriteIOs == nil || *io.WriteIOs != 0 || io.DiscardIOs == nil || *io.DiscardIOs != 0 {
		t.Fatalf("empty io.stat semantics: %+v", io)
	}

	data := "8:1 rbytes=10 wbytes=20 rios=1 wios=2 dbytes=3 dios=4 future=9\n8:2 rbytes=30 rios=5 wios=6 dbytes=7 dios=8 future=11\n"
	if err := os.WriteFile(filepath.Join(mount, "io.stat"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	io = collect().IO
	if io == nil || len(io.Devices) != 2 || io.Devices[0].Device != "8:1" || io.Devices[1].Device != "8:2" ||
		io.ReadBytes == nil || *io.ReadBytes != 40 || io.WriteBytes != nil || io.ReadIOs == nil || *io.ReadIOs != 6 ||
		io.WriteIOs == nil || *io.WriteIOs != 8 || io.DiscardBytes == nil || *io.DiscardBytes != 10 || io.DiscardIOs == nil || *io.DiscardIOs != 12 ||
		io.Devices[0].Counters["future"] != 9 || io.Devices[1].Counters["future"] != 11 {
		t.Fatalf("incorrect io.stat accounting: %+v", io)
	}

	if err := os.WriteFile(filepath.Join(mount, "io.stat"), []byte("008:01 rbytes=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	io = collect().IO
	if len(io.Devices) != 1 || io.Devices[0].Device != "8:1" || io.ReadBytes == nil || *io.ReadBytes != 0 || io.WriteBytes != nil {
		t.Fatalf("partial/zero io.stat semantics: %+v", io)
	}
}

func TestCgroupIOStatValidation(t *testing.T) {
	tooMany := strings.Builder{}
	for i := 0; i <= 4096; i++ {
		fmt.Fprintf(&tooMany, "%d:0 rbytes=1\n", i)
	}
	for name, data := range map[string]string{
		"bad device": "8 rbytes=1", "negative device": "-1:0 rbytes=1",
		"overflow device": "4294967296:0 rbytes=1", "bad counter": "8:0 rbytes", "negative counter": "8:0 rbytes=-1",
		"overflow counter": "8:0 rbytes=18446744073709551616", "duplicate device": "8:0 rbytes=1\n8:0 rbytes=2",
		"canonical duplicate device": "08:0 rbytes=1\n8:00 rbytes=2", "duplicate counter": "8:0 rbytes=1 rbytes=2",
		"sum overflow": "8:0 rbytes=18446744073709551615\n8:1 rbytes=1", "too many devices": tooMany.String(),
	} {
		t.Run(name, func(t *testing.T) {
			mount := t.TempDir()
			if err := os.WriteFile(filepath.Join(mount, "io.stat"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := cgroupSnapshot(context.Background(), mount, ""); err == nil || !strings.Contains(err.Error(), "io.stat") {
				t.Fatalf("invalid io.stat accepted: %v", err)
			}
		})
	}
}

func TestCgroupIOStatAnchoredRead(t *testing.T) {
	mount := t.TempDir()
	outside := filepath.Join(t.TempDir(), "io.stat")
	if err := os.WriteFile(outside, []byte("8:0 rbytes=42"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(mount, "io.stat")); err != nil {
		t.Fatal(err)
	}
	if _, err := cgroupSnapshot(context.Background(), mount, ""); err == nil {
		t.Fatal("io.stat symlink escaped cgroup directory")
	}
}

func TestCgroupHostSnapshot(t *testing.T) {
	var stat unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &stat); err != nil || stat.Type != unix.CGROUP2_SUPER_MAGIC {
		t.Skip("no cgroup v2 mount in test environment")
	}
	got, err := querySnapshot(context.Background(), MonitorRequest{Source: "cgroups", Mode: "snapshot", Path: "/"})
	if err != nil || len(got.Cgroups) != 1 || got.Cgroups[0].Path != "/" || got.Cgroups[0].ID == "" || got.Cgroups[0].CPUSeconds == nil || *got.Cgroups[0].CPUSeconds < 0 {
		t.Fatalf("host cgroup snapshot: %+v, %v", got.Cgroups, err)
	}
}
