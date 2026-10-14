package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"miren.dev/runtime/pkg/cloudapi"
	"miren.dev/runtime/pkg/theme"
)

// One renderer owns the terminal while Cloud is working. Tool callbacks may
// arrive concurrently with text deltas, so both go through the same program.
type workbenchDisplay struct {
	mu      sync.Mutex
	output  io.Writer
	status  io.Writer
	program *tea.Program
	done    chan workbenchUIResult
}

type workbenchUIResult struct {
	model tea.Model
	err   error
}

func (d *workbenchDisplay) start(label string) {
	if !isInteractiveOutput(d.output) || !isInteractiveOutput(d.status) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	s := spinner.New(spinner.WithSpinner(Meter))
	m := &workbenchUI{spinner: s, label: label, width: 80}
	// The request owns cancellation; finish shuts the renderer down gracefully
	// before the next prompt or error, including after Ctrl-C.
	d.program = tea.NewProgram(m, tea.WithOutput(d.output), tea.WithInput(nil), tea.WithoutSignalHandler(), tea.WithoutBracketedPaste())
	d.done = make(chan workbenchUIResult, 1)
	program, done := d.program, d.done
	go func() {
		model, err := program.Run()
		done <- workbenchUIResult{model, err}
	}()
}

func (d *workbenchDisplay) finish() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.program == nil {
		return nil
	}
	d.program.Send(workbenchUIDone{})
	result := <-d.done
	d.program = nil
	if m, ok := result.model.(*workbenchUI); ok && m.text.Len() > 0 {
		_, err := fmt.Fprintln(d.output, strings.TrimRight(m.rendered, "\n"))
		return errors.Join(result.err, m.err, err)
	}
	return result.err
}

func (d *workbenchDisplay) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.program != nil {
		d.program.Send(workbenchUIText(string(p)))
		return len(p), nil
	}
	return d.output.Write(p)
}

func (d *workbenchDisplay) tool(name string, input json.RawMessage) {
	// Re-encode to keep arguments on one line and escape terminal controls.
	details, err := json.Marshal(input)
	if err != nil {
		details = []byte("(invalid arguments)")
	}
	label := fmt.Sprintf("%s %s", name, details)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.program != nil {
		d.program.Println(lipgloss.NewStyle().Foreground(theme.Muted).Render(label))
	} else {
		fmt.Fprintln(d.status, label)
	}
}

func (d *workbenchDisplay) ask(ctx context.Context, conversation *cloudapi.Workbench, question string) error {
	d.start("Waiting for Miren Cloud")
	err := conversation.Ask(ctx, question, uuid.NewString(), d)
	if d.program == nil {
		fmt.Fprintln(d.output)
	}
	return errors.Join(err, d.finish())
}

type workbenchUIText string
type workbenchUIDone struct{}

type workbenchUI struct {
	spinner  spinner.Model
	label    string
	width    int
	text     strings.Builder
	rendered string
	err      error
	done     bool
}

func (m *workbenchUI) Init() tea.Cmd { return m.spinner.Tick }

func (m *workbenchUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workbenchUIText:
		m.text.WriteString(string(msg))
		m.render()
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.render()
	case workbenchUIDone:
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *workbenchUI) render() {
	var style string
	switch theme.Current() {
	case theme.VariantDark:
		style = "dark"
	case theme.VariantLight:
		style = "light"
	case theme.VariantNoColor:
		style = "notty"
	}
	renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(max(1, m.width-4)))
	if err == nil {
		m.rendered, err = renderer.Render(m.text.String())
	}
	if err != nil {
		m.rendered = m.text.String()
	}
	m.err = err
}

func (m *workbenchUI) View() string {
	if m.done {
		return ""
	}
	return m.rendered + m.spinner.View() + " " + m.label
}
