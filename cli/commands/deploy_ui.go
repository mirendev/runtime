package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/colortheory"
	"miren.dev/runtime/pkg/progress/progressui"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/theme"
)

// UI Styles
var (
	liveFaint         = lipgloss.NewStyle().Foreground(theme.Muted)
	deployPrefixStyle = lipgloss.NewStyle().Foreground(theme.Muted)
	phaseSummaryStyle = lipgloss.NewStyle().Foreground(theme.Success) // Green for completed phases
	phaseTimeStyle    = lipgloss.NewStyle().Foreground(theme.Muted)
	phaseFailStyle    = lipgloss.NewStyle().Foreground(theme.Error) // Red for failed phases
)

// Custom spinner styles
var (
	mirenBlue = "#3E53FB"
	lightBlue = colortheory.ChangeLightness(mirenBlue, -10)

	spinStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#3E53FB",
		Dark:  lightBlue,
	})
)

func line(str string) string {
	var sb strings.Builder

	sb.WriteString(spinStyle.Render(str))

	return sb.String()
}

// Meter is the custom spinner animation
var Meter = spinner.Spinner{
	Frames: []string{
		line("⠋"),
		line("⠙"),
		line("⠹"),
		line("⠸"),
		line("⠼"),
		line("⠴"),
		line("⠦"),
		line("⠧"),
		line("⠇"),
		line("⠏"),
	},
	FPS: time.Second / 10, //nolint:mnd
}

// Data types for UI
type buildProgress struct {
	total     int
	completed int
}

type phaseSummary struct {
	name     string
	duration time.Duration
	details  string
}

// TEA message types
type updateMsg struct {
	msg string
}

// buildDoneMsg tells the model preparation finished. image is populated when
// Miren selected an upstream image directly, allowing the summary to describe
// what actually happened rather than treating a zero-step build as cached.
type buildDoneMsg struct{ image string }

// healthWaitMsg sets the in-progress label for the health phase (e.g.
// "Waiting for version X to become healthy" or "1/3 instances ready").
type healthWaitMsg struct{ label string }

// healthDoneMsg resolves the health phase: it commits the pre-rendered verdict
// line to scrollback and quits the program. line is built by the caller (which
// knows the version display string); outcome/snap are recorded for the caller's
// post-quit follow-up (port warning, crash logs).
type healthDoneMsg struct {
	line    string
	outcome terminalOutcome
	snap    healthSnapshot
}

// deployInfo is the TEA model for the deploy UI
type deployInfo struct {
	spinner spinner.Model

	message string
	update  chan string

	buildCh    chan buildProgress
	prog       progress.Model
	buildSteps int
	buildPct   float64

	uploadProgress chan upload.Progress
	uploadSpin     spinner.Model
	uploadSpeed    string
	isUploading    bool

	// Phase tracking
	phaseStart       time.Time
	currentPhase     string
	completedPhases  []phaseSummary
	uploadBytes      int64
	uploadDuration   time.Duration
	finalUploadSpeed float64

	// Upload progress estimation
	uploadPct float64       // 0.0–1.0 fraction, -1 if unknown
	uploadETA time.Duration // estimated time remaining; 0 if unknown

	// Source cache info
	cachedFiles int32
	totalFiles  int
	cachedBytes int64

	interrupted bool

	showProgress bool
	bp           tea.Model

	// Health phase: populated once the build TUI keeps running through the
	// wait-for-healthy step so it renders as one continuous process.
	healthResolved bool
	healthOutcome  terminalOutcome
	healthSnap     healthSnapshot
}

func initialModel(update chan string, buildCh chan buildProgress, uploadProgress chan upload.Progress, cachedFiles int32, totalFiles int, cachedBytes int64) *deployInfo {
	s := spinner.New()
	s.Spinner = Meter
	s.Style = lipgloss.NewStyle()

	p := progress.New(progress.WithWidth(20), progress.WithGradient(
		colortheory.ChangeLightness("#3E53FB", -10),
		colortheory.ChangeLightness("#3E53FB", 20),
	))

	uploadS := spinner.New()
	uploadS.Spinner = Meter
	uploadS.Style = lipgloss.NewStyle()

	return &deployInfo{
		spinner:        s,
		message:        "Reading application data",
		update:         update,
		buildCh:        buildCh,
		prog:           p,
		uploadProgress: uploadProgress,
		uploadSpin:     uploadS,
		isUploading:    true,
		uploadSpeed:    "calculating...",
		phaseStart:     time.Now(),
		currentPhase:   "upload",
		cachedFiles:    cachedFiles,
		totalFiles:     totalFiles,
		cachedBytes:    cachedBytes,
		bp:             progressui.TeaModel(),
	}
}

