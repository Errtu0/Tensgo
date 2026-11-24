package reporter

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
)

type tickMsg time.Time

type testDoneMsg struct {
	results  []metrics.Result
	duration time.Duration
}

type progressModel struct {
	cfg       engine.Config
	completed *atomic.Int64
	startTime time.Time
	spinner   spinner.Model
	progress  progress.Model
	width     int
	results   []metrics.Result
	elapsed   time.Duration
	done      bool
}

func newProgressModel(cfg engine.Config, completed *atomic.Int64) progressModel {
	termW := TerminalWidth()
	boxW := ClampBoxWidth(termW, 76, 40)
	barW := boxW - 10
	if barW > 44 {
		barW = 44
	}
	if barW < 16 {
		barW = 16
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#DE8B4B"))

	prog := progress.New(
		progress.WithScaledGradient("#DE8B4B", "#6B8E7A"),
		progress.WithWidth(barW),
	)

	return progressModel{
		cfg:       cfg,
		completed: completed,
		startTime: time.Now(),
		spinner:   sp,
		progress:  prog,
		width:     termW,
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickCmd())
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
			boxW := ClampBoxWidth(m.width, 76, 40)
			barW := boxW - 10
			if barW > 44 {
				barW = 44
			}
			if barW < 16 {
				barW = 16
			}
			m.progress.Width = barW
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	case tickMsg:
		if m.done {
			return m, nil
		}
		elapsed := time.Since(m.startTime)
		pct := 0.0
		if m.cfg.Duration > 0 {
			pct = float64(elapsed) / float64(m.cfg.Duration)
		}
		if pct > 1.0 {
			pct = 1.0
		}
		cmd := m.progress.SetPercent(pct)
		return m, tea.Batch(cmd, tickCmd())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		m.progress = pm.(progress.Model)
		return m, cmd

	case testDoneMsg:
		m.results = msg.results
		m.elapsed = msg.duration
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m progressModel) View() string {
	if m.done {
		return ""
	}

	elapsed := time.Since(m.startTime).Round(100 * time.Millisecond)
	if elapsed > m.cfg.Duration && m.cfg.Duration > 0 {
		elapsed = m.cfg.Duration
	}

	boxW := ClampBoxWidth(m.width, 76, 40)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ECEAE6"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7075"))
	countStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#6B8E7A"))
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6C7075")).
		Padding(1, 2).
		Width(boxW)

	targetLabel := TruncateRunes(fmt.Sprintf("(%s %s • %d workers)", m.cfg.Method, m.cfg.URL, m.cfg.Concurrency), max(16, boxW-26))
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s %s %s\n\n",
		m.spinner.View(),
		titleStyle.Render("Running Load Test"),
		mutedStyle.Render(targetLabel),
	))
	b.WriteString(m.progress.View() + "\n\n")
	b.WriteString(fmt.Sprintf("%s %s   %s %s",
		mutedStyle.Render("Elapsed:"),
		titleStyle.Render(fmt.Sprintf("%s / %s", elapsed, m.cfg.Duration)),
		mutedStyle.Render("Completed:"),
		countStyle.Render(fmt.Sprintf("%d reqs", m.completed.Load())),
	))

	return "\n" + boxStyle.Render(b.String()) + "\n"
}

// RunWithProgress executes the load test while rendering a live Bubbletea progress UI.
func RunWithProgress(cfg engine.Config) ([]metrics.Result, time.Duration, error) {
	var completed atomic.Int64
	cfg.Completed = &completed

	m := newProgressModel(cfg, &completed)
	p := tea.NewProgram(m)

	go func() {
		results, duration := engine.Run(cfg)
		p.Send(testDoneMsg{results: results, duration: duration})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return nil, 0, err
	}

	fm := finalModel.(progressModel)
	return fm.results, fm.elapsed, nil
}
