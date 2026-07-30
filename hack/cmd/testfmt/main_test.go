package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runEvents feeds events through a Formatter and returns everything it printed.
//
// opts run against the Formatter the events are actually fed through. Anything
// a test needs configured has to be set here rather than on a formatter of its
// own, or the assertion ends up describing a formatter that never saw an event.
func runEvents(t *testing.T, githubActions bool, events []TestEvent, opts ...func(*Formatter)) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()

	f := &Formatter{
		Packages:      make(map[string]*PackageState),
		githubActions: githubActions,
	}
	for _, opt := range opts {
		opt(f)
	}
	for _, ev := range events {
		line, err := json.Marshal(ev)
		require.NoError(t, err)
		f.ProcessLine(string(line))
	}
	f.PrintSummary()

	require.NoError(t, w.Close())
	return <-done
}

// A test that dies without a terminal event — a panic, an os.Exit, or the
// process being killed — produces no pass/fail action, so it never lands in
// FailedTests. Its buffered output is the only evidence of what happened and
// must still be replayed.
func TestIncompleteTestOutputIsReplayed(t *testing.T) {
	const pkg = "miren.dev/runtime/pkg/addon/mysql"

	out := runEvents(t, false, []TestEvent{
		{Action: "run", Package: pkg, Test: "TestMySQL_Integration"},
		{Action: "output", Package: pkg, Test: "TestMySQL_Integration",
			Output: "    rotate_integration_test.go:113: provisioning shared\n"},
		{Action: "output", Package: pkg, Test: "TestMySQL_Integration",
			Output: "panic: runtime error: invalid memory address\n"},
		{Action: "output", Package: pkg, Output: "FAIL\t" + pkg + "\t51.462s\n"},
		{Action: "fail", Package: pkg, Elapsed: 51.462},
	})

	assert.Contains(t, out, "INCOMPLETE TEST OUTPUT")
	assert.Contains(t, out, "--- INCOMPLETE TestMySQL_Integration")
	assert.Contains(t, out, "panic: runtime error: invalid memory address",
		"the crash output is the whole point; it must survive to the summary")
	assert.Contains(t, out, "rotate_integration_test.go:113")
}

// The GitHub annotation should point at the test's source location and be an
// error (not a warning) when the package itself failed.
func TestIncompleteTestAnnotation(t *testing.T) {
	const pkg = "miren.dev/runtime/pkg/addon/mysql"

	// modulePath has to be set on the formatter the events go through. Without
	// it pkgDir yields "", findTestLocation gives up, and the annotation falls
	// back to its no-location form, so the file=/line= branch this test exists
	// to cover would never run.
	out := runEvents(t, true, []TestEvent{
		{Action: "run", Package: pkg, Test: "TestMySQL_Integration"},
		{Action: "output", Package: pkg, Test: "TestMySQL_Integration",
			Output: "    rotate_integration_test.go:113: boom\n"},
		{Action: "fail", Package: pkg, Elapsed: 51.462},
	}, func(f *Formatter) { f.modulePath = "miren.dev/runtime" })

	assert.Contains(t, out, "::group::Incomplete test output")
	assert.Contains(t, out, "::error file=pkg/addon/mysql/rotate_integration_test.go,line=113,")
	assert.Contains(t, out, "title=INCOMPLETE TestMySQL_Integration")
	assert.NotContains(t, out, "::warning", "package failed, so this is an error")
}

// A package that passes cleanly must not grow a spurious incomplete section.
func TestNoIncompleteSectionOnCleanRun(t *testing.T) {
	const pkg = "miren.dev/runtime/pkg/shellwords"

	out := runEvents(t, false, []TestEvent{
		{Action: "run", Package: pkg, Test: "TestSplitPosix"},
		{Action: "pass", Package: pkg, Test: "TestSplitPosix", Elapsed: 0.01},
		{Action: "run", Package: pkg, Test: "TestQuotePosix"},
		{Action: "skip", Package: pkg, Test: "TestQuotePosix"},
		{Action: "pass", Package: pkg, Elapsed: 0.02},
	})

	assert.NotContains(t, out, "INCOMPLETE")
}

// An ordinary failing test keeps using the existing FailedTests path and is not
// double-reported as incomplete.
func TestFailedTestNotReportedAsIncomplete(t *testing.T) {
	const pkg = "miren.dev/runtime/pkg/tasks"

	out := runEvents(t, false, []TestEvent{
		{Action: "run", Package: pkg, Test: "TestParseFile"},
		{Action: "output", Package: pkg, Test: "TestParseFile", Output: "    tasks_test.go:12: mismatch\n"},
		{Action: "fail", Package: pkg, Test: "TestParseFile", Elapsed: 0.01},
		{Action: "fail", Package: pkg, Elapsed: 0.02},
	})

	assert.Contains(t, out, "FAILED TEST OUTPUT")
	assert.Contains(t, out, "mismatch")
	assert.NotContains(t, out, "INCOMPLETE")
}

// Subtests are tracked independently, so a parent that dies mid-subtest surfaces
// both buffers — the panic text is often attributed to the subtest.
func TestIncompleteSubtestOutputIsReplayed(t *testing.T) {
	const pkg = "miren.dev/runtime/pkg/addon/mysql"

	out := runEvents(t, false, []TestEvent{
		{Action: "run", Package: pkg, Test: "TestMySQL_Integration"},
		{Action: "run", Package: pkg, Test: "TestMySQL_Integration/RotateSharedRoot"},
		{Action: "output", Package: pkg, Test: "TestMySQL_Integration/RotateSharedRoot",
			Output: "panic: send on closed channel\n"},
		{Action: "fail", Package: pkg, Elapsed: 51.4},
	})

	assert.Contains(t, out, "--- INCOMPLETE TestMySQL_Integration ")
	assert.Contains(t, out, "--- INCOMPLETE TestMySQL_Integration/RotateSharedRoot")
	assert.Contains(t, out, "panic: send on closed channel")

	// Reported in run order: parent before subtest.
	assert.Less(t,
		strings.Index(out, "--- INCOMPLETE TestMySQL_Integration "),
		strings.Index(out, "--- INCOMPLETE TestMySQL_Integration/RotateSharedRoot"))
}
