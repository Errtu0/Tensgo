package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// RenderMarkdown formats ReportData into a concise, scannable Markdown pipeline summary.
func RenderMarkdown(data ReportData) string {
	var b strings.Builder

	badge := "✅ PASS"
	if !data.Passed {
		badge = "❌ FAIL"
	}

	b.WriteString(fmt.Sprintf("## %s — tensgo Load Test Summary\n\n", badge))

	metaParts := []string{}
	if data.Target != "" {
		if data.Method != "" {
			metaParts = append(metaParts, fmt.Sprintf("**Target:** `%s %s`", data.Method, data.Target))
		} else {
			metaParts = append(metaParts, fmt.Sprintf("**Target:** `%s`", data.Target))
		}
	}
	if data.VUs > 0 {
		metaParts = append(metaParts, fmt.Sprintf("**VUs:** `%d`", data.VUs))
	}
	if data.Duration > 0 {
		metaParts = append(metaParts, fmt.Sprintf("**Duration:** `%s`", data.Duration.Round(time.Millisecond)))
	}
	if data.Profile != "" {
		metaParts = append(metaParts, fmt.Sprintf("**Profile:** `%s`", data.Profile))
	}
	if len(metaParts) > 0 {
		b.WriteString(strings.Join(metaParts, " | ") + "\n\n")
	}

	b.WriteString("### Overview Metrics\n\n")
	b.WriteString("| Total Requests | Success | Errors | Error Rate | RPS | Min | Avg | P50 | P90 | P95 | P99 | Max |\n")
	b.WriteString("| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	b.WriteString(fmt.Sprintf("| %d | %d | %d | %.2f%% | %.2f | %s | %s | %s | %s | %s | %s | %s |\n",
		data.Summary.TotalRequests,
		data.Summary.SuccessCount,
		data.Summary.ErrorCount,
		errorRatePct(data.Summary),
		data.Summary.RPS,
		metrics.FormatDuration(data.Summary.Min),
		metrics.FormatDuration(data.Summary.Avg),
		metrics.FormatDuration(data.Summary.P50),
		metrics.FormatDuration(data.Summary.P90),
		metrics.FormatDuration(data.Summary.P95),
		metrics.FormatDuration(data.Summary.P99),
		metrics.FormatDuration(data.Summary.Max),
	))

	if len(data.Endpoints) > 0 {
		b.WriteString("\n### Per-Endpoint Breakdown\n\n")
		b.WriteString("| Endpoint | Method | Requests | RPS | P50 | P95 | P99 | Error Rate |\n")
		b.WriteString("| :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, ep := range data.Endpoints {
			label := ep.Target
			if label == "" {
				label = ep.Name
			}
			b.WriteString(fmt.Sprintf("| `%s` | `%s` | %d | %.2f | %s | %s | %s | %.2f%% |\n",
				label,
				ep.Method,
				ep.Summary.TotalRequests,
				ep.Summary.RPS,
				metrics.FormatDuration(ep.Summary.P50),
				metrics.FormatDuration(ep.Summary.P95),
				metrics.FormatDuration(ep.Summary.P99),
				errorRatePct(ep.Summary),
			))
		}
	}

	if len(data.Gates) > 0 {
		b.WriteString("\n### Quality Gate Evaluations\n\n")
		b.WriteString("| Gate | Status | Expected | Actual |\n")
		b.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, g := range data.Gates {
			status := "✅ PASS"
			if !g.Passed {
				status = "❌ FAIL"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | `%s` | `%s` |\n",
				g.Name, status, g.Expected, g.Actual,
			))
		}
	}

	return b.String()
}
