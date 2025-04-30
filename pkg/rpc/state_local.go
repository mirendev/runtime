package rpc

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"miren.dev/runtime/pkg/packet"
	"miren.dev/runtime/pkg/stdio"
)

func (s *State) setupLocal(ctx context.Context) error {
	pm := packet.NewPacketConnMultiplex(ctx)
	s.localMP = pm
	s.localTransport = &quic.Transport{Conn: pm}

	return nil
}

func (s *State) connectLocal(addr string) (*NetworkClient, error) {
	c, err := net.Dial("unix", addr)
	if err != nil {
		return nil, err
	}

	remote, err := s.localMP.AddConn(c)
	if err != nil {
		return nil, err
	}

	return &NetworkClient{
		State:      s,
		transport:  s.localTransport,
		remote:     "local",
		remoteAddr: remote,
	}, nil
}

func (s *State) connectProcess(cmd *exec.Cmd) (*NetworkClient, error) {
	se, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	bse := bufio.NewReader(se)
	go func() {
		defer se.Close()

		for {
			line, _, err := bse.ReadLine()
			if err != nil {
				return
			}

			s.log.Debug("process stderr", "line", string(line))
		}
	}()

	nc, err := stdio.Dial(cmd)
	if err != nil {
		return nil, err
	}

	remote, err := s.localMP.AddConn(nc)
	if err != nil {
		return nil, err
	}

	go func() {
		err := cmd.Wait()

		s.localMP.RemoveConn(remote)

		time.Sleep(1 * time.Second)

		if err != nil {
			s.log.Error("process exited with error", "error", err)
		} else {
			s.log.Debug("process exited")
		}
	}()

	return &NetworkClient{
		State:      s,
		transport:  s.localTransport,
		remote:     "local",
		remoteAddr: remote,
	}, nil

}

func (s *State) startLocalListener(ctx context.Context, addr string) error {
	pm := s.localMP

	os.Remove(addr)

	li, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	os.Chmod(addr, 0777)

	go func() {
		for {
			c, err := li.Accept()
			if err != nil {
				s.log.Error("failed to accept connection", "error", err)
				return
			}

			go func() {
				ir, err := pm.AddConn(c)
				if err != nil {
					s.log.Error("failed to add connection to multiplexer", "error", err)
				}

				s.log.Debug("accepted local connection", "remote", c.RemoteAddr(), "ir", ir)
			}()
		}
	}()

	// We build our own TLSConfig because for the unix listener,
	// we don't check client certs because it's assumed that unix
	// local access is governing access.

	tlsCfg := s.serverTlsCfg.Clone()
	tlsCfg.ClientCAs = nil
	tlsCfg.ClientAuth = tls.NoClientCert

	ec, err := s.localTransport.ListenEarly(tlsCfg, &s.qc)
	if err != nil {
		return err
	}

	subS := &State{
		StateCommon: s.StateCommon,
		transport:   s.localTransport,
	}

	subS.server = s.server.Clone(subS)

	serv := &http3.Server{
		Handler: subS.server,
		Logger:  subS.log.With("module", "http3-local"),
	}

	subS.hs = serv

	go func() {
		<-ctx.Done()
		os.Remove(addr)
		serv.Shutdown(context.Background())
	}()

	s.log.Debug("starting local listener", "addr", addr)
	go subS.hs.ServeListener(ec)

	return nil
}
