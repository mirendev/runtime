package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/rpc/standard"
)

const (
	rpcAppStatus = "dev.miren.runtime/app-status"
	rpcLogs      = "dev.miren.runtime/logs"
)

// appInfoGetter returns the application status for an app. The real
// implementation adapts app_v1alpha.AppStatusClient (see appStatusAdapter);
// returning *ApplicationStatus directly keeps the poller testable without the
// unexported RPC result wrapper.
type appInfoGetter interface {
	AppInfo(ctx context.Context, application string) (*app_v1alpha.ApplicationStatus, error)
}

// appStatusAdapter adapts the generated AppStatusClient to appInfoGetter by
// unwrapping its result to the bare ApplicationStatus.
type appStatusAdapter struct {
	client *app_v1alpha.AppStatusClient
}

func (a appStatusAdapter) AppInfo(ctx context.Context, application string) (*app_v1alpha.ApplicationStatus, error) {
	res, err := a.client.AppInfo(ctx, application)
	if err != nil {
		return nil, err
	}
	return res.Status(), nil
}

// logTailer fetches recent application log lines. The real implementation
// adapts app_v1alpha.LogsClient (see logsAdapter); it's an interface so the
// failure path can be tested without a server.
type logTailer interface {
	RecentLogs(ctx context.Context, application string) ([]*app_v1alpha.LogEntry, error)
}

// recentLogWindow bounds how far back the failure-path log tail looks.
const recentLogWindow = 5 * time.Minute

// logsAdapter adapts the generated LogsClient to logTailer, bounding the query
// to a recent window and unwrapping the result to bare log entries.
type logsAdapter struct {
	client *app_v1alpha.LogsClient
}

func (l logsAdapter) RecentLogs(ctx context.Context, application string) ([]*app_v1alpha.LogEntry, error) {
	from := standard.ToTimestamp(time.Now().Add(-recentLogWindow))
	res, err := l.client.AppLogs(ctx, application, from, false)
	if err != nil {
		return nil, err
	}
	return res.Logs(), nil
}

// healthSnapshot is the readiness-relevant slice of ApplicationStatus,
// normalized for the target version. versionActive is false until the active
// version pointer matches the version we deployed.
type healthSnapshot struct {
	versionActive   bool
	health          string
	ready           int32
	desired         int32
	crashCount      int64
	cooldownSeconds int32
	boundPorts      []*app_v1alpha.BoundPort
}

type activationDecision int

const (
	// decisionWait: not a terminal state yet — keep polling.
	decisionWait activationDecision = iota
	// decisionHealthy: at least one instance of the target version is serving.
	decisionHealthy
	// decisionScaledToZero: the app is deliberately scaled to zero; there's no
	// instance to wait for and that's fine.
	decisionScaledToZero
	// decisionCrashed: a pool is in crash cooldown — the version came up and died.
	decisionCrashed
	// decisionTaskOnly: the app has no long-running process by design. It is
	// deployed and invokable, which is as "up" as it gets.
	decisionTaskOnly
)

// decideActivation maps a snapshot to a terminal decision (or "keep waiting").
// We treat ready > 0 as success rather than requiring ready == desired so a
// multi-instance rollout reports as soon as it's serving instead of blocking
// the CLI until every instance is up.
func decideActivation(snap healthSnapshot) activationDecision {
	if !snap.versionActive {
		return decisionWait
	}
	// Back-compat: a server that predates health reporting never sets this
	// field, and the current server always sets it to a non-empty value, so an
	// empty health means we're talking to an older server that can't tell us
	// whether the app is serving. Fall back to "the version is active = done"
	// (the old CLI's behavior) rather than waiting out a timeout that will
	// never resolve into a health state.
	if snap.health == "" {
		return decisionHealthy
	}
	switch {
	case snap.health == apphealth.Crashed:
		return decisionCrashed
	case snap.health == apphealth.Ready:
		// A task-only app never has a serving instance, so the ready > 0 test
		// below would never fire and the deploy would time out despite having
		// succeeded.
		return decisionTaskOnly
	case snap.health == apphealth.Idle:
		return decisionScaledToZero
	case snap.ready > 0:
		return decisionHealthy
	default:
		return decisionWait
	}
}

