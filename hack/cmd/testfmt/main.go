package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TestEvent mirrors the JSON output from `go test -json`.
type TestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type TestState struct {
	Name    string
	Package string
	Output  []string
	Failed  bool
	Started bool
	Done    bool
}

type PackageState struct {
	Name      string
	Tests     map[string]*TestState
	TestOrder []string
	Output    []string
	Failed    bool
	Elapsed   float64
	Started   bool
	Done      bool
}

type Formatter struct {
	Packages      map[string]*PackageState
	PackageOrder  []string
	FailedTests   []*TestState
	HasFailure    bool
	githubActions bool
	modulePath    string
}

// matches "    foo_test.go:42: some message"
var testLocationRe = regexp.MustCompile(`^\s+(\S+_test\.go):(\d+):`)

func NewFormatter() *Formatter {
	f := &Formatter{
		Packages:      make(map[string]*PackageState),
		githubActions: os.Getenv("GITHUB_ACTIONS") == "true",
	}
	if f.githubActions {
		// Read module path to map packages to repo-relative file paths.
		if data, err := os.ReadFile("go.mod"); err == nil {
			if lines := strings.SplitN(string(data), "\n", 2); len(lines) > 0 {
				f.modulePath = strings.TrimPrefix(lines[0], "module ")
				f.modulePath = strings.TrimSpace(f.modulePath)
			}
		}
	}
	return f
}

// pkgDir converts a full package path to a repo-relative directory.
func (f *Formatter) pkgDir(pkg string) string {
	if f.modulePath != "" && strings.HasPrefix(pkg, f.modulePath) {
		rel := strings.TrimPrefix(pkg, f.modulePath)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return "."
		}
		return rel
	}
	return ""
}

// findTestLocation scans output lines for a _test.go:line reference
// and returns file (repo-relative) and line. Returns empty strings if not found.
func (f *Formatter) findTestLocation(pkgName string, output []string) (string, string) {
	dir := f.pkgDir(pkgName)
	if dir == "" {
		return "", ""
	}
	for _, line := range output {
		if m := testLocationRe.FindStringSubmatch(line); m != nil {
			file := dir + "/" + m[1]
			return file, m[2]
		}
	}
	return "", ""
}

func (f *Formatter) getPackage(name string) *PackageState {
	pkg, ok := f.Packages[name]
	if !ok {
		pkg = &PackageState{
			Name:  name,
			Tests: make(map[string]*TestState),
		}
		f.Packages[name] = pkg
		f.PackageOrder = append(f.PackageOrder, name)
	}
	return pkg
}

func (f *Formatter) getTest(pkg *PackageState, name string) *TestState {
	ts, ok := pkg.Tests[name]
	if !ok {
		ts = &TestState{
			Name:    name,
			Package: pkg.Name,
		}
		pkg.Tests[name] = ts
		pkg.TestOrder = append(pkg.TestOrder, name)
	}
	return ts
}

func (f *Formatter) ProcessLine(line string) {
	var ev TestEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		// Non-JSON lines (iso setup noise) go to stderr
		fmt.Fprintln(os.Stderr, line)
		return
	}

	if ev.Package == "" {
		return
	}

	pkg := f.getPackage(ev.Package)

	if ev.Test == "" {
		f.processPackageEvent(pkg, &ev)
	} else {
		f.processTestEvent(pkg, &ev)
	}
}

func (f *Formatter) processPackageEvent(pkg *PackageState, ev *TestEvent) {
	if !pkg.Started {
		pkg.Started = true
	}

	switch ev.Action {
	case "output":
		pkg.Output = append(pkg.Output, ev.Output)
	case "pass":
		pkg.Elapsed = ev.Elapsed
		pkg.Done = true
		fmt.Printf("=== PASS %s (%.1fs)\n", pkg.Name, ev.Elapsed)
	case "fail":
		pkg.Elapsed = ev.Elapsed
		pkg.Failed = true
		pkg.Done = true
		f.HasFailure = true
		fmt.Printf("=== FAIL %s (%.1fs)\n", pkg.Name, ev.Elapsed)
	case "skip":
		pkg.Elapsed = ev.Elapsed
		pkg.Done = true
		fmt.Printf("=== SKIP %s (%.1fs)\n", pkg.Name, ev.Elapsed)
	}
}

