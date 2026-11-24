package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/openapi"
	"github.com/Errtu0/tensgo/internal/reporter"
)

type openAPIScanForm struct {
	inputs    []textinput.Model
	focusIdx  int
	width     int
	errMsg    string
	submitted bool
	cancelled bool
}

func newOpenAPIScanForm() openAPIScanForm {
	inputs := make([]textinput.Model, 4)

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "http://localhost:5001/openapi.json or ./openapi.json"
	inputs[0].Prompt = "Spec URL / Path  : "
	inputs[0].CharLimit = 512
	inputs[0].Width = 52
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Bearer eyJ... (optional)"
	inputs[1].Prompt = "Authorization    : "
	inputs[1].CharLimit = 1024
	inputs[1].Width = 52

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "10"
	inputs[2].SetValue("10")
	inputs[2].Prompt = "Concurrency      : "
	inputs[2].CharLimit = 6
	inputs[2].Width = 20

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "3s"
	inputs[3].SetValue("3s")
	inputs[3].Prompt = "Duration / Route : "
	inputs[3].CharLimit = 16
	inputs[3].Width = 20

	for i := range inputs {
		inputs[i].PromptStyle = SelectedStyle
		inputs[i].TextStyle = TextStyle
		inputs[i].PlaceholderStyle = MutedStyle
	}

	return openAPIScanForm{inputs: inputs, width: reporter.TerminalWidth()}
}

func (m openAPIScanForm) Init() tea.Cmd { return textinput.Blink }

func (m openAPIScanForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
			boxW := reporter.ClampBoxWidth(m.width, 80, 42)
			inW := max(16, boxW-28)
			m.inputs[0].Width = inW
			m.inputs[1].Width = inW
		}
		return m, nil
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
		case "tab", "down":
			m.focusIdx = (m.focusIdx + 1) % len(m.inputs)
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focusIdx = (m.focusIdx - 1 + len(m.inputs)) % len(m.inputs)
			return m, m.updateFocus()
		case "enter":
			if strings.TrimSpace(m.inputs[0].Value()) == "" {
				m.errMsg = "OpenAPI URL or file path is required"
				return m, nil
			}
			if conc, err := strconv.Atoi(strings.TrimSpace(m.inputs[2].Value())); err != nil || conc <= 0 {
				m.errMsg = "Concurrency must be a positive integer"
				return m, nil
			}
			if dur, err := time.ParseDuration(strings.TrimSpace(m.inputs[3].Value())); err != nil || dur <= 0 {
				m.errMsg = "Duration must be valid (e.g. 5s, 10s)"
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

func (m *openAPIScanForm) updateFocus() tea.Cmd {
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

func (m openAPIScanForm) View() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s  %s\n\n",
		BrandBadgeStyle.Render("OPENAPI / SWAGGER SCAN"),
		MutedStyle.Render("Auto-discover routes & generate payloads"),
	))
	for i := range m.inputs {
		b.WriteString(m.inputs[i].View() + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}
	boxW := reporter.ClampBoxWidth(m.width, 80, 42)
	b.WriteString("\n" + HintStyle.Render("Tab/↑/↓: Navigate  •  Enter: Parse Schema  •  Esc: Back"))
	return "\n" + BoxStyle.Width(boxW).Render(b.String()) + "\n"
}

// RunOpenAPIScanForm prompts the user for an OpenAPI spec source and test parameters.
func RunOpenAPIScanForm() (source string, token string, concurrency int, duration time.Duration, cancelled bool, err error) {
	p := tea.NewProgram(newOpenAPIScanForm())
	finalModel, err := p.Run()
	if err != nil {
		return "", "", 0, 0, false, err
	}
	fm := finalModel.(openAPIScanForm)
	if fm.cancelled || !fm.submitted {
		return "", "", 0, 0, true, nil
	}
	conc, _ := strconv.Atoi(strings.TrimSpace(fm.inputs[2].Value()))
	dur, _ := time.ParseDuration(strings.TrimSpace(fm.inputs[3].Value()))
	return strings.TrimSpace(fm.inputs[0].Value()), strings.TrimSpace(fm.inputs[1].Value()), conc, dur, false, nil
}

