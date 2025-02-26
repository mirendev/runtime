package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"miren.dev/runtime/health/portreg"
)

type HTTPPortChecker struct{}

var _ = portreg.Register("http", &HTTPPortChecker{})

func (h *HTTPPortChecker) CheckPort(ctx context.Context, log *slog.Logger, addr string, port int) (bool, error) {
	var url string

	if strings.IndexByte(addr, ':') != -1 {
		url = fmt.Sprintf("http://[%s]:%d/", addr, port)
	} else {
		url = fmt.Sprintf("http://%s:%d/", addr, port)
	}

	resp, err := http.Get(url)
	if err != nil {
		var netErr *net.OpError
		if errors.As(err, &netErr) {
			if !netErr.Temporary() && netErr.Op != "dial" {
				log.Error("unable to check http port", "addr", addr, "port", port, "error", err, "op", netErr.Op)
			}
		} else {
			log.Error("error checking http port", "addr", addr, "port", port, "error", err)
		}
	} else if resp.StatusCode < 400 {
		log.Info("http port active", "addr", addr, "port", port, "status", resp.StatusCode)
		return true, nil
	} else {
		log.Warn("http port bad status", "addr", addr, "port", port, "status", resp.StatusCode)
	}

	return false, nil
}

type TCPPortChecker struct{}

var _ = portreg.Register("tcp", &TCPPortChecker{})

func (t *TCPPortChecker) CheckPort(ctx context.Context, log *slog.Logger, addr string, port int) (bool, error) {
	if strings.IndexByte(addr, ':') != -1 {
		addr = fmt.Sprintf("[%s]:%d", addr, port)
	} else {
		addr = fmt.Sprintf("%s:%d", addr, port)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false, nil
	}

	defer conn.Close()

	log.Info("tcp port active", "addr", addr, "port", port)

	return true, nil
}

type UDPPortChecker struct{}

var _ = portreg.Register("udp", &UDPPortChecker{})

func (t *UDPPortChecker) CheckPort(ctx context.Context, log *slog.Logger, addr string, port int) (bool, error) {
	return true, nil
}