// awaitHealthy wires up the app-status and logs clients and waits for the
// deployed version to become healthy, reporting the truth. It returns an error
// when the version never goes healthy so the deploy/rollback exits non-zero. If
// app-status is unreachable we skip the wait rather than fail the deploy: the
// version is already recorded server-side and we simply can't confirm health.
func awaitHealthy(ctx *Context, appName, versionID, versionDisplay string) error {
	getter, tailer, ok := healthClients(ctx)
	if !ok {
		return nil
	}
	return waitForActivation(ctx, getter, tailer, appName, versionID, versionDisplay)
}

// healthClients wires up the app-status and logs clients. ok is false when
// app-status is unreachable, in which case callers skip the health wait rather
// than fail: the version is already recorded server-side, we just can't confirm
// it. The logs client is best-effort (nil tailer just means no log tail).
func healthClients(ctx *Context) (appInfoGetter, logTailer, bool) {
	appCl, err := ctx.RPCClient(rpcAppStatus)
	if err != nil {
		ctx.Log.Debug("app-status unavailable, skipping health wait", "error", err)
		return nil, nil, false
	}
	getter := appStatusAdapter{client: app_v1alpha.NewAppStatusClient(appCl)}

	var tailer logTailer
	if logsCl, logErr := ctx.RPCClient(rpcLogs); logErr == nil {
		tailer = logsAdapter{client: app_v1alpha.NewLogsClient(logsCl)}
	}
	return getter, tailer, true
}

// terminalOutcome is the resolved result of the health wait.
type terminalOutcome int

const (
	outcomeHealthy terminalOutcome = iota
	outcomeScaledToZero
	outcomeTaskOnly
	outcomeCrashed
	outcomeTimeout
	outcomeCanceled
)

// healthWaitTimeout bounds how long any path waits for a version to become
// healthy before giving up.
const healthWaitTimeout = 90 * time.Second

// pollOutcome polls once and classifies the result into a terminal outcome.
// done is false while the version is still coming up (keep waiting).
func pollOutcome(ctx context.Context, getter appInfoGetter, appName, versionID string) (oc terminalOutcome, snap healthSnapshot, done bool) {
	snap = pollHealth(ctx, getter, appName, versionID)
	//exhaustive:ignore decisionWait falls through to the default (not terminal yet)
	switch decideActivation(snap) {
	case decisionHealthy:
		return outcomeHealthy, snap, true
	case decisionScaledToZero:
		return outcomeScaledToZero, snap, true
	case decisionTaskOnly:
		return outcomeTaskOnly, snap, true
	case decisionCrashed:
		return outcomeCrashed, snap, true
	default:
		return outcomeTimeout, snap, false
	}
}

// healthOutcomeText renders the one-line verdict for an outcome. ok reports
// whether it's a success (✓) or failure (✗). Shared by the plain path and the
// build-TUI path so the wording is identical.
func healthOutcomeText(versionDisplay string, outcome terminalOutcome, snap healthSnapshot) (text string, ok bool) {
	//exhaustive:ignore outcomeTimeout is the default; outcomeCanceled is handled before this is called
	switch outcome {
	case outcomeHealthy:
		if snap.health == "" {
			// Older server that doesn't report health: we confirmed the version
			// is active but can't claim it's serving.
			return fmt.Sprintf("Version %s deployed (this cluster doesn't report health)", versionDisplay), true
		}
		detail := ""
		if snap.desired > 0 && snap.ready < snap.desired {
			detail = fmt.Sprintf(" — %d/%d instances ready", snap.ready, snap.desired)
		}
		return fmt.Sprintf("Version %s is live and serving%s", versionDisplay, detail), true
	case outcomeScaledToZero:
		return fmt.Sprintf("Version %s deployed — scaled to zero, no instance running right now", versionDisplay), true
	case outcomeTaskOnly:
		// Deliberately not the scaled-to-zero wording: nothing went to sleep,
		// this app never had a long-running process to begin with.
		return fmt.Sprintf("Version %s deployed — no long-running process; run a task with `miren app run --task`", versionDisplay), true
	case outcomeCrashed:
		detail := ""
		if snap.crashCount > 0 {
			detail = fmt.Sprintf(" after %d consecutive crash(es)", snap.crashCount)
		}
		cooldown := ""
		if snap.cooldownSeconds > 0 {
			cooldown = fmt.Sprintf(", retrying in %ds", snap.cooldownSeconds)
		}
		return fmt.Sprintf("Version %s never became healthy: it crash-looped%s%s", versionDisplay, detail, cooldown), false
	default: // outcomeTimeout
		if !snap.versionActive {
			return fmt.Sprintf("Version %s never became active (recorded server-side but hasn't taken over)", versionDisplay), false
		}
		return fmt.Sprintf("Version %s came up but never became healthy: %d of %d instance(s) ready before timeout", versionDisplay, snap.ready, snap.desired), false
	}
}

