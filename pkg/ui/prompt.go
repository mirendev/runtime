package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PromptOption configures a text input prompt
type PromptOption func(*promptConfig)

type promptConfig struct {
	label       string
	sensitive   bool
	placeholder string
	charLimit   int
	width       int
}

// WithLabel sets the prompt label
func WithLabel(label string) PromptOption {
	return func(c *promptConfig) {
		c.label = label
	}
}

// WithSensitive makes the input masked (for passwords/secrets)
func WithSensitive(sensitive bool) PromptOption {
	return func(c *promptConfig) {
		c.sensitive = sensitive
	}
}

// WithPlaceholder sets the placeholder text
func WithPlaceholder(placeholder string) PromptOption {
	return func(c *promptConfig) {
		c.placeholder = placeholder
	}
}

// WithCharLimit sets the character limit (0 for unlimited)
func WithCharLimit(limit int) PromptOption {
	return func(c *promptConfig) {
		c.charLimit = limit
	}
}

// WithWidth sets the input field width
func WithWidth(width int) PromptOption {
	return func(c *promptConfig) {
		c.width = width
	}
}

// PromptForInput displays an interactive text input prompt and returns the user's input
func PromptForInput(opts ...PromptOption) (string, error) {
	// Apply default configuration
	config := &promptConfig{
		label:     "Enter value",
		sensitive: false,
		charLimit: 0,
		width:     60,
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	// Create the text input
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = config.charLimit
	ti.Width = config.width

	if config.placeholder != "" {
		ti.Placeholder = config.placeholder
	}

	if config.sensitive {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}

	// Create and run the model
	model := textInputModel{
		textInput: ti,
		label:     config.label,
		sensitive: config.sensitive,
	}

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	m := finalModel.(textInputModel)
	if m.err != nil {
		return "", m.err
	}

	return strings.TrimSpace(m.value), nil
}

// textInputModel is the internal model for text input prompts
type textInputModel struct {
	textInput textinput.Model
	label     string
	sensitive bool
	submitted bool
	value     string
	err       error
}

func (m textInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m textInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.value = m.textInput.Value()
			m.submitted = true
			return m, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m textInputModel) View() string {
	if m.submitted || m.err != nil {
		return ""
	}

	promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	return fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		promptStyle.Render(m.label),
		m.textInput.View(),
		promptStyle.Render("(press enter to submit, esc to cancel)"),
	)
}

// ConfirmOption configures a confirmation prompt
type ConfirmOption func(*confirmConfig)

type confirmConfig struct {
	message     string
	affirmative string
	negative    string
	defaultYes  bool
	indent      string
}

// WithMessage sets the confirmation message
func WithMessage(message string) ConfirmOption {
	return func(c *confirmConfig) {
		c.message = message
	}
}

// WithAffirmative sets the affirmative response text (default: "yes")
func WithAffirmative(text string) ConfirmOption {
	return func(c *confirmConfig) {
		c.affirmative = text
	}
}

// WithNegative sets the negative response text (default: "no")
func WithNegative(text string) ConfirmOption {
	return func(c *confirmConfig) {
		c.negative = text
	}
}

// WithDefault sets the default response when user just presses enter
func WithDefault(defaultYes bool) ConfirmOption {
	return func(c *confirmConfig) {
		c.defaultYes = defaultYes
	}
}

// WithIndent sets the indentation prefix for the prompt
func WithIndent(indent string) ConfirmOption {
	return func(c *confirmConfig) {
		c.indent = indent
	}
}

// Confirm displays a yes/no confirmation prompt
func Confirm(opts ...ConfirmOption) (bool, error) {
	// Apply default configuration
	config := &confirmConfig{
		message:     "Are you sure?",
		affirmative: "yes",
		negative:    "no",
		defaultYes:  false,
		indent:      "",
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	// Create and run the model
	model := confirmModel{
		message:     config.message,
		affirmative: config.affirmative,
		negative:    config.negative,
		defaultYes:  config.defaultYes,
		indent:      config.indent,
	}

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}

	m := finalModel.(confirmModel)
	if m.err != nil {
		return false, m.err
	}

	return m.confirmed, nil
}

// confirmModel is the internal model for confirmation prompts
type confirmModel struct {
	message     string
	affirmative string
	negative    string
	defaultYes  bool
	indent      string
	confirmed   bool
	err         error
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		input := strings.ToLower(msg.String())
		switch input {
		case "y", "yes", strings.ToLower(m.affirmative):
			m.confirmed = true
			return m, tea.Quit
		case "n", "no", strings.ToLower(m.negative):
			m.confirmed = false
			return m, tea.Quit
		case "enter":
			m.confirmed = m.defaultYes
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	highlightStyle := lipgloss.NewStyle().Bold(true)

	var prompt string
	if m.defaultYes {
		prompt = fmt.Sprintf("(%s/%s)", highlightStyle.Render("Y"), "n")
	} else {
		prompt = fmt.Sprintf("(y/%s)", highlightStyle.Render("N"))
	}

	return fmt.Sprintf("%s%s %s", m.indent, m.message, prompt)
}
