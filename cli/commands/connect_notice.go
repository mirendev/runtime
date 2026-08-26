package commands

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/theme"
)

const (
	// connectNoticeDelay is how long a connection may take before we say
	// anything. The overwhelming majority of connections finish well inside
	// this, and those stay completely silent — a spinner that flashes on every
	// command is worse than no spinner at all.
	connectNoticeDelay = 700 * time.Millisecond

	// connectNoticeEscalate is when "connecting to" becomes "still waiting on".
	// Past this point the wait is no longer routine and the wording should stop
	// implying that it is.
	connectNoticeEscalate = 5 * time.Second

	connectNoticeInterval = 100 * time.Millisecond
)

// connectNotice animates a single line while an RPC connection is being
// established, and erases it completely when the connection resolves.
//
// It writes to stderr, never stdout: this runs before every RPC command, and a
// progress line in stdout would corrupt `--format json` output the moment
// anyone pipes it. Stderr alone isn't enough, though — plenty of callers merge
// the two streams — so a stderr that isn't a terminal produces no notice at
// all. The only thing this line does is reassure someone watching a terminal
// that we haven't hung, and nobody is watching a pipe.
//
// It also deliberately avoids a full terminal UI. Raw mode on a code path this
// hot means a Ctrl-C during a slow connect can leave the user's terminal
// broken, and rewriting one line with \r cannot do that.
type connectNotice struct {
	out         io.Writer
	interactive bool
	target      string
	delay       time.Duration
	interval    time.Duration

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// startConnectNotice begins watching for a slow connection. The returned
// notice must be stopped, which is why callers use it as
// `defer c.startConnectNotice().Stop()`.
func (c *Context) startConnectNotice() *connectNotice {
	n := &connectNotice{
		out:         c.Stderr,
		interactive: isInteractiveOutput(c.Stderr),
		target:      c.connectTarget(),
		delay:       connectNoticeDelay,
		interval:    connectNoticeInterval,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	go n.run(time.Now())
	return n
}

// Stop ends the notice and erases the line. It blocks until the display has
// been cleaned up, so nothing the caller prints next can race with it.
func (n *connectNotice) Stop() {
	n.stopOnce.Do(func() { close(n.stop) })
	<-n.done
}

func (n *connectNotice) run(start time.Time) {
	defer close(n.done)

	// Without a terminal there's no one to reassure, and the line would just be
	// debris in whatever captured the stream.
	if !n.interactive {
		return
	}

	// Stay quiet unless the connection is actually slow.
	select {
	case <-n.stop:
		return
	case <-time.After(n.delay):
	}

	w := &waitingLine{out: n.out, interactive: n.interactive}

	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()

	for {
		w.label = connectNoticeLabel(n.target, time.Since(start), n.interactive)
		w.tick()

		select {
		case <-n.stop:
			w.clear()
			return
		case <-ticker.C:
		}
	}
}

// connectNoticeLabel describes the wait, escalating its wording as it drags on
// so a long wait reports more than a short one. withAge adds a running clock,
// which only makes sense on a line we can rewrite.
func connectNoticeLabel(target string, elapsed time.Duration, withAge bool) string {
	verb := "connecting to"
	if elapsed >= connectNoticeEscalate {
		verb = "still waiting on"
	}

	label := verb
	if target != "" {
		label += " " + target
	}

	if !withAge {
		return label
	}

	// Rounded, not truncated: the first frame lands just after the delay, and
	// a spinner whose clock reads "0s" looks broken rather than precise.
	age := lipgloss.NewStyle().Foreground(theme.Muted).
		Render(fmt.Sprintf("%ds", int(elapsed.Round(time.Second).Seconds())))
	return label + "  " + age
}

// connectTarget names what we're connecting to, preferring the cluster's name
// and falling back through whatever identifying detail is available.
func (c *Context) connectTarget() string {
	addr := ""
	if c.ClusterConfig != nil {
		// A cloud-routed cluster's hostname is not what we dial, and is often
		// unset or stale. Naming the route is both true and the more useful
		// thing to read while a connection is taking a while.
		if c.ClusterConfig.ViaCloud {
			addr = "via miren cloud"
		} else {
			addr = c.ClusterConfig.Hostname
		}
	}
	if addr == "" {
		addr = c.Config.ServerAddress
	}

	switch {
	case c.ClusterName != "" && addr != "":
		return fmt.Sprintf("cluster %q (%s)", c.ClusterName, addr)
	case c.ClusterName != "":
		return fmt.Sprintf("cluster %q", c.ClusterName)
	default:
		return addr
	}
}
