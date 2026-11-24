package reporter

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/scenario"
)

// RenderScenarioReport prints the overall and step-by-step scenario metrics in a Larch table.
func RenderScenarioReport(rep scenario.Report) {
	var (
		colorBg      = lipgloss.Color("#0B0D0C")
		colorBrand   = lipgloss.Color("#DE8B4B")
		colorSuccess = lipgloss.Color("#6B8E7A")
		colorText    = lipgloss.Color("#ECEAE6")
		colorMuted   = lipgloss.Color("#6C7075")
	)

	brandBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorBg).
		Background(colorBrand).
		Padding(0, 1).
		Render("SCENARIO FLOW")

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBrand)
	cellStyle := lipgloss.NewStyle().Foreground(colorText)
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)

	var overallErrPct float64
	if rep.Overall.TotalRequests > 0 {
		overallErrPct = float64(rep.Overall.ErrorCount) / float64(rep.Overall.TotalRequests) * 100.0
	}

	titleBlock := fmt.Sprintf("%s  %s\n%s",
		brandBadge,
		lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(rep.Name),
		mutedStyle.Render(fmt.Sprintf("Base URL: %s  •  VUs: %d  •  Duration: %s  •  Total Reqs: %d  •  RPS: %.1f  •  Err: %.2f%%",
			rep.BaseURL, rep.VUs, rep.TotalDuration.Round(time.Millisecond),
			rep.Overall.TotalRequests, rep.Overall.RPS, overallErrPct,
		)),
	)

	termW := TerminalWidth()
	boxW := ClampBoxWidth(termW, 122, 56)
	innerW := boxW - 6

	const (
		wMethod = 6
		wStatus = 12
		wRPS    = 7
		wP50    = 8
		wP95    = 8
		wErr    = 8
	)

	fixed := wMethod + wStatus + wRPS + wP50 + wP95 + wErr + 7*3 // 7 separators " │ "
	flex := innerW - fixed
	if flex < 20 {
		flex = 20
	}
	wStep := flex * 45 / 100
	wPath := flex - wStep
	if termW >= WideBreakpoint {
		if wStep > 22 {
			wStep = 22
		}
		if wPath > 26 {
			wPath = 26
		}
	}

	totalLineW := wStep + wPath + fixed
	if totalLineW > innerW {
		totalLineW = innerW
	}

	headerRow := strings.Join([]string{
		headerStyle.Render(PadRunes("Step", wStep)),
		headerStyle.Render(PadRunes("Method", wMethod)),
		headerStyle.Render(PadRunes("Path", wPath)),
		headerStyle.Render(PadRunes("Status", wStatus)),
		headerStyle.Render(PadRunes("RPS", wRPS)),
		headerStyle.Render(PadRunes("p50", wP50)),
		headerStyle.Render(PadRunes("p95", wP95)),
		headerStyle.Render(PadRunes("Error %", wErr)),
	}, " │ ")

	divider := mutedStyle.Render(strings.Repeat("─", totalLineW))

	var rows []string
	rows = append(rows, titleBlock, "", headerRow, divider)

	for _, sr := range rep.StepResults {
		var errPct float64
		if sr.Summary.TotalRequests > 0 {
			errPct = float64(sr.Summary.ErrorCount) / float64(sr.Summary.TotalRequests) * 100.0
		}

		errStyle := lipgloss.NewStyle().Foreground(colorSuccess)
		if errPct > 0 {
			errStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBrand)
		}

		statusText, statusColor := formatStepStatus(sr.Summary, colorSuccess, colorBrand, colorMuted)
		statusStyle := lipgloss.NewStyle().Bold(true).Foreground(statusColor)

		row := strings.Join([]string{
			cellStyle.Render(PadRunes(sr.Name, wStep)),
			lipgloss.NewStyle().Bold(true).Foreground(colorBrand).Render(PadRunes(sr.Method, wMethod)),
			mutedStyle.Render(PadRunes(sr.Path, wPath)),
			statusStyle.Render(PadRunes(statusText, wStatus)),
			cellStyle.Render(PadRunes(fmt.Sprintf("%.1f", sr.Summary.RPS), wRPS)),
			cellStyle.Render(PadRunes(metrics.FormatDuration(sr.Summary.P50), wP50)),
			cellStyle.Render(PadRunes(metrics.FormatDuration(sr.Summary.P95), wP95)),
			errStyle.Render(PadRunes(fmt.Sprintf("%.2f%%", errPct), wErr)),
		}, " │ ")
		rows = append(rows, row)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Padding(1, 2).
		Width(boxW).
		Render(strings.Join(rows, "\n"))

	fmt.Println()
	fmt.Println(box)

	printedWarnings := make(map[string]bool)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C89B54"))
	for _, sr := range rep.StepResults {
		varNames := make([]string, 0, len(sr.ExtractWarnings))
		for k, cnt := range sr.ExtractWarnings {
			if cnt > 0 {
				varNames = append(varNames, k)
			}
		}
		slices.Sort(varNames)
		for _, varName := range varNames {
			if printedWarnings[varName] {
				continue
			}
			printedWarnings[varName] = true
			fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ Warning: variable '%s' could not be extracted from JSON response (check JSONPath)", varName)))
		}
		if sr.AssertionFailures > 0 {
			reasons := make([]string, 0, len(sr.FailureReasons))
			for r := range sr.FailureReasons {
				reasons = append(reasons, r)
			}
			slices.Sort(reasons)
			for _, r := range reasons {
				fmt.Println(warnStyle.Render(fmt.Sprintf("  ✗ [%s] Assertion failed (%dx): %s", sr.Name, sr.FailureReasons[r], r)))
			}
		}
	}
	fmt.Println()
}

func formatStepStatus(s metrics.Summary, colorSuccess, colorBrand, colorMuted lipgloss.Color) (string, lipgloss.Color) {
	var parts []string
	codes := make([]int, 0, len(s.StatusCodes))
	for code := range s.StatusCodes {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	has5xx := s.NetworkErrors > 0
	has4xx := false
	has2xx := false

	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%d (%d)", code, s.StatusCodes[code]))
		switch {
		case code >= 500:
			has5xx = true
		case code >= 400:
			has4xx = true
		case code >= 200 && code < 300:
			has2xx = true
		}
	}
	if s.NetworkErrors > 0 {
		parts = append(parts, fmt.Sprintf("NET (%d)", s.NetworkErrors))
	}

	if len(parts) == 0 {
		return "-", colorMuted
	}

	text := strings.Join(parts, ", ")
	switch {
	case has5xx:
		return text, colorBrand
	case has4xx:
		return text, lipgloss.Color("#C89B54")
	case has2xx:
		return text, colorSuccess
	default:
		return text, colorMuted
	}
}
