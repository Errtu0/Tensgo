package tui

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/profiles"
	"github.com/Errtu0/tensgo/internal/reporter"
)

type chaosProfileOption struct {
	key   string
	label string
	desc  string
}

var chaosOptions = []chaosProfileOption{
	{profiles.ProfileSpike, "Spike Test", "5s Baseline (20 VU) -> 5s Spike (Peak VU) -> Recovery"},
	{profiles.ProfileRampUp, "Ramp-Up (Breaking Point)", "Starts at 10 VU, +30 VU / 3s until p95>1s or err>5%"},
	{profiles.ProfileSoak, "Soak Test", "Sustained load (50 VU) tracking Q1 vs Q4 P95 degradation"},
}

type chaosFormModel struct {
	profileIdx int
	inputs     []textinput.Model
	focusIdx   int
	errMsg     string
	submitted  bool
	cancelled  bool
}

func newChaosFormModel() chaosFormModel {
	inputs := make([]textinput.Model, 4)

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "http://localhost:8000/health"
	inputs[0].Prompt = "Target URL       : "
	inputs[0].CharLimit = 512
	inputs[0].Width = 48
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "GET"
	inputs[1].SetValue("GET")
	inputs[1].Prompt = "Method           : "
	inputs[1].CharLimit = 16
	inputs[1].Width = 20

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "300 (Spike) / 50 (Soak)"
	inputs[2].SetValue("300")
	inputs[2].Prompt = "Peak / Target VU : "
	inputs[2].CharLimit = 6
	inputs[2].Width = 20

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "Bearer <token> (optional)"
	inputs[3].Prompt = "Authorization    : "
	inputs[3].CharLimit = 1024
	inputs[3].Width = 48

	for i := range inputs {
		inputs[i].PromptStyle = SelectedStyle
		inputs[i].TextStyle = TextStyle
		inputs[i].PlaceholderStyle = MutedStyle
	}

	return chaosFormModel{inputs: inputs}
}

func (m chaosFormModel) Init() tea.Cmd { return textinput.Blink }

func (m chaosFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if sanitized, wasPaste := sanitizePasteMsg(msg); wasPaste {
			if len(sanitized.Runes) == 0 {
				return m, nil
			}
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := range m.inputs {
				m.inputs[i], cmds[i] = m.inputs[i].Update(sanitized)
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "left":
			m.profileIdx = (m.profileIdx - 1 + len(chaosOptions)) % len(chaosOptions)
			return m, nil
		case "right":
			m.profileIdx = (m.profileIdx + 1) % len(chaosOptions)
			return m, nil
		case "tab", "down":
			m.focusIdx = (m.focusIdx + 1) % len(m.inputs)
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focusIdx = (m.focusIdx - 1 + len(m.inputs)) % len(m.inputs)
			return m, m.updateFocus()
		case "enter":
			if _, err := m.buildConfig(); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit
		}
	}

	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *chaosFormModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		if i == m.focusIdx {
			cmds[i] = m.inputs[i].Focus()
			m.inputs[i].PromptStyle = SelectedStyle
		} else {
			m.inputs[i].Blur()
			m.inputs[i].PromptStyle = MutedStyle
		}
	}
	return tea.Batch(cmds...)
}

func (m chaosFormModel) buildConfig() (profiles.Config, error) {
	rawURL := strings.TrimSpace(m.inputs[0].Value())
	if rawURL == "" {
		return profiles.Config{}, fmt.Errorf("Target URL is required")
	}
	if strings.ContainsAny(rawURL, " \t\r\n") {
		return profiles.Config{}, fmt.Errorf("Invalid URL format (e.g. http://localhost:8000/health)")
	}
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return profiles.Config{}, fmt.Errorf("Invalid URL format (e.g. http://localhost:8000/health)")
	}

	method := strings.ToUpper(strings.TrimSpace(m.inputs[1].Value()))
	if method == "" {
		method = "GET"
	}

	vus, err := strconv.Atoi(strings.TrimSpace(m.inputs[2].Value()))
	if err != nil || vus <= 0 {
		return profiles.Config{}, fmt.Errorf("Peak / Target VU must be a positive integer")
	}

	headers := make(map[string]string)
	if auth := strings.TrimSpace(m.inputs[3].Value()); auth != "" {
		headers["Authorization"] = auth
	}

	opt := chaosOptions[m.profileIdx]
	cfg := profiles.Config{
		Type:    opt.key,
		URL:     rawURL,
		Method:  method,
		Headers: headers,
		Timeout: 5 * time.Second,
	}

	switch opt.key {
	case profiles.ProfileSpike:
		cfg.BaselineVUs = 20
		cfg.PeakVUs = vus
		cfg.PhaseDuration = 5 * time.Second
	case profiles.ProfileRampUp:
		cfg.StartVUs = 10
		cfg.StepVUs = 30
		cfg.MaxVUs = max(vus, 100)
		cfg.PhaseDuration = 3 * time.Second
	case profiles.ProfileSoak:
		cfg.SoakVUs = vus
		cfg.SoakDuration = 30 * time.Second
	}

	return cfg, nil
}

