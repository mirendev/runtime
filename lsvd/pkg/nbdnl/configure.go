//go:build linux

// Copyright 2018 Axel Wagner
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nbdnl

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// Export specifies the data needed for the NBD network protocol.
type Export struct {
	Name        string
	Description string
	Size        uint64
	Flags       uint16 // TODO: Determine Flags from Device.
	BlockSizes  *BlockSizeConstraints
}

// Device is the interface that should be implemented to expose an NBD device
// to the network or the kernel. Errors returned should implement Error -
// otherwise, EIO is assumed as the error number.
type Device interface {
	io.ReaderAt
	io.WriterAt
	// Sync should block until all previous writes where written to persistent
	// storage and return any errors that occurred.
	Sync() error
}

// BlockSizeConstraints optionally specifies possible block sizes for a given
// export.
type BlockSizeConstraints struct {
	Min       uint32
	Preferred uint32
	Max       uint32
}

// Configure passes the given set of sockets to the kernel to provide them as
// an NBD device. socks must be connected to the same server (which must
// support multiple connections) and be in transmission phase. It returns the
// device-numbers that was chosen by the kernel or any error. You can then use
// /dev/nbdX as a block device. Use Disconnect to disconnect the device
// once you're done with it.
//
// This is a Linux-only API.
func Configure(e Export, socks ...*os.File) (uint32, error) {
	var opts []ConnectOption
	if e.BlockSizes != nil {
		opts = append(opts, WithBlockSize(uint64(e.BlockSizes.Preferred)))
	}
	return Connect(IndexAny, socks, e.Size, 0, ServerFlags(e.Flags), opts...)
}

var defaultBlockSizes = BlockSizeConstraints{
	Min:       4096,
	Preferred: 4096,
	Max:       4096,
}

// Loopback serves d on a private socket, passing the other end to the kernel
// to connect to an NBD device. It returns the device-number that the kernel
// chose. wait should be called to check for errors from serving the device. It
// blocks until ctx is cancelled or an error occurs (so it behaves like Serve).
// When ctx is cancelled, the device will be disconnected, and any error
// encountered while disconnecting will be returned by wait.
// If preferredIdx is not IndexAny, it will attempt to use that specific device index.
//
// This is a Linux-only API.
func Loopback(ctx context.Context, size uint64, preferredIdx uint32) (uint32, net.Conn, *os.File, func() error, error) {
	exp := Export{
		Size:       size,
		BlockSizes: &defaultBlockSizes,
		Flags:      uint16(FlagHasFlags | FlagSendFlush | FlagSendTrim | FlagSendWriteZeros),
	}

	sp, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return 0, nil, nil, nil, err
	}

	err = unix.SetNonblock(sp[0], true)
	if err != nil {
		unix.Close(sp[0])
		unix.Close(sp[1])
		return 0, nil, nil, nil, err
	}

	err = unix.SetNonblock(sp[1], true)
	if err != nil {
		unix.Close(sp[0])
		unix.Close(sp[1])
		return 0, nil, nil, nil, err
	}

	client, server := os.NewFile(uintptr(sp[0]), "client"), os.NewFile(uintptr(sp[1]), "server")
	serverc, err := net.FileConn(server)
	if err != nil {
		client.Close()
		return 0, nil, nil, nil, err
	}

	var opts []ConnectOption
	if exp.BlockSizes != nil {
		opts = append(opts, WithBlockSize(uint64(exp.BlockSizes.Preferred)))
	}
	idx, err := Connect(preferredIdx, []*os.File{client}, exp.Size, 0, ServerFlags(exp.Flags), opts...)
	if err != nil {
		client.Close()
		return 0, nil, nil, nil, err
	}

	cleanup := func() error {
		var err error

		if e := Disconnect(idx); e != nil {
			err = fmt.Errorf("failed to disconnect device: %w", e)
		}

		// these might already be closed, so ignore errors
		client.Close()
		serverc.Close()

		return err
	}

	return idx, serverc, server, cleanup, nil
}

// Reconnect creates a new socketpair and reconfigures an existing NBD device
// Returns the server connection and client file for the NBD handler
func Reconnect(ctx context.Context, idx uint32) (net.Conn, *os.File, error) {
	sp, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create socketpair: %w", err)
	}

	err = unix.SetNonblock(sp[0], true)
	if err != nil {
		unix.Close(sp[0])
		unix.Close(sp[1])
		return nil, nil, fmt.Errorf("failed to set client nonblocking: %w", err)
	}

	err = unix.SetNonblock(sp[1], true)
	if err != nil {
		unix.Close(sp[0])
		unix.Close(sp[1])
		return nil, nil, fmt.Errorf("failed to set server nonblocking: %w", err)
	}

	client := os.NewFile(uintptr(sp[0]), "client")
	server := os.NewFile(uintptr(sp[1]), "server")

	serverConn, err := net.FileConn(server)
	if err != nil {
		client.Close()
		server.Close()
		return nil, nil, fmt.Errorf("failed to create server connection: %w", err)
	}

	// Reconfigure the existing NBD device with new socket
	err = Reconfigure(idx, []*os.File{client}, 0, 0)
	if err != nil {
		client.Close()
		serverConn.Close()
		return nil, nil, fmt.Errorf("failed to reconfigure NBD device: %w", err)
	}

	return serverConn, server, nil
}
