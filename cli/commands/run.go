package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progresswriter"

	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/colortheory"
	"miren.dev/runtime/pkg/rpc/stream"
)

func Run(ctx *Context, opts struct {
	App     string `short:"a" long:"app" description:"Application to run"`
	Dir     string `short:"d" long:"dir" description:"Directory to run from"`
	Explain bool   `short:"x" long:"explain" description:"Explain the build process"`
}) error {
	c := &cliRun{}

	_, err := c.buildCode(ctx, opts.App, opts.Dir, opts.Explain)
	if err != nil {
		return err
	}

	ctx.Printf("\nUpdated version deployed!\n")

	return nil
}

type cliRun struct{}

type transfer struct {
	total, current int64
}

type transferUpdate struct {
	transfers map[string]transfer
}

func (c *cliRun) buildCode(ctx *Context, name, dir string, explain bool) (string, error) {
	cl, err := ctx.RPCClient("build")
	if err != nil {
		return "", err
	}

	bc := build.BuilderClient{Client: cl}

	ctx.Log.Info("building code", "name", name, "dir", dir)

	r, err := build.MakeTar(dir)
	if err != nil {
		return "", err
	}

	var (
		pw progresswriter.Writer
		//spin     *mspinner.Spinner
		updateCh   = make(chan string, 1)
		transferCh = make(chan transferUpdate, 1)
	)

	if explain {
		pw, err = progresswriter.NewPrinter(ctx, os.Stderr, "auto")
		if err != nil {
			return "", err
		}
	} else {
		var wg sync.WaitGroup
		defer wg.Wait()

		p := tea.NewProgram(initialModel(updateCh, transferCh))
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Run()
		}()

		defer p.Quit()
	}

	transfers := map[string]transfer{}

	cb := stream.Callback(func(su *build.Status) error {
		update := su.Update()

		switch update.Which() {
		case "buildkit":
			sj := update.Buildkit()

			var status client.SolveStatus
			if err := json.Unmarshal(sj, &status); err != nil {
				return err
			}

			if !explain {
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

				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case pw.Status() <- &status:
				// ok
			}
		case "message":
			if explain {
				return nil
			}

			msg := update.Message()
			go func() {
				updateCh <- msg
			}()
		}

		return nil
	})

	results, err := bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
	if err != nil {
		return "", err
	}

	return results.Version(), nil
}

type pushInfo struct {
	spinner  spinner.Model
	quitting bool
	err      error

	message    string
	update     chan string
	lastUpdate time.Time

	sub      string
	subStyle lipgloss.Style

	width int

	transfers chan transferUpdate
	prog      progress.Model
	parts     int

	fetch string
}

var Meter = spinner.Spinner{
	Frames: []string{
		"▱▱▱",
		"▰▱▱",
		"▰▰▱",
		"▰▰▰",
		"▱▰▰",
		"▱▱▰",
		"▱▱▱",
	},
	FPS: time.Second / 7, //nolint:gomnd
}

var (
	mirenBlue = "#3E53FB"
	lightBlue = colortheory.ChangeLightness(mirenBlue, -10)
)

func initialModel(update chan string, transfers chan transferUpdate) *pushInfo {
	s := spinner.New()
	s.Spinner = Meter
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#3E53FB",
		Dark:  lightBlue,
	})

	p := progress.New(progress.WithWidth(20), progress.WithGradient(
		colortheory.ChangeLightness("#3E53FB", -10),
		colortheory.ChangeLightness("#3E53FB", 20),
	))

	return &pushInfo{
		spinner:   s,
		message:   "Starting build",
		sub:       "doing things",
		update:    update,
		transfers: transfers,
		prog:      p,
		fetch:     lipgloss.NewStyle().Faint(true).Render("Fetching data:"),
	}
}

func (m *pushInfo) Init() tea.Cmd {
	return tea.Batch(
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

func (m *pushInfo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case error:
		m.err = msg
		return m, nil
	case updateMsg:
		m.message = msg.msg
		return m, func() tea.Msg {
			return updateMsg{msg: <-m.update}
		}
	case transferUpdate:
		var total, current int64
		for _, t := range msg.transfers {
			total += t.total
			current += t.current
		}

		m.parts = len(msg.transfers)

		cmd := m.prog.SetPercent(float64(current) / float64(total))
		return m,
			tea.Batch(cmd,
				func() tea.Msg {
					return <-m.transfers
				},
			)
	case progress.FrameMsg:
		p, cmd := m.prog.Update(msg)
		m.prog = p.(progress.Model)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

func (m *pushInfo) View() string {
	if m.err != nil {
		return m.err.Error()
	}

	fetch := lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("Fetching %d data:", m.parts))

	return fmt.Sprintf("%s %s...\n    %s %s\n", m.spinner.View(), m.message, fetch, m.prog.View())
}
