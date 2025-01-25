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

	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/colortheory"
	"miren.dev/runtime/pkg/rpc/stream"
)

func Deploy(ctx *Context, opts struct {
	App     string `short:"a" long:"app" description:"Application to run"`
	Dir     string `short:"d" long:"dir" description:"Directory to run from"`
	Explain bool   `short:"x" long:"explain" description:"Explain the build process"`
}) error {
	cl, err := ctx.RPCClient("build")
	if err != nil {
		return err
	}

	bc := build.BuilderClient{Client: cl}

	name := opts.App
	dir := opts.Dir

	ctx.Log.Info("building code", "name", name, "dir", dir)

	r, err := build.MakeTar(dir)
	if err != nil {
		return err
	}

	var (
		cb stream.SendStream[*build.Status]
	)

	if opts.Explain {
		pw, err := progresswriter.NewPrinter(ctx, os.Stderr, "auto")
		if err != nil {
			return err
		}

		cb = stream.Callback(func(su *build.Status) error {
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

		cb = stream.Callback(func(su *build.Status) error {
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

				return nil
			case "message":
				msg := update.Message()
				go func() {
					updateCh <- msg
				}()
			}

			return nil
		})
	}

	results, err := bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
	if err != nil {
		return err
	}

	ctx.Printf("\nUpdated version %s deployed. All traffic moved to new version.\n", results.Version())

	return nil
}

type transfer struct {
	total, current int64
}

type transferUpdate struct {
	transfers map[string]transfer
}

type deployInfo struct {
	spinner spinner.Model

	message string
	update  chan string

	transfers chan transferUpdate
	prog      progress.Model
	parts     int
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
	FPS: time.Second / 7, //nolint:gomnd
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
	}
}

func (m *deployInfo) Init() tea.Cmd {
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

func (m *deployInfo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

var deployPrefixStyle = lipgloss.NewStyle().Faint(true)

func (m *deployInfo) View() string {
	fetch := deployPrefixStyle.Render(fmt.Sprintf("Fetching %d items:", m.parts))

	return fmt.Sprintf("  %s %s...\n      %s %s\n", m.spinner.View(), m.message, fetch, m.prog.View())
}
