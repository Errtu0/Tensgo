package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/reporter"
)

type menuChoice int

const (
	choiceNone menuChoice = iota - 1
	choiceQuickTest
	choiceOpenAPIScan
	choiceWhatIfChaos
	choiceScenarioFlow
)

type menuItem struct {
	title string
	desc  string
	badge string
}

type mainMenuModel struct {
	items    []menuItem
	cursor   int
	chosen   menuChoice
	quitting bool
}

func newMainMenuModel() mainMenuModel {
	return mainMenuModel{
		chosen: choiceNone,
		items: []menuItem{
			{
				title: "Quick Stress Test",
				desc:  "Interactive form for URL, method, concurrency, duration & auth",
				badge: "CORE",
			},
			{
				title: "OpenAPI / Swagger Scan",
				desc:  "Auto-discover endpoints from JSON spec and benchmark selected routes",
				badge: "OPENAPI",
			},
			{
				title: "What-If Chaos Profiles",
				desc:  "Spike, Ramp-Up (Breaking Point) & Soak resilience profiles",
				badge: "CHAOS",
			},
			{
				title: "Scenario Flow (tensgo.yaml)",
				desc:  "Multi-step stateful YAML user journeys & token extraction",
				badge: "YAML",
			},
		},
	}
}

func (m mainMenuModel) Init() tea.Cmd { return nil }

func (m mainMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case "1":
			m.chosen = choiceQuickTest
			return m, tea.Quit
		case "2":
			m.chosen = choiceOpenAPIScan
			return m, tea.Quit
		case "3":
			m.chosen = choiceWhatIfChaos
			return m, tea.Quit
		case "4":
			m.chosen = choiceScenarioFlow
			return m, tea.Quit

		case "enter":
			m.chosen = menuChoice(m.cursor)
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mainMenuModel) View() string {
	var b strings.Builder

	header := fmt.Sprintf("%s  %s",
		BrandBadgeStyle.Render("⬡ TENSGO "+Version),
		MutedStyle.Render("by Larch Studios"),
	)
	b.WriteString(header + "\n\n")

	for i, item := range m.items {
		cursor := "  "
		title := TextStyle.Render(item.title)
		if i == m.cursor {
			cursor = SelectedStyle.Render("▸ ")
			title = SelectedStyle.Render(item.title)
		}

		badgeStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorSuccess).
			Padding(0, 1)

		b.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, title, badgeStyle.Render(item.badge)))
		b.WriteString(fmt.Sprintf("    %s\n\n", MutedStyle.Render(item.desc)))
	}

	b.WriteString(HintStyle.Render("↑/↓ or j/k: Navigate  •  Enter: Select  •  q: Quit"))
	return "\n" + BoxStyle.Width(78).Render(b.String()) + "\n"
}

// RunMainMenu launches the interactive Bubble Tea main control center.
func RunMainMenu() error {
	for {
		p := tea.NewProgram(newMainMenuModel())
		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		fm := finalModel.(mainMenuModel)
		if fm.quitting || fm.chosen == choiceNone {
			return nil
		}

		switch fm.chosen {
		case choiceQuickTest:
			cfg, back, err := RunQuickForm()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
			if back || cfg == nil {
				continue
			}

			results, elapsed, err := reporter.RunWithProgress(*cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error running test: %v\n", err)
				continue
			}
			summary := metrics.Calculate(results, elapsed)
			reporter.RenderTerminalReport(*cfg, summary, nil, false)
			saveFn := func(path string) error {
				return reporter.GenerateHTMLReport(path, *cfg, summary, nil, false)
			}
			if !waitForMenuReturn(saveFn) {
				return nil
			}

		case choiceOpenAPIScan:
			source, token, conc, dur, back, err := RunOpenAPIScanForm()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
			if back {
				continue
			}
			saveFn, err := RunOpenAPIPickerWithReport(source, token, conc, dur, 5*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "OpenAPI scan error: %v\n", err)
				if !waitForMenuReturn(nil) {
					return nil
				}
				continue
			}
			if !waitForMenuReturn(saveFn) {
				return nil
			}

		case choiceWhatIfChaos:
			back, saveFn, err := RunChaosView()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Chaos profile error: %v\n", err)
				if !waitForMenuReturn(nil) {
					return nil
				}
				continue
			}
			if back {
				continue
			}
			if !waitForMenuReturn(saveFn) {
				return nil
			}

		case choiceScenarioFlow:
			back, saveFn, err := RunScenarioView()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Scenario error: %v\n", err)
				if !waitForMenuReturn(nil) {
					return nil
				}
				continue
			}
			if back {
				continue
			}
			if !waitForMenuReturn(saveFn) {
				return nil
			}
		}
	}
}
