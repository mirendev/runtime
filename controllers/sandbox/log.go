package sandbox

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"miren.dev/runtime/observability"
)

var traceIdRegx = regexp.MustCompile(`trace_id"?\s*[=:]\s*\"?(\w+)`)

type SandboxLogs struct {
	log    *slog.Logger
	entity string
	attrs  map[string]string
	extra  map[string]string
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
		extra:  make(map[string]string),
		stream: observability.Stdout,
		lw:     lw,
	}
}

// maxLineBytes caps a single newline-terminated log line. A sandbox that emits
// a huge, newline-poor blob to stdout (e.g. a verbose scraper dumping a raw
// payload) would otherwise grow s.buf — and the control process's Go heap —
// without bound, since a line is only flushed on '\n'. containerd streams in
// small chunks, so the unbounded growth happens by accumulation across many
// Write calls into s.buf; capping the buffer bounds memory to roughly
// maxLineBytes per concurrent sandbox regardless of what an app spews. Bytes
// past the cap are dropped until the next newline flushes the truncated line.
const maxLineBytes = 1 << 20 // 1 MiB

func (s *SandboxLogs) Write(p []byte) (n int, err error) {
	n = len(p)

	for len(p) > 0 {
		nl := bytes.IndexByte(p, '\n')
		if nl == -1 {
			// No newline yet: stash the remainder for the next Write, capped.
			s.bufferCapped(p)
			break
		}

		if s.buf.Len() > 0 {
			// Complete a line started in a previous Write.
			s.bufferCapped(p[:nl])
			s.processLine(s.buf.String())
			s.buf.Reset()
		} else {
			line := p[:nl]
			if len(line) > maxLineBytes {
				line = line[:maxLineBytes]
			}
			s.processLine(string(line))
		}

		p = p[nl+1:]
	}

	return
}

// bufferCapped appends p to the partial-line buffer but never lets it exceed
// maxLineBytes, so an unterminated firehose can't grow the buffer (and the
// heap) without bound.
func (s *SandboxLogs) bufferCapped(p []byte) {
	room := maxLineBytes - s.buf.Len()
	if room <= 0 {
		return
	}
	if len(p) > room {
		p = p[:room]
	}
	s.buf.Write(p)
}

var jsonLevelToStream = map[string]observability.LogStream{
	"ERROR": observability.Stderr,
	"error": observability.Stderr,
	"WARN":  observability.Stderr,
	"warn":  observability.Stderr,
}

func (s *SandboxLogs) processLine(line string) {
	ts := time.Now()

	line = strings.TrimRight(line, "\t\n\r")

	stream := s.stream

	if after, ok := strings.CutPrefix(line, "!USER "); ok {
		line = after
		stream = observability.UserOOB
	} else if after, ok := strings.CutPrefix(line, "!ERROR "); ok {
		line = after
		stream = observability.Error
	}

	traceId := ""
	if matches := traceIdRegx.FindStringSubmatch(line); len(matches) > 1 {
		traceId = matches[1]
	}

	var extra map[string]string
	if body, lvlStream, ok := s.scanJSON(line); ok {
		line = body
		if lvlStream != "" {
			stream = lvlStream
		}
		extra = s.extra
	}

	err := s.lw.WriteEntry(s.entity, observability.LogEntry{
		Timestamp:  ts,
		Stream:     stream,
		Body:       line,
		TraceID:    traceId,
		Attributes: s.attrs,
		Extra:      extra,
	})
	if err != nil {
		s.log.Error("failed to write log entry", "error", err, "line", line)
	}
}

// scanJSON extracts structured fields from a JSON log line into s.extra,
// promoting scalar fields and skipping nested objects/arrays. Returns the
// message, an optional stream override derived from the level, and whether
// parsing succeeded. Lines that aren't a JSON object, carry no string msg, or
// have trailing content fall through to be logged verbatim.
func (s *SandboxLogs) scanJSON(line string) (string, observability.LogStream, bool) {
	if len(line) == 0 || line[0] != '{' {
		return "", "", false
	}

	// gjson.Parse ignores trailing content, so validate the whole line first
	// to keep logging malformed or trailing-junk lines verbatim.
	if !gjson.Valid(line) {
		return "", "", false
	}

	root := gjson.Parse(line)
	if !root.IsObject() {
		return "", "", false
	}

	clear(s.extra)

	var msg string
	var stream observability.LogStream

	root.ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "msg", "message":
			if value.Type == gjson.String {
				msg = value.Str
			}
		case "level":
			if value.Type == gjson.String {
				stream = jsonLevelToStream[value.Str]
			}
		case "time":
			// skip
		default:
			// Escape user fields that would collide with internal attribution
			// so app logs can't shadow it: the miren.* namespace and the lone
			// un-namespaced "source" key both get a leading dash.
			k := key.Str
			if strings.HasPrefix(k, "miren.") || k == "source" {
				k = "-" + k
			}
			switch value.Type {
			case gjson.String:
				s.extra[k] = value.Str
			case gjson.Number, gjson.True, gjson.False:
				// Raw preserves the original numeric literal (large integers
				// included) and renders bools as "true"/"false".
				s.extra[k] = value.Raw
			case gjson.Null, gjson.JSON:
				// Null and nested objects/arrays are skipped.
			}
		}
		return true
	})

	if msg == "" {
		return "", "", false
	}

	return msg, stream, true
}

func (s *SandboxLogs) Stderr() *SandboxLogs {
	x := *s
	x.stream = observability.Stderr
	x.extra = make(map[string]string)

	return &x
}