func (m *deployInfo) uploadDetails() string {
	// All files cached — special case
	if m.cachedFiles > 0 && m.cachedFiles == int32(m.totalFiles) {
		return fmt.Sprintf("all %d files reused from previous deploy", m.totalFiles)
	}

	var parts []string
	if m.uploadBytes > 0 {
		parts = append(parts, fmt.Sprintf("%s at %s",
			upload.FormatBytes(m.uploadBytes),
			upload.FormatSpeed(m.finalUploadSpeed)))
	}
	if m.cachedFiles > 0 {
		detail := fmt.Sprintf("reused %d/%d files", m.cachedFiles, m.totalFiles)
		if m.cachedBytes > 0 && m.finalUploadSpeed > 0 {
			savedSec := float64(m.cachedBytes) / m.finalUploadSpeed
			detail += fmt.Sprintf(" (saved %s, ~%s)", upload.FormatBytes(m.cachedBytes), upload.FormatDuration(time.Duration(savedSec*float64(time.Second))))
		} else if m.cachedBytes > 0 {
			detail += fmt.Sprintf(" (saved %s)", upload.FormatBytes(m.cachedBytes))
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, ", ")
}

// completePhase records a finished phase and returns a command that commits its
// summary line to permanent scrollback (above the live spinner). Completed lines
// must be emitted this way rather than rendered in View: bubbletea clears its
// live region on exit, so anything left in View at quit time can be wiped or
// overwritten by later output.
func (m *deployInfo) completePhase(p phaseSummary) tea.Cmd {
	m.completedPhases = append(m.completedPhases, p)
	return tea.Println(renderPhaseSummary(p))
}

func (m *deployInfo) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.bp.Init(),
		m.spinner.Tick,
		m.uploadSpin.Tick,
	}

	// Only wait for channels that are expected to have data
	if m.update != nil {
		cmds = append(cmds, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		})
	}

	if m.buildCh != nil {
		cmds = append(cmds, func() tea.Msg {
			return <-m.buildCh
		})
	}

	// Start listening for upload progress
	if m.uploadProgress != nil {
		cmds = append(cmds, func() tea.Msg {
			return <-m.uploadProgress
		})
	}

	return tea.Batch(cmds...)
}

