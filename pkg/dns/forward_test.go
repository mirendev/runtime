package dns

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/slogfmt"
)

type exchangeResult struct {
	msg *mdns.Msg
	err error
}

type fakeExchanger struct {
	results []exchangeResult
	calls   []string
}

func (f *fakeExchanger) ExchangeContext(_ context.Context, _ *mdns.Msg, upstream string) (*mdns.Msg, time.Duration, error) {
	f.calls = append(f.calls, upstream)
	if len(f.results) == 0 {
		return nil, 0, errors.New("unexpected exchange")
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result.msg, 0, result.err
}

type blockingExchanger struct {
	calls int
}

func (f *blockingExchanger) ExchangeContext(ctx context.Context, _ *mdns.Msg, _ string) (*mdns.Msg, time.Duration, error) {
	f.calls++
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

type captureResponseWriter struct {
	remote  net.Addr
	message *mdns.Msg
}

func (w *captureResponseWriter) LocalAddr() net.Addr          { return &net.UDPAddr{} }
func (w *captureResponseWriter) RemoteAddr() net.Addr         { return w.remote }
func (w *captureResponseWriter) WriteMsg(msg *mdns.Msg) error { w.message = msg; return nil }
func (w *captureResponseWriter) Write([]byte) (int, error)    { return 0, nil }
func (w *captureResponseWriter) Close() error                 { return nil }
func (w *captureResponseWriter) TsigStatus() error            { return nil }
func (w *captureResponseWriter) TsigTimersOnly(bool)          {}
func (w *captureResponseWriter) Hijack()                      {}

func testForwardServer(t *testing.T, upstreams []string, udp, tcp exchanger) (*Server, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newServer("127.0.0.1:0", upstreams, nil, log)
	s.udpClient = udp
	s.tcpClient = tcp
	return s, &logs
}

func query(name string) *mdns.Msg {
	msg := new(mdns.Msg)
	msg.SetQuestion(name, mdns.TypeA)
	return msg
}

func successfulResponse(request *mdns.Msg) *mdns.Msg {
	response := new(mdns.Msg)
	response.SetReply(request)
	return response
}

func oversizedResponse(request *mdns.Msg, size int) *mdns.Msg {
	response := successfulResponse(request)
	for response.Len() <= size {
		response.Answer = append(response.Answer, &mdns.TXT{
			Hdr: mdns.RR_Header{Name: request.Question[0].Name, Rrtype: mdns.TypeTXT, Class: mdns.ClassINET},
			Txt: []string{strings.Repeat("x", 200)},
		})
	}
	return response
}

func TestNewServerSetsExplicitUpstreamTimeouts(t *testing.T) {
	s := newServer("127.0.0.1:0", []string{"192.0.2.1"}, nil, slog.Default())

	udp, ok := s.udpClient.(*mdns.Client)
	require.True(t, ok)
	tcp, ok := s.tcpClient.(*mdns.Client)
	require.True(t, ok)
	assert.Equal(t, forwardTimeout, udp.Timeout)
	assert.Equal(t, "udp", udp.Net)
	assert.Equal(t, forwardTimeout, tcp.Timeout)
	assert.Equal(t, "tcp", tcp.Net)
	assert.Equal(t, forwardTimeout, s.forwardBudget)
	assert.NotNil(t, s.forwardLogs)
}

func TestServerListensOnUDPAndTCP(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newServer("127.0.0.1:0", []string{"192.0.2.1"}, nil, log)
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()

	select {
	case <-s.ready:
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("DNS listeners did not start")
	}

	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			client := &mdns.Client{Net: network, Timeout: time.Second}
			msg := new(mdns.Msg)
			msg.SetQuestion("web.app.miren.", mdns.TypeAAAA)
			response, _, err := client.Exchange(msg, s.boundAddr)
			require.NoError(t, err)
			assert.Equal(t, mdns.RcodeSuccess, response.Rcode)
		})
	}

	require.NoError(t, s.Shutdown())
	require.NoError(t, <-errCh)
}

func TestShutdownRequestedBeforeListenStopsBothListeners(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newServer("127.0.0.1:0", []string{"192.0.2.1"}, nil, log)
	require.NoError(t, s.Shutdown())

	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("DNS listeners did not honor the pending shutdown")
	}
	assert.Empty(t, s.boundAddr, "a server stopped before startup must not bind")
}

