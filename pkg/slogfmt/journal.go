package slogfmt

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"syscall"
)

// WithJournalPriority returns a copy of h that starts every line it writes
// with the <N> syslog priority prefix journald reads off a stream, so that
// `journalctl -p warning` finds the warnings. Without it journald files every
// line at its default priority, info, whatever level the text says.
//
// level picks the level a record's priority comes from, given the context it
// was logged with and the level it prints at. Nil means the printed level.
//
// Only use it on a stream that is known to reach journald (see
// OnJournalStream). Anywhere else the prefixes would print as raw text.
func (h *TextHandler) WithJournalPriority(level func(context.Context, slog.Level) slog.Level) *TextHandler {
	if level == nil {
		level = func(_ context.Context, l slog.Level) slog.Level { return l }
	}
	ch := h.clone()
	ch.journalLevel = level
	return &TextHandler{commonHandler: ch, outboard: h.outboard, module: h.module}
}

// syslogPriority maps a slog level onto the syslog priorities journald
// filters on: err, warning, info and debug.
func syslogPriority(l slog.Level) byte {
	switch {
	case l >= slog.LevelError:
		return '3'
	case l >= slog.LevelWarn:
		return '4'
	case l >= slog.LevelInfo:
		return '6'
	default:
		return '7'
	}
}

// appendJournalLines appends src to dst with a priority prefix on every line.
// journald splits a stream into one entry per line, so the continuation lines
// of a multi-line record would otherwise land at info. Empty lines are
// dropped rather than sent as a bare prefix: a record ending in a multi-line
// attr carries a trailing blank line, and it must not become an empty err.
func appendJournalLines(dst, src []byte, priority byte) []byte {
	prefix := [3]byte{'<', priority, '>'}
	for len(src) > 0 {
		i := bytes.IndexByte(src, '\n')
		if i == 0 {
			src = src[1:]
			continue
		}
		dst = append(dst, prefix[:]...)
		if i < 0 {
			return append(dst, src...)
		}
		dst = append(dst, src[:i+1]...)
		src = src[i+1:]
	}
	return dst
}

// OnJournalStream reports whether f is the stream systemd connected to
// journald. systemd sets JOURNAL_STREAM to that stream's device and inode,
// and the variable is inherited by children whose output goes elsewhere, so
// it only counts when it matches f itself.
func OnJournalStream(f *os.File) bool {
	want := os.Getenv("JOURNAL_STREAM")
	if want == "" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return want == fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}
