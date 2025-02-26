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
	// storage and return any errors that occured.
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
//
// This is a Linux-only API.
func Loopback(ctx context.Context, size uint64) (uint32, net.Conn, func() error, error) {
	sp, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return 0, nil, nil, err
	}
	exp := Export{
		Size:       size,
		BlockSizes: &defaultBlockSizes,
		Flags:      uint16(FlagHasFlags | FlagSendFlush | FlagSendTrim | FlagSendWriteZeros),
	}

	client, server := os.NewFile(uintptr(sp[0]), "client"), os.NewFile(uintptr(sp[1]), "server")
	serverc, err := net.FileConn(server)
	//server.Close()
	if err != nil {
		client.Close()
		return 0, nil, nil, err
	}

	idx, err := Configure(exp, client)
	if err != nil {
		client.Close()
		return 0, nil, nil, err
	}

	cleanup := func() error {
		var err error

		if e := Disconnect(idx); e != nil {
			err = fmt.Errorf("failed to disconnect device: %w", e)
		}
		if e := client.Close(); e != nil && err == nil {
			err = fmt.Errorf("failed to close client socket: %w", e)
		}
		if e := serverc.Close(); e != nil && err == nil {
			err = fmt.Errorf("failed to close server connection: %w", e)
		}

		return err
	}

	return idx, serverc, cleanup, nil
}
