//go:build !linux

package query

import (
	"context"
	"errors"
)

func readContainers(context.Context, string) ([]ContainerInfo, error) {
	return nil, errors.New("Docker container snapshots require Linux")
}