type routePickerModel struct {
	spec      *openapi.Spec
	selected  map[int]bool
	cursor    int
	offset    int
	width     int
	confirmed bool
	cancelled bool
	errMsg    string
}

func newRoutePickerModel(spec *openapi.Spec) routePickerModel {
	return routePickerModel{
		spec:     spec,
		selected: make(map[int]bool, len(spec.Endpoints)),
		width:    reporter.TerminalWidth(),
	}
}

func (m routePickerModel) Init() tea.Cmd { return nil }

func (m routePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	const maxVisible = 14
	total := len(m.spec.Endpoints)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < total-1 {
				m.cursor++
				if m.cursor >= m.offset+maxVisible {
					m.offset = m.cursor - maxVisible + 1
				}
			}

		case " ":
			m.selected[m.cursor] = !m.selected[m.cursor]
			m.errMsg = ""

		case "a":
			allSelected := true
			for i := range m.spec.Endpoints {
				if !m.selected[i] {
					allSelected = false
					break
				}
			}
			for i := range m.spec.Endpoints {
				m.selected[i] = !allSelected
			}
			m.errMsg = ""

		case "g":
			for i, ep := range m.spec.Endpoints {
				m.selected[i] = ep.Method == "GET"
			}
			m.errMsg = ""

		case "enter":
			count := 0
			for i := range m.spec.Endpoints {
				if m.selected[i] {
					count++
				}
			}
			if count == 0 {
				m.errMsg = "Select at least one endpoint to run"
				return m, nil
			}
			m.confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m routePickerModel) View() string {
	const maxVisible = 14
	var b strings.Builder

	selectedCount := 0
	for i := range m.spec.Endpoints {
		if m.selected[i] {
			selectedCount++
		}
	}

	b.WriteString(fmt.Sprintf("%s  %s\n",
		BrandBadgeStyle.Render("ROUTE PICKER"),
		TitleStyle.Render(m.spec.Title),
	))
	b.WriteString(MutedStyle.Render(fmt.Sprintf("Base URL: %s  •  Selected: %d / %d routes",
		m.spec.BaseURL, selectedCount, len(m.spec.Endpoints),
	)) + "\n\n")

	end := m.offset + maxVisible
	if end > len(m.spec.Endpoints) {
		end = len(m.spec.Endpoints)
	}

	boxWidth := reporter.ClampBoxWidth(m.width, 104, 48)
	// BoxStyle border (2) + horizontal padding (4) + extra safety margin (8) = 14,
	// plus fixed prefix ("▸ [x] METHOD " = 13 chars).
	baseAvail := boxWidth - 14 - 13
	if baseAvail < 16 {
		baseAvail = 16
	}

	for i := m.offset; i < end; i++ {
		ep := m.spec.Endpoints[i]
		cursor := "  "
		if i == m.cursor {
			cursor = SelectedStyle.Render("▸ ")
		}

		check := MutedStyle.Render("[ ]")
		if m.selected[i] {
			check = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("[x]")
		}

		methodStyle := lipgloss.NewStyle().Bold(true).Width(7).Foreground(ColorBrand)
		if ep.Method == "GET" {
			methodStyle = methodStyle.Foreground(ColorSuccess)
		}

		warnTag := ""
		warnLen := 0
		if ep.Method == "DELETE" || ep.Method == "PATCH" {
			warnTag = ErrorStyle.Render("[!] ")
			warnLen = 4
		}

		authTag := ""
		authLen := 0
		if ep.RequiresAuth {
			authTag = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand).Render("[AUTH] ")
			authLen = 7
		}

		avail := baseAvail - warnLen - authLen
		if avail < 10 {
			avail = 10
		}
		pathRunes := []rune(ep.Path)
		summaryStr := ""
		if ep.Summary != "" {
			summaryStr = " — " + ep.Summary
		}

		fullRunes := []rune(ep.Path + summaryStr)
		if len(fullRunes) > avail {
			if avail > 3 {
				fullRunes = append(fullRunes[:avail-3], '.', '.', '.')
			} else {
				fullRunes = fullRunes[:avail]
			}
		}

		var renderedText string
		if len(fullRunes) <= len(pathRunes) {
			txt := string(fullRunes)
			if i == m.cursor {
				renderedText = SelectedStyle.Render(txt)
			} else {
				renderedText = TextStyle.Render(txt)
			}
		} else {
			pPart := string(fullRunes[:len(pathRunes)])
			sPart := string(fullRunes[len(pathRunes):])
			if i == m.cursor {
				renderedText = SelectedStyle.Render(pPart) + MutedStyle.Render(sPart)
			} else {
				renderedText = TextStyle.Render(pPart) + MutedStyle.Render(sPart)
			}
		}

		b.WriteString(fmt.Sprintf("%s%s %s %s%s%s\n", cursor, check, methodStyle.Render(ep.Method), warnTag, authTag, renderedText))
	}

	if len(m.spec.Endpoints) > maxVisible {
		b.WriteString("\n" + MutedStyle.Render(fmt.Sprintf("  Showing %d-%d of %d routes", m.offset+1, end, len(m.spec.Endpoints))) + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}

	b.WriteString("\n" + HintStyle.Render("↑/↓ or j/k: Move  •  Space: Toggle  •  a: All  •  g: GET only  •  Enter: Run  •  Esc: Back"))

	return "\n" + BoxStyle.Width(boxWidth).Render(b.String()) + "\n"
}

