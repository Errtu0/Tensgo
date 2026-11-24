package reporter

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/scenario"
)

func sampleSummary(p95 time.Duration, errCount, total int) metrics.Summary {
	return metrics.Summary{
		TotalRequests: total,
		SuccessCount:  total - errCount,
		ErrorCount:    errCount,
		NetworkErrors: 0,
		StatusCodes:   map[int]int{200: total - errCount, 500: errCount},
		TotalDuration: 10 * time.Second,
		RPS:           float64(total) / 10.0,
		Min:           2 * time.Millisecond,
		Avg:           45 * time.Millisecond,
		P50:           30 * time.Millisecond,
		P90:           p95 - 10*time.Millisecond,
		P95:           p95,
		P99:           p95 + 20*time.Millisecond,
		Max:           p95 + 50*time.Millisecond,
	}
}

func TestParseFormats(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty", input: "", want: nil},
		{name: "single json", input: "json", want: []string{"json"}},
		{name: "multi with spaces and dupes", input: " JSON , junit , markdown , json ", want: []string{"json", "junit", "markdown"}},
		{name: "unsupported", input: "json,csv", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFormats(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d]=%q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBuildGateEvaluations(t *testing.T) {
	tests := []struct {
		name         string
		summary      metrics.Summary
		endpoints    []EndpointReport
		maxP95       time.Duration
		maxErrorRate float64
		wantPassed   bool
		wantGates    int
	}{
		{
			name:         "no thresholds",
			summary:      sampleSummary(120*time.Millisecond, 0, 100),
			maxP95:       0,
			maxErrorRate: -1,
			wantPassed:   true,
			wantGates:    0,
		},
		{
			name:    "single endpoint skips duplicate per-endpoint gates",
			summary: sampleSummary(120*time.Millisecond, 1, 200),
			endpoints: []EndpointReport{
				{Method: "GET", Target: "/health", Summary: sampleSummary(100*time.Millisecond, 0, 100)},
			},
			maxP95:       250 * time.Millisecond,
			maxErrorRate: 1.0,
			wantPassed:   true,
			wantGates:    2,
		},
		{
			name:    "fail p95 and error rate with multi-endpoint SLAs",
			summary: sampleSummary(400*time.Millisecond, 10, 100),
			endpoints: []EndpointReport{
				{Method: "POST", Target: "/login", Summary: sampleSummary(150*time.Millisecond, 0, 50)},
				{Method: "POST", Target: "/orders", Summary: sampleSummary(450*time.Millisecond, 10, 50)},
			},
			maxP95:       250 * time.Millisecond,
			maxErrorRate: 2.0,
			wantPassed:   false,
			wantGates:    6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gates, passed := BuildGateEvaluations(tc.summary, tc.endpoints, tc.maxP95, tc.maxErrorRate)
			if passed != tc.wantPassed {
				t.Fatalf("passed=%v, want %v", passed, tc.wantPassed)
			}
			if len(gates) != tc.wantGates {
				t.Fatalf("len(gates)=%d, want %d", len(gates), tc.wantGates)
			}
		})
	}
}

func TestRenderJSON(t *testing.T) {
	sum := sampleSummary(150*time.Millisecond, 2, 200)
	eps := []EndpointReport{
		{Name: "Health", Method: "GET", Target: "http://localhost:8080/health", Summary: sum},
	}
	gates, passed := BuildGateEvaluations(sum, eps, 300*time.Millisecond, 2.0)

	raw, err := RenderJSON(ReportData{
		SchemaVersion: SchemaVersion,
		Timestamp:     time.Date(2026, 11, 24, 20, 0, 0, 0, time.UTC),
		Target:        "http://localhost:8080/health",
		Method:        "GET",
		Duration:      10 * time.Second,
		VUs:           20,
		Profile:       "constant",
		Summary:       sum,
		Endpoints:     eps,
		Gates:         gates,
		Passed:        passed,
	})
	if err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}

	var parsed jsonReport
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if parsed.SchemaVersion != "0.3.0" {
		t.Fatalf("schema_version=%q, want 0.3.0", parsed.SchemaVersion)
	}
	if !parsed.Passed {
		t.Fatalf("expected passed=true")
	}
	if parsed.Metadata.VUs != 20 || parsed.Metrics.TotalRequests != 200 {
		t.Fatalf("unexpected metadata/metrics: %+v %+v", parsed.Metadata, parsed.Metrics)
	}
	if parsed.Metrics.Latency.AvgMs <= 0 || parsed.Metrics.Latency.P90Ms <= 0 {
		t.Fatalf("expected avg_ms and p90_ms > 0, got %+v", parsed.Metrics.Latency)
	}
	if len(parsed.Endpoints) != 1 || len(parsed.Gates) != 2 {
		t.Fatalf("unexpected endpoints/gates counts: %d / %d", len(parsed.Endpoints), len(parsed.Gates))
	}
	if !strings.Contains(string(raw), "<= 300ms") || strings.Contains(string(raw), `\u003c`) {
		t.Fatalf("expected unescaped '<=' in JSON output, got:\n%s", string(raw))
	}
}