// reportHealthResult prints the post-verdict detail common to both paths: the
// port-divergence warning and, on failure, the recent log tail. It returns the
// error (or nil) the deploy should propagate. The ✓/✗ verdict line itself is
// printed by the caller (plain path) or rendered by the TUI.
func reportHealthResult(ctx *Context, tailer logTailer, appName string, snap healthSnapshot, versionDisplay string, ok bool) error {
	reportBoundPortDivergence(ctx, snap)
	if ok {
		return nil
	}
	printRecentLogs(ctx, tailer, appName)
	return fmt.Errorf("version %s did not become healthy", versionDisplay)
}

// waitForActivation polls AppInfo until the deployed version is actually
// healthy, then reports the truth. It returns an error when the version never
// becomes healthy (crash-loop or timeout) so callers can exit non-zero; a
// scaled-to-zero app and a serving app both return nil. This is the plain path
// (rollback, env, --version, and any non-TTY deploy); the interactive build
// deploy drives health through its TUI instead (see awaitHealthyInProgram).
func waitForActivation(ctx *Context, getter appInfoGetter, tailer logTailer, appName, versionID, versionDisplay string) error {
	if versionDisplay == "" {
		versionDisplay = versionID
	}

	start := time.Now()
	outcome, snap := pollUntilTerminal(ctx, getter, appName, versionID, versionDisplay)
	elapsed := time.Since(start)

	if outcome == outcomeCanceled {
		return ctx.Err()
	}

	text, ok := healthOutcomeText(versionDisplay, outcome, snap)
	ctx.Printf("%s\n", healthSummaryLine(ok, text, elapsed))
	return reportHealthResult(ctx, tailer, appName, snap, versionDisplay, ok)
}

// pollUntilTerminal runs the poll loop until the version reaches a terminal
// state (healthy, scaled-to-zero, crashed), the deadline passes, or the context
// is cancelled. On an interactive terminal it animates a spinner line in the
// same style as the build phases; otherwise it prints a single static waiting
// line. It returns the outcome and the most recent snapshot.
func pollUntilTerminal(ctx *Context, getter appInfoGetter, appName, versionID, versionDisplay string) (terminalOutcome, healthSnapshot) {
	const spinInterval = 100 * time.Millisecond

	w := &waitingLine{
		out:         ctx.Stdout,
		label:       fmt.Sprintf("Waiting for version %s to become healthy...", versionDisplay),
		interactive: isInteractiveOutput(ctx.Stdout),
	}

	var snap healthSnapshot
	check := func() (terminalOutcome, bool) {
		oc, s, done := pollOutcome(ctx, getter, appName, versionID)
		snap = s
		return oc, done
	}

	w.tick()
	if oc, done := check(); done {
		w.clear()
		return oc, snap
	}

	deadline := time.After(healthWaitTimeout)
	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	var spinC <-chan time.Time
	if w.interactive {
		spinTick := time.NewTicker(spinInterval)
		defer spinTick.Stop()
		spinC = spinTick.C
	}

	for {
		select {
		case <-ctx.Done():
			w.clear()
			return outcomeCanceled, snap
		case <-deadline:
			// One last look: if it crossed into a terminal state right at the
			// buzzer, report that rather than a misleading timeout.
			oc, done := check()
			w.clear()
			if done {
				return oc, snap
			}
			return outcomeTimeout, snap
		case <-pollTick.C:
			if oc, done := check(); done {
				w.clear()
				return oc, snap
			}
		case <-spinC:
			w.tick()
		}
	}
}