// RunOpenAPIPicker parses an OpenAPI spec, launches the interactive route picker,
// executes load tests against selected routes, and renders the bottleneck comparison matrix.
func RunOpenAPIPicker(source string, token string, concurrency int, duration time.Duration, timeout time.Duration) error {
	_, err := RunOpenAPIPickerWithReport(source, token, concurrency, duration, timeout)
	return err
}

// RunOpenAPIPickerWithReport runs the OpenAPI picker and returns an optional HTML report exporter.
func RunOpenAPIPickerWithReport(source string, token string, concurrency int, duration time.Duration, timeout time.Duration) (func(string) error, error) {
	spec, err := openapi.Parse(source)
	if err != nil {
		return nil, err
	}

	if spec.BaseURL == "" {
		spec.BaseURL = "http://localhost:8080"
	}

	p := tea.NewProgram(newRoutePickerModel(spec))
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	fm := finalModel.(routePickerModel)
	if fm.cancelled || !fm.confirmed {
		return nil, nil
	}

	var chosen []openapi.EndpointSpec
	for i, ep := range spec.Endpoints {
		if fm.selected[i] {
			chosen = append(chosen, ep)
		}
	}

	if len(chosen) == 0 {
		return nil, nil
	}

	protectedCount := 0
	for _, ep := range chosen {
		if ep.RequiresAuth {
			protectedCount++
		}
	}

	if protectedCount > 0 && strings.TrimSpace(token) == "" {
		acquiredToken, aborted := runAuthPrompt(spec, protectedCount, timeout)
		if aborted {
			return nil, nil
		}
		if acquiredToken != "" {
			token = acquiredToken
			fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("✓ Authentication successful, Bearer token applied."))
		}
	}

	fi, statErr := os.Stdout.Stat()
	interactive := statErr == nil && (fi.Mode()&os.ModeCharDevice) != 0

	var (
		matrixResults []reporter.RouteResult
		allRawResults []metrics.Result
		totalElapsed  time.Duration
	)
	baseURL := strings.TrimRight(spec.BaseURL, "/")

	for idx, ep := range chosen {
		targetURL := baseURL + ep.Path
		if !strings.HasPrefix(ep.Path, "/") {
			targetURL = baseURL + "/" + ep.Path
		}

		headers := make(map[string]string)
		if token != "" {
			headers["Authorization"] = token
		}
		if ep.SampleBody != "" {
			headers["Content-Type"] = "application/json"
		}

		cfg := engine.Config{
			URL:         targetURL,
			Method:      ep.Method,
			Headers:     headers,
			Body:        ep.SampleBody,
			Concurrency: concurrency,
			Duration:    duration,
			Timeout:     timeout,
		}

		fmt.Printf("\n[%d/%d] Testing %s %s\n", idx+1, len(chosen), ep.Method, ep.Path)

		var (
			results []metrics.Result
			elapsed time.Duration
		)
		if interactive {
			results, elapsed, err = reporter.RunWithProgress(cfg)
			if err != nil {
				results, elapsed = engine.Run(cfg)
			}
		} else {
			results, elapsed = engine.Run(cfg)
		}

		allRawResults = append(allRawResults, results...)
		totalElapsed += elapsed

		summary := metrics.Calculate(results, elapsed)
		matrixResults = append(matrixResults, reporter.RouteResult{
			Endpoint: ep.Path,
			Method:   ep.Method,
			Summary:  summary,
		})
	}

	reporter.RenderComparisonMatrix(matrixResults)
	overall := metrics.Calculate(allRawResults, totalElapsed)
	saveFn := func(path string) error {
		return reporter.GenerateHTMLSummaryReport(path, "OPENAPI SCAN", fmt.Sprintf("%s (%s)", spec.Title, spec.BaseURL), overall)
	}
	return saveFn, nil
}