func TestRenderJUnit(t *testing.T) {
	tests := []struct {
		name         string
		p95          time.Duration
		errCount     int
		maxP95       time.Duration
		maxErrorRate float64
		wantFailures int
		wantCases    int
	}{
		{
			name:         "pass gates",
			p95:          100 * time.Millisecond,
			errCount:     0,
			maxP95:       200 * time.Millisecond,
			maxErrorRate: 1.0,
			wantFailures: 0,
			wantCases:    2,
		},
		{
			name:         "fail both gates",
			p95:          500 * time.Millisecond,
			errCount:     15,
			maxP95:       200 * time.Millisecond,
			maxErrorRate: 1.0,
			wantFailures: 2,
			wantCases:    2,
		},
		{
			name:         "no gates fallback",
			p95:          50 * time.Millisecond,
			errCount:     0,
			maxP95:       0,
			maxErrorRate: -1,
			wantFailures: 0,
			wantCases:    1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sum := sampleSummary(tc.p95, tc.errCount, 100)
			gates, passed := BuildGateEvaluations(sum, nil, tc.maxP95, tc.maxErrorRate)
			raw, err := RenderJUnit(ReportData{
				Target:   "http://localhost:8080/health",
				Duration: 5 * time.Second,
				Summary:  sum,
				Gates:    gates,
				Passed:   passed,
			})
			if err != nil {
				t.Fatalf("RenderJUnit error: %v", err)
			}

			var suites junitTestSuites
			if err := xml.Unmarshal(raw, &suites); err != nil {
				t.Fatalf("xml.Unmarshal error: %v", err)
			}
			if suites.Failures != tc.wantFailures || suites.Tests != tc.wantCases {
				t.Fatalf("got tests=%d failures=%d, want tests=%d failures=%d",
					suites.Tests, suites.Failures, tc.wantCases, tc.wantFailures)
			}
			if len(suites.Suites) != 1 || len(suites.Suites[0].Cases) != tc.wantCases {
				t.Fatalf("unexpected suite/case structure: %+v", suites)
			}
		})
	}
}

func TestRenderMarkdown(t *testing.T) {
	sum := sampleSummary(350*time.Millisecond, 5, 100)
	eps := []EndpointReport{
		{Method: "POST", Target: "/api/login", Summary: sum},
	}
	gates, passed := BuildGateEvaluations(sum, eps, 200*time.Millisecond, 1.0)

	md := RenderMarkdown(ReportData{
		Target:    "http://localhost:8080",
		Method:    "POST",
		Duration:  10 * time.Second,
		VUs:       15,
		Profile:   "constant",
		Summary:   sum,
		Endpoints: eps,
		Gates:     gates,
		Passed:    passed,
	})

	for _, want := range []string{
		"❌ FAIL — tensgo Load Test Summary",
		"### Overview Metrics",
		"### Per-Endpoint Breakdown",
		"### Quality Gate Evaluations",
		"`/api/login`",
		"P95 Latency Gate",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("expected markdown to contain %q, got:\n%s", want, md)
		}
	}
}

func TestExportReportsAndGitHubSummary(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sum := sampleSummary(80*time.Millisecond, 0, 100)
	gates, passed := BuildGateEvaluations(sum, nil, 200*time.Millisecond, 1.0)
	data := ReportData{
		Target:   "http://localhost:8080/health",
		Method:   "GET",
		Duration: 5 * time.Second,
		VUs:      10,
		Profile:  "constant",
		Summary:  sum,
		Gates:    gates,
		Passed:   passed,
	}

	// Multi-format export creates default filenames in tensgo-reports/ dir.
	written, err := ExportReports(data, []string{"json", "junit", "markdown"}, "", "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ExportReports multi-format error: %v", err)
	}
	if len(written) != 3 {
		t.Fatalf("expected 3 files, got %v", written)
	}
	for _, f := range []string{"tensgo-report.json", "tensgo-junit.xml", "tensgo-report.md"} {
		if _, err := os.Stat(filepath.Join(tmp, DefaultOutputDir, f)); err != nil {
			t.Fatalf("expected %s/%s to exist: %v", DefaultOutputDir, f, err)
		}
	}

	// Custom output directory.
	customDir := filepath.Join(tmp, "ci-artifacts")
	written, err = ExportReports(data, []string{"json"}, "", customDir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ExportReports custom dir error: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 file, got %v", written)
	}
	if _, err := os.Stat(filepath.Join(customDir, "tensgo-report.json")); err != nil {
		t.Fatalf("expected custom dir file to exist: %v", err)
	}

	// Stdout export for single format.
	var buf bytes.Buffer
	written, err = ExportReports(data, []string{"json"}, "-", "", &buf)
	if err != nil {
		t.Fatalf("ExportReports stdout error: %v", err)
	}
	if len(written) != 1 || written[0] != "stdout" || !strings.Contains(buf.String(), `"schema_version": "0.3.0"`) {
		t.Fatalf("unexpected stdout export: %v %s", written, buf.String())
	}

	// Stdout with multiple formats must fail.
	if _, err := ExportReports(data, []string{"json", "junit"}, "-", "", &buf); err == nil {
		t.Fatalf("expected error when using '-' with multiple formats")
	}

	// GitHub Step Summary appending.
	summaryFile := filepath.Join(tmp, "step-summary.md")
	if err := os.WriteFile(summaryFile, []byte("# Existing Header\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendGitHubStepSummary(data, summaryFile); err != nil {
		t.Fatalf("AppendGitHubStepSummary error: %v", err)
	}
	content, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "# Existing Header") || !strings.Contains(text, "✅ PASS — tensgo Load Test Summary") {
		t.Fatalf("unexpected step summary content:\n%s", text)
	}
}

