package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// RouteResult holds the aggregated load test summary for a single OpenAPI route.
type RouteResult struct {
	Endpoint string
	Method   string
	Summary  metrics.Summary
}

// RenderComparisonMatrix prints a Lipgloss comparison table for all tested OpenAPI routes.
func RenderComparisonMatrix(results []RouteResult) {
	if len(results) == 0 {
		return
	}

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
		Render("TENSGO v0.2.0")

	titleLine := fmt.Sprintf("%s  %s",
		brandBadge,
		lipgloss.NewStyle().Bold(true).Foreground(colorText).Render("OpenAPI Route Bottleneck Comparison Matrix"),
	)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBrand)
	cellStyle := lipgloss.NewStyle().Foreground(colorText)
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)

	// Column widths
	const (
		wEndpoint = 28
		wMethod   = 8
		wRPS      = 10
		wP50      = 10
		wP95      = 10
		wP99      = 10
		wErr      = 9
		wStatus   = 21
	)

	pad := func(s string, width int) string {
		if len(s) > width {
			if width > 3 {
				return s[:width-3] + "..."
			}
			return s[:width]
		}
		return s + strings.Repeat(" ", width-len(s))
	}

	headerRow := strings.Join([]string{
		headerStyle.Render(pad("Endpoint", wEndpoint)),
		headerStyle.Render(pad("Method", wMethod)),
		headerStyle.Render(pad("RPS", wRPS)),
		headerStyle.Render(pad("p50", wP50)),
		headerStyle.Render(pad("p95", wP95)),
		headerStyle.Render(pad("p99", wP99)),
		headerStyle.Render(pad("Error %", wErr)),
		headerStyle.Render(pad("Status", wStatus)),
	}, " │ ")

	divider := mutedStyle.Render(strings.Repeat("─", wEndpoint+wMethod+wRPS+wP50+wP95+wP99+wErr+wStatus+21))

	var rows []string
	rows = append(rows, titleLine, "", headerRow, divider)

	badgeHealthy := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(colorSuccess).Padding(0, 1).Render("Healthy")
	badgeNormal := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(colorText).Padding(0, 1).Render("Normal")
	badgeAuth401 := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(lipgloss.Color("#C89B54")).Padding(0, 1).Render("Auth Required (401)")
	badgeAuth403 := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(lipgloss.Color("#C89B54")).Padding(0, 1).Render("Auth Required (403)")
	badge404 := lipgloss.NewStyle().Bold(true).Foreground(colorText).Background(lipgloss.Color("#3A3D40")).Padding(0, 1).Render("Not Found (404)")
	badge400 := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(lipgloss.Color("#C89B54")).Padding(0, 1).Render("Bad Request (400)")
	badgeBottleneck := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(colorBrand).Padding(0, 1).Render("Bottleneck")

	for _, r := range results {
		var errPct, serverErrPct float64
		auth401 := r.Summary.StatusCodes[401]
		auth403 := r.Summary.StatusCodes[403]
		authCount := auth401 + auth403
		notFoundCount := r.Summary.StatusCodes[404]
		badReqCount := r.Summary.StatusCodes[400] + r.Summary.StatusCodes[422]

		if r.Summary.TotalRequests > 0 {
			errPct = float64(r.Summary.ErrorCount) / float64(r.Summary.TotalRequests) * 100.0
			serverOrNetErrors := r.Summary.NetworkErrors
			for code, cnt := range r.Summary.StatusCodes {
				if code >= 500 {
					serverOrNetErrors += cnt
				}
			}
			serverErrPct = float64(serverOrNetErrors) / float64(r.Summary.TotalRequests) * 100.0
		}

		statusBadge := badgeNormal
		switch {
		case r.Summary.TotalRequests > 0 && authCount*2 > r.Summary.TotalRequests:
			if auth403 > auth401 {
				statusBadge = badgeAuth403
			} else {
				statusBadge = badgeAuth401
			}
		case r.Summary.TotalRequests > 0 && notFoundCount*2 > r.Summary.TotalRequests:
			statusBadge = badge404
		case r.Summary.TotalRequests > 0 && badReqCount*2 > r.Summary.TotalRequests:
			statusBadge = badge400
		case serverErrPct >= 2.0 || r.Summary.P95 >= 800*time.Millisecond:
			statusBadge = badgeBottleneck
		case errPct == 0 && r.Summary.P95 < 200*time.Millisecond:
			statusBadge = badgeHealthy
		}

		row := strings.Join([]string{
			cellStyle.Render(pad(r.Endpoint, wEndpoint)),
			lipgloss.NewStyle().Bold(true).Foreground(colorBrand).Render(pad(r.Method, wMethod)),
			cellStyle.Render(pad(fmt.Sprintf("%.1f", r.Summary.RPS), wRPS)),
			cellStyle.Render(pad(metrics.FormatDuration(r.Summary.P50), wP50)),
			cellStyle.Render(pad(metrics.FormatDuration(r.Summary.P95), wP95)),
			cellStyle.Render(pad(metrics.FormatDuration(r.Summary.P99), wP99)),
			cellStyle.Render(pad(fmt.Sprintf("%.2f%%", errPct), wErr)),
			statusBadge,
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

	hasAuthError := false
	for _, r := range results {
		if r.Summary.StatusCodes[401] > 0 || r.Summary.StatusCodes[403] > 0 {
			hasAuthError = true
			break
		}
	}
	if hasAuthError {
		fmt.Println(lipgloss.NewStyle().Foreground(colorBrand).Render("  Tip: Protected routes returned 401/403. Re-run with Auto-Auth or a Bearer token."))
	}
	fmt.Println()
}