type authPromptModel struct {
	spec           *openapi.Spec
	protectedCount int
	timeout        time.Duration
	manualMode     bool
	loginInputs    []textinput.Model
	loginFields    []string
	manualInput    textinput.Model
	focusIdx       int
	width          int
	errMsg         string
	token          string
	skipped        bool
	aborted        bool
}

func newAuthPromptModel(spec *openapi.Spec, protectedCount int, timeout time.Duration) authPromptModel {
	fields := spec.LoginFields
	if len(fields) == 0 {
		fields = []string{"email", "password"}
	}

	loginInputs := make([]textinput.Model, len(fields))
	for i, f := range fields {
		ti := textinput.New()
		ti.Prompt = fmt.Sprintf("%-14s : ", f)
		ti.CharLimit = 256
		ti.Width = 44
		ti.PromptStyle = SelectedStyle
		ti.TextStyle = TextStyle
		ti.PlaceholderStyle = MutedStyle

		lower := strings.ToLower(f)
		if strings.Contains(lower, "pass") || strings.Contains(lower, "secret") {
			ti.Placeholder = "••••••••"
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		} else if strings.Contains(lower, "email") {
			ti.Placeholder = "user@example.com"
		} else {
			ti.Placeholder = f
		}
		if i == 0 {
			ti.Focus()
		} else {
			ti.PromptStyle = MutedStyle
		}
		loginInputs[i] = ti
	}

	manual := textinput.New()
	manual.Prompt = "Bearer Token   : "
	manual.Placeholder = "Bearer eyJ... (or raw JWT)"
	manual.CharLimit = 2048
	manual.Width = 52
	manual.PromptStyle = SelectedStyle
	manual.TextStyle = TextStyle
	manual.PlaceholderStyle = MutedStyle

	manualMode := spec.LoginEndpoint == nil
	if manualMode {
		manual.Focus()
	}

	return authPromptModel{
		spec:           spec,
		protectedCount: protectedCount,
		timeout:        timeout,
		manualMode:     manualMode,
		loginInputs:    loginInputs,
		loginFields:    fields,
		manualInput:    manual,
		width:          reporter.TerminalWidth(),
	}
}

func (m authPromptModel) Init() tea.Cmd { return textinput.Blink }

