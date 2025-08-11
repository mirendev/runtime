package commands

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/app/app_v1alpha"
)

func Set(ctx *Context, opts struct {
	AppCentric
	Env         []string `short:"e" long:"env" description:"Set environment variables (use KEY to prompt, KEY=VALUE to set directly, KEY=@file to read from file)"`
	Sensitive   []string `short:"s" long:"sensitive" description:"Set sensitive environment variables (use KEY to prompt with masking, KEY=VALUE to set directly, KEY=@file to read from file)"`
	Delete      []string `short:"D" long:"delete" description:"Delete environment variables"`
	Concurrency *int     `short:"c" long:"concurrency" description:"Set maximum concurrency of application instances"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewCrudClient(cl)

	res, err := ac.GetConfiguration(ctx, opts.App)
	if err != nil {
		return err
	}

	cfg := res.Configuration()

	var changes bool

	var envvars []*app_v1alpha.NamedValue

	if cfg.HasEnvVars() {
		envvars = cfg.EnvVars()
	}

	// Process all environment variables
	// Note: go-flags doesn't preserve the exact order of mixed flags,
	// so we process regular env vars first, then sensitive ones.
	// Within each group, the order is preserved.
	type envVar struct {
		spec      string
		sensitive bool
	}

	var allVars []envVar

	// Process regular env vars first
	for _, v := range opts.Env {
		allVars = append(allVars, envVar{spec: v, sensitive: false})
	}
	// Then process sensitive env vars
	for _, v := range opts.Sensitive {
		allVars = append(allVars, envVar{spec: v, sensitive: true})
	}

	// Process all variables
	for _, ev := range allVars {
		var key, value string
		var wasFile, wasPrompt bool

		// Check if it's just a key (for prompting) or key=value
		parts := strings.SplitN(ev.spec, "=", 2)
		key = parts[0]

		if len(parts) == 1 {
			// No value provided, prompt for it
			if ev.sensitive {
				promptedValue, err := promptForSensitiveValue(ctx, key)
				if err != nil {
					return fmt.Errorf("failed to read sensitive value for %s: %w", key, err)
				}
				value = promptedValue
			} else {
				promptedValue, err := promptForValue(ctx, key)
				if err != nil {
					return fmt.Errorf("failed to read value for %s: %w", key, err)
				}
				value = promptedValue
			}
			wasPrompt = true
		} else {
			// Value was provided
			value = parts[1]

			if strings.HasPrefix(value, "@") {
				if _, err := os.Stat(value[1:]); err == nil {
					data, err := os.ReadFile(value[1:])
					if err != nil {
						return fmt.Errorf("failed to read env var from file %s: %w", parts[1][1:], err)
					}

					wasFile = true
					value = string(data)
				} else if ev.sensitive {
					ctx.Log.Warn("sensitive env var starts with @ but file does not exist", "file", value[1:])
				}
			}
		}

		// Simply append the new variable - server will handle deduplication with last-value-wins
		changes = true

		if wasFile {
			ctx.Printf("setting %s from file %s...\n", key, parts[1][1:])
		} else if wasPrompt {
			if ev.sensitive {
				ctx.Printf("setting %s (sensitive, from prompt)...\n", key)
			} else {
				ctx.Printf("setting %s (from prompt)...\n", key)
			}
		} else {
			if ev.sensitive {
				ctx.Printf("setting %s (sensitive)...\n", key)
			} else {
				ctx.Printf("setting %s...\n", key)
			}
		}

		var nv app_v1alpha.NamedValue
		nv.SetKey(key)
		nv.SetValue(value)
		nv.SetSensitive(ev.sensitive)

		envvars = append(envvars, &nv)
	}

	for _, v := range opts.Delete {
		envvars = slices.DeleteFunc(envvars, func(nv *app_v1alpha.NamedValue) bool {
			if nv.Key() == v {
				changes = true
				ctx.Printf("deleting %s...\n", v)
				return true
			}

			return false
		})
	}

	if opts.Concurrency != nil && cfg.Concurrency() != int32(*opts.Concurrency) {
		changes = true
		ctx.Printf("setting concurrency to %d...\n", *opts.Concurrency)
		cfg.SetConcurrency(int32(*opts.Concurrency))
	}

	if !changes {
		ctx.Printf("no changes to configuration\n")
		return nil
	}

	cfg.SetEnvVars(envvars)

	setres, err := ac.SetConfiguration(ctx, opts.App, cfg)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", setres.VersionId())

	return nil
}

func promptForValue(ctx *Context, key string) (string, error) {
	return runTextInputPrompt(key, false)
}

func promptForSensitiveValue(ctx *Context, key string) (string, error) {
	return runTextInputPrompt(key, true)
}

// textInputModel is a simple model for text input prompts
type textInputModel struct {
	textInput textinput.Model
	key       string
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

	var promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	var label string
	if m.sensitive {
		label = fmt.Sprintf("Enter value for sensitive variable '%s'", m.key)
	} else {
		label = fmt.Sprintf("Enter value for variable '%s'", m.key)
	}

	return fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		promptStyle.Render(label),
		m.textInput.View(),
		promptStyle.Render("(press enter to submit, esc to cancel)"),
	)
}

func runTextInputPrompt(key string, sensitive bool) (string, error) {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 60

	if sensitive {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}

	model := textInputModel{
		textInput: ti,
		key:       key,
		sensitive: sensitive,
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
