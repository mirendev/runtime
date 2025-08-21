package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/color"
	"miren.dev/runtime/pkg/colortheory"
	"miren.dev/runtime/pkg/progress/progressui"
	"miren.dev/runtime/pkg/progress/upload"
)

// UI Styles
var (
	liveFaint         lipgloss.Style
	deployPrefixStyle = lipgloss.NewStyle().Faint(true)
	phaseSummaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // Green for completed phases
	phaseTimeStyle    = lipgloss.NewStyle().Faint(true)
)

func init() {
	lf := color.LiveFaint()
	if lf == "" {
		liveFaint = lipgloss.NewStyle().Faint(true)
	} else {
		liveFaint = lipgloss.NewStyle().Foreground(lipgloss.Color(lf))
	}
}

// Custom spinner styles
var (
	mirenBlue = "#3E53FB"
	lightBlue = colortheory.ChangeLightness(mirenBlue, -10)
	square    = "▰"

	spinBlankStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#111111",
		Dark:  colortheory.ChangeLightness(mirenBlue, 25),
	})

	spinStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#3E53FB",
		Dark:  lightBlue,
	})
)

func line(str string) string {
	var sb strings.Builder

	for _, b := range str {
		if b == ' ' {
			sb.WriteString(spinBlankStyle.Render(square))
		} else {
			sb.WriteString(spinStyle.Render(square))
		}
	}

	return sb.String()
}

// Meter is the custom spinner animation
var Meter = spinner.Spinner{
	Frames: []string{
		line("   "),
		line("▰  "),
		line("▰▰ "),
		line("▰▰▰"),
		line(" ▰▰"),
		line("  ▰"),
	},
	FPS: time.Second / 7, //nolint:mnd
}

// Data types for UI
type transfer struct {
	total, current int64
}

type transferUpdate struct {
	transfers map[string]transfer
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

type timeoutCheckMsg struct{}

// deployInfo is the TEA model for the deploy UI
type deployInfo struct {
	spinner spinner.Model

	message string
	update  chan string

	transfers chan transferUpdate
	prog      progress.Model
	parts     int

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

	// Timeout and interrupt handling
	lastActivity    time.Time
	buildkitTimeout time.Duration
	buildkitStarted bool // Track if buildkit has shown any activity
	interrupted     bool

	showProgress bool
	bp           tea.Model
}

func initialModel(update chan string, transfers chan transferUpdate, uploadProgress chan upload.Progress) *deployInfo {
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
		spinner:         s,
		message:         "Reading application data",
		update:          update,
		transfers:       transfers,
		prog:            p,
		uploadProgress:  uploadProgress,
		uploadSpin:      uploadS,
		isUploading:     true,
		uploadSpeed:     "calculating...",
		phaseStart:      time.Now(),
		currentPhase:    "upload",
		lastActivity:    time.Now(),
		buildkitTimeout: 60 * time.Second, // 60 second timeout for buildkit to start
		buildkitStarted: false,
		bp:              progressui.TeaModel(),
	}
}

