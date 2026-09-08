package rpc

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/quic-go/quic-go/http3"
)

const drainPath = "/_rpc/drain"
const drainVersionHeader = "Rpc-Drain-Version"

// drainingHTTP keeps response bodies alive while retiring a pool. HTTP/3's
// CloseIdleConnections counts RoundTrip calls, which end at response headers,
// rather than responses the application is still reading.
type drainingHTTP struct {
	mu      sync.Mutex
	current *drainingHTTPPool
	base    *http3.Transport
	ctx     context.Context
	prepare func(context.Context, *http.Request) error
}

type drainingHTTPPool struct {
	transport *http3.Transport
	ctx       context.Context
	cancel    context.CancelFunc
	active    int
	retiring  bool
	closed    bool
}

func (d *drainingHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	p := d.current
	startWatch := p == nil
	if p == nil {
		ctx, cancel := context.WithCancel(d.ctx)
		p = &drainingHTTPPool{
			ctx: ctx, cancel: cancel,
			transport: &http3.Transport{
				TLSClientConfig: d.base.TLSClientConfig,
				QUICConfig:      d.base.QUICConfig,
				Dial:            d.base.Dial,
				Logger:          d.base.Logger,
			},
		}
		context.AfterFunc(ctx, func() { _ = p.transport.Close() })
		d.current = p
	}
	p.active++
	d.mu.Unlock()
	if startWatch {
		u := *req.URL
		go d.watch(p, u)
	}

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		d.release(p)
		return nil, err
	}
	resp.Body = &drainResponseBody{ReadCloser: resp.Body, release: func() { d.release(p) }}

	return resp, nil
}

func (d *drainingHTTP) release(p *drainingHTTPPool) {
	d.mu.Lock()
	p.active--
	closePool := d.closeIfDrained(p)
	d.mu.Unlock()
	if closePool {
		p.cancel()
	}
}

func (d *drainingHTTP) retire(p *drainingHTTPPool) {
	d.mu.Lock()
	p.retiring = true
	if d.current == p {
		d.current = nil
	}
	closePool := d.closeIfDrained(p)
	d.mu.Unlock()
	if closePool {
		p.cancel()
	}
}

// Called with mu held. A pool can be retired by its notification while the last
// response is being consumed, so exactly one of those paths owns closure.
func (d *drainingHTTP) closeIfDrained(p *drainingHTTPPool) bool {
	if !p.retiring || p.active != 0 || p.closed {
		return false
	}
	p.closed = true
	return true
}

func (d *drainingHTTP) watch(p *drainingHTTPPool, u url.URL) {
	u.Path, u.RawPath, u.RawQuery = drainPath, "", ""
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		d.retire(p)
		return
	}
	if err := d.prepare(p.ctx, req); err != nil {
		d.retire(p)
		return
	}
	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		// If the notification channel dies, don't leave an unwatched pool
		// available for future calls. Existing calls keep their own errors.
		d.retire(p)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get(drainVersionHeader) != "1" {
		// Old servers return 404. No new messages are sent to their RPC
		// decoder, and their ordinary GOAWAY behavior remains available.
		return
	}
	// A supported watch returns "drain\n" at shutdown. An interrupted or
	// malformed watch also retires the pool; neither case retries any call.
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 7))
	_ = resp.Body.Close()
	d.retire(p)
}

type drainResponseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *drainResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.once.Do(b.release)
	}
	return n, err
}

func (b *drainResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
