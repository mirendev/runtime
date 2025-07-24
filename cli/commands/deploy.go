package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/buildkit/client"

	"github.com/moby/buildkit/util/progress/progresswriter"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/color"
	"miren.dev/runtime/pkg/colortheory"
	"miren.dev/runtime/pkg/progress/progressui"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
)

func Deploy(ctx *Context, opts struct {
	AppCentric

	Explain       bool   `short:"x" long:"explain" description:"Explain the build process"`
	ExplainFormat string `long:"explain-format" description:"Explain format" choice:"auto" choice:"plain" choice:"tty" choice:"rawjson" default:"auto"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/build")
	if err != nil {
		return err
	}

	bc := build_v1alpha.NewBuilderClient(cl)

	name := opts.App
	dir := opts.Dir

	ctx.Log.Info("building code", "name", name, "dir", dir)

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		return err
	}
	if ac != nil && ac.Include != nil {
		// Validate patterns before using them
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	r, err := tarx.MakeTar(dir, includePatterns)
	if err != nil {
		return err
	}

	var (
		cb      stream.SendStream[*build_v1alpha.Status]
		results *build_v1alpha.BuilderClientBuildFromTarResults
	)

	if opts.Explain {
		pw, err := progresswriter.NewPrinter(ctx, os.Stderr, opts.ExplainFormat)
		if err != nil {
			return err
		}

		cb = stream.Callback(func(su *build_v1alpha.Status) error {
			update := su.Update()

			switch update.Which() {
			case "buildkit":
				sj := update.Buildkit()

				var status client.SolveStatus
				if err := json.Unmarshal(sj, &status); err != nil {
					return err
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case pw.Status() <- &status:
					// ok
				}
			}

			return nil
		})

		results, err = bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
		if err != nil {
			return err
		}

		close(pw.Status())
		<-pw.Done()

		if pw.Err() != nil {
			return pw.Err()
		}
	} else {
		var (
			updateCh   = make(chan string, 1)
			transferCh = make(chan transferUpdate, 1)
			transfers  = map[string]transfer{}
			wg         sync.WaitGroup
		)

		defer wg.Wait()

		p := tea.NewProgram(initialModel(updateCh, transferCh))
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Run()
		}()

		defer p.Quit()

		cb = stream.Callback(func(su *build_v1alpha.Status) error {
			update := su.Update()

			switch update.Which() {
			case "buildkit":
				sj := update.Buildkit()

				var status client.SolveStatus
				if err := json.Unmarshal(sj, &status); err != nil {
					return err
				}

				var updated bool
				for _, st := range status.Statuses {
					if st.Total != 0 {
						updated = true
						transfers[st.ID] = transfer{total: st.Total, current: st.Current}
					}
				}

				if updated {
					select {
					case <-ctx.Done():
						// none
					case transferCh <- transferUpdate{transfers: transfers}:
						// ok
					}
				}

				p.Send(&status)

				return nil
			case "message":
				msg := update.Message()
				go func() {
					updateCh <- msg
				}()
			}

			return nil
		})

		results, err = bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
		if err != nil {
			return err
		}

	}

	if results.Version() == "" {
		ctx.Printf("\n\nError detected in building %s. No version returned.\n", name)
		return nil
	}

	ctx.Printf("\n\nUpdated version %s deployed. All traffic moved to new version.\n", results.Version())

	return nil
}

var liveFaint lipgloss.Style

func init() {
	lf := color.LiveFaint()
	if lf == "" {
		liveFaint = lipgloss.NewStyle().Faint(true)
	} else {
		liveFaint = lipgloss.NewStyle().Foreground(lipgloss.Color(lf))
	}
}

type transfer struct {
	total, current int64
}

type transferUpdate struct {
	transfers map[string]transfer
}

type deployInfo struct {
	cancel  func()
	spinner spinner.Model

	message string
	update  chan string

	transfers chan transferUpdate
	prog      progress.Model
	parts     int

	showProgress bool
	bp           tea.Model
}

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

func initialModel(update chan string, transfers chan transferUpdate) *deployInfo {
	s := spinner.New()
	s.Spinner = Meter
	s.Style = lipgloss.NewStyle()

	p := progress.New(progress.WithWidth(20), progress.WithGradient(
		colortheory.ChangeLightness("#3E53FB", -10),
		colortheory.ChangeLightness("#3E53FB", 20),
	))

	return &deployInfo{
		spinner:   s,
		message:   "Starting build",
		update:    update,
		transfers: transfers,
		prog:      p,
		bp:        progressui.TeaModel(),
	}
}

func (m *deployInfo) Init() tea.Cmd {
	return tea.Batch(
		m.bp.Init(),
		m.spinner.Tick,
		func() tea.Msg {
			return updateMsg{msg: <-m.update}
		},
		func() tea.Msg {
			return <-m.transfers
		},
	)
}

type updateMsg struct {
	msg   string
	silly bool
}

type tickMsg struct{}

func (m *deployInfo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	m.bp, cmd = m.bp.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case updateMsg:
		m.message = msg.msg

		cmds = append(cmds, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		})
	case transferUpdate:
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
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			m.showProgress = false
		case tea.KeyEnter:
			m.showProgress = !m.showProgress
		}
	}

	return m, tea.Batch(cmds...)
}

var deployPrefixStyle = lipgloss.NewStyle().Faint(true)

func (m *deployInfo) View() string {
	fetch := deployPrefixStyle.Render(fmt.Sprintf("Fetching %d items:", m.parts))

	str := fmt.Sprintf("  %s %s...\n      %s %s", m.spinner.View(), m.message, fetch, m.prog.View())

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
