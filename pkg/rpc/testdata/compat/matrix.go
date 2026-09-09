package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

func startServer(ctx context.Context, binary, target string) (string, func(), error) {
	cmd := exec.CommandContext(ctx, binary, "server")
	if target != "" {
		cmd.Args = append(cmd.Args, target)
	}
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	if err = cmd.Start(); err != nil {
		return "", nil, err
	}
	stop := func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		stop()
		return "", nil, fmt.Errorf("%s server exited before ready", binary)
	}
	return scanner.Text(), stop, nil
}

func matrix(oldBinary, newBinary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	versions := []struct{ name, path string }{{"old", oldBinary}, {"new", newBinary}}
	for _, loss := range []bool{false, true} {
		for _, server := range versions {
			for _, client := range versions {
				err := func() error {
					addr, stop, err := startServer(ctx, server.path, "")
					if err != nil {
						return err
					}
					defer stop()
					if loss {
						proxy, err := newProxy(addr)
						if err != nil {
							return err
						}
						defer proxy.close()
						addr = proxy.listener.LocalAddr().String()
					}
					fmt.Printf("client=%s server=%s loss=%v: ", client.name, server.name, loss)
					cmd := exec.CommandContext(ctx, client.path, "client", addr)
					cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
					return cmd.Run()
				}()
				if err != nil {
					return err
				}
			}
		}
	}
	// Cover every placement of old and new versions in the forwarding topology.
	for _, runner := range versions {
		for _, coordinator := range versions {
			for _, client := range versions {
				err := func() error {
					target, stopRunner, err := startServer(ctx, runner.path, "")
					if err != nil {
						return err
					}
					defer stopRunner()
					proxy, err := newProxy(target)
					if err != nil {
						return err
					}
					defer proxy.close()
					addr, stopCoordinator, err := startServer(ctx, coordinator.path, proxy.listener.LocalAddr().String())
					if err != nil {
						return err
					}
					defer stopCoordinator()
					fmt.Printf("client=%s coordinator=%s runner=%s (runner link loss): ", client.name, coordinator.name, runner.name)
					cmd := exec.CommandContext(ctx, client.path, "client", addr)
					cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
					return cmd.Run()
				}()
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Each client source address gets its own upstream socket, preserving distinct
// unary HTTP/3 and WebTransport connections. Every 17th packet is dropped and
// every 11th is delayed, independently in each direction of each flow, to exercise retransmission and
// reordering without altering QUIC itself.
type udpProxy struct {
	listener *net.UDPConn
	target   *net.UDPAddr
	mu       sync.Mutex
	flows    map[string]*udpFlow
	closed   bool
	wg       sync.WaitGroup
	packets  atomic.Uint64
	dropped  atomic.Uint64
	delayed  atomic.Uint64
}

type udpFlow struct {
	conn  *net.UDPConn
	upSeq uint64 // owned by the client-to-server receive loop
}

func newProxy(target string) (*udpProxy, error) {
	addr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	p := &udpProxy{listener: ln, target: addr, flows: make(map[string]*udpFlow)}
	p.wg.Add(1)
	go p.serve()
	return p, nil
}
func (p *udpProxy) forward(seq *uint64, data []byte, send func([]byte)) {
	p.packets.Add(1)
	*seq++
	n := *seq
	if n%17 == 0 {
		p.dropped.Add(1)
		return
	}
	if n%11 == 0 {
		p.delayed.Add(1)
		copyData := append([]byte(nil), data...)
		p.wg.Add(1)
		go func() { defer p.wg.Done(); time.Sleep(20 * time.Millisecond); send(copyData) }()
		return
	}
	send(data)
}
func (p *udpProxy) serve() {
	defer p.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, client, err := p.listener.ReadFromUDP(buf)
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		flow := p.flows[client.String()]
		if flow == nil {
			conn, err := net.DialUDP("udp", nil, p.target)
			if err != nil {
				p.mu.Unlock()
				return
			}
			flow = &udpFlow{conn: conn}
			p.flows[client.String()] = flow
			p.wg.Add(1)
			go func(conn *net.UDPConn) {
				defer p.wg.Done()
				b := make([]byte, 65536)
				var downSeq uint64
				for {
					n, err := conn.Read(b)
					if err != nil {
						return
					}
					p.forward(&downSeq, b[:n], func(data []byte) { _, _ = p.listener.WriteToUDP(data, client) })
				}
			}(flow.conn)
		}
		p.mu.Unlock()
		p.forward(&flow.upSeq, buf[:n], func(data []byte) { _, _ = flow.conn.Write(data) })
	}
}
func (p *udpProxy) close() {
	p.mu.Lock()
	p.closed = true
	_ = p.listener.Close()
	for _, flow := range p.flows {
		_ = flow.conn.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
	fmt.Printf("  proxy: %d packets, %d dropped, %d delayed\n", p.packets.Load(), p.dropped.Load(), p.delayed.Load())
}
