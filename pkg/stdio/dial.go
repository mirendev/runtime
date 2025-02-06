package stdio

import (
	"io"
	"net"
	"os/exec"
	"time"
)

type stdioConn struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (c *stdioConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

func (c *stdioConn) Write(b []byte) (int, error) {
	return c.w.Write(b)
}

func (c *stdioConn) Close() error {
	c.r.Close()
	return c.w.Close()
}

type stdioAddr struct {
	name string
}

func (a *stdioAddr) Network() string {
	return "stdio"
}

func (a *stdioAddr) String() string {
	return a.name
}

func (c *stdioConn) LocalAddr() net.Addr {
	return &stdioAddr{"stdio"}
}

func (c *stdioConn) RemoteAddr() net.Addr {
	return &stdioAddr{"stdio"}
}

func (c *stdioConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *stdioConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *stdioConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func Dial(cmd *exec.Cmd) (net.Conn, error) {
	r, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	w, err := cmd.StdinPipe()
	if err != nil {
		r.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}

	return &stdioConn{r: r, w: w}, nil
}