func (m chaosFormModel) View() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s  %s\n\n",
		BrandBadgeStyle.Render("WHAT-IF CHAOS"),
		MutedStyle.Render("Dynamic VU & resilience profiles"),
	))

	b.WriteString(TextStyle.Render("Profile Type     : "))
	for i, opt := range chaosOptions {
		if i == m.profileIdx {
			b.WriteString(BrandBadgeStyle.Render(opt.label) + " ")
		} else {
			b.WriteString(MutedStyle.Render("["+opt.label+"]") + " ")
		}
	}
	b.WriteString("\n")
	b.WriteString("                   " + MutedStyle.Render(chaosOptions[m.profileIdx].desc) + "\n\n")

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View() + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}

	b.WriteString("\n" + HintStyle.Render("←/→: Switch Profile  •  Tab/↑/↓: Navigate  •  Enter: Start  •  Esc: Back"))
	return "\n" + BoxStyle.Width(86).Render(b.String()) + "\n"
}

type chaosTickMsg time.Time
type chaosDoneMsg struct{ report profiles.Report }

type chaosRunModel struct {
	runner  *profiles.Runner
	cancel  context.CancelFunc
	spinner spinner.Model
	report  profiles.Report
	done    bool
}

func newChaosRunModel(runner *profiles.Runner, cancel context.CancelFunc) chaosRunModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = SelectedStyle
	return chaosRunModel{
		runner:  runner,
		cancel:  cancel,
		spinner: sp,
	}
}

func chaosTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg { return chaosTickMsg(t) })
}

func (m chaosRunModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, chaosTickCmd())
}

func (m chaosRunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.cancel()
			return m, nil
		}
	case chaosTickMsg:
		if m.done {
			return m, nil
		}
		return m, chaosTickCmd()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case chaosDoneMsg:
		m.report = msg.report
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m chaosRunModel) View() string {
	if m.done {
		return ""
	}
	snap := m.runner.Snapshot()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s %s  %s\n\n",
		m.spinner.View(),
		BrandBadgeStyle.Render("CHAOS LIVE"),
		SelectedStyle.Render(fmt.Sprintf("Phase: %s", snap.Phase)),
	))
	b.WriteString(fmt.Sprintf("Active VUs : %s    Elapsed : %s    Requests : %s\n",
		lipgloss.NewStyle().Bold(true).Foreground(ColorBrand).Render(fmt.Sprintf("%d", snap.ActiveVUs)),
		TextStyle.Render(snap.Elapsed.Round(100*time.Millisecond).String()),
		lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("%d", snap.TotalRequests)),
	))
	b.WriteString(fmt.Sprintf("Live RPS   : %s    Live P95 : %s    Error %%  : %s\n\n",
		TextStyle.Render(fmt.Sprintf("%.1f req/s", snap.CurrentRPS)),
		TextStyle.Render(metrics.FormatDuration(snap.CurrentP95)),
		TextStyle.Render(fmt.Sprintf("%.2f%%", snap.CurrentErrPct)),
	))
	b.WriteString(HintStyle.Render("Ctrl+C / Esc: Stop safely"))
	return "\n" + BoxStyle.Width(80).Render(b.String()) + "\n"
}

