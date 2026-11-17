package reporter

import (
	"fmt"
	"html/template"
	"os"
	"slices"
	"time"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
)

type statusRow struct {
	Code    int
	Count   int
	Percent string
	Class   string
}

type htmlReportData struct {
	GeneratedAt   string
	URL           string
	Method        string
	Concurrency   int
	TotalDuration string
	Timeout       string
	TotalRequests int
	SuccessCount  int
	ErrorCount    int
	NetworkErrors int
	SuccessRate   string
	ErrorRate     string
	SuccessWidth  string
	ErrorWidth    string
	RPS           string
	Min           string
	P50           string
	P95           string
	P99           string
	Max           string
	StatusRows    []statusRow
	HasThresholds bool
	Passed        bool
	Violations    []string
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>tensgo Load Test Report</title>
<style>
  :root {
    --bg: #0B0D0C;
    --surface: #131614;
    --surface-2: #1A1E1B;
    --border: #2A2E2B;
    --text: #ECEAE6;
    --muted: #6C7075;
    --accent: #DE8B4B;
    --green: #6B8E7A;
    --yellow: #C89B54;
    --red: #C85A48;
    --cyan: #DE8B4B;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background: var(--bg);
    color: var(--text);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Inter, sans-serif;
    line-height: 1.5;
    padding: 32px 20px;
  }
  .container { max-width: 980px; margin: 0 auto; }
  .header {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 24px;
    margin-bottom: 20px;
  }
  .header-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    margin-bottom: 12px;
  }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-weight: 700;
    font-size: 18px;
    letter-spacing: 0.04em;
  }
  .brand-pill {
    background: var(--accent);
    color: #0B0D0C;
    padding: 3px 10px;
    border-radius: 6px;
    font-size: 12px;
    letter-spacing: 0.08em;
  }
  .method-pill {
    background: rgba(222, 139, 75, 0.14);
    color: var(--accent);
    border: 1px solid rgba(222, 139, 75, 0.35);
    padding: 2px 10px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 700;
  }
  .target-url {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 15px;
    color: var(--text);
    word-break: break-all;
  }
  .meta { color: var(--muted); font-size: 13px; margin-top: 8px; }
  .gate {
    border-radius: 12px;
    padding: 16px 20px;
    margin-bottom: 20px;
    border: 1px solid var(--border);
  }
  .gate.pass {
    background: rgba(16, 185, 129, 0.08);
    border-color: rgba(16, 185, 129, 0.4);
  }
  .gate.fail {
    background: rgba(239, 68, 68, 0.08);
    border-color: rgba(239, 68, 68, 0.4);
  }
  .gate-title { font-weight: 700; font-size: 14px; display: flex; align-items: center; gap: 10px; }
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 6px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.05em;
  }
  .badge-pass { background: var(--green); color: #0B0C0E; }
  .badge-fail { background: var(--red); color: #fff; }
  .violations { margin-top: 10px; padding-left: 20px; font-size: 13px; color: #FCA5A5; }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 14px;
    margin-bottom: 20px;
  }
  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 18px;
  }
  .card-label { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; }
  .card-value { font-size: 24px; font-weight: 700; margin-top: 6px; font-variant-numeric: tabular-nums; }
  .c-cyan { color: var(--cyan); }
  .c-green { color: var(--green); }
  .c-yellow { color: var(--yellow); }
  .c-red { color: var(--red); }
  .section {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 22px;
    margin-bottom: 20px;
  }
  .section-title {
    font-size: 14px;
    font-weight: 700;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    margin-bottom: 14px;
  }
  .ratio-bar {
    height: 12px;
    background: var(--surface-2);
    border-radius: 999px;
    overflow: hidden;
    display: flex;
    margin-bottom: 10px;
  }
  .ratio-ok { background: var(--green); height: 100%; }
  .ratio-err { background: var(--red); height: 100%; }
  .ratio-legend {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    color: var(--muted);
  }
  .latency-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 12px;
  }
  .lat-item {
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 12px 14px;
  }
  .lat-item span { display: block; font-size: 12px; color: var(--muted); }
  .lat-item strong { font-size: 17px; font-variant-numeric: tabular-nums; }
  table { width: 100%; border-collapse: collapse; font-size: 14px; }
  th, td { text-align: left; padding: 10px 12px; border-bottom: 1px solid var(--border); }
  th { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; }
  tr:last-child td { border-bottom: none; }
  .status-pill {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 6px;
    font-weight: 700;
    font-size: 12px;
  }
  .s-2xx { background: rgba(16, 185, 129, 0.16); color: var(--green); }
  .s-3xx { background: rgba(6, 182, 212, 0.16); color: var(--cyan); }
  .s-4xx { background: rgba(245, 158, 11, 0.16); color: var(--yellow); }
  .s-5xx { background: rgba(239, 68, 68, 0.16); color: var(--red); }
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <div class="header-top">
      <div class="brand">
        <span class="brand-pill">TENSGO</span>
        <span>Load Test Report</span>
      </div>
      <span class="method-pill">{{.Method}}</span>
    </div>
    <div class="target-url">{{.URL}}</div>
    <div class="meta">
      Generated: {{.GeneratedAt}} &nbsp;•&nbsp; Concurrency: {{.Concurrency}} workers &nbsp;•&nbsp; Duration: {{.TotalDuration}} &nbsp;•&nbsp; Timeout: {{.Timeout}}
    </div>
  </div>

  {{if .HasThresholds}}
  <div class="gate {{if .Passed}}pass{{else}}fail{{end}}">
    <div class="gate-title">
      {{if .Passed}}
        <span class="badge badge-pass">PASS</span>
        <span>All CI/CD quality gate thresholds passed</span>
      {{else}}
        <span class="badge badge-fail">FAIL</span>
        <span>CI/CD quality gate thresholds violated</span>
      {{end}}
    </div>
    {{if not .Passed}}
    <ul class="violations">
      {{range .Violations}}<li>{{.}}</li>{{end}}
    </ul>
    {{end}}
  </div>
  {{end}}

  <div class="grid">
    <div class="card">
      <div class="card-label">Total Requests</div>
      <div class="card-value">{{.TotalRequests}}</div>
    </div>
    <div class="card">
      <div class="card-label">Throughput (RPS)</div>
      <div class="card-value c-cyan">{{.RPS}}</div>
    </div>
    <div class="card">
      <div class="card-label">Success Rate</div>
      <div class="card-value c-green">{{.SuccessRate}}%</div>
    </div>
    <div class="card">
      <div class="card-label">Error Rate</div>
      <div class="card-value {{if gt .ErrorCount 0}}c-red{{else}}c-green{{end}}">{{.ErrorRate}}%</div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">Success vs Error Ratio</div>
    <div class="ratio-bar">
      <div class="ratio-ok" style="width: {{.SuccessWidth}}%"></div>
      <div class="ratio-err" style="width: {{.ErrorWidth}}%"></div>
    </div>
    <div class="ratio-legend">
      <span>Success: <strong class="c-green">{{.SuccessCount}}</strong> ({{.SuccessRate}}%)</span>
      <span>Errors: <strong class="{{if gt .ErrorCount 0}}c-red{{else}}c-green{{end}}">{{.ErrorCount}}</strong> ({{.ErrorRate}}%) &nbsp;•&nbsp; Network Errors: <strong>{{.NetworkErrors}}</strong></span>
    </div>
  </div>

  <div class="section">
    <div class="section-title">Latency Percentiles</div>
    <div class="latency-grid">
      <div class="lat-item"><span>Min</span><strong>{{.Min}}</strong></div>
      <div class="lat-item"><span>P50 (Median)</span><strong>{{.P50}}</strong></div>
      <div class="lat-item"><span>P95</span><strong class="c-yellow">{{.P95}}</strong></div>
      <div class="lat-item"><span>P99</span><strong class="c-yellow">{{.P99}}</strong></div>
      <div class="lat-item"><span>Max</span><strong>{{.Max}}</strong></div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">HTTP Status Code Distribution</div>
    <table>
      <thead>
        <tr>
          <th>Status Code</th>
          <th>Count</th>
          <th>Share</th>
        </tr>
      </thead>
      <tbody>
        {{range .StatusRows}}
        <tr>
          <td><span class="status-pill {{.Class}}">{{.Code}}</span></td>
          <td>{{.Count}}</td>
          <td>{{.Percent}}%</td>
        </tr>
        {{end}}
        {{if gt .NetworkErrors 0}}
        <tr>
          <td><span class="status-pill s-5xx">Network Error</span></td>
          <td>{{.NetworkErrors}}</td>
          <td>-</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>
