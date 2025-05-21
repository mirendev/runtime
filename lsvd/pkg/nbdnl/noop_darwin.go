//go:build darwin

// Meet the package interface so Darwin can at least build
package nbdnl

import (
	"context"
	"net"
)

func Loopback(ctx context.Context, size uint64) (uint32, net.Conn, func() error, error) {
	return 0, nil, nil, nil
}

func Disconnect(idx uint32) error {
	return nil
}