func TestListenAndServeRejectsSecondCall(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newServer("127.0.0.1:0", []string{"192.0.2.1"}, nil, log)
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()

	select {
	case <-s.ready:
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("DNS listeners did not start")
	}

	assert.EqualError(t, s.ListenAndServe(), "dns server already started")
	require.NoError(t, s.Shutdown())
	require.NoError(t, <-errCh)
}

func TestForwardRetruncatesTCPResponseForUDPClient(t *testing.T) {
	request := query("example.com.")
	truncated := successfulResponse(request)
	truncated.Truncated = true
	tcpResponse := oversizedResponse(request, mdns.MinMsgSize)
	require.Greater(t, tcpResponse.Len(), mdns.MinMsgSize)

	udp := &fakeExchanger{results: []exchangeResult{{msg: truncated}}}
	tcp := &fakeExchanger{results: []exchangeResult{{msg: tcpResponse}}}
	s, logs := testForwardServer(t, []string{"192.0.2.1"}, udp, tcp)
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.Same(t, tcpResponse, w.message)
	assert.True(t, w.message.Truncated)
	assert.LessOrEqual(t, w.message.Len(), mdns.MinMsgSize)
	assert.Equal(t, []string{"192.0.2.1:53"}, udp.calls)
	assert.Equal(t, []string{"192.0.2.1:53"}, tcp.calls)
	assert.Contains(t, logs.String(), "retrying over TCP")
}