// runHealthPoll polls until terminal, the deadline passes, or ctx is cancelled,
// invoking onProgress with each non-terminal snapshot. It carries no rendering
// of its own; the caller decides how to show progress (a spinner line, or a
// TUI label). Returns the outcome, the latest snapshot, and elapsed time.
func runHealthPoll(ctx context.Context, getter appInfoGetter, appName, versionID string, onProgress func(healthSnapshot)) (terminalOutcome, healthSnapshot, time.Duration) {
	start := time.Now()

	oc, snap, done := pollOutcome(ctx, getter, appName, versionID)
	if done {
		return oc, snap, time.Since(start)
	}
	if onProgress != nil {
		onProgress(snap)
	}

	deadline := time.After(healthWaitTimeout)
	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return outcomeCanceled, snap, time.Since(start)
		case <-deadline:
			// One last look: report a terminal state reached right at the buzzer
			// rather than a misleading timeout.
			if o, s, done := pollOutcome(ctx, getter, appName, versionID); done {
				return o, s, time.Since(start)
			} else {
				snap = s
			}
			return outcomeTimeout, snap, time.Since(start)
		case <-pollTick.C:
			if o, s, ok := pollOutcome(ctx, getter, appName, versionID); ok {
				return o, s, time.Since(start)
			} else {
				snap = s
				if onProgress != nil {
					onProgress(s)
				}
			}
		}
	}
}

// awaitHealthyInProgram drives the health wait as a phase inside the already
// running build TUI, so build → activate → health render as one continuous
// process. It feeds the program label/result messages, waits for it to render
// the verdict and quit (via waitFinal), then prints the follow-up detail
// (port-divergence warning, crash logs) and returns an error if the version
// never became healthy. The program must already have been told the build is
// done (buildDoneMsg) before this is called.
func awaitHealthyInProgram(ctx *Context, prog *tea.Program, waitFinal func() *deployInfo, appName, versionID, versionDisplay string) error {
	if versionDisplay == "" {
		versionDisplay = versionID
	}

	getter, tailer, ok := healthClients(ctx)
	if !ok {
		// Can't confirm health: commit a neutral line so the program quits, and
		// don't fail the deploy.
		prog.Send(healthDoneMsg{
			line:    healthSummaryLine(true, fmt.Sprintf("Version %s deployed", versionDisplay), 0),
			outcome: outcomeScaledToZero,
		})
		waitFinal()
		return nil
	}

	label := fmt.Sprintf("Waiting for version %s to become healthy", versionDisplay)
	prog.Send(healthWaitMsg{label: label})

	healthCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		outcome, snap, elapsed := runHealthPoll(healthCtx, getter, appName, versionID, func(s healthSnapshot) {
			lbl := label
			if s.desired > 0 && s.ready > 0 && s.ready < s.desired {
				lbl = fmt.Sprintf("%s (%d/%d ready)", label, s.ready, s.desired)
			}
			prog.Send(healthWaitMsg{label: lbl})
		})
		msg := healthDoneMsg{outcome: outcome, snap: snap}
		// A cancellation isn't a health verdict, so don't render one (no ✓/✗
		// line, no crash logs); just quit the program and let the caller return
		// the context error.
		if outcome != outcomeCanceled {
			text, ok := healthOutcomeText(versionDisplay, outcome, snap)
			msg.line = healthSummaryLine(ok, text, elapsed)
		}
		prog.Send(msg)
	}()

	final := waitFinal()
	cancel() // stop the poll goroutine if the program quit first (e.g. ctrl-c)

	// The program committed the verdict line and quit. stdout is ours again for
	// the follow-up detail (port warning, crash logs).
	if final == nil {
		return nil
	}
	if final.interrupted {
		return context.Canceled
	}
	if !final.healthResolved {
		return nil
	}
	if final.healthOutcome == outcomeCanceled {
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	}

	_, ok = healthOutcomeText(versionDisplay, final.healthOutcome, final.healthSnap)
	return reportHealthResult(ctx, tailer, appName, final.healthSnap, versionDisplay, ok)
}

// waitingLine renders the "waiting for health" status. On an interactive
// terminal it animates the build's Meter spinner on a single rewritten line; on
// a non-interactive writer it prints one static line and then stays quiet.
// waitingLine animates a single "still working" line on an interactive
// terminal, and degrades to one static line when output isn't a terminal.
//
// out is explicit rather than always ctx.Stdout because some callers must write
// to stderr: anything that runs during an ordinary command would otherwise land
// in piped stdout and corrupt --format json output.
type waitingLine struct {
	out         io.Writer
	label       string
	interactive bool
	frame       int
	printed     bool
}

func (w *waitingLine) tick() {
	if !w.interactive {
		if !w.printed {
			fmt.Fprintf(w.out, "  %s\n", w.label)
			w.printed = true
		}
		return
	}
	frame := Meter.Frames[w.frame%len(Meter.Frames)]
	w.frame++
	fmt.Fprintf(w.out, "\r  %s %s", frame, w.label)
}

