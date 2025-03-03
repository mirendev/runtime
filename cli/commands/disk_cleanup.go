package commands

import (
	"os"
	"path/filepath"
	"strconv"

	"miren.dev/runtime/lsvd/pkg/nbdnl"
)

func DiskCleanup(ctx *Context, opts struct {
	Dir string `short:"d" long:"dir" description:"Directory to cleanup"`
}) error {

	data, err := os.ReadFile(filepath.Join(opts.Dir, "idx"))
	if err != nil {
		return err
	}

	idx, err := strconv.Atoi(string(data))
	if err != nil {
		return err
	}

	return nbdnl.Disconnect(uint32(idx))
}
