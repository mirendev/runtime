package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"golang.org/x/term"

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
	var args struct {
		Path      string `json:"path"`
		Query     string `json:"query"`
		StartLine int    `json:"start_line"`
		Lines     int    `json:"lines"`
	}
	action, subject, detail := "Local tool", name, ""
	if err := json.Unmarshal(input, &args); err != nil {
		detail = "invalid arguments"
	} else {
		if args.Path == "" {
			args.Path = "."
		}
		// Paths stay readable, but control characters must not alter the terminal.
		path := strconv.QuoteToGraphic(args.Path)
		path = path[1 : len(path)-1]
		subject = path
		switch name {
		case "local_list":
			action = "Listing"
		case "local_search":
			action = "Searching"
			subject = strconv.QuoteToGraphic(args.Query)
			detail = "in " + path
		case "local_read":
			action = "Reading"
			start, lines := args.StartLine, args.Lines
			if start == 0 {
				start = 1
			}
			if lines == 0 {
				lines = 200
			}
			switch {
			case start < 1 || lines < 1 || lines > 200:
				detail = "invalid line range"
			case lines == 1:
				detail = fmt.Sprintf("line %d", start)
			default:
				detail = fmt.Sprintf("lines %d–%d", start, uint64(start)+uint64(lines)-1)
			}
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	muted, emphasis, accent := lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle()
	interactive := isInteractiveOutput(d.output) && isInteractiveOutput(d.status)
	if interactive {
		muted = muted.Foreground(theme.Muted)
		emphasis = emphasis.Bold(true)
		accent = accent.Foreground(theme.Info)
	}
	label := accent.Render("  › ") + emphasis.Render(action) + " " + accent.Render(subject)
	if detail != "" {
		separator := " · "
		if interactive {
			width, _, err := term.GetSize(int(d.output.(*os.File).Fd()))
			if err == nil && lipgloss.Width(label)+3+lipgloss.Width(detail) > width {
				separator = "\n    "
			}
		}
		label += muted.Render(separator + detail)
	}
	if d.program != nil {
		d.program.Println(label)
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
