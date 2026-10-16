package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// Bubble Tea owns stdin for the whole conversation, including waiting turns.
// Keystrokes typed while a response is running cannot become approval answers.
func (d *workbenchDisplay) run(ctx context.Context, conversation *cloudapi.Workbench, input io.Reader, question string, chat bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	prompt := textinput.New()
	prompt.Prompt = "> "
	prompt.CharLimit = 1 << 20
	prompt.Focus()
	m := &workbenchUI{spinner: spinner.New(spinner.WithSpinner(Meter)), label: "Waiting for Miren Cloud", width: 80, height: 24, prompt: prompt, chat: chat}
	questions := make(chan string)
	m.submit = func(question string) tea.Cmd {
		return func() tea.Msg {
			select {
			case questions <- question:
			case <-ctx.Done():
			}
			return nil
		}
	}
	if question != "" {
		m.initial = question
		m.busy = true
	}
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(d.output), tea.WithoutSignalHandler())
	d.mu.Lock()
	d.program = program
	d.mu.Unlock()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case question := <-questions:
				err := conversation.AskWithApprovals(ctx, question, uuid.NewString(), d, func(ctx context.Context, request cloudapi.WorkbenchApproval) (string, error) {
					answer := make(chan string, 1)
					program.Send(workbenchUIApproval{request, answer})
					select {
					case decision := <-answer:
						return decision, nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				})
				program.Send(workbenchUIResponse{err})
			}
		}
	}()
	result, err := program.Run()
	cancel()
	// Wait for Ask's abort cleanup before the CLI can exit.
	<-workerDone
	d.mu.Lock()
	d.program = nil
	d.mu.Unlock()
	if final, ok := result.(*workbenchUI); ok {
		if final.rendered != "" {
			_, writeErr := fmt.Fprintln(d.output, strings.TrimRight(final.rendered, "\n"))
			err = errors.Join(err, writeErr)
		}
		err = errors.Join(err, final.err)
	}
	return err
}

type workbenchUIText string
type workbenchUIDone struct{}
type workbenchUIResponse struct{ err error }
type workbenchUIApproval struct {
	request cloudapi.WorkbenchApproval
	answer  chan string
}

type workbenchUI struct {
	spinner  spinner.Model
	label    string
	width    int
	height   int
	text     strings.Builder
	rendered string
	err      error
	done     bool
	prompt   textinput.Model
	chat     bool
	busy     bool
	initial  string
	submit   func(string) tea.Cmd
	approval *workbenchUIApproval
	review   viewport.Model
	choice   int
}

func (m *workbenchUI) Init() tea.Cmd {
	if m.initial != "" {
		return tea.Batch(m.spinner.Tick, m.submit(m.initial))
	}
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

func (m *workbenchUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workbenchUIApproval:
		m.approval = &msg
		m.choice = 2 // Enter alone denies; granting requires deliberate selection.
		m.prompt.Reset()
		m.renderApproval()
	case workbenchUIResponse:
		m.busy = false
		if msg.err != nil || !m.chat {
			m.err = msg.err
			m.done = true
			return m, tea.Quit
		}
		text := strings.TrimRight(m.rendered, "\n")
		m.text.Reset()
		m.rendered = ""
		return m, tea.Println(text)
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			if msg.Type == tea.KeyCtrlC || m.busy || m.approval != nil {
				m.err = context.Canceled
			}
			m.done = true
			return m, tea.Quit
		}
		if m.approval != nil {
			// Pasted text (including newlines) is never a decision.
			if msg.Paste {
				m.choice = 2
				return m, nil
			}
			//exhaustive:ignore only navigation and confirmation keys can admit a decision
			switch msg.Type {
			case tea.KeyUp, tea.KeyLeft:
				m.choice = (m.choice + 2) % 3
			case tea.KeyDown, tea.KeyRight, tea.KeyTab:
				m.choice = (m.choice + 1) % 3
			case tea.KeyEnter:
				m.approval.answer <- []string{"once", "session", "deny"}[m.choice]
				m.approval = nil
				return m, tea.Println("Approval: " + []string{"Allow once", "Allow for conversation", "Deny"}[m.choice])
			case tea.KeyPgUp:
				m.review.HalfViewUp()
			case tea.KeyPgDown:
				m.review.HalfViewDown()
			default:
				m.choice = 2
			}
			return m, nil
		}
		if m.busy || !m.chat {
			return m, nil
		}
		if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlJ {
			question := strings.TrimSpace(m.prompt.Value())
			m.prompt.Reset()
			if question == "/quit" {
				m.done = true
				return m, tea.Quit
			}
			if question != "" {
				m.busy = true
				return m, tea.Batch(tea.Println("> "+question), m.submit(question))
			}
		}
	case workbenchUIText:
		m.text.WriteString(string(msg))
		m.render()
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.render()
		if m.approval != nil {
			m.renderApproval()
		}
	case workbenchUIDone:
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	if m.chat && !m.busy && m.approval == nil {
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
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

	if m.text.Len() == 0 {
		m.rendered = ""
	} else {
		renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(max(1, m.width-4)))
		if err == nil {
			m.rendered, err = renderer.Render(m.text.String())
		}
		if err != nil {
			m.rendered = m.text.String()
		}
		m.err = err
	}
}

func (m *workbenchUI) renderApproval() {
	r := m.approval.request
	var detail bytes.Buffer
	if len(r.Detail) > 0 {
		_ = json.Indent(&detail, r.Detail, "", "  ")
	}
	// Cloud strings are untrusted terminal content, not Markdown or ANSI.
	safe := func(s string) string {
		quoted := strconv.QuoteToGraphic(s)
		return quoted[1 : len(quoted)-1]
	}
	var arguments strings.Builder
	for _, r := range detail.String() {
		if unicode.IsGraphic(r) || r == '\n' {
			arguments.WriteRune(r)
		} else {
			fmt.Fprintf(&arguments, "\\u%04x", r)
		}
	}
	content := "Approval required: " + safe(r.Summary) + "\nTool: " + safe(r.Tool) + "\nConsequences: " + safe(r.Consequence) + "\nArguments:\n" + arguments.String()
	content = ansi.Wrap(content, max(1, m.width), "")
	m.review = viewport.New(max(1, m.width), max(1, min(m.height-7, strings.Count(content, "\n")+1)))
	m.review.SetContent(content)
}

func (m *workbenchUI) View() string {
	if m.done {
		return ""
	}
	if m.approval != nil {
		view := m.review.View() + "\n\n"
		for i, label := range []string{"Allow once", "Allow for conversation", "Deny"} {
			cursor := "  "
			if i == m.choice {
				cursor = "› "
			}
			view += cursor + label + "\n"
		}
		return view + ansi.Wrap("↑/↓ select · Enter confirm · PgUp/PgDn review · Ctrl-C exit", max(1, m.width), "")
	}
	if m.chat && !m.busy {
		return "Ask Workbench. Type /quit or press Ctrl-D to exit.\n" + m.prompt.View()
	}
	return m.rendered + m.spinner.View() + " " + m.label
}
