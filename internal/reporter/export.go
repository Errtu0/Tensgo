package reporter

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// SchemaVersion is the version of the machine-readable JSON report schema.
const SchemaVersion = "0.3.0"

// GateEvaluation represents a single evaluated CI/CD quality gate or per-endpoint SLA check.
type GateEvaluation struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint,omitempty"`
	Passed   bool   `json:"passed"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message,omitempty"`
}

// EndpointReport holds per-endpoint or per-step metrics for a report.
type EndpointReport struct {
	Name    string
	Method  string
	Target  string
	Summary metrics.Summary
}

// ReportData is the unified payload consumed by JSON, JUnit XML, and Markdown exporters.
type ReportData struct {
	SchemaVersion string
	Timestamp     time.Time
	Target        string
	Method        string
	Duration      time.Duration
	VUs           int
	Profile       string
	Summary       metrics.Summary
	Endpoints     []EndpointReport
	Gates         []GateEvaluation
	Passed        bool
}

// BuildGateEvaluations evaluates overall and per-endpoint SLA thresholds.
func BuildGateEvaluations(s metrics.Summary, endpoints []EndpointReport, maxP95 time.Duration, maxErrorRate float64) ([]GateEvaluation, bool) {
	var gates []GateEvaluation
	allPassed := true

	errRate := errorRatePct(s)

	if maxP95 > 0 {
		passed := s.P95 <= maxP95
		if !passed {
			allPassed = false
		}
		var msg string
		if !passed {
			msg = fmt.Sprintf("P95 latency %s exceeded threshold %s", metrics.FormatDuration(s.P95), maxP95)
		}
		gates = append(gates, GateEvaluation{
			Name:     "P95 Latency Gate",
			Passed:   passed,
			Expected: "<= " + maxP95.String(),
			Actual:   metrics.FormatDuration(s.P95),
			Message:  msg,
		})
	}

	if maxErrorRate >= 0 {
		passed := errRate <= maxErrorRate
		if !passed {
			allPassed = false
		}
		var msg string
		if !passed {
			msg = fmt.Sprintf("Error rate %.2f%% exceeded threshold %.2f%%", errRate, maxErrorRate)
		}
		gates = append(gates, GateEvaluation{
			Name:     "Error Rate Gate",
			Passed:   passed,
			Expected: fmt.Sprintf("<= %.2f%%", maxErrorRate),
			Actual:   fmt.Sprintf("%.2f%%", errRate),
			Message:  msg,
		})
	}

	if len(endpoints) > 1 && (maxP95 > 0 || maxErrorRate >= 0) {
		for _, ep := range endpoints {
			epLabel := strings.TrimSpace(ep.Method + " " + ep.Target)
			if epLabel == "" {
				epLabel = ep.Name
			}
			if maxP95 > 0 {
				passed := ep.Summary.P95 <= maxP95
				if !passed {
					allPassed = false
				}
				var msg string
				if !passed {
					msg = fmt.Sprintf("%s P95 latency %s exceeded %s", epLabel, metrics.FormatDuration(ep.Summary.P95), maxP95)
				}
				gates = append(gates, GateEvaluation{
					Name:     fmt.Sprintf("P95 Latency SLA (%s)", epLabel),
					Endpoint: epLabel,
					Passed:   passed,
					Expected: "<= " + maxP95.String(),
					Actual:   metrics.FormatDuration(ep.Summary.P95),
					Message:  msg,
				})
			}
			if maxErrorRate >= 0 {
				epErrRate := errorRatePct(ep.Summary)
				passed := epErrRate <= maxErrorRate
				if !passed {
					allPassed = false
				}
				var msg string
				if !passed {
					msg = fmt.Sprintf("%s error rate %.2f%% exceeded %.2f%%", epLabel, epErrRate, maxErrorRate)
				}
				gates = append(gates, GateEvaluation{
					Name:     fmt.Sprintf("Error Rate SLA (%s)", epLabel),
					Endpoint: epLabel,
					Passed:   passed,
					Expected: fmt.Sprintf("<= %.2f%%", maxErrorRate),
					Actual:   fmt.Sprintf("%.2f%%", epErrRate),
					Message:  msg,
				})
			}
		}
	}

	return gates, allPassed
}

