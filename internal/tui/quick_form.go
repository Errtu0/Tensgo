package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Errtu0/tensgo/internal/engine"
)

type quickFormModel struct {
	inputs    []textinput.Model
	focusIdx  int
	errMsg    string
	submitted bool
	cancelled bool
}

func newQuickFormModel() quickFormModel {
	inputs := make([]textinput.Model, 6)

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
	inputs[2].Placeholder = "10"
	inputs[2].SetValue("10")
	inputs[2].Prompt = "Concurrency      : "
	inputs[2].CharLimit = 6
	inputs[2].Width = 20

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "10s"
	inputs[3].SetValue("10s")
	inputs[3].Prompt = "Duration         : "
	inputs[3].CharLimit = 16
	inputs[3].Width = 20

	inputs[4] = textinput.New()
	inputs[4].Placeholder = "Bearer <token> (optional)"
	inputs[4].Prompt = "Authorization    : "
	inputs[4].CharLimit = 512
	inputs[4].Width = 48

	inputs[5] = textinput.New()
	inputs[5].Placeholder = `{"key":"value"} (optional)`
	inputs[5].Prompt = "Request Body     : "
	inputs[5].CharLimit = 2048
	inputs[5].Width = 48

	for i := range inputs {
		inputs[i].PromptStyle = SelectedStyle
		inputs[i].TextStyle = TextStyle
		inputs[i].PlaceholderStyle = MutedStyle
	}

	return quickFormModel{inputs: inputs}
}

func (m quickFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m quickFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if sanitized, wasPaste := sanitizePasteMsg(msg); wasPaste {
			if len(sanitized.Runes) == 0 {
				return m, nil
			}
			cmd := m.updateInputs(sanitized)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit

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

	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *quickFormModel) updateFocus() tea.Cmd {
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

func (m *quickFormModel) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	return tea.Batch(cmds...)
}

func (m quickFormModel) buildConfig() (engine.Config, error) {
	rawURL := strings.TrimSpace(m.inputs[0].Value())
	if rawURL == "" {
		return engine.Config{}, fmt.Errorf("Target URL is required")
	}
	if strings.ContainsAny(rawURL, " \t\r\n") {
		return engine.Config{}, fmt.Errorf("Invalid URL format (e.g. http://localhost:8000/health)")
	}
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return engine.Config{}, fmt.Errorf("Invalid URL format (e.g. http://localhost:8000/health)")
	}

	method := strings.ToUpper(strings.TrimSpace(m.inputs[1].Value()))
	if method == "" {
		method = "GET"
	}

	concStr := strings.TrimSpace(m.inputs[2].Value())
	conc, err := strconv.Atoi(concStr)
	if err != nil || conc <= 0 {
		return engine.Config{}, fmt.Errorf("Concurrency must be a positive integer")
	}

	durStr := strings.TrimSpace(m.inputs[3].Value())
	dur, err := time.ParseDuration(durStr)
	if err != nil || dur <= 0 {
		return engine.Config{}, fmt.Errorf("Duration must be valid (e.g. 10s, 1m)")
	}

	headers := make(map[string]string)
	auth := strings.TrimSpace(m.inputs[4].Value())
	if auth != "" {
		headers["Authorization"] = auth
	}

	body := strings.TrimSpace(m.inputs[5].Value())
	if body != "" {
		headers["Content-Type"] = "application/json"
	}

	return engine.Config{
		URL:         rawURL,
		Method:      method,
		Headers:     headers,
		Body:        body,
		Concurrency: conc,
		Duration:    dur,
		Timeout:     5 * time.Second,
	}, nil
}

func (m quickFormModel) View() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s  %s\n\n",
		BrandBadgeStyle.Render("QUICK STRESS TEST"),
		MutedStyle.Render("Configure HTTP target & load parameters"),
	))

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View() + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}

	b.WriteString("\n" + HintStyle.Render("Tab/↑/↓: Navigate  •  Enter: Start Test  •  Esc: Back to Menu"))

	return "\n" + BoxStyle.Width(76).Render(b.String()) + "\n"
}

// RunQuickForm displays the interactive quick stress test form and returns the resulting config.
func RunQuickForm() (*engine.Config, bool, error) {
	p := tea.NewProgram(newQuickFormModel())
	finalModel, err := p.Run()
	if err != nil {
		return nil, false, err
	}

	fm := finalModel.(quickFormModel)
	if fm.cancelled || !fm.submitted {
		return nil, true, nil
	}

	cfg, err := fm.buildConfig()
	if err != nil {
		return nil, false, err
	}
	return &cfg, false, nil
}
