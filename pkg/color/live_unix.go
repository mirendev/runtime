//go:build (darwin || dragonfly || freebsd || netbsd || openbsd) && !solaris && !illumos

package color

import "golang.org/x/sys/unix"

const (
	tcgetattr = unix.TIOCGETA
	tcsetattr = unix.TIOCSETA
)
