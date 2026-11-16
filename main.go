package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/reporter"
)

type headerFlag map[string]string

func (h headerFlag) String() string {
	var parts []string
	for k, v := range h {
		parts = append(parts, fmt.Sprintf("%s: %s", k, v))
	}
	return strings.Join(parts, ", ")
}

func (h headerFlag) Set(value string) error {
	key, val, found := strings.Cut(value, ":")
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)
	if !found || key == "" {
		return fmt.Errorf("invalid header %q: expected 'Key: Value'", value)
	}
	h[key] = val
	return nil
}

func isInteractiveTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func main() {
	url := flag.String("u", "", "Target URL to load test (required)")
	method := flag.String("m", "GET", "HTTP method")
	body := flag.String("b", "", "Request body")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	duration := flag.Duration("d", 10*time.Second, "Test duration")
	timeout := flag.Duration("t", 5*time.Second, "Per-request timeout")
	noTUI := flag.Bool("no-tui", false, "Disable live TUI progress animation (auto-disabled in non-TTY)")
	maxP95 := flag.Duration("max-p95", 0, "CI/CD threshold: maximum allowed P95 latency (e.g. 300ms)")
	maxErrorRate := flag.Float64("max-error-rate", -1, "CI/CD threshold: maximum allowed error rate in percent (e.g. 1.0)")
	htmlPath := flag.String("html", "", "Write standalone dark-mode HTML report to file (e.g. report.html)")

	headers := make(headerFlag)
	flag.Var(headers, "H", "HTTP header in 'Key: Value' format (can be repeated)")

	flag.Parse()

	if strings.TrimSpace(*url) == "" {
		fmt.Fprintln(os.Stderr, "Error: target URL (-u) is required.")
		fmt.Fprintln(os.Stderr, `Usage: tensgo -u https://example.com [-m POST] [-H "Content-Type: application/json"] [-b '{"ok":true}'] [-c 10] [-d 10s] [-t 5s] [--max-p95 300ms] [--max-error-rate 1.0] [--html report.html] [--no-tui]`)
		flag.PrintDefaults()
		os.Exit(1)
	}

	cfg := engine.Config{
		URL:         *url,
		Method:      strings.ToUpper(*method),
		Headers:     headers,
		Body:        *body,
		Concurrency: *concurrency,
		Duration:    *duration,
		Timeout:     *timeout,
	}

	var (
		results       []metrics.Result
		totalDuration time.Duration
		err           error
	)

	if !*noTUI && isInteractiveTerminal() {
		results, totalDuration, err = reporter.RunWithProgress(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: TUI progress failed (%v), falling back to standard runner.\n", err)
			results, totalDuration = engine.Run(cfg)
		}
	} else {
		results, totalDuration = engine.Run(cfg)
	}

	summary := metrics.Calculate(results, totalDuration)
	hasThresholds := *maxP95 > 0 || *maxErrorRate >= 0
	violations := summary.CheckThresholds(*maxP95, *maxErrorRate)

	reporter.RenderTerminalReport(cfg, summary, violations, hasThresholds)

	if *htmlPath != "" {
		if err := reporter.GenerateHTMLReport(*htmlPath, cfg, summary, violations, hasThresholds); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating HTML report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("HTML report written to %s\n", *htmlPath)
	}

	if len(violations) > 0 {
		os.Exit(1)
	}
}
