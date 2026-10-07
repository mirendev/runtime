package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParsePoints reads the Prometheus text lines VictoriaMetricsWriter sends
// back into points. It exists for the coordinator's runner telemetry ingest,
// which needs the runner's operational series as points so it can hand them
// to the same fanout its own collectors write to; everything else a runner
// ships stays opaque bytes.
//
// It accepts what the writer produces, not the whole exposition format: one
// sample per line, an optional label set, an optional millisecond timestamp.
// Blank lines and # comments are skipped. A sample without a timestamp is
// stamped with the time of the parse, which is what VictoriaMetrics' own
// import would do with it.
//
// Metric and label names must already be valid, which the writer guarantees
// by sanitizing them. Accepting "miren.cluster" here would let it reach a
// Labeled sink that sanitizes it into the reserved "miren_cluster" after any
// check against that exact name had already passed.
//
// A line that does not parse is skipped and counted rather than failing the
// batch. The runner's writer retries a refused batch whole, so failing it would
// let one bad sample block every good one queued behind it.
//
// At most limit points are parsed; samples past it are counted without being
// parsed or kept, so a huge batch costs a scan rather than an allocation per
// sample. A limit of zero or less means no limit.
func ParsePoints(data []byte, limit int) (ParseResult, error) {
	var result ParseResult
	now := time.Now()

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), len(data)+1)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if limit > 0 && len(result.Points) >= limit {
			result.OverLimit++
			continue
		}
		point, err := parseLine(line, now)
		if err != nil {
			if result.Invalid == 0 {
				result.FirstInvalid = fmt.Errorf("line %d: %w", lineNo, err)
			}
			result.Invalid++
			continue
		}
		result.Points = append(result.Points, point)
	}
	if err := scanner.Err(); err != nil {
		return ParseResult{}, err
	}
	return result, nil
}

// ParseResult is what ParsePoints kept and what it declined.
type ParseResult struct {
	Points []MetricPoint

	// OverLimit counts samples past the limit, which were not parsed.
	OverLimit int

	// Invalid counts lines that did not parse, and FirstInvalid says why the
	// first of them did not.
	Invalid      int
	FirstInvalid error
}

func parseLine(line string, now time.Time) (MetricPoint, error) {
	var point MetricPoint

	end := strings.IndexAny(line, "{ ")
	if end <= 0 {
		return point, fmt.Errorf("no metric name")
	}
	point.Name = line[:end]
	if !validName(point.Name, true) {
		return point, fmt.Errorf("invalid metric name %q", point.Name)
	}
	rest := line[end:]

	if rest[0] == '{' {
		labels, remaining, err := parseLabels(rest[1:])
		if err != nil {
			return point, err
		}
		point.Labels = labels
		rest = remaining
	}

	fields := strings.Fields(rest)
	if len(fields) < 1 || len(fields) > 2 {
		return point, fmt.Errorf("want a value and an optional timestamp, got %q", rest)
	}

	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return point, fmt.Errorf("value: %w", err)
	}
	point.Value = value

	point.Timestamp = now
	if len(fields) == 2 {
		ms, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return point, fmt.Errorf("timestamp: %w", err)
		}
		point.Timestamp = time.UnixMilli(ms)
	}

	return point, nil
}

// parseLabels consumes a label set up to and including its closing brace,
// returning what follows it.
func parseLabels(s string) (map[string]string, string, error) {
	labels := map[string]string{}
	for {
		s = strings.TrimLeft(s, " ")
		if strings.HasPrefix(s, "}") {
			return labels, s[1:], nil
		}

		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, "", fmt.Errorf("malformed label set")
		}
		name := strings.TrimSpace(s[:eq])
		if !validName(name, false) {
			return nil, "", fmt.Errorf("invalid label name %q", name)
		}
		s = s[eq+1:]
		if !strings.HasPrefix(s, `"`) {
			return nil, "", fmt.Errorf("label %q: value is not quoted", name)
		}

		value, remaining, err := parseLabelValue(s[1:])
		if err != nil {
			return nil, "", fmt.Errorf("label %q: %w", name, err)
		}
		labels[name] = value
		s = strings.TrimLeft(remaining, " ")

		if strings.HasPrefix(s, ",") {
			s = s[1:]
		} else if !strings.HasPrefix(s, "}") {
			return nil, "", fmt.Errorf("label %q: expected , or }", name)
		}
	}
}

// parseLabelValue undoes escapeLabelValue, consuming through the closing quote.
func parseLabelValue(s string) (string, string, error) {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return sb.String(), s[i+1:], nil
		case '\\':
			i++
			if i == len(s) {
				return "", "", fmt.Errorf("unterminated escape")
			}
			switch s[i] {
			case 'n':
				sb.WriteByte('\n')
			case '\\', '"':
				sb.WriteByte(s[i])
			default:
				return "", "", fmt.Errorf("unknown escape \\%c", s[i])
			}
		default:
			sb.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("unterminated value")
}

// validName reports whether name is what sanitizeMetricName or
// sanitizeLabelName would have left unchanged. Colons are only valid in
// metric names.
func validName(name string, metric bool) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r == ':' && metric:
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
