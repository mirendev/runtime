//go:build linux

package query

import (
	"bytes"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf/btf"
)

func TestRequestCgroupOffsets(t *testing.T) {
	id := &btf.Int{Name: "u64", Size: 8}
	kn := &btf.Struct{Name: "kernfs_node", Size: 64, Members: []btf.Member{{Name: "id", Type: id, Offset: 40 * 8}}}
	cgroup := &btf.Struct{Name: "cgroup", Size: 48, Members: []btf.Member{{Name: "kn", Type: &btf.Pointer{Target: kn}, Offset: 24 * 8}}}
	css := &btf.Struct{Name: "cgroup_subsys_state", Size: 32, Members: []btf.Member{{Name: "cgroup", Type: &btf.Pointer{Target: cgroup}, Offset: 8 * 8}}}
	blkcg := &btf.Struct{Name: "blkcg", Size: 64, Members: []btf.Member{{Name: "css", Type: css, Offset: 16 * 8}}}
	blkg := &btf.Struct{Name: "blkcg_gq", Size: 24, Members: []btf.Member{{Name: "blkcg", Type: &btf.Pointer{Target: blkcg}, Offset: 8 * 8}}}
	bio := &btf.Struct{Name: "bio", Size: 32, Members: []btf.Member{{Name: "bi_blkg", Type: &btf.Pointer{Target: blkg}, Offset: 16 * 8}}}
	rq := &btf.Struct{Name: "request", Size: 24, Members: []btf.Member{{Name: "bio", Type: &btf.Pointer{Target: bio}, Offset: 8 * 8}}}
	load := func() *btf.Spec {
		t.Helper()
		builder, err := btf.NewBuilder([]btf.Type{rq})
		if err != nil {
			t.Fatal(err)
		}
		data, err := builder.Marshal(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := btf.LoadSpecFromReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	if got, err := requestCgroupOffsets(load()); err != nil || !reflect.DeepEqual(got, []int16{8, 16, 8, 24, 24, 40}) {
		t.Fatalf("embedded CSS offset must be added, not dereferenced: %v, %v", got, err)
	}
	bio.Members[0].Name = "not_configured"
	if _, err := requestCgroupOffsets(load()); err == nil {
		t.Fatal("accepted missing CONFIG_BLK_CGROUP field")
	}
	bio.Members[0].Name = "bi_blkg"
	id.Size = 4
	if _, err := requestCgroupOffsets(load()); err == nil {
		t.Fatal("accepted truncated kernfs ID")
	}
}

func TestDiskCgroupPathResolution(t *testing.T) {
	info, err := os.Stat("/sys/fs/cgroup")
	if err != nil {
		t.Skip("no cgroup mount")
	}
	id := info.Sys().(*syscall.Stat_t).Ino
	d := &diskCgroupPaths{mount: "/sys/fs/cgroup"}
	e := &DiskEvent{IOCgroupID: &id}
	d.enrich(e)
	if e.IOCgroupPath != "/" || e.IOCgroupError != "" {
		t.Fatalf("root ID resolution: %+v", e)
	}
	// Deliberately different from any task's cgroup. Never substitute a
	// process path or root when the captured association is absent/invisible.
	unknown := ^uint64(0)
	for _, id := range []*uint64{nil, &unknown} {
		e := &DiskEvent{IOCgroupID: id}
		d.enrich(e)
		if e.IOCgroupPath != "" || e.IOCgroupError == "" {
			t.Fatalf("invented ownership: %+v", e)
		}
	}
	// Cached path lookup is keyed by charged ID, not PID or issuer name.
	id = 777
	d.paths[id], d.next = "/apps/postgres", time.Now().Add(time.Second)
	e = &DiskEvent{IOCgroupID: &id}
	d.enrich(e)
	if e.IOCgroupPath != "/apps/postgres" || e.IOCgroupError != "" {
		t.Fatalf("cached association: %+v", e)
	}
}
