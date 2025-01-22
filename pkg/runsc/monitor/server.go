// Copyright 2022 The gVisor Authors.
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

// Package server provides a common server implementation that can connect with
// remote.Remote.
package monitor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"

	"miren.dev/runtime/pkg/runsc/pb"
)

// ClientHandler is used to interface with client that connect to the server.
type ClientHandler interface {
	// NewClient is called when a new client connects to the server. It returns
	// a handler that will be bound to the client.
	NewClient() (MessageHandler, error)
}

// MessageHandler is used to process messages from a client.
type MessageHandler interface {
	// Message processes a single message. raw contains the entire unparsed
	// message. hdr is the parser message header and payload is the unparsed
	// message data.
	Message(raw []byte, hdr Header, payload []byte) error

	// Version returns what wire version of the protocol is supported.
	Version() uint32

	// Close closes the handler.
	Close()
}

type client struct {
	conn    net.Conn
	handler MessageHandler
}

func (c client) close() {
	_ = c.conn.Close()
	c.handler.Close()
}

// CommonServer provides common functionality to connect and process messages
// from different clients. Implementors decide how clients and messages are
// handled, e.g. counting messages for testing.
type CommonServer struct {
	Log *slog.Logger

	// Endpoint is the path to the socket that the server listens to.
	Endpoint string

	listener net.Listener

	handler ClientHandler

	mu   sync.Mutex
	cond *sync.Cond

	// +checklocks:cond.L
	clients []client
}

// Init initializes the server. It must be called before it is used.
func (s *CommonServer) Init(log *slog.Logger, path string, handler ClientHandler) {
	s.Log = log
	s.Endpoint = path
	s.handler = handler

	s.cond = sync.NewCond(&s.mu)
}

// Start creates the socket file and listens for new connections.
func (s *CommonServer) Start() error {
	li, err := net.Listen("unixpacket", s.Endpoint)
	if err != nil {
		return err
	}

	s.listener = li

	s.Log.Debug("starting runsc monitor", "endpoint", s.Endpoint)
	go s.run()
	return nil
}

func (s *CommonServer) run() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.Log.Error("socket.Accept()", "error", err)
			}
			return
		}
		s.Log.Debug("new connection to runsc monitor")
		msgHandler, err := s.handler.NewClient()
		if err != nil {
			s.Log.Error("handler.NewClient", "error", err)
			return
		}
		client := client{
			conn:    conn,
			handler: msgHandler,
		}
		s.cond.L.Lock()
		s.clients = append(s.clients, client)
		s.cond.Broadcast()
		s.cond.L.Unlock()

		if err := s.handshake(client); err != nil {
			s.Log.Error("error in handshake", "error", err.Error())
			s.closeClient(client)
			continue
		}
		go s.handleClient(client)
	}
}

// handshake performs version exchange with client. See common.proto for details
// about the protocol.
func (s *CommonServer) handshake(client client) error {
	var in [1024]byte
	read, err := client.conn.Read(in[:])
	if err != nil {
		return fmt.Errorf("reading handshake message: %w", err)
	}
	hsIn := pb.Handshake{}
	if err := proto.Unmarshal(in[:read], &hsIn); err != nil {
		return fmt.Errorf("unmarshalling handshake message: %w", err)
	}
	if hsIn.Version != CurrentVersion {
		return fmt.Errorf("wrong version number, want: %d, got, %d", CurrentVersion, hsIn.Version)
	}

	hsOut := pb.Handshake{Version: client.handler.Version()}
	out, err := proto.Marshal(&hsOut)
	if err != nil {
		return fmt.Errorf("marshalling handshake message: %w", err)
	}
	if _, err := client.conn.Write(out); err != nil {
		return fmt.Errorf("sending handshake message: %w", err)
	}
	return nil
}

func (s *CommonServer) handleClient(client client) {
	s.Log.Debug("handling runsc client")

	defer s.closeClient(client)

	var buf = make([]byte, 1024*1024)
	for {
		read, err := client.conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, unix.EBADF) {
				// Both errors indicate that the socket has been closed.
				return
			}

			s.Log.Error("error reading from client", "error", err)
			return
		}

		if read < HeaderStructSize {
			s.Log.Error("header truncated")
			continue
		}
		hdr := Header{}

		binary.Read(bytes.NewReader(buf[:HeaderStructSize]), binary.LittleEndian, &hdr)

		if read < int(hdr.HeaderSize) {
			s.Log.Error("message truncated", "header", hdr.HeaderSize, "read", read)
			continue
		}

		if err := client.handler.Message(buf[:read], hdr, buf[hdr.HeaderSize:read]); err != nil {
			s.Log.Error("error processing message", "error", err)
		}
	}
}

func (s *CommonServer) closeClient(client client) {
	client.close()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop tracking this client.
	for i, c := range s.clients {
		if c == client {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			break
		}
	}
	s.cond.Broadcast()
}

// Close stops listening and closes all connections.
func (s *CommonServer) Close() {
	if s.listener != nil {
		_ = s.listener.Close()
	}

	err := os.Remove(s.Endpoint)
	if err != nil && os.IsNotExist(err) {
		err = nil
	}

	s.Log.Debug("removed runsc monitor endpoint", "endpoint", s.Endpoint, "error", err)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, client := range s.clients {
		client.close()
	}
	s.clients = nil
	s.cond.Broadcast()
}

// WaitForNoClients waits until the number of clients connected reaches 0.
func (s *CommonServer) WaitForNoClients() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.clients) > 0 {
		s.cond.Wait()
	}
}
