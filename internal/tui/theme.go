package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sanitizePasteMsg strips CR/LF from pasted key events so multi-line pastes
// never act as Enter/submit or inject newlines into single-line inputs.
func sanitizePasteMsg(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if !msg.Paste {
		return msg, false
	}
	cleaned := make([]rune, 0, len(msg.Runes))
	for _, r := range msg.Runes {
		if r == '\r' || r == '\n' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	msg.Type = tea.KeyRunes
	msg.Runes = cleaned
	return msg, true
}

// Version is the current release version of tensgo.
const Version = "v0.2.0"

// Larch Studios color palette.
const (
	ColorBg       = lipgloss.Color("#0B0D0C")
	ColorBrand    = lipgloss.Color("#DE8B4B")
	ColorSuccess  = lipgloss.Color("#6B8E7A")
	ColorText     = lipgloss.Color("#ECEAE6")
	ColorMuted    = lipgloss.Color("#6C7075")
	ColorAmber    = lipgloss.Color("#C89B54")
	ColorStone    = lipgloss.Color("#3A3D40")
	ColorDanger   = lipgloss.Color("#C85A48")
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBrand)

	BrandBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorBrand).
			Padding(0, 1)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(1, 2)

	SelectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBrand)

	TextStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	ErrorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorDanger)

	HintStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	BadgeHealthy = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorSuccess).
			Padding(0, 1)

	BadgeNormal = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorText).
			Padding(0, 1)

	BadgeWarning = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorAmber).
			Padding(0, 1)

	BadgeNotFound = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(ColorStone).
			Padding(0, 1)

	BadgeBottleneck = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorBrand).
			Padding(0, 1)
)

type returnPromptModel struct {
	saveHTML  func(path string) error
	savedMsg  string
	errMsg    string
	quit      bool
}

func (m returnPromptModel) Init() tea.Cmd { return nil }

func (m returnPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "h":
			if m.saveHTML != nil {
				filename := fmt.Sprintf("tensgo-report-%s.html", time.Now().Format("20060102-150405"))
				if err := m.saveHTML(filename); err != nil {
					m.errMsg = err.Error()
					m.savedMsg = ""
				} else {
					m.savedMsg = fmt.Sprintf("Saved HTML report to %s", filename)
					m.errMsg = ""
				}
			}
		case "enter", " ", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m returnPromptModel) View() string {
	line := SelectedStyle.Render("Press [Enter] to return to Main Menu")
	if m.saveHTML != nil {
		line += MutedStyle.Render("  •  ") + lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("[h] Save HTML Report")
	}
	line += MutedStyle.Render("  •  ") + TextStyle.Render("[q] to Quit")

	if m.savedMsg != "" {
		line += "\n" + lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("✓ "+m.savedMsg)
	}
	if m.errMsg != "" {
		line += "\n" + ErrorStyle.Render("✗ "+m.errMsg)
	}

	prompt := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorMuted).
		Padding(0, 2).
		Render(line)
	return prompt + "\n"
}

// waitForMenuReturn pauses after a TUI test report until the user chooses to save HTML, return, or quit.
func waitForMenuReturn(saveHTML func(path string) error) bool {
	p := tea.NewProgram(returnPromptModel{saveHTML: saveHTML})
	finalModel, err := p.Run()
	if err != nil {
		return false
	}
	return !finalModel.(returnPromptModel).quit
}
