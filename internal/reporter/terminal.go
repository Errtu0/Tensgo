package reporter

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
)

// RenderTerminalReport prints a styled Lipgloss terminal report for the load test.
func RenderTerminalReport(cfg engine.Config, s metrics.Summary, violations []string, hasThresholds bool) {
	var (
		colorBorder  = lipgloss.Color("#6C7075")
		colorMuted   = lipgloss.Color("#6C7075")
		colorText    = lipgloss.Color("#ECEAE6")
		colorAccent  = lipgloss.Color("#DE8B4B")
		colorGreen   = lipgloss.Color("#6B8E7A")
		colorYellow  = lipgloss.Color("#DE8B4B")
		colorRed     = lipgloss.Color("#D9534F")
		colorCyan    = lipgloss.Color("#DE8B4B")
		colorBadgeBg = lipgloss.Color("#0B0D0C")
	)

	brandBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#0B0D0C")).
		Background(colorAccent).
		Padding(0, 1).
		Render("TENSGO")

	methodBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorCyan).
		Background(colorBadgeBg).
		Padding(0, 1).
		Render(cfg.Method)

	headerBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Width(76).
		Render(fmt.Sprintf("%s  %s %s\n%s",
			brandBadge,
			methodBadge,
			lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(cfg.URL),
			lipgloss.NewStyle().Foreground(colorMuted).Render(
				fmt.Sprintf("Concurrency: %d workers   •   Duration: %s   •   Timeout: %s",
					cfg.Concurrency, s.TotalDuration.Round(time.Millisecond), cfg.Timeout),
			),
		))

	// Summary & Latency side-by-side cards
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Width(37)

	cardTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().Foreground(colorMuted).Width(16)
	valStyle := lipgloss.NewStyle().Bold(true).Foreground(colorText)

	var successRate, errorRate float64
	if s.TotalRequests > 0 {
		successRate = float64(s.SuccessCount) / float64(s.TotalRequests) * 100.0
		errorRate = float64(s.ErrorCount) / float64(s.TotalRequests) * 100.0
	}

	errColor := colorGreen
	if s.ErrorCount > 0 {
		errColor = colorRed
	}

	summaryLines := []string{
		cardTitleStyle.Render("Throughput & Reliability"),
		fmt.Sprintf("%s %s", labelStyle.Render("Total Requests"), valStyle.Render(fmt.Sprintf("%d", s.TotalRequests))),
		fmt.Sprintf("%s %s", labelStyle.Render("RPS"), lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render(fmt.Sprintf("%.2f req/s", s.RPS))),
		fmt.Sprintf("%s %s", labelStyle.Render("Success"), lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(fmt.Sprintf("%d (%.1f%%)", s.SuccessCount, successRate))),
		fmt.Sprintf("%s %s", labelStyle.Render("Errors"), lipgloss.NewStyle().Bold(true).Foreground(errColor).Render(fmt.Sprintf("%d (%.1f%%)", s.ErrorCount, errorRate))),
		fmt.Sprintf("%s %s", labelStyle.Render("Network Errors"), lipgloss.NewStyle().Bold(true).Foreground(errColor).Render(fmt.Sprintf("%d", s.NetworkErrors))),
	}
	summaryCard := cardStyle.Render(strings.Join(summaryLines, "\n"))

	latencyLines := []string{
		cardTitleStyle.Render("Latency Distribution"),
		fmt.Sprintf("%s %s", labelStyle.Render("Min"), valStyle.Render(metrics.FormatDuration(s.Min))),
		fmt.Sprintf("%s %s", labelStyle.Render("P50 (Median)"), valStyle.Render(metrics.FormatDuration(s.P50))),
		fmt.Sprintf("%s %s", labelStyle.Render("P95"), lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(metrics.FormatDuration(s.P95))),
		fmt.Sprintf("%s %s", labelStyle.Render("P99"), lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(metrics.FormatDuration(s.P99))),
		fmt.Sprintf("%s %s", labelStyle.Render("Max"), valStyle.Render(metrics.FormatDuration(s.Max))),
	}
	latencyCard := cardStyle.Render(strings.Join(latencyLines, "\n"))

	cardsRow := lipgloss.JoinHorizontal(lipgloss.Top, summaryCard, "  ", latencyCard)

	// Status Code Distribution Table
	var statusLines []string
	statusLines = append(statusLines, cardTitleStyle.Render("HTTP Status Code Distribution"))

	if len(s.StatusCodes) == 0 && s.NetworkErrors == 0 {
		statusLines = append(statusLines, lipgloss.NewStyle().Foreground(colorMuted).Render("No responses recorded."))
	} else {
		codes := make([]int, 0, len(s.StatusCodes))
		for code := range s.StatusCodes {
			codes = append(codes, code)
		}
		slices.Sort(codes)

		for _, code := range codes {
			count := s.StatusCodes[code]
			pct := 0.0
			if s.TotalRequests > 0 {
				pct = float64(count) / float64(s.TotalRequests) * 100.0
			}

			badgeColor := colorCyan
			switch {
			case code >= 200 && code < 300:
				badgeColor = colorGreen
			case code >= 400 && code < 500:
				badgeColor = colorYellow
			case code >= 500:
				badgeColor = colorRed
			}

			codeBadge := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#0B0C0E")).
				Background(badgeColor).
				Padding(0, 1).
				Render(fmt.Sprintf("%d", code))

			barWidth := int(pct / 100.0 * 30)
			if barWidth == 0 && count > 0 {
				barWidth = 1
			}
			bar := lipgloss.NewStyle().Foreground(badgeColor).Render(strings.Repeat("█", barWidth))

			statusLines = append(statusLines, fmt.Sprintf("  %s  %-8d  %s  %s",
				codeBadge,
				count,
				lipgloss.NewStyle().Foreground(colorMuted).Width(10).Render(fmt.Sprintf("(%.1f%%)", pct)),
				bar,
			))
		}

		if s.NetworkErrors > 0 {
			errBadge := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(colorRed).
				Padding(0, 1).
				Render("NET")
			statusLines = append(statusLines, fmt.Sprintf("  %s  %-8d  %s",
				errBadge,
				s.NetworkErrors,
				lipgloss.NewStyle().Foreground(colorRed).Render("Network / Timeout Errors"),
			))
		}
	}

	statusBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Width(76).
		Render(strings.Join(statusLines, "\n"))

	fmt.Println()
	fmt.Println(headerBox)
	fmt.Println(cardsRow)
	fmt.Println(statusBox)

	// Quality Gate / Threshold Box
	if hasThresholds {
		var gateContent string
		gateBorderColor := colorGreen
		if len(violations) == 0 {
			passBadge := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#0B0C0E")).
				Background(colorGreen).
				Padding(0, 1).
				Render("PASS")
			gateContent = fmt.Sprintf("%s  %s", passBadge, lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("All CI/CD quality gate thresholds passed."))
		} else {
			gateBorderColor = colorRed
			failBadge := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(colorRed).
				Padding(0, 1).
				Render("FAIL")
			var lines []string
			lines = append(lines, fmt.Sprintf("%s  %s", failBadge, lipgloss.NewStyle().Bold(true).Foreground(colorRed).Render("CI/CD quality gate failed:")))
			for _, v := range violations {
				lines = append(lines, lipgloss.NewStyle().Foreground(colorText).Render("  • "+v))
			}
			gateContent = strings.Join(lines, "\n")
		}

		gateBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(gateBorderColor).
			Padding(0, 2).
			Width(76).
			Render(gateContent)

		fmt.Println(gateBox)
	}
	fmt.Println()
}