func TestForwardKeepsFullTCPResponseForTCPClient(t *testing.T) {
	request := query("example.com.")
	truncated := successfulResponse(request)
	truncated.Truncated = true
	tcpResponse := oversizedResponse(request, mdns.MinMsgSize)
	fullSize := tcpResponse.Len()

	udp := &fakeExchanger{results: []exchangeResult{{msg: truncated}}}
	tcp := &fakeExchanger{results: []exchangeResult{{msg: tcpResponse}}}
	s, _ := testForwardServer(t, []string{"192.0.2.1"}, udp, tcp)
	w := &captureResponseWriter{remote: &net.TCPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.Same(t, tcpResponse, w.message)
	assert.False(t, w.message.Truncated)
	assert.Equal(t, fullSize, w.message.Len())
}

func TestForwardHonorsEDNSUDPSize(t *testing.T) {
	request := query("example.com.")
	request.SetEdns0(1232, false)
	truncated := successfulResponse(request)
	truncated.Truncated = true
	tcpResponse := oversizedResponse(request, 1232)

	udp := &fakeExchanger{results: []exchangeResult{{msg: truncated}}}
	tcp := &fakeExchanger{results: []exchangeResult{{msg: tcpResponse}}}
	s, _ := testForwardServer(t, []string{"192.0.2.1"}, udp, tcp)
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	assert.True(t, w.message.Truncated)
	assert.LessOrEqual(t, w.message.Len(), 1232)
}

func TestForwardTriesNextUpstreamAfterTCPRetryFails(t *testing.T) {
	request := query("example.com.")
	truncated := successfulResponse(request)
	truncated.Truncated = true
	secondResponse := successfulResponse(request)

	udp := &fakeExchanger{results: []exchangeResult{{msg: truncated}, {msg: secondResponse}}}
	tcp := &fakeExchanger{results: []exchangeResult{{err: errors.New("TCP unavailable")}}}
	s, logs := testForwardServer(t, []string{"192.0.2.1", "192.0.2.2"}, udp, tcp)
	w := &captureResponseWriter{remote: &net.TCPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.Same(t, secondResponse, w.message)
	assert.Equal(t, []string{"192.0.2.1:53", "192.0.2.2:53"}, udp.calls)
	assert.Equal(t, []string{"192.0.2.1:53"}, tcp.calls)
	assert.Contains(t, logs.String(), "network=tcp")
	assert.Contains(t, logs.String(), "TCP unavailable")
}

func TestForwardReturnsTruncatedFallbackWhenTCPRetriesFail(t *testing.T) {
	request := query("example.com.")
	truncated := successfulResponse(request)
	truncated.Truncated = true

	udp := &fakeExchanger{results: []exchangeResult{
		{msg: truncated},
		{err: errors.New("second resolver unavailable")},
	}}
	tcp := &fakeExchanger{results: []exchangeResult{{err: errors.New("TCP unavailable")}}}
	s, logs := testForwardServer(t, []string{"192.0.2.1", "192.0.2.2"}, udp, tcp)
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.Same(t, truncated, w.message)
	assert.True(t, w.message.Truncated)
	assert.NotEqual(t, mdns.RcodeServerFailure, w.message.Rcode)
	assert.NotContains(t, logs.String(), "DNS forwarding failed; returning SERVFAIL")
}

func TestForwardTriesNextUpstreamAfterSERVFAIL(t *testing.T) {
	request := query("example.com.")
	servfail := successfulResponse(request)
	servfail.Rcode = mdns.RcodeServerFailure
	success := successfulResponse(request)

	udp := &fakeExchanger{results: []exchangeResult{{msg: servfail}, {msg: success}}}
	s, logs := testForwardServer(t, []string{"192.0.2.1", "192.0.2.2"}, udp, &fakeExchanger{})
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.Same(t, success, w.message)
	assert.Equal(t, []string{"192.0.2.1:53", "192.0.2.2:53"}, udp.calls)
	assert.Contains(t, logs.String(), "DNS upstream returned SERVFAIL")
	assert.Contains(t, logs.String(), "qname=example.com.")
	assert.Contains(t, logs.String(), "client_ip=10.8.95.12")
}

func TestForwardLogsAndReturnsSERVFAILWhenAllUpstreamsFail(t *testing.T) {
	request := query("example.com.")
	udp := &fakeExchanger{results: []exchangeResult{
		{err: errors.New("first resolver timed out")},
		{err: errors.New("second resolver timed out")},
	}}
	s, logs := testForwardServer(t, []string{"192.0.2.1", "192.0.2.2"}, udp, &fakeExchanger{})
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, request)

	require.NotNil(t, w.message)
	assert.Equal(t, mdns.RcodeServerFailure, w.message.Rcode)
	assert.Contains(t, logs.String(), "first resolver timed out")
	assert.Contains(t, logs.String(), "second resolver timed out")
	assert.Contains(t, logs.String(), "DNS forwarding failed; returning SERVFAIL")
	assert.Contains(t, logs.String(), "qtype=A")
	assert.Contains(t, logs.String(), "client_ip=10.8.95.12")
	assert.NotContains(t, logs.String(), "level=ERROR")
}

func TestForwardSlicesOneBudgetAcrossUpstreams(t *testing.T) {
	blocking := &blockingExchanger{}
	s, _ := testForwardServer(t, []string{"192.0.2.1", "192.0.2.2"}, blocking, &fakeExchanger{})
	s.forwardBudget = 100 * time.Millisecond
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	started := time.Now()
	s.forwardToUpstream(w, query("example.com."))

	assert.Less(t, time.Since(started), 500*time.Millisecond)
	assert.Equal(t, 2, blocking.calls, "a blackholed primary must not starve the secondary")
	require.NotNil(t, w.message)
	assert.Equal(t, mdns.RcodeServerFailure, w.message.Rcode)
}

func TestForwardFailureLogsAreDeduplicated(t *testing.T) {
	udp := &fakeExchanger{results: []exchangeResult{
		{err: errors.New("resolver timed out")},
		{err: errors.New("resolver timed out")},
		{err: errors.New("resolver timed out")},
	}}
	s, logs := testForwardServer(t, []string{"192.0.2.1"}, udp, &fakeExchanger{})
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	s.forwardLogs.now = func() time.Time { return now }
	w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP("10.8.95.12"), Port: 53000}}

	s.forwardToUpstream(w, query("first.example."))
	s.forwardToUpstream(w, query("second.example."))
	assert.Equal(t, 1, strings.Count(logs.String(), "DNS upstream query failed"))
	assert.Equal(t, 1, strings.Count(logs.String(), "DNS forwarding failed; returning SERVFAIL"))

	now = now.Add(forwardLogInterval)
	s.forwardToUpstream(w, query("third.example."))
	assert.Equal(t, 2, strings.Count(logs.String(), "DNS upstream query failed"))
	assert.Equal(t, 2, strings.Count(logs.String(), "DNS forwarding failed; returning SERVFAIL"))
	assert.Contains(t, logs.String(), "suppressed=1")
}