func renderChaosReport(cfg profiles.Config, rep profiles.Report) {
	var errPct float64
	if rep.Summary.TotalRequests > 0 {
		errPct = float64(rep.Summary.ErrorCount) / float64(rep.Summary.TotalRequests) * 100.0
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s  %s\n",
		BrandBadgeStyle.Render("CHAOS REPORT"),
		TitleStyle.Render(strings.ToUpper(rep.Type)+" • "+cfg.Method+" "+cfg.URL),
	))
	b.WriteString(MutedStyle.Render(fmt.Sprintf("Duration: %s  •  Requests: %d  •  RPS: %.2f  •  P50: %s  •  P95: %s  •  Error: %.2f%%\n\n",
		rep.Summary.TotalDuration.Round(time.Millisecond),
		rep.Summary.TotalRequests,
		rep.Summary.RPS,
		metrics.FormatDuration(rep.Summary.P50),
		metrics.FormatDuration(rep.Summary.P95),
		errPct,
	)))

	switch rep.Type {
	case profiles.ProfileSpike:
		recText := ErrorStyle.Render("Did not fully recover within window")
		if rep.Recovered {
			recText = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(rep.RecoveryTime.String())
		}
		b.WriteString(fmt.Sprintf("Peak P95      : %s\n", SelectedStyle.Render(metrics.FormatDuration(rep.PeakP95))))
		b.WriteString(fmt.Sprintf("Peak Error %%  : %s\n", SelectedStyle.Render(fmt.Sprintf("%.2f%%", rep.PeakErrorRate))))
		b.WriteString(fmt.Sprintf("Recovery Time : %s\n", recText))

	case profiles.ProfileRampUp:
		if rep.StoppedEarly {
			b.WriteString(fmt.Sprintf("Breaking Point       : %s (%s)\n",
				ErrorStyle.Render(fmt.Sprintf("%d VUs / %.1f RPS", rep.BreakingPointVUs, rep.BreakingPointRPS)),
				MutedStyle.Render(rep.BreakingReason),
			))
		} else {
			b.WriteString(fmt.Sprintf("Breaking Point       : %s\n",
				lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("Not reached up to max VUs"),
			))
		}
		b.WriteString(fmt.Sprintf("Max Healthy Capacity : %s\n",
			lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("%d VUs / %.1f RPS", rep.MaxHealthyVUs, rep.MaxHealthyRPS)),
		))

	case profiles.ProfileSoak:
		b.WriteString(fmt.Sprintf("First Quarter P95   : %s\n", TextStyle.Render(metrics.FormatDuration(rep.FirstQuarterP95))))
		b.WriteString(fmt.Sprintf("Last Quarter P95    : %s\n", TextStyle.Render(metrics.FormatDuration(rep.LastQuarterP95))))
		degStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
		if rep.DegradationPct > 15.0 {
			degStyle = SelectedStyle
		}
		b.WriteString(fmt.Sprintf("Latency Degradation : %s\n", degStyle.Render(fmt.Sprintf("%+.1f%%", rep.DegradationPct))))
	}

	fmt.Println()
	fmt.Println(BoxStyle.Width(86).Render(b.String()))
	fmt.Println()
}

// RunChaosView launches the Phase 3 What-If Chaos Profiles TUI workflow.
func RunChaosView() (bool, func(string) error, error) {
	p := tea.NewProgram(newChaosFormModel())
	finalModel, err := p.Run()
	if err != nil {
		return false, nil, err
	}
	fm := finalModel.(chaosFormModel)
	if fm.cancelled || !fm.submitted {
		return true, nil, nil
	}

	cfg, err := fm.buildConfig()
	if err != nil {
		return false, nil, err
	}

	runner := profiles.NewRunner(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runModel := newChaosRunModel(runner, cancel)
	runProg := tea.NewProgram(runModel)

	go func() {
		rep := runner.Run(ctx)
		runProg.Send(chaosDoneMsg{report: rep})
	}()

	endModel, err := runProg.Run()
	if err != nil {
		return false, nil, err
	}
	rm := endModel.(chaosRunModel)
	renderChaosReport(cfg, rm.report)

	saveFn := func(path string) error {
		return reporter.GenerateHTMLSummaryReport(path, "CHAOS: "+strings.ToUpper(rm.report.Type), cfg.Method+" "+cfg.URL, rm.report.Summary)
	}
	return false, saveFn, nil
}
