package tui

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/reporter"
	"github.com/Errtu0/tensgo/internal/scenario"
)

type scenarioFormModel struct {
	input     textinput.Model
	width     int
	errMsg    string
	infoMsg   string
	missing   bool
	submitted bool
	cancelled bool
}

func newScenarioFormModel() scenarioFormModel {
	ti := textinput.New()
	ti.Prompt = "Scenario YAML    : "
	ti.Placeholder = "./tensgo.yaml or ./examples/tensgo.scenario.yaml"
	ti.SetValue("./tensgo.yaml")
	ti.CharLimit = 512
	ti.Width = 52
	ti.PromptStyle = SelectedStyle
	ti.TextStyle = TextStyle
	ti.PlaceholderStyle = MutedStyle
	ti.Focus()

	return scenarioFormModel{input: ti, width: reporter.TerminalWidth()}
}

func (m scenarioFormModel) Init() tea.Cmd { return textinput.Blink }

func (m scenarioFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
			boxW := reporter.ClampBoxWidth(m.width, 86, 44)
			m.input.Width = max(16, boxW-28)
		}
		return m, nil
	case tea.KeyMsg:
		if sanitized, wasPaste := sanitizePasteMsg(msg); wasPaste {
			if len(sanitized.Runes) == 0 {
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(sanitized)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "ctrl+g":
			path := strings.TrimSpace(m.input.Value())
			if path == "" {
				path = "./tensgo.yaml"
				m.input.SetValue(path)
			}
			if err := os.WriteFile(path, []byte(scenario.DefaultTemplateYAML()), 0o644); err != nil {
				m.errMsg = fmt.Sprintf("Failed to create template: %v", err)
				return m, nil
			}
			m.missing = false
			m.errMsg = ""
			m.infoMsg = fmt.Sprintf("Sample template created at %s — press Enter to run.", path)
			return m, nil

		case "enter":
			path := strings.TrimSpace(m.input.Value())
			if path == "" {
				m.errMsg = "Scenario YAML path is required"
				return m, nil
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				m.missing = true
				m.infoMsg = ""
				m.errMsg = fmt.Sprintf("File not found: %s (Press Ctrl+G to generate sample template)", path)
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m scenarioFormModel) View() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s  %s\n\n",
		BrandBadgeStyle.Render("SCENARIO FLOW"),
		MutedStyle.Render("Multi-step YAML user journey runner"),
	))
	b.WriteString(m.input.View() + "\n")

	if m.infoMsg != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("✓ "+m.infoMsg) + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}

	boxW := reporter.ClampBoxWidth(m.width, 86, 44)
	b.WriteString("\n" + HintStyle.Render("Enter: Run Scenario  •  Ctrl+G: Generate Sample tensgo.yaml  •  Esc: Back"))
	return "\n" + BoxStyle.Width(boxW).Render(b.String()) + "\n"
}

type scenarioTickMsg time.Time
type scenarioDoneMsg struct{ report scenario.Report }

type scenarioRunModel struct {
	cfg       *scenario.ParsedConfig
	completed *atomic.Int64
	startTime time.Time
	spinner   spinner.Model
	width     int
	report    scenario.Report
	done      bool
}

func newScenarioRunModel(cfg *scenario.ParsedConfig, completed *atomic.Int64) scenarioRunModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = SelectedStyle
	return scenarioRunModel{
		cfg:       cfg,
		completed: completed,
		startTime: time.Now(),
		spinner:   sp,
		width:     reporter.TerminalWidth(),
	}
}

func scenarioTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return scenarioTickMsg(t) })
}

func (m scenarioRunModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, scenarioTickCmd())
}

func (m scenarioRunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil
	case scenarioTickMsg:
		if m.done {
			return m, nil
		}
		return m, scenarioTickCmd()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case scenarioDoneMsg:
		m.report = msg.report
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m scenarioRunModel) View() string {
	if m.done {
		return ""
	}
	elapsed := time.Since(m.startTime).Round(100 * time.Millisecond)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s %s  %s\n\n",
		m.spinner.View(),
		BrandBadgeStyle.Render("SCENARIO LIVE"),
		TextStyle.Render(m.cfg.Scenario.Name),
	))
	b.WriteString(fmt.Sprintf("VUs: %d  •  Steps: %d  •  Elapsed: %s / %s  •  Completed Steps: %s",
		m.cfg.Scenario.VUs,
		len(m.cfg.Scenario.Steps),
		elapsed,
		m.cfg.Duration,
		lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("%d", m.completed.Load())),
	))
	boxW := reporter.ClampBoxWidth(m.width, 86, 44)
	return "\n" + BoxStyle.Width(boxW).Render(b.String()) + "\n"
}

// RunScenarioView prompts for a YAML scenario file and executes it via TUI.
func RunScenarioView() (bool, func(string) error, error) {
	p := tea.NewProgram(newScenarioFormModel())
	finalModel, err := p.Run()
	if err != nil {
		return false, nil, err
	}
	fm := finalModel.(scenarioFormModel)
	if fm.cancelled || !fm.submitted {
		return true, nil, nil
	}

	saveFn, err := RunScenarioFileWithReport(strings.TrimSpace(fm.input.Value()), false)
	return false, saveFn, err
}

// RunScenarioFile parses and executes a scenario YAML file (CLI or TUI).
func RunScenarioFile(path string, noTUI bool) error {
	_, err := RunScenarioFileWithReport(path, noTUI)
	return err
}

// RunScenarioFileWithReport parses and executes a scenario YAML file and returns an HTML exporter.
func RunScenarioFileWithReport(path string, noTUI bool) (func(string) error, error) {
	rep, err := RunScenarioReport(path, noTUI)
	if err != nil {
		return nil, err
	}
	saveFn := func(outPath string) error {
		return reporter.GenerateHTMLSummaryReport(outPath, "SCENARIO", fmt.Sprintf("%s (%s)", rep.Name, rep.BaseURL), rep.Overall)
	}
	return saveFn, nil
}

// RunScenarioReport parses and executes a scenario YAML file and returns the full Report.
func RunScenarioReport(path string, noTUI bool) (scenario.Report, error) {
	cfg, err := scenario.ParseFile(path)
	if err != nil {
		return scenario.Report{}, err
	}

	fi, statErr := os.Stdout.Stat()
	interactive := !noTUI && statErr == nil && (fi.Mode()&os.ModeCharDevice) != 0

	var completed atomic.Int64
	var rep scenario.Report

	if interactive {
		prog := tea.NewProgram(newScenarioRunModel(cfg, &completed))
		go func() {
			r := scenario.Run(cfg, &completed)
			prog.Send(scenarioDoneMsg{report: r})
		}()
		endModel, err := prog.Run()
		if err != nil {
			return scenario.Report{}, err
		}
		rep = endModel.(scenarioRunModel).report
	} else {
		rep = scenario.Run(cfg, &completed)
	}

	reporter.RenderScenarioReport(rep)
	return rep, nil
}
