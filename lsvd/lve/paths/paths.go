package paths

import (
	"os"
	"path/filepath"

	"miren.dev/runtime/lsvd/lve/pkg/id"
)

type Root string

func (r Root) Join(parts ...string) string {
	return filepath.Join(
		append([]string{string(r)}, parts...)...)
}

func (r Root) Bin() string {
	return r.Join("bin")
}

func (r Root) LSVD() string {
	return r.Join("bin", "lsvd")
}

func (r Root) DiskSocket(id id.Id) string {
	return r.Join("disks", id.String(), "nbd.sock")
}

func (r Root) DiskData(id id.Id, item string) string {
	return r.Join("disks", id.String(), item)
}

func (r Root) Instance(id id.Id) string {
	return r.Join("instances", id.String())
}

func (r Root) InstancePid(id id.Id) string {
	return r.Join("instances", id.String(), "pid")
}

func (r Root) InstanceConsole(id id.Id) string {
	return r.Join("instances", id.String(), "console.sock")
}

func (r Root) InstanceCidata(id id.Id) string {
	return r.Join("instances", id.String(), "cidata")
}

func (r Root) VPC(id id.Id) string {
	return r.Join("vpcs", id.String())
}

func (r Root) VPCInterfaces(id id.Id) string {
	return r.Join("vpcs", id.String(), "sockets")
}

func (r Root) VPCInterface(id, inf id.Id) string {
	return r.Join("vpcs", id.String(), "sockets", inf.String()+".sock")
}

func (r Root) CoreData(item string) string {
	return r.Join("coredata", item)
}

func (r Root) SetupVPC(id id.Id) error {
	vpc := r.VPCInterfaces(id)

	err := os.MkdirAll(vpc, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (r Root) SetupDisk(id id.Id) error {
	path := r.Join("disks", id.String(), "cache")

	err := os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (r Root) SetupInstance(id id.Id) error {
	path := r.Instance(id)
	return os.MkdirAll(path, 0755)
}