</body>
</html>
`

// GenerateHTMLReport writes a standalone dark-mode HTML report to the given file path.
func GenerateHTMLReport(path string, cfg engine.Config, s metrics.Summary, violations []string, hasThresholds bool) error {
	var successRate, errorRate float64
	if s.TotalRequests > 0 {
		successRate = float64(s.SuccessCount) / float64(s.TotalRequests) * 100.0
		errorRate = float64(s.ErrorCount) / float64(s.TotalRequests) * 100.0
	}

	codes := make([]int, 0, len(s.StatusCodes))
	for code := range s.StatusCodes {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	rows := make([]statusRow, 0, len(codes))
	for _, code := range codes {
		cnt := s.StatusCodes[code]
		pct := 0.0
		if s.TotalRequests > 0 {
			pct = float64(cnt) / float64(s.TotalRequests) * 100.0
		}
		cls := "s-3xx"
		switch {
		case code >= 200 && code < 300:
			cls = "s-2xx"
		case code >= 400 && code < 500:
			cls = "s-4xx"
		case code >= 500:
			cls = "s-5xx"
		}
		rows = append(rows, statusRow{
			Code:    code,
			Count:   cnt,
			Percent: fmt.Sprintf("%.1f", pct),
			Class:   cls,
		})
	}

	data := htmlReportData{
		GeneratedAt:   time.Now().Format(time.RFC3339),
		URL:           cfg.URL,
		Method:        cfg.Method,
		Concurrency:   cfg.Concurrency,
		TotalDuration: s.TotalDuration.Round(time.Millisecond).String(),
		Timeout:       cfg.Timeout.String(),
		TotalRequests: s.TotalRequests,
		SuccessCount:  s.SuccessCount,
		ErrorCount:    s.ErrorCount,
		NetworkErrors: s.NetworkErrors,
		SuccessRate:   fmt.Sprintf("%.1f", successRate),
		ErrorRate:     fmt.Sprintf("%.1f", errorRate),
		SuccessWidth:  fmt.Sprintf("%.2f", successRate),
		ErrorWidth:    fmt.Sprintf("%.2f", errorRate),
		RPS:           fmt.Sprintf("%.2f", s.RPS),
		Min:           metrics.FormatDuration(s.Min),
		P50:           metrics.FormatDuration(s.P50),
		P95:           metrics.FormatDuration(s.P95),
		P99:           metrics.FormatDuration(s.P99),
		Max:           metrics.FormatDuration(s.Max),
		StatusRows:    rows,
		HasThresholds: hasThresholds,
		Passed:        len(violations) == 0,
		Violations:    violations,
	}

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse html template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create html report %q: %w", path, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render html report: %w", err)
	}
	return nil
}

// GenerateHTMLSummaryReport writes a standalone Larch-themed HTML report from a generic Summary.
func GenerateHTMLSummaryReport(path, title, subtitle string, s metrics.Summary) error {
	cfg := engine.Config{
		URL:         subtitle,
		Method:      title,
		Concurrency: 1,
		Duration:    s.TotalDuration,
		Timeout:     5 * time.Second,
	}
	return GenerateHTMLReport(path, cfg, s, nil, false)
}