func (m *deployInfo) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.bp.Init(),
		m.spinner.Tick,
		m.uploadSpin.Tick,
		m.checkTimeout(), // Start timeout monitoring
	}

	// Only wait for channels that are expected to have data
	if m.update != nil {
		cmds = append(cmds, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		})
	}

	if m.transfers != nil {
		cmds = append(cmds, func() tea.Msg {
			return <-m.transfers
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

func (m *deployInfo) checkTimeout() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return timeoutCheckMsg{}
	})
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
		switch msg.Type {
		case tea.KeyCtrlC:
			m.interrupted = true
			return m, tea.Quit
		case tea.KeyEsc:
			m.showProgress = false
		case tea.KeyEnter:
			m.showProgress = !m.showProgress
		}
	case timeoutCheckMsg:
		// Only check for timeout if buildkit hasn't started yet
		if m.currentPhase == "buildkit" && !m.buildkitStarted && time.Since(m.lastActivity) > m.buildkitTimeout {
			m.currentPhase = "timeout"
			return m, tea.Sequence(
				tea.Println("\n\n❌ Buildkit failed to start after 60 seconds. This may indicate a server issue."),
				tea.Quit,
			)
		}
		// Continue checking
		cmds = append(cmds, m.checkTimeout())
	case updateMsg:
		prevMessage := m.message
		m.message = msg.msg
		m.lastActivity = time.Now() // Reset activity timer

		// Track phase transitions
		if prevMessage != msg.msg {
			// Complete the upload phase
			if m.isUploading && msg.msg == "Launching builder" {
				m.isUploading = false
				duration := time.Since(m.phaseStart)
				m.uploadDuration = duration

				// Create upload summary
				var details string
				if m.uploadBytes > 0 {
					details = fmt.Sprintf("%s uploaded at %s",
						upload.FormatBytes(m.uploadBytes),
						upload.FormatSpeed(m.finalUploadSpeed))
				} else if m.finalUploadSpeed > 0 {
					details = fmt.Sprintf("Uploaded at %s", upload.FormatSpeed(m.finalUploadSpeed))
				}

				m.completedPhases = append(m.completedPhases, phaseSummary{
					name:     "Upload artifacts",
					duration: duration,
					details:  details,
				})

				// Start tracking buildkit phase
				m.phaseStart = time.Now()
				m.currentPhase = "buildkit"
			}

		}

		cmds = append(cmds, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		})
	case upload.Progress:
		m.lastActivity = time.Now() // Reset activity timer
		if m.isUploading {
			m.uploadSpeed = upload.FormatSpeed(msg.BytesPerSecond)
			m.uploadBytes = msg.BytesRead
			m.finalUploadSpeed = msg.BytesPerSecond

			// Spinner tick is already handled in the Update method
			// Continue reading upload progress
			if m.uploadProgress != nil {
				cmds = append(cmds, func() tea.Msg {
					return <-m.uploadProgress
				})
			}
		}
	case transferUpdate:
		m.lastActivity = time.Now() // Reset activity timer

		// Mark buildkit as started once we receive transfer updates
		if m.currentPhase == "buildkit" && !m.buildkitStarted {
			m.buildkitStarted = true
		}

		// Buildkit transfers mean upload is complete (fallback if no "Launching builder" message)
		if m.isUploading {
			m.isUploading = false
			duration := time.Since(m.phaseStart)
			m.uploadDuration = duration

			// Only create upload summary if we haven't already from "Launching builder" message
			// Check both that we don't have phases OR the last phase isn't Upload artifacts
			hasUploadPhase := false
			for _, phase := range m.completedPhases {
				if phase.name == "Upload artifacts" {
					hasUploadPhase = true
					break
				}
			}

			if !hasUploadPhase {
				var details string
				if m.uploadBytes > 0 {
					details = fmt.Sprintf("%s uploaded at %s",
						upload.FormatBytes(m.uploadBytes),
						upload.FormatSpeed(m.finalUploadSpeed))
				}

				m.completedPhases = append(m.completedPhases, phaseSummary{
					name:     "Upload artifacts",
					duration: duration,
					details:  details,
				})
			}

			// Always start tracking buildkit phase when transitioning
			m.phaseStart = time.Now()
			m.currentPhase = "buildkit"
		}

		var total, current int64
		for _, t := range msg.transfers {
			total += t.total
			current += t.current
		}

		m.parts = len(msg.transfers)

		cmd := m.prog.SetPercent(float64(current) / float64(total))
		cmds = append(cmds,
			cmd,
			func() tea.Msg {
				return <-m.transfers
			})
	case progress.FrameMsg:
		p, cmd := m.prog.Update(msg)
		m.prog = p.(progress.Model)

		if cmd != nil {
			cmds = append(cmds, cmd)
		}
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

	// Show completed phase summaries
	for _, phase := range m.completedPhases {
		phaseStr := fmt.Sprintf("  ✓ %s", phaseSummaryStyle.Render(phase.name))
		timeStr := phaseTimeStyle.Render(fmt.Sprintf("(%s)", formatPhaseDuration(phase.duration)))

		if phase.details != "" {
			lines = append(lines, fmt.Sprintf("%s %s - %s", phaseStr, timeStr, phase.details))
		} else {
			lines = append(lines, fmt.Sprintf("%s %s", phaseStr, timeStr))
		}
	}

	// Show current progress
	var currentLine string
	if m.isUploading {
		bytesInfo := upload.FormatBytes(m.uploadBytes)
		speedInfo := deployPrefixStyle.Render(fmt.Sprintf("%s uploaded at %s", bytesInfo, m.uploadSpeed))
		currentLine = fmt.Sprintf("  %s %s...\n      %s %s",
			m.uploadSpin.View(), m.message, m.spinner.View(), speedInfo)
	} else if m.currentPhase != "completed" {
		fetch := deployPrefixStyle.Render(fmt.Sprintf("Fetching %d items:", m.parts))
		currentLine = fmt.Sprintf("  %s %s...\n      %s %s", m.spinner.View(), m.message, fetch, m.prog.View())
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	// Join all lines
	str := strings.Join(lines, "\n")

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