func TestLayoutHelpers(t *testing.T) {
	t.Setenv("COLUMNS", "92")
	if w := TerminalWidth(); w <= 0 {
		t.Fatalf("expected positive TerminalWidth, got %d", w)
	}

	if got := ClampBoxWidth(80, 120, 40); got != 78 {
		t.Fatalf("ClampBoxWidth(80, 120, 40) = %d, want 78", got)
	}
	if got := ClampBoxWidth(130, 88, 40); got != 88 {
		t.Fatalf("ClampBoxWidth(130, 88, 40) = %d, want 88", got)
	}
	if got := TruncateRunes("abcdefghij", 7); got != "abcd..." {
		t.Fatalf("TruncateRunes = %q, want abcd...", got)
	}
	if got := PadRunes("hi", 5); got != "hi   " {
		t.Fatalf("PadRunes = %q, want 'hi   '", got)
	}
}

func TestHTMLAndTerminalReporters(t *testing.T) {
	tmp := t.TempDir()
	sum := sampleSummary(120*time.Millisecond, 1, 100)
	sum.StatusCodes[401] = 1
	cfg := engine.Config{
		URL:         "http://localhost:8080/health",
		Method:      "GET",
		Concurrency: 10,
		Duration:    5 * time.Second,
		Timeout:     2 * time.Second,
	}

	htmlFile := filepath.Join(tmp, "report.html")
	if err := GenerateHTMLReport(htmlFile, cfg, sum, []string{"P95 exceeded"}, true); err != nil {
		t.Fatalf("GenerateHTMLReport error: %v", err)
	}
	raw, err := os.ReadFile(htmlFile)
	if err != nil || !strings.Contains(string(raw), "TENSGO") || !strings.Contains(string(raw), "P95 exceeded") {
		t.Fatalf("unexpected HTML output: err=%v", err)
	}

	summaryHTML := filepath.Join(tmp, "summary.html")
	if err := GenerateHTMLSummaryReport(summaryHTML, "SCENARIO", "My Flow", sum); err != nil {
		t.Fatalf("GenerateHTMLSummaryReport error: %v", err)
	}

	RenderTerminalReport(cfg, sum, []string{"P95 exceeded"}, true)
	RenderComparisonMatrix([]RouteResult{
		{Endpoint: "/health", Method: "GET", Summary: sampleSummary(50*time.Millisecond, 0, 100)},
		{Endpoint: "/auth", Method: "GET", Summary: metrics.Summary{TotalRequests: 10, ErrorCount: 10, StatusCodes: map[int]int{401: 10}}},
		{Endpoint: "/missing", Method: "GET", Summary: metrics.Summary{TotalRequests: 10, ErrorCount: 10, StatusCodes: map[int]int{404: 10}}},
		{Endpoint: "/bad", Method: "POST", Summary: metrics.Summary{TotalRequests: 10, ErrorCount: 10, StatusCodes: map[int]int{400: 10}}},
		{Endpoint: "/slow", Method: "GET", Summary: sampleSummary(900*time.Millisecond, 5, 100)},
	})
	RenderScenarioReport(scenario.Report{
		Name:          "Demo",
		BaseURL:       "http://localhost:8080",
		VUs:           5,
		TotalDuration: 2 * time.Second,
		Overall:       sum,
		StepResults: []scenario.StepResult{
			{
				Name:              "Login",
				Method:            "POST",
				Path:              "/api/login",
				Summary:           sum,
				ExtractWarnings:   map[string]int64{"token": 1},
				AssertionFailures: 1,
				FailureReasons:    map[string]int64{"status: expected [201], got 200": 1},
			},
		},
	})
}
