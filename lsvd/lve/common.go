package lve

import "path/filepath"

type CommonOptions struct {
	WorkDir  string
	CPUs     int
	MemoryMB int

	UserNat bool
	MacVtap string

	LSVDSocket string
	LSVDVolume string

	LNFSocketDir string
	LNFSocket    string

	ConfigMount string
	HostMount   string
}

func (co *CommonOptions) MemoryFile() string {
	return filepath.Join(co.WorkDir, "memory")
}
