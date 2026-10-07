//go:build !linux

package query

import (
	"context"
	"errors"
)

func readCgroups(context.Context, string) ([]CgroupInfo, error) {
	return nil, errors.New("cgroup v2 snapshots require Linux")
}
