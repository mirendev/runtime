package observability

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync/atomic"

	"google.golang.org/protobuf/proto"
	"miren.dev/runtime/pkg/runsc"
	"miren.dev/runtime/pkg/runsc/monitor"

	"miren.dev/runtime/pkg/runsc/pb"
)

var MonitorPoints = []string{
	runsc.EnterSyscallByName("accept"),
	runsc.EnterSyscallByName("accept4"),
	runsc.EnterSyscallByName("bind"),
	runsc.ContainerStart,
	runsc.SentryClone,
	runsc.SentryExecve,
	runsc.SentryTaskExit,
}

type PortTracker interface {
	SetPortStatus(containerID string, bp BoundPort, status PortStatus)
}

type RunSCMonitor struct {
	Log   *slog.Logger
	Ports PortTracker

	endpoint string

	cs       *monitor.CommonServer
	messages atomic.Uint64
}

func (r *RunSCMonitor) SetEndpoint(endpoint string) {
	r.endpoint = endpoint
}

func (r *RunSCMonitor) WritePodInit(path string) error {
	if r.endpoint == "" {
		r.endpoint = "/run/runsc-mon.sock"
	}

	cfg := runsc.InitConfig{
		TraceSession: runsc.SessionConfig{
			Name: "Default",
			Sinks: []runsc.SinkConfig{
				{
					Name: "remote",
					Config: map[string]interface{}{
						"endpoint": r.endpoint,
					},
				},
			},
		},
	}

	cfg.TraceSession.AddPoints(MonitorPoints...)

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer f.Close()

	return json.NewEncoder(f).Encode(cfg)
}

func (r *RunSCMonitor) Monitor(ctx context.Context) error {
	if r.endpoint == "" {
		r.endpoint = "/run/runsc-mon.sock"
	}

	var cs monitor.CommonServer

	cs.Init(r.Log, r.endpoint, r)

	r.cs = &cs

	return cs.Start()
}

func (r *RunSCMonitor) Close() error {
	r.cs.Close()
	return nil
}

func (r *RunSCMonitor) Messages() uint64 {
	return r.messages.Load()
}

type clientHandler struct {
	r *RunSCMonitor
}

func (r *RunSCMonitor) NewClient() (monitor.MessageHandler, error) {
	return &clientHandler{r: r}, nil
}

func (c *clientHandler) Message(raw []byte, hdr monitor.Header, payload []byte) error {
	c.r.messages.Add(1)

	var msg proto.Message

	switch hdr.MessageType {
	case uint16(pb.MessageType_MESSAGE_CONTAINER_START):
		msg = &pb.Start{}
	case uint16(pb.MessageType_MESSAGE_SENTRY_CLONE):
		msg = &pb.Clone{}
	case uint16(pb.MessageType_MESSAGE_SENTRY_EXEC):
		msg = &pb.ExecveInfo{}
	case uint16(pb.MessageType_MESSAGE_SENTRY_TASK_EXIT):
		msg = &pb.TaskExit{}
	case uint16(pb.MessageType_MESSAGE_SYSCALL_ACCEPT):
		msg = &pb.Accept{}
	case uint16(pb.MessageType_MESSAGE_SYSCALL_BIND):
		msg = &pb.Bind{}
	}

	if msg != nil {
		err := proto.Unmarshal(payload, msg)
		if err != nil {
			return err
		}
	}

	switch v := msg.(type) {
	case *pb.Bind:
		family := binary.NativeEndian.Uint16(v.Address[:2])

		var ip SockAddr

		switch family {
		case 2:
			var addr SockAddrInet
			binary.Read(bytes.NewReader(v.Address), binary.NativeEndian, &addr)

			ip = &addr
		case 10:
			var addr SockAddrInet6
			binary.Read(bytes.NewReader(v.Address), binary.NativeEndian, &addr)

			ip = &addr
		}

		if ip != nil {
			bp := BoundPort{
				Addr: ip.Address(),
				Port: ip.Port(),
			}

			c.r.Ports.SetPortStatus(v.ContextData.ContainerId, bp, PortStatusBound)
		}
	}

	return nil
}

func (c *clientHandler) Version() uint32 {
	return monitor.CurrentVersion
}

func (c *clientHandler) Close() {

}

type InetAddr [4]byte

type SockAddrInet struct {
	Family  uint16
	NetPort uint16
	Addr    InetAddr
	_       [8]uint8 // pad to sizeof(struct sockaddr).
}

func ntohs(net uint16) int {
	return int((net>>8)&0xff | (net<<8)&0xff00)
}

type SockAddr interface {
	Port() int
	Address() netip.Addr
}

func (s *SockAddrInet) Port() int {
	return ntohs(s.NetPort)
}

func (s *SockAddrInet) Address() netip.Addr {
	return netip.AddrFrom4(s.Addr)
}

func (s *SockAddrInet) String() string {
	return fmt.Sprintf("%s:%d", s.Address(), s.Port())
}

// Inet6Addr is struct in6_addr, from uapi/linux/in6.h.
type Inet6Addr [16]byte

// SockAddrInet6 is struct sockaddr_in6, from uapi/linux/in6.h.
type SockAddrInet6 struct {
	Family   uint16
	NetPort  uint16
	Flowinfo uint32
	Addr     [16]byte
	Scope_id uint32
}

func (s *SockAddrInet6) Port() int {
	return ntohs(s.NetPort)
}

func (s *SockAddrInet6) Address() netip.Addr {
	return netip.AddrFrom16(s.Addr)
}

func (s *SockAddrInet6) String() string {
	return fmt.Sprintf("[%s]:%d", s.Address(), s.Port())
}

const UnixPathMax = 108

// SockAddrUnix is struct sockaddr_un, from uapi/linux/un.h.
type SockAddrUnix struct {
	Family uint16
	Path   [UnixPathMax]int8
}