// clear erases the animated waiting line so the summary can take its place.
func (w *waitingLine) clear() {
	if w.interactive {
		fmt.Fprint(w.out, "\r\033[K")
	}
}

// healthSummaryLine renders a phase-summary line matching the build phases:
// "  ✓ <text> (<duration>)" in green for success, red for failure.
func healthSummaryLine(ok bool, text string, elapsed time.Duration) string {
	mark, style := "✓", phaseSummaryStyle
	if !ok {
		mark, style = "✗", phaseFailStyle
	}
	timeStr := phaseTimeStyle.Render(fmt.Sprintf("(%s)", formatPhaseDuration(elapsed)))
	return fmt.Sprintf("  %s %s %s", mark, style.Render(text), timeStr)
}

// isInteractiveOutput reports whether w is a terminal we can animate on.
func isInteractiveOutput(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// pollHealth fetches AppInfo once and projects it onto a healthSnapshot for the
// target version. Any error (or a mismatched active version) yields a zero
// snapshot, which decideActivation reads as "keep waiting."
func pollHealth(ctx context.Context, getter appInfoGetter, appName, versionID string) healthSnapshot {
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var snap healthSnapshot

	status, err := getter.AppInfo(pollCtx, appName)
	if err != nil || status == nil {
		return snap
	}

	// Normalize versionID: the CLI may pass an entity ID (e.g.
	// "app_version/go-server-vXXX") while AppInfo returns the short
	// version string ("go-server-vXXX"). Strip the kind prefix if present.
	shortVersion := versionID
	if idx := strings.LastIndex(shortVersion, "/"); idx >= 0 {
		shortVersion = shortVersion[idx+1:]
	}

	if status.ActiveVersion() != shortVersion {
		return snap
	}

	snap.versionActive = true
	snap.health = status.Health()
	snap.ready = status.ReadyInstances()
	snap.desired = status.DesiredInstances()
	snap.crashCount = status.CrashCount()
	snap.cooldownSeconds = status.CooldownSeconds()
	snap.boundPorts = status.BoundPorts()
	return snap
}

// reportBoundPortDivergence surfaces the loud port-divergence warning when the
// app bound a port other than the one we configured and probed. The server
// records the observed port on the sandbox (MIR-1246); here we turn it into a
// real deploy-time warning instead of a quiet line in `m logs`.
func reportBoundPortDivergence(ctx *Context, snap healthSnapshot) {
	for _, bp := range snap.boundPorts {
		if bp == nil || bp.Port() == 0 {
			continue
		}
		addr := ""
		if bp.Address() != "" {
			addr = fmt.Sprintf(" on %s", bp.Address())
		}
		ctx.Printf("⚠ Heads up: your app bound port %d%s, not the port Miren configured via $PORT.\n", bp.Port(), addr)
		ctx.Printf("  Traffic is being auto-routed there. Set $PORT (or [services.web] port) to match to remove this warning.\n")
	}
}

// printRecentLogs tails recent application logs (best effort) so a failed
// deploy shows *why* instead of a bare timeout. Any error is swallowed — we're
// already on the failure path and logs are supplementary.
func printRecentLogs(ctx *Context, tailer logTailer, appName string) {
	if tailer == nil {
		return
	}

	const tailLines = 20

	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	logs, err := tailer.RecentLogs(logCtx, appName)
	if err != nil {
		return
	}

	// Keep the app's own runtime output and drop build/infra chatter: a just-built
	// app's build logs share the recent window but carry a build/system source,
	// while process output is sourced to the sandbox. We want the crash, not the
	// compile.
	var lines []string
	for _, l := range logs {
		if nonAppLogSources[l.Source()] {
			continue
		}
		lines = append(lines, strings.TrimRight(l.Line(), "\n"))
	}
	if len(lines) == 0 {
		return
	}

	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}

	ctx.Printf("\nRecent logs:\n")
	for _, line := range lines {
		ctx.Printf("  %s\n", line)
	}
}

// nonAppLogSources are log sources that don't reflect the running app: build
// pipeline output and system/infra logs. Everything else (sourced to the
// sandbox) is the app's own output.
var nonAppLogSources = map[string]bool{
	"build":  true,
	"system": true,
}
