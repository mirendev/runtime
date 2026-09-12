package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"

	"miren.dev/runtime/pkg/progress/upload"
)

func TestBuildStats(t *testing.T) {
	now := time.Now()
	vertex := func(name string, done, cached bool) *client.Vertex {
		v := &client.Vertex{Digest: digest.FromString(name), Name: name, Cached: cached}
		if done {
			v.Completed = &now
		}
		return v
	}

	var s buildStats

	p, changed := s.observe(&client.SolveStatus{Vertexes: []*client.Vertex{
		vertex("a", false, false),
		vertex("b", true, true),
	}})
	if !changed {
		t.Fatal("first update must report a change")
	}
	if p != (buildProgress{total: 2, completed: 1, cached: 1}) {
		t.Fatalf("progress after first update = %+v", p)
	}

	// Repeating the same state is not a change.
	if _, changed := s.observe(&client.SolveStatus{Vertexes: []*client.Vertex{vertex("b", true, true)}}); changed {
		t.Fatal("unchanged vertex must not report a change")
	}

	p, changed = s.observe(&client.SolveStatus{Vertexes: []*client.Vertex{vertex("a", true, false)}})
	if !changed {
		t.Fatal("completing a vertex must report a change")
	}
	if p != (buildProgress{total: 2, completed: 2, cached: 1}) {
		t.Fatalf("progress after completion = %+v", p)
	}

	snap, elapsed := s.snapshot()
	if snap != p {
		t.Fatalf("snapshot = %+v, want %+v", snap, p)
	}
	if elapsed <= 0 {
		t.Fatalf("elapsed = %s, want > 0 once a status was seen", elapsed)
	}

	var empty buildStats
	if snap, elapsed := empty.snapshot(); snap.total != 0 || elapsed != 0 {
		t.Fatalf("empty stats snapshot = %+v, %s; want zero", snap, elapsed)
	}
}

func TestPlainProgress_NonInteractiveNeverWritesEscapes(t *testing.T) {
	var buf bytes.Buffer
	p := newPlainProgress(&buf) // a bytes.Buffer is never a terminal

	p.update("Uploading artifacts: 10%")
	p.update("Uploading artifacts: 50%")
	p.finish("Upload complete")

	got := buf.String()
	if strings.Contains(got, "\033") || strings.Contains(got, "\r") {
		t.Fatalf("non-interactive output must not contain escape codes: %q", got)
	}
	want := "Uploading artifacts: 10%\nUploading artifacts: 50%\nUpload complete\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if p.interval() != 2*time.Second {
		t.Fatalf("non-interactive interval = %s, want 2s", p.interval())
	}
}

func TestPlainProgress_InteractiveRewritesInPlace(t *testing.T) {
	var buf bytes.Buffer
	p := &plainProgress{out: &buf, interactive: true}

	p.update("10%")
	p.update("50%")
	p.finish("done")

	want := "\r\033[K10%\r\033[K50%\r\033[Kdone\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if p.interval() != 500*time.Millisecond {
		t.Fatalf("interactive interval = %s, want 500ms", p.interval())
	}

	// finish with nothing live must not emit a stray clear sequence.
	buf.Reset()
	p.finish("final")
	if got := buf.String(); got != "final\n" {
		t.Fatalf("finish with no live line = %q, want %q", got, "final\n")
	}
}

func TestUploadProgressLine(t *testing.T) {
	line := uploadProgressLine(upload.Progress{
		BytesRead:      2048,
		BytesPerSecond: 1024,
		Fraction:       0.5,
	})
	if line != "Uploading artifacts: 50% — 2.0 KB at 1.0 KB/s" {
		t.Fatalf("line = %q", line)
	}

	// An unknown fraction (no manifest) must not be reported as 0%.
	unknown := uploadProgressLine(upload.Progress{BytesRead: 2048, BytesPerSecond: 1024})
	if unknown != "Uploading artifacts: 2.0 KB at 1.0 KB/s" {
		t.Fatalf("line with unknown fraction = %q", unknown)
	}

	withETA := uploadProgressLine(upload.Progress{
		BytesRead:      2048,
		BytesPerSecond: 1024,
		Fraction:       0.5,
		ETA:            3 * time.Second,
	})
	if !strings.HasSuffix(withETA, " (eta ~3s)") {
		t.Fatalf("line with ETA = %q, want eta suffix", withETA)
	}
}

func TestExplainProgressMode(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	withEnv := func(k string) (string, bool) {
		if k == "BUILDKIT_PROGRESS" {
			return "tty", true
		}
		return "", false
	}

	cases := []struct {
		name      string
		requested string
		tty       bool
		env       func(string) (string, bool)
		want      string
	}{
		{"auto on a terminal stays auto", "auto", true, noEnv, "auto"},
		{"auto when piped becomes plain", "auto", false, noEnv, "plain"},
		{"explicit format is respected when piped", "tty", false, noEnv, "tty"},
		{"BUILDKIT_PROGRESS keeps auto so buildkit can honor it", "auto", false, withEnv, "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := explainProgressMode(tc.requested, tc.tty, tc.env); got != tc.want {
				t.Fatalf("explainProgressMode(%q, %v) = %q, want %q", tc.requested, tc.tty, got, tc.want)
			}
		})
	}
}
