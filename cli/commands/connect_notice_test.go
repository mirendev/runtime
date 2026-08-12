package commands

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"miren.dev/runtime/clientconfig"
)

func clusterContext(name, addr string) *Context {
	c := &Context{ClusterName: name}
	if addr != "" {
		c.ClusterConfig = &clientconfig.ClusterConfig{Hostname: addr}
	}
	return c
}

// A writer that's safe to read while the notice goroutine is writing to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestNotice(out *syncBuffer, interactive bool, delay time.Duration) *connectNotice {
	return &connectNotice{
		out:         out,
		interactive: interactive,
		target:      `cluster "local" (localhost:8443)`,
		delay:       delay,
		interval:    5 * time.Millisecond,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// waitForOutput blocks until the notice has written something, so tests
// synchronize on the actual event rather than guessing how long a goroutine
// needs. A fixed sleep here is a test that passes on an idle machine and fails
// on a loaded CI runner.
func waitForOutput(t *testing.T, out *syncBuffer) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := out.String(); s != "" {
			return s
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatal("notice never wrote anything")
	return ""
}

// The common case is a fast connection, and it has to stay completely silent.
// A spinner that flashes on every command is worse than no spinner.
func TestConnectNoticeSilentWhenFast(t *testing.T) {
	var out syncBuffer
	// A delay no test run can outlast, so "the connection finished first" is a
	// certainty rather than a race we hope to win.
	n := newTestNotice(&out, true, time.Hour)
	go n.run(time.Now())

	n.Stop()

	if out.String() != "" {
		t.Fatalf("fast connection produced output: %q", out.String())
	}
}

func TestConnectNoticeAppearsWhenSlow(t *testing.T) {
	var out syncBuffer
	n := newTestNotice(&out, true, time.Millisecond)
	go n.run(time.Now())

	got := waitForOutput(t, &out)
	n.Stop()
	if !strings.Contains(got, "connecting to") {
		t.Errorf("slow connection did not announce itself: %q", got)
	}
	if !strings.Contains(got, "localhost:8443") {
		t.Errorf("notice does not name the target: %q", got)
	}
}

// Stop must erase the line, so whatever the command prints next starts clean.
func TestConnectNoticeErasesItself(t *testing.T) {
	var out syncBuffer
	n := newTestNotice(&out, true, time.Millisecond)
	go n.run(time.Now())

	waitForOutput(t, &out)
	n.Stop()

	if !strings.HasSuffix(out.String(), "\r\033[K") {
		t.Errorf("notice did not clear the line on stop: %q", out.String())
	}
}

// Without a terminal nobody is watching for a hang, and anything written here
// is debris in whatever captured the stream — `--format json` output with
// stderr merged in, most painfully.
func TestConnectNoticeSilentWhenNotInteractive(t *testing.T) {
	var out syncBuffer
	n := newTestNotice(&out, false, time.Millisecond)
	go n.run(time.Now())

	// Long enough that an interactive notice would be several frames in, so
	// silence here means suppressed rather than merely not started yet.
	time.Sleep(50 * time.Millisecond)
	n.Stop()

	if out.String() != "" {
		t.Errorf("non-interactive notice wrote output: %q", out.String())
	}
}

func TestConnectNoticeStopIsIdempotent(t *testing.T) {
	var out syncBuffer
	n := newTestNotice(&out, true, time.Millisecond)
	go n.run(time.Now())

	n.Stop()
	n.Stop() // must not panic on a second close
}

func TestConnectNoticeLabelEscalates(t *testing.T) {
	target := `cluster "local"`

	early := connectNoticeLabel(target, time.Second, true)
	if !strings.Contains(early, "connecting to") {
		t.Errorf("early label = %q, want 'connecting to'", early)
	}

	late := connectNoticeLabel(target, connectNoticeEscalate+time.Second, true)
	if !strings.Contains(late, "still waiting on") {
		t.Errorf("late label = %q, want 'still waiting on'", late)
	}
	if strings.Contains(late, "connecting to") {
		t.Errorf("late label still says 'connecting to': %q", late)
	}
}

func TestConnectNoticeLabelShowsElapsed(t *testing.T) {
	label := connectNoticeLabel(`cluster "local"`, 4*time.Second, true)
	if !strings.Contains(label, "4s") {
		t.Errorf("label = %q, want it to report elapsed seconds", label)
	}
}

func TestConnectTarget(t *testing.T) {
	tests := []struct {
		name string
		ctx  *Context
		want string
	}{
		{
			name: "cluster name and address",
			ctx:  clusterContext("local", "localhost:8443"),
			want: `cluster "local" (localhost:8443)`,
		},
		{
			name: "address only",
			ctx:  clusterContext("", "localhost:8443"),
			want: "localhost:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.connectTarget(); got != tt.want {
				t.Errorf("connectTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}
