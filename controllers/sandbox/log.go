package sandbox

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"miren.dev/runtime/observability"
)

var traceIdRegx = regexp.MustCompile(`trace_id"?\s*[=:]\s*\"?(\w+)`)

type SandboxLogs struct {
	log    *slog.Logger
	entity string
	attrs  map[string]string
	buf    bytes.Buffer
	stream observability.LogStream
	lw     observability.LogWriter
}

func NewSandboxLogs(
	log *slog.Logger,
	entity string,
	attrs map[string]string,
	lw observability.LogWriter,
) *SandboxLogs {
	return &SandboxLogs{
		log:    log,
		entity: entity,
		attrs:  attrs,
		stream: observability.Stdout,
		lw:     lw,
	}
}

func (s *SandboxLogs) Write(p []byte) (n int, err error) {
	n = len(p)

	if s.buf.Len() > 0 {
		s.buf.Write(p)
		p = s.buf.Bytes()
	}

	for len(p) > 0 {
		nl := bytes.IndexByte(p, '\n')
		if nl == -1 {
			s.buf.Write(p)
			break
		}

		s.processLine(string(p[:nl]))

		p = p[nl+1:]
	}

	return
}

func (s *SandboxLogs) processLine(line string) {
	ts := time.Now()

	line = strings.TrimRight(line, "\t\n\r")

	stream := s.stream

	if strings.HasPrefix(line, "!USER ") {
		line = strings.TrimPrefix(line, "!USER ")
		stream = observability.UserOOB
	} else if strings.HasPrefix(line, "!ERROR ") {
		line = strings.TrimPrefix(line, "!ERROR ")
		stream = observability.Error
	}

	traceId := ""
	if matches := traceIdRegx.FindStringSubmatch(line); len(matches) > 1 {
		traceId = matches[1]
	}

	err := s.lw.WriteEntry(s.entity, observability.LogEntry{
		Timestamp:  ts,
		Stream:     stream,
		Body:       line,
		TraceID:    traceId,
		Attributes: s.attrs,
	})
	if err != nil {
		s.log.Error("failed to write log entry", "error", err, "line", line)
	}
}

func (s *SandboxLogs) Stderr() *SandboxLogs {
	x := *s
	x.stream = observability.Stderr

	return &x
}