func (m *deployInfo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	m.bp, cmd = m.bp.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	m.uploadSpin, cmd = m.uploadSpin.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		//exhaustive:ignore tea.KeyType has ~90 members; default handles the rest
		switch msg.Type {
		case tea.KeyCtrlC:
			m.interrupted = true
			return m, tea.Quit
		case tea.KeyEsc:
			m.showProgress = false
		case tea.KeyEnter:
			m.showProgress = !m.showProgress
		}
	case updateMsg:
		prevMessage := m.message
		m.message = msg.msg

		// Track phase transitions
		if prevMessage != msg.msg {
			// Complete the upload phase
			if m.isUploading && msg.msg == "Launching builder" {
				m.isUploading = false
				duration := time.Since(m.phaseStart)
				m.uploadDuration = duration

				cmds = append(cmds, m.completePhase(phaseSummary{
					name:     "Upload artifacts",
					duration: duration,
					details:  m.uploadDetails(),
				}))

				// Start tracking buildkit phase
				m.phaseStart = time.Now()
				m.currentPhase = "buildkit"
			}

		}

		cmds = append(cmds, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		})
	case upload.Progress:
		if m.isUploading {
			m.uploadSpeed = upload.FormatSpeed(msg.BytesPerSecond)
			m.uploadBytes = msg.BytesRead
			m.finalUploadSpeed = msg.BytesPerSecond
			m.uploadPct = msg.Fraction
			m.uploadETA = msg.ETA

			// Continue reading upload progress
			if m.uploadProgress != nil {
				cmds = append(cmds, func() tea.Msg {
					return <-m.uploadProgress
				})
			}
		}
	case buildProgress:

		// Build progress means upload is complete (fallback if no "Launching builder" message)
		if m.isUploading {
			m.isUploading = false
			duration := time.Since(m.phaseStart)
			m.uploadDuration = duration

			// Only create upload summary if we haven't already from "Launching builder" message
			hasUploadPhase := false
			for _, phase := range m.completedPhases {
				if phase.name == "Upload artifacts" {
					hasUploadPhase = true
					break
				}
			}

			if !hasUploadPhase {
				cmds = append(cmds, m.completePhase(phaseSummary{
					name:     "Upload artifacts",
					duration: duration,
					details:  m.uploadDetails(),
				}))
			}

			// Always start tracking buildkit phase when transitioning
			m.phaseStart = time.Now()
			m.currentPhase = "buildkit"
		}
		m.buildSteps = msg.total

		if msg.total > 0 {
			m.buildPct = float64(msg.completed) / float64(msg.total)
		}
		cmds = append(cmds, func() tea.Msg {
			return <-m.buildCh
		})
	case buildDoneMsg:
		// Emit the phase summaries in order. tea.Batch gives no ordering
		// guarantee, so when a fully cached build finalizes both the upload and
		// build phases in one update, sequence the prints or they can land out
		// of order in scrollback.
		var phasePrints []tea.Cmd

		// A very fast or fully cached build can finish before the upload-phase
		// transition fires, so close out the upload phase first or it never gets
		// committed to the TUI.
		if m.isUploading {
			m.isUploading = false
			m.uploadDuration = time.Since(m.phaseStart)
			phasePrints = append(phasePrints, m.completePhase(phaseSummary{
				name:     "Upload artifacts",
				duration: m.uploadDuration,
				details:  m.uploadDetails(),
			}))
			m.phaseStart = time.Now()
		}

		// Commit the build or direct-image phase, then show an "activating"
		// spinner until the health phase reports in.
		phasePrints = append(phasePrints, m.completePhase(buildPhaseSummary(
			msg.image,
			m.buildSteps,
			time.Since(m.phaseStart),
		)))
		cmds = append(cmds, tea.Sequence(phasePrints...))

		m.isUploading = false
		m.buildSteps = 0
		m.currentPhase = "activating"
		m.message = "Activating version"
		m.phaseStart = time.Now()
	case healthWaitMsg:
		m.currentPhase = "health"
		m.message = msg.label
	case healthDoneMsg:
		// Commit the verdict line to scrollback, then clear the spinner and quit.
		// A canceled wait carries no line, so just quit.
		m.currentPhase = "completed"
		m.healthResolved = true
		m.healthOutcome = msg.outcome
		m.healthSnap = msg.snap
		if msg.line == "" {
			return m, tea.Quit
		}
		return m, tea.Sequence(tea.Println(msg.line), tea.Quit)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *deployInfo) View() string {
	var lines []string

	// Completed phases are committed to scrollback via tea.Println (see
	// completePhase); View renders only the live, in-progress line.

	// Show current progress
	var currentLine string
	if m.isUploading {
		pct := m.uploadPct
		if pct < 0 {
			pct = 0
		}
		pctText := fmt.Sprintf("%d%% — %s at %s",
			int(pct*100),
			upload.FormatBytes(m.uploadBytes),
			m.uploadSpeed)
		if m.uploadETA > 0 {
			pctText += fmt.Sprintf(" (eta ~%s)", upload.FormatDuration(m.uploadETA))
		}
		pctStr := deployPrefixStyle.Render(pctText)
		currentLine = fmt.Sprintf("  %s Uploading artifacts...\n      %s %s",
			m.uploadSpin.View(), m.prog.ViewAs(pct), pctStr)
	} else if m.currentPhase != "completed" {
		if m.buildSteps > 0 {
			steps := deployPrefixStyle.Render(fmt.Sprintf("Building %d steps:", m.buildSteps))
			currentLine = fmt.Sprintf("  %s %s...\n      %s %s", m.spinner.View(), m.message, steps, m.prog.ViewAs(m.buildPct))
		} else {
			currentLine = fmt.Sprintf("  %s %s...", m.spinner.View(), m.message)
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	// Join all lines
	str := strings.Join(lines, "\n")

	// The explain toggle only applies to the build; once we're activating or
	// waiting on health there's nothing to explain, so drop the footer.
	inBuild := m.currentPhase == "upload" || m.currentPhase == "buildkit"
	if !inBuild {
		return str
	}

	if !m.showProgress {
		return lipgloss.JoinVertical(lipgloss.Top,
			str,
			liveFaint.Render("      [enter: explain]"),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Top,
		str,
		m.bp.View(),
		liveFaint.Render("      [enter: hide explain]"),
	)
}

func formatPhaseDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

// Helper function to render a phase summary line
func renderPhaseSummary(phase phaseSummary) string {
	phaseStr := fmt.Sprintf("  ✓ %s", phaseSummaryStyle.Render(phase.name))
	timeStr := phaseTimeStyle.Render(fmt.Sprintf("(%s)", formatPhaseDuration(phase.duration)))
	if phase.details != "" {
		return fmt.Sprintf("%s %s - %s", phaseStr, timeStr, phase.details)
	}
	return fmt.Sprintf("%s %s", phaseStr, timeStr)
}

func buildPhaseSummary(image string, steps int, duration time.Duration) phaseSummary {
	if image != "" {
		return phaseSummary{
			name:     "Use image",
			duration: duration,
			details:  image,
		}
	}
	return phaseSummary{
		name:     "Build & push image",
		duration: duration,
		details:  buildStepsSummary(steps),
	}
}
