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

	const (
		wStep   = 20
		wMethod = 7
		wPath   = 22
		wStatus = 14
		wRPS    = 8
		wP50    = 9
		wP95    = 9
		wErr    = 8
	)

	pad := func(s string, width int) string {
		r := []rune(s)
		if len(r) > width {
			if width > 3 {
				return string(r[:width-3]) + "..."
			}
			return string(r[:width])
		}
		return s + strings.Repeat(" ", width-len(r))
	}

	headerRow := strings.Join([]string{
		headerStyle.Render(pad("Step", wStep)),
		headerStyle.Render(pad("Method", wMethod)),
		headerStyle.Render(pad("Path", wPath)),
		headerStyle.Render(pad("Status", wStatus)),
		headerStyle.Render(pad("RPS", wRPS)),
		headerStyle.Render(pad("p50", wP50)),
		headerStyle.Render(pad("p95", wP95)),
		headerStyle.Render(pad("Error %", wErr)),
	}, " │ ")

	divider := mutedStyle.Render(strings.Repeat("─", wStep+wMethod+wPath+wStatus+wRPS+wP50+wP95+wErr+21))

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
			cellStyle.Render(pad(sr.Name, wStep)),
			lipgloss.NewStyle().Bold(true).Foreground(colorBrand).Render(pad(sr.Method, wMethod)),
			mutedStyle.Render(pad(sr.Path, wPath)),
			statusStyle.Render(pad(statusText, wStatus)),
			cellStyle.Render(pad(fmt.Sprintf("%.1f", sr.Summary.RPS), wRPS)),
			cellStyle.Render(pad(metrics.FormatDuration(sr.Summary.P50), wP50)),
			cellStyle.Render(pad(metrics.FormatDuration(sr.Summary.P95), wP95)),
			errStyle.Render(pad(fmt.Sprintf("%.2f%%", errPct), wErr)),
		}, " │ ")
		rows = append(rows, row)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Padding(1, 2).
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