func (f *Formatter) processTestEvent(pkg *PackageState, ev *TestEvent) {
	if !pkg.Started {
		pkg.Started = true
	}

	ts := f.getTest(pkg, ev.Test)
	isTopLevel := !strings.Contains(ev.Test, "/")

	switch ev.Action {
	case "run":
		ts.Started = true
		if isTopLevel {
			fmt.Printf("    --- START  %s  %s\n", pkg.Name, ev.Test)
		}
	case "output":
		ts.Output = append(ts.Output, ev.Output)
	case "pass":
		ts.Done = true
		if isTopLevel {
			fmt.Printf("    --- PASS   %s  %s (%.1fs)\n", pkg.Name, ev.Test, ev.Elapsed)
		}
	case "fail":
		ts.Failed = true
		ts.Done = true
		f.HasFailure = true
		f.FailedTests = append(f.FailedTests, ts)
		if isTopLevel {
			fmt.Printf("    --- FAIL   %s  %s (%.1fs)\n", pkg.Name, ev.Test, ev.Elapsed)
		}
	case "skip":
		ts.Done = true
		if isTopLevel {
			fmt.Printf("    --- SKIP   %s  %s (%.1fs)\n", pkg.Name, ev.Test, ev.Elapsed)
		}
	}
}

func (f *Formatter) PrintSummary() {
	// Detect crashed packages (started but never got a terminal event)
	var crashed []string
	for _, name := range f.PackageOrder {
		pkg := f.Packages[name]
		if pkg.Started && !pkg.Done {
			crashed = append(crashed, name)
			f.HasFailure = true
		}
	}

	if len(crashed) > 0 {
		fmt.Println()
		fmt.Println("=== CRASHED PACKAGES (no result received) ===")
		for _, name := range crashed {
			fmt.Printf("    %s\n", name)
			if f.githubActions {
				fmt.Printf("::error title=CRASH %s::Package did not produce a result (possible panic or timeout)\n", name)
			}
		}
	}

	// Failed test output replay
	if len(f.FailedTests) > 0 {
		fmt.Println()
		if f.githubActions {
			fmt.Println("::group::Failed test output")
		}
		fmt.Println("=== FAILED TEST OUTPUT ===")
		for _, ts := range f.FailedTests {
			fmt.Printf("\n--- FAIL %s (in %s)\n", ts.Name, ts.Package)
			for _, line := range ts.Output {
				fmt.Print("    ", line)
			}
		}
		if f.githubActions {
			fmt.Println("::endgroup::")
			// Emit ::error annotations after the group so they appear as PR annotations.
			for _, ts := range f.FailedTests {
				file, line := f.findTestLocation(ts.Package, ts.Output)
				if file != "" {
					fmt.Printf("::error file=%s,line=%s,title=FAIL %s::%s\n", file, line, ts.Name, ts.Package)
				} else {
					fmt.Printf("::error title=FAIL %s::%s\n", ts.Name, ts.Package)
				}
			}
		}
	}

	// Tests that started but never produced a terminal event. That is the
	// signature of a panic, an os.Exit, or the process being killed: go test
	// emits no pass/fail for the test, so it never lands in FailedTests and its
	// buffered output would otherwise be discarded — precisely the case where
	// that output is the only evidence of what went wrong.
	var incomplete []*TestState
	for _, name := range f.PackageOrder {
		pkg := f.Packages[name]
		for _, testName := range pkg.TestOrder {
			if ts := pkg.Tests[testName]; ts.Started && !ts.Done {
				incomplete = append(incomplete, ts)
			}
		}
	}
	if len(incomplete) > 0 {
		fmt.Println()
		if f.githubActions {
			fmt.Println("::group::Incomplete test output")
		}
		fmt.Println("=== INCOMPLETE TEST OUTPUT (no pass/fail received) ===")
		for _, ts := range incomplete {
			fmt.Printf("\n--- INCOMPLETE %s (in %s)\n", ts.Name, ts.Package)
			for _, line := range ts.Output {
				fmt.Print("    ", line)
			}
		}
		if f.githubActions {
			fmt.Println("::endgroup::")
			// Annotate after the group so these surface as PR annotations. The
			// package result still owns the exit code; a package that passed
			// with an incomplete test is odd but not itself a failure.
			for _, ts := range incomplete {
				level := "warning"
				if pkg := f.Packages[ts.Package]; pkg != nil && pkg.Failed {
					level = "error"
				}
				const msg = "test did not report a result (possible panic or crash)"
				file, line := f.findTestLocation(ts.Package, ts.Output)
				if file != "" {
					fmt.Printf("::%s file=%s,line=%s,title=INCOMPLETE %s::%s\n", level, file, line, ts.Name, msg)
				} else {
					fmt.Printf("::%s title=INCOMPLETE %s::%s\n", level, ts.Name, msg)
				}
			}
		}
	}

	// Failed package output (build errors, etc.)
	var failedPkgs []*PackageState
	for _, name := range f.PackageOrder {
		pkg := f.Packages[name]
		if pkg.Failed && len(pkg.Output) > 0 {
			failedPkgs = append(failedPkgs, pkg)
		}
	}
	if len(failedPkgs) > 0 {
		fmt.Println()
		if f.githubActions {
			fmt.Println("::group::Failed package output")
		}
		fmt.Println("=== FAILED PACKAGE OUTPUT ===")
		for _, pkg := range failedPkgs {
			fmt.Printf("\n--- FAIL %s\n", pkg.Name)
			for _, line := range pkg.Output {
				fmt.Print("    ", line)
			}
		}
		if f.githubActions {
			fmt.Println("::endgroup::")
			for _, pkg := range failedPkgs {
				fmt.Printf("::error title=FAIL %s::Package failed (build error or test failure)\n", pkg.Name)
			}
		}
	}

	// Package timing table
	type pkgTiming struct {
		name    string
		elapsed float64
		status  string
	}

	var timings []pkgTiming
	var total float64

	for _, name := range f.PackageOrder {
		pkg := f.Packages[name]
		status := "PASS"
		if pkg.Failed {
			status = "FAIL"
		} else if !pkg.Done {
			status = "CRASH"
		}
		timings = append(timings, pkgTiming{
			name:    name,
			elapsed: pkg.Elapsed,
			status:  status,
		})
		total += pkg.Elapsed
	}

	sort.Slice(timings, func(i, j int) bool {
		return timings[i].elapsed > timings[j].elapsed
	})

	fmt.Println()
	if f.githubActions {
		fmt.Printf("::group::Package timing (%.1fs total)\n", total)
	} else {
		fmt.Println("=== PACKAGE TIMING ===")
	}
	for _, t := range timings {
		fmt.Printf("    %-6s %6.1fs  %s\n", t.status, t.elapsed, t.name)
	}
	fmt.Printf("    %-6s %6.1fs  %s\n", "TOTAL", total, "(all packages)")
	if f.githubActions {
		fmt.Println("::endgroup::")
	}
}

func main() {
	args := os.Args[1:]

	// Separate KEY=VALUE env args from test args.
	var envArgs, testArgs []string
	for _, arg := range args {
		if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
			envArgs = append(envArgs, arg)
		} else {
			testArgs = append(testArgs, arg)
		}
	}

	isoArgs := []string{"run", "VERBOSE=", "TESTFMT_JSON=1"}
	isoArgs = append(isoArgs, envArgs...)
	isoArgs = append(isoArgs, "bash", "hack/test.sh")
	isoArgs = append(isoArgs, testArgs...)

	cmd := exec.Command("iso", isoArgs...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testfmt: failed to create stdout pipe: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "testfmt: failed to start iso: %v\n", err)
		os.Exit(1)
	}

	formatter := NewFormatter()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		formatter.ProcessLine(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "testfmt: scanner error: %v\n", err)
	}

	cmdErr := cmd.Wait()

	formatter.PrintSummary()

	if formatter.HasFailure || cmdErr != nil {
		os.Exit(1)
	}
}