// ParseFormats parses a comma-separated list of report formats (json, junit, markdown).
func ParseFormats(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	var formats []string
	for _, part := range strings.Split(raw, ",") {
		f := strings.ToLower(strings.TrimSpace(part))
		if f == "" {
			continue
		}
		switch f {
		case "json", "junit", "markdown":
			if !seen[f] {
				seen[f] = true
				formats = append(formats, f)
			}
		default:
			return nil, fmt.Errorf("unsupported output format %q (supported: json, junit, markdown)", f)
		}
	}
	return formats, nil
}

// DefaultFilename returns the default output filename for a given export format.
func DefaultFilename(format string) string {
	switch format {
	case "json":
		return "tensgo-report.json"
	case "junit":
		return "tensgo-junit.xml"
	case "markdown":
		return "tensgo-report.md"
	default:
		return "tensgo-report.txt"
	}
}

// DefaultOutputDir is the default directory for generated machine-readable reports.
const DefaultOutputDir = "tensgo-reports"

// ExportReports writes one or more formatted reports to files or stdout ('-').
func ExportReports(data ReportData, formats []string, outputFile string, outputDir string, stdout io.Writer) ([]string, error) {
	if len(formats) == 0 {
		return nil, nil
	}
	if data.SchemaVersion == "" {
		data.SchemaVersion = SchemaVersion
	}
	if data.Timestamp.IsZero() {
		data.Timestamp = time.Now().UTC()
	}

	if outputFile == "-" && len(formats) > 1 {
		return nil, fmt.Errorf("--output-file - (stdout) can only be used with a single output format")
	}

	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = DefaultOutputDir
	}

	var written []string
	for _, format := range formats {
		var payload []byte
		var err error

		switch format {
		case "json":
			payload, err = RenderJSON(data)
		case "junit":
			payload, err = RenderJUnit(data)
		case "markdown":
			payload = []byte(RenderMarkdown(data))
		default:
			return nil, fmt.Errorf("unsupported output format %q", format)
		}
		if err != nil {
			return nil, fmt.Errorf("render %s report: %w", format, err)
		}

		if outputFile == "-" {
			if _, err := stdout.Write(payload); err != nil {
				return nil, fmt.Errorf("write %s report to stdout: %w", format, err)
			}
			if len(payload) > 0 && payload[len(payload)-1] != '\n' {
				_, _ = stdout.Write([]byte("\n"))
			}
			written = append(written, "stdout")
			continue
		}

		var dest string
		if len(formats) == 1 && strings.TrimSpace(outputFile) != "" {
			dest = outputFile
			if parent := filepath.Dir(dest); parent != "" && parent != "." {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return nil, fmt.Errorf("create output directory %q: %w", parent, err)
				}
			}
		} else {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create output directory %q: %w", dir, err)
			}
			dest = filepath.Join(dir, DefaultFilename(format))
		}

		if err := os.WriteFile(dest, payload, 0o644); err != nil {
			return nil, fmt.Errorf("write %s report to %q: %w", format, dest, err)
		}
		written = append(written, dest)
	}

	return written, nil
}

// AppendGitHubStepSummary appends the Markdown summary report to the file at summaryPath.
func AppendGitHubStepSummary(data ReportData, summaryPath string) error {
	if strings.TrimSpace(summaryPath) == "" {
		return fmt.Errorf("GITHUB_STEP_SUMMARY path is empty")
	}
	if data.Timestamp.IsZero() {
		data.Timestamp = time.Now().UTC()
	}
	md := RenderMarkdown(data)

	f, err := os.OpenFile(summaryPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open GitHub step summary %q: %w", summaryPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(md + "\n"); err != nil {
		return fmt.Errorf("write GitHub step summary %q: %w", summaryPath, err)
	}
	return nil
}

func errorRatePct(s metrics.Summary) float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.ErrorCount) / float64(s.TotalRequests) * 100.0
}
