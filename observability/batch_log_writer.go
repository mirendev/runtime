package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultFlushInterval = 250 * time.Millisecond
	defaultFlushCount    = 50

	// maxErrorBodyBytes bounds how much of a failed response we quote back.
	// Enough to carry VictoriaLogs' complaint or an auth rejection, not enough
	// to turn one bad batch into a wall of output.
	maxErrorBodyBytes = 512
)

// BatchErrorHandler reports a batch that never reached VictoriaLogs. dropped is
// the number of entries lost with it; the buffer is not retried, so they are
// gone by the time this is called.
//
// It runs synchronously on the flush loop, so it must return promptly. A
// handler that blocks holds up every later flush while WriteEntry keeps
// accepting entries with no backpressure, so the buffer grows for exactly as
// long as the block lasts. That is worst at precisely the wrong moment, since
// whatever is making the handler slow is likely the same outage that made the
// flush fail. Hand slow work to something else rather than doing it here.
//
// Think about where this reports before setting it on a writer that receives
// the process's own logs. The coordinator tees its slog output into a
// BatchLogWriter (see cli/commands/server.go), so logging a flush failure
// through that same logger feeds the failure back into the buffer and
// re-amplifies it on every flush. Report somewhere that cannot loop back, or
// leave it unset.
type BatchErrorHandler func(err error, dropped int)

// BatchLogWriterOption customizes a BatchLogWriter at construction.
type BatchLogWriterOption func(*BatchLogWriter)

// WithBatchErrorHandler makes flush failures observable.
//
// The default is to stay silent, which is what this writer has always done and
// is survivable while it carries a copy of logs that are also going to stderr.
// It stops being survivable once a writer is the only path for a runner's logs
// and the send can fail for a reason nobody would guess, an expired credential
// being the obvious one. Callers in that position should set this.
func WithBatchErrorHandler(fn BatchErrorHandler) BatchLogWriterOption {
	return func(b *BatchLogWriter) {
		b.onError = fn
	}
}

// BatchLogWriter implements LogWriter by buffering entries and flushing them
// as a single NDJSON HTTP POST to VictoriaLogs. This reduces write pressure
// compared to one POST per log record.
type BatchLogWriter struct {
	writer *PersistentLogWriter

	mu    sync.Mutex
	buf   bytes.Buffer
	count int

	flushCh   chan struct{}
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once

	onError BatchErrorHandler
}

// NewBatchLogWriter wraps a PersistentLogWriter with batching. Entries are
// buffered and flushed either every 250ms or when 50 entries accumulate,
// whichever comes first.
func NewBatchLogWriter(writer *PersistentLogWriter, opts ...BatchLogWriterOption) *BatchLogWriter {
	b := &BatchLogWriter{
		writer:  writer,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(b)
	}

	b.wg.Add(1)
	go b.run()
	return b
}

// WriteEntry marshals the entry to NDJSON and appends it to the internal
// buffer. It never blocks the caller — writes are best-effort.
func (b *BatchLogWriter) WriteEntry(entity string, le LogEntry) error {
	msg := le.Body
	if msg == "" {
		msg = " "
	}

	logData := map[string]any{
		"_msg":     msg,
		"_time":    le.Timestamp.UTC().Format(time.RFC3339Nano),
		"entity":   entity,
		"stream":   string(le.Stream),
		"trace_id": le.TraceID,
	}
	for k, v := range le.Attributes {
		if isReservedLogField(k) {
			continue
		}
		logData[k] = v
	}
	for k, v := range le.Extra {
		if isReservedLogField(k) {
			continue
		}
		logData[k] = v
	}

	jsonData, err := json.Marshal(logData)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	b.mu.Lock()
	b.buf.Write(jsonData)
	b.buf.WriteByte('\n')
	b.count++
	shouldFlush := b.count >= defaultFlushCount
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.flushCh <- struct{}{}:
		default:
		}
	}

	return nil
}

// Close signals the background goroutine to perform a final flush and stop.
// It is safe to call multiple times.
func (b *BatchLogWriter) Close() {
	b.closeOnce.Do(func() {
		close(b.done)
	})
	b.wg.Wait()
}

func (b *BatchLogWriter) run() {
	defer b.wg.Done()
	ticker := time.NewTicker(defaultFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.flush()
		case <-b.flushCh:
			b.flush()
		case <-b.done:
			b.flush()
			return
		}
	}
}

func (b *BatchLogWriter) flush() {
	b.mu.Lock()
	if b.count == 0 {
		b.mu.Unlock()
		return
	}
	data := make([]byte, b.buf.Len())
	copy(data, b.buf.Bytes())
	dropped := b.count
	b.buf.Reset()
	b.count = 0
	b.mu.Unlock()

	baseURL := normalizeBaseURL(b.writer.Address)
	insertURL := baseURL + "/insert/jsonline"

	resp, err := b.writer.Client().Post(insertURL, "application/x-ndjson", bytes.NewReader(data))
	if err != nil {
		b.reportError(fmt.Errorf("sending log batch to victorialogs: %w", err), dropped)
		return
	}
	defer resp.Body.Close()

	// A rejected batch answers with a status, not a transport error, so this is
	// the only place an authentication or quota failure becomes visible at all.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		_, _ = io.Copy(io.Discard, resp.Body)
		b.reportError(fmt.Errorf("victorialogs returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail))), dropped)
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
}

func (b *BatchLogWriter) reportError(err error, dropped int) {
	if b.onError == nil {
		return
	}
	b.onError(err, dropped)
}