func (m authPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
			boxW := reporter.ClampBoxWidth(m.width, 86, 44)
			inW := max(16, boxW-28)
			m.manualInput.Width = inW
			for i := range m.loginInputs {
				m.loginInputs[i].Width = inW
			}
		}
		return m, nil
	case tea.KeyMsg:
		if sanitized, wasPaste := sanitizePasteMsg(msg); wasPaste {
			if len(sanitized.Runes) == 0 {
				return m, nil
			}
			if m.manualMode {
				var cmd tea.Cmd
				m.manualInput, cmd = m.manualInput.Update(sanitized)
				return m, cmd
			}
			cmds := make([]tea.Cmd, len(m.loginInputs))
			for i := range m.loginInputs {
				m.loginInputs[i], cmds[i] = m.loginInputs[i].Update(sanitized)
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit

		case "esc":
			m.skipped = true
			return m, tea.Quit

		case "ctrl+t":
			if m.spec.LoginEndpoint != nil {
				m.manualMode = !m.manualMode
				m.errMsg = ""
				if m.manualMode {
					m.manualInput.Focus()
				} else {
					m.manualInput.Blur()
					return m, m.updateLoginFocus()
				}
			}
			return m, nil

		case "tab", "down":
			if !m.manualMode && len(m.loginInputs) > 0 {
				m.focusIdx = (m.focusIdx + 1) % len(m.loginInputs)
				return m, m.updateLoginFocus()
			}
			return m, nil

		case "shift+tab", "up":
			if !m.manualMode && len(m.loginInputs) > 0 {
				m.focusIdx = (m.focusIdx - 1 + len(m.loginInputs)) % len(m.loginInputs)
				return m, m.updateLoginFocus()
			}
			return m, nil

		case "enter":
			if m.manualMode {
				tok := strings.TrimSpace(m.manualInput.Value())
				if tok == "" {
					m.errMsg = "Token cannot be empty (press Esc to skip)"
					return m, nil
				}
				if !strings.HasPrefix(strings.ToLower(tok), "bearer ") {
					tok = "Bearer " + tok
				}
				m.token = tok
				return m, tea.Quit
			}

			creds := make(map[string]string, len(m.loginFields))
			for i, f := range m.loginFields {
				val := strings.TrimSpace(m.loginInputs[i].Value())
				if val == "" {
					m.errMsg = fmt.Sprintf("Field '%s' is required", f)
					return m, nil
				}
				creds[f] = val
			}

			tok, err := openapi.Login(m.spec.BaseURL, *m.spec.LoginEndpoint, creds, m.timeout)
			if err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.token = tok
			return m, tea.Quit
		}
	}

	if m.manualMode {
		var cmd tea.Cmd
		m.manualInput, cmd = m.manualInput.Update(msg)
		return m, cmd
	}

	cmds := make([]tea.Cmd, len(m.loginInputs))
	for i := range m.loginInputs {
		m.loginInputs[i], cmds[i] = m.loginInputs[i].Update(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *authPromptModel) updateLoginFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.loginInputs))
	for i := range m.loginInputs {
		if i == m.focusIdx {
			cmds[i] = m.loginInputs[i].Focus()
			m.loginInputs[i].PromptStyle = SelectedStyle
		} else {
			m.loginInputs[i].Blur()
			m.loginInputs[i].PromptStyle = MutedStyle
		}
	}
	return tea.Batch(cmds...)
}

func (m authPromptModel) View() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s  %s\n\n",
		BrandBadgeStyle.Render("AUTO-AUTH"),
		SelectedStyle.Render(fmt.Sprintf("⚠ %d selected routes require authentication (Bearer / Auth).", m.protectedCount)),
	))
	b.WriteString(MutedStyle.Render("Sign in to continue, enter a manual token, or press Esc to run without auth.\n\n"))

	if m.manualMode {
		b.WriteString(TextStyle.Render("Mode: Manual Token Input") + "\n\n")
		b.WriteString(m.manualInput.View() + "\n")
	} else {
		b.WriteString(TextStyle.Render(fmt.Sprintf("Mode: Auto-Login (%s %s)", m.spec.LoginEndpoint.Method, m.spec.LoginEndpoint.Path)) + "\n\n")
		for i := range m.loginInputs {
			b.WriteString(m.loginInputs[i].View() + "\n")
		}
	}

	if m.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("✗ "+m.errMsg) + "\n")
	}

	hints := "Enter: Sign In & Start Test  •  Esc: Continue Without Auth"
	if m.spec.LoginEndpoint != nil {
		hints += "  •  Ctrl+T: Toggle Manual Token / Auto-Login"
	}
	boxW := reporter.ClampBoxWidth(m.width, 86, 44)
	b.WriteString("\n" + HintStyle.Render(hints))

	return "\n" + BoxStyle.Width(boxW).Render(b.String()) + "\n"
}

func runAuthPrompt(spec *openapi.Spec, protectedCount int, timeout time.Duration) (string, bool) {
	p := tea.NewProgram(newAuthPromptModel(spec, protectedCount, timeout))
	finalModel, err := p.Run()
	if err != nil {
		return "", false
	}
	fm := finalModel.(authPromptModel)
	if fm.aborted {
		return "", true
	}
	if fm.skipped {
		return "", false
	}
	return fm.token, false
}
