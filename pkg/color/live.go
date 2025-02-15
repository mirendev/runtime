package color

import (
	"fmt"
	"os"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
	"golang.org/x/sys/unix"
)

func Background() string {
	to := termenv.NewOutput(os.Stdout)
	s, err := termStatusReport(to, 11)
	if err != nil {
		return ""
	}

	bgc, err := xTermColor(s)
	if err != nil {
		return ""
	}

	rgb := termenv.ConvertToRGB(bgc)

	return rgb.Hex()
}

func LiveFaint() string {
	to := termenv.NewOutput(os.Stdout)
	sr, err := termStatusReport(to, 11)
	if err != nil {
		return ""
	}

	bgc, err := xTermColor(sr)
	if err != nil {
		return ""
	}

	rgb := termenv.ConvertToRGB(bgc)

	h, s, l := rgb.Hsl()

	if l < 0.5 {
		l *= 2.2
	} else {
		l /= 2.2
	}

	return colorful.Hsl(h, s, l).Hex()
}

// Pulled over from termenv because termenv disables reading
// doing termStatusReport on tmux, even though tmux supports it.

func isForeground(fd int) bool {
	pgrp, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return false
	}

	return pgrp == unix.Getpgrp()
}

func termStatusReport(o *termenv.Output, sequence int) (string, error) {
	tty := o.TTY()
	if tty == nil {
		return "", termenv.ErrStatusReport
	}
	fd := int(tty.Fd())
	// if in background, we can't control the terminal
	if !isForeground(fd) {
		return "", termenv.ErrStatusReport
	}

	t, err := unix.IoctlGetTermios(fd, tcgetattr)
	if err != nil {
		return "", fmt.Errorf("%s: %s", termenv.ErrStatusReport, err)
	}
	defer unix.IoctlSetTermios(fd, tcsetattr, t) //nolint:errcheck

	noecho := *t
	noecho.Lflag = noecho.Lflag &^ unix.ECHO
	noecho.Lflag = noecho.Lflag &^ unix.ICANON
	if err := unix.IoctlSetTermios(fd, tcsetattr, &noecho); err != nil {
		return "", fmt.Errorf("%s: %s", termenv.ErrStatusReport, err)
	}

	// first, send OSC query, which is ignored by terminal which do not support it
	fmt.Fprintf(tty, termenv.OSC+"%d;?"+termenv.ST, sequence)

	// then, query cursor position, should be supported by all terminals
	fmt.Fprintf(tty, termenv.CSI+"6n")

	// read the next response
	res, isOSC, err := readNextResponse(o)
	if err != nil {
		return "", fmt.Errorf("%s: %s", termenv.ErrStatusReport, err)
	}

	// if this is not OSC response, then the terminal does not support it
	if !isOSC {
		return "", termenv.ErrStatusReport
	}

	// read the cursor query response next and discard the result
	_, _, err = readNextResponse(o)
	if err != nil {
		return "", err
	}

	// fmt.Println("Rcvd", res[1:])
	return res, nil
}

func xTermColor(s string) (termenv.RGBColor, error) {
	if len(s) < 24 || len(s) > 25 {
		return termenv.RGBColor(""), termenv.ErrInvalidColor
	}

	switch {
	case strings.HasSuffix(s, string(termenv.BEL)):
		s = strings.TrimSuffix(s, string(termenv.BEL))
	case strings.HasSuffix(s, string(termenv.ESC)):
		s = strings.TrimSuffix(s, string(termenv.ESC))
	case strings.HasSuffix(s, termenv.ST):
		s = strings.TrimSuffix(s, termenv.ST)
	default:
		return termenv.RGBColor(""), termenv.ErrInvalidColor
	}

	s = s[4:]

	prefix := ";rgb:"
	if !strings.HasPrefix(s, prefix) {
		return termenv.RGBColor(""), termenv.ErrInvalidColor
	}
	s = strings.TrimPrefix(s, prefix)

	h := strings.Split(s, "/")
	if len(h) != 3 {
		return termenv.RGBColor(""), termenv.ErrInvalidColor
	}

	for _, part := range h {
		if len(part) < 2 {
			return termenv.RGBColor(""), termenv.ErrInvalidColor
		}
	}

	hex := fmt.Sprintf("#%s%s%s", h[0][:2], h[1][:2], h[2][:2])
	return termenv.RGBColor(hex), nil
}

func readNextByte(o *termenv.Output) (byte, error) {
	var b [1]byte
	n, err := o.TTY().Read(b[:])
	if err != nil {
		return 0, err
	}

	if n == 0 {
		return 0, fmt.Errorf("read returned no data")
	}

	return b[0], nil
}

// readNextResponse reads either an OSC response or a cursor position response:
//   - OSC response: "\x1b]11;rgb:1111/1111/1111\x1b\\"
//   - cursor position response: "\x1b[42;1R"
func readNextResponse(o *termenv.Output) (response string, isOSC bool, err error) {
	start, err := readNextByte(o)
	if err != nil {
		return "", false, err
	}

	// first byte must be ESC
	for start != termenv.ESC {
		start, err = readNextByte(o)
		if err != nil {
			return "", false, err
		}
	}

	response += string(start)

	// next byte is either '[' (cursor position response) or ']' (OSC response)
	tpe, err := readNextByte(o)
	if err != nil {
		return "", false, err
	}

	response += string(tpe)

	var oscResponse bool
	switch tpe {
	case '[':
		oscResponse = false
	case ']':
		oscResponse = true
	default:
		return "", false, termenv.ErrStatusReport
	}

	for {
		b, err := readNextByte(o)
		if err != nil {
			return "", false, err
		}

		response += string(b)

		if oscResponse {
			// OSC can be terminated by BEL (\a) or ST (ESC)
			if b == termenv.BEL || strings.HasSuffix(response, string(termenv.ESC)) {
				return response, true, nil
			}
		} else {
			// cursor position response is terminated by 'R'
			if b == 'R' {
				return response, false, nil
			}
		}

		// both responses have less than 25 bytes, so if we read more, that's an error
		if len(response) > 25 {
			break
		}
	}

	return "", false, termenv.ErrStatusReport
}
