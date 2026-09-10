package commands

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/moby/buildkit/client"

	"miren.dev/runtime/pkg/progress/upload"
)

// buildStats accumulates BuildKit vertex state across status updates so the
// deploy can summarize the build as counts ("5 steps, 3 cached") instead of
// replaying every step. The build status callback writes from RPC goroutines
// while the deploy command reads, hence the mutex.
type buildStats struct {
	mu       sync.Mutex
	vertices map[string]vertexState // digest → state
	started  time.Time              // first BuildKit status seen
}

type vertexState struct {
	done   bool
	cached bool
}

// observe folds one status update in. changed reports whether any vertex was
// added or completed, so callers can skip redundant UI updates.
func (s *buildStats) observe(status *client.SolveStatus) (progress buildProgress, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.IsZero() {
		s.started = time.Now()
	}
	if s.vertices == nil {
		s.vertices = map[string]vertexState{}
	}
	for _, v := range status.Vertexes {
		d := v.Digest.String()
		prev, seen := s.vertices[d]
		next := vertexState{done: v.Completed != nil, cached: v.Cached}
		if !seen || next != prev {
			changed = true
		}
		s.vertices[d] = next
	}
	return s.progressLocked(), changed
}

// snapshot returns the current counts and how long the build has been running.
func (s *buildStats) snapshot() (progress buildProgress, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started.IsZero() {
		elapsed = time.Since(s.started)
	}
	return s.progressLocked(), elapsed
}

func (s *buildStats) progressLocked() buildProgress {
	p := buildProgress{total: len(s.vertices)}
	for _, v := range s.vertices {
		if v.done {
			p.completed++
		}
		if v.cached {
			p.cached++
		}
	}
	return p
}

// plainProgress reports upload progress on the explain-mode path (the one used
// whenever stdout is not a terminal). On an interactive writer it rewrites a
// single line in place, the way a progress meter usually does. On a
// non-interactive writer (CI logs, a file, a pipe) every report is its own
// newline-terminated line and no cursor-control escapes are ever written, so the
// captured log stays readable and greppable.
type plainProgress struct {
	out         io.Writer
	interactive bool
	live        bool // an in-place line is currently on screen
}

func newPlainProgress(out io.Writer) *plainProgress {
	return &plainProgress{out: out, interactive: isInteractiveOutput(out)}
}

// interval is how often update should be called: fast enough to feel live on a
// terminal, slow enough not to flood a log file.
func (p *plainProgress) interval() time.Duration {
	if p.interactive {
		return 500 * time.Millisecond
	}
	return 2 * time.Second
}

// update shows an in-progress line. On a terminal it replaces the previous
// one; otherwise it appends.
func (p *plainProgress) update(line string) {
	if !p.interactive {
		fmt.Fprintln(p.out, line)
		return
	}
	fmt.Fprintf(p.out, "\r\033[K%s", line)
	p.live = true
}

// finish clears any in-place line and prints a final, newline-terminated one.
func (p *plainProgress) finish(line string) {
	p.clear()
	fmt.Fprintln(p.out, line)
}

// clear erases the live line on a terminal. It is a no-op elsewhere, so the
// only escape sequence this type emits never reaches a non-terminal writer.
func (p *plainProgress) clear() {
	if p.interactive && p.live {
		fmt.Fprint(p.out, "\r\033[K")
		p.live = false
	}
}

// uploadProgressLine formats one upload progress report.
func uploadProgressLine(progress upload.Progress) string {
	// The fraction is unknown (zero) when the file manifest could not be
	// computed; claiming "0%" then would be wrong, so only bytes and speed show.
	line := "Uploading artifacts: "
	if progress.Fraction > 0 {
		line += fmt.Sprintf("%d%% — ", int(progress.Fraction*100))
	}
	line += fmt.Sprintf("%s at %s",
		upload.FormatBytes(progress.BytesRead),
		upload.FormatSpeed(progress.BytesPerSecond))
	if progress.ETA > 0 {
		line += fmt.Sprintf(" (eta ~%s)", upload.FormatDuration(progress.ETA))
	}
	return line
}

// explainProgressMode picks the BuildKit progress renderer for explain mode.
// BuildKit's "auto" chooses its TTY renderer whenever the writer (stderr) is a
// console, so `miren deploy | tee log` would still get cursor movement on a
// terminal stderr. When stdout is not a terminal we therefore fall back to
// "plain" unless the user chose a format explicitly or set BUILDKIT_PROGRESS,
// which BuildKit honors itself for "auto".
func explainProgressMode(requested string, stdoutIsTTY bool, lookupEnv func(string) (string, bool)) string {
	if requested != "auto" || stdoutIsTTY {
		return requested
	}
	if v, ok := lookupEnv("BUILDKIT_PROGRESS"); ok && v != "" {
		return requested
	}
	return "plain"
}
