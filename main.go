package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Errtu0/tensgo/internal/engine"
	"github.com/Errtu0/tensgo/internal/metrics"
	"github.com/Errtu0/tensgo/internal/reporter"
	"github.com/Errtu0/tensgo/internal/scenario"
	"github.com/Errtu0/tensgo/internal/tui"
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
	if len(os.Args) == 1 {
		if err := tui.RunMainMenu(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) >= 2 && os.Args[1] == "import" {
		if err := runImportSubcommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	url := flag.String("u", "", "Target URL to load test (required unless --openapi is set)")
	method := flag.String("m", "GET", "HTTP method")
	body := flag.String("b", "", "Request body")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	duration := flag.Duration("d", 10*time.Second, "Test duration")
	timeout := flag.Duration("t", 5*time.Second, "Per-request timeout")
	noTUI := flag.Bool("no-tui", false, "Disable live TUI progress animation (auto-disabled in non-TTY)")
	maxP95 := flag.Duration("max-p95", 0, "CI/CD threshold: maximum allowed P95 latency (e.g. 300ms)")
	maxErrorRate := flag.Float64("max-error-rate", -1, "CI/CD threshold: maximum allowed error rate in percent (e.g. 1.0)")
	baselinePath := flag.String("baseline", "", "Path to previous tensgo JSON report for regression checks")
	maxP95Regression := flag.String("max-p95-regression", "", "Maximum allowed P95 latency increase vs baseline (e.g. 15 or 15%)")
	maxRPSDrop := flag.String("max-rps-drop", "", "Maximum allowed RPS drop vs baseline (e.g. 20 or 20%)")
	htmlPath := flag.String("html", "", "Write standalone dark-mode HTML report to file (e.g. report.html)")
	outputFormat := flag.String("output-format", "", "Comma-separated machine-readable report formats: json,junit,markdown")
	outputFile := flag.String("output-file", "", "Output file path for single format, or '-' for stdout")
	outputDir := flag.String("output-dir", reporter.DefaultOutputDir, "Directory for generated reports when --output-file is omitted")
	githubSummary := flag.Bool("github-summary", false, "Append Markdown summary to GITHUB_STEP_SUMMARY (auto-detected in CI)")
	openapiSource := flag.String("openapi", "", "OpenAPI/Swagger JSON URL or file path to scan and benchmark")
	token := flag.String("token", "", "Authorization header value for OpenAPI scan (e.g. 'Bearer xyz')")
	scenarioPath := flag.String("scenario", "", "Path to YAML scenario file (e.g. ./tensgo.yaml)")
	showVersion := flag.Bool("version", false, "Print tensgo version")
	showVersionShort := flag.Bool("v", false, "Print tensgo version")

	headers := make(headerFlag)
	flag.Var(headers, "H", "HTTP header in 'Key: Value' format (can be repeated)")

	flag.Parse()

	if *showVersion || *showVersionShort {
		fmt.Printf("tensgo %s (by Larch Studios)\n", tui.Version)
		return
	}

	formats, err := reporter.ParseFormats(*outputFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p95RegPct, err := reporter.ParsePercent(*maxP95Regression)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	rpsDropPct, err := reporter.ParsePercent(*maxRPSDrop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var baselineRep *reporter.BaselineReport
	if strings.TrimSpace(*baselinePath) != "" {
		baselineRep, err = reporter.LoadBaseline(*baselinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if strings.TrimSpace(*scenarioPath) != "" {
		rep, err := tui.RunScenarioReport(*scenarioPath, *noTUI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		endpoints := make([]reporter.EndpointReport, 0, len(rep.StepResults))
		for _, sr := range rep.StepResults {
			endpoints = append(endpoints, reporter.EndpointReport{
				Name:    sr.Name,
				Method:  sr.Method,
				Target:  sr.Path,
				Summary: sr.Summary,
			})
		}

		gates, passed := reporter.BuildGateEvaluations(rep.Overall, endpoints, *maxP95, *maxErrorRate)
		baseGates, baseViolations, basePassed := reporter.EvaluateBaselineGates(baselineRep, rep.Overall, p95RegPct, rpsDropPct)
		gates = append(gates, baseGates...)
		passed = passed && basePassed
		for _, v := range baseViolations {
			fmt.Fprintf(os.Stderr, "Baseline Gate Violation: %s\n", v)
		}

		data := reporter.ReportData{
			SchemaVersion: reporter.SchemaVersion,
			Timestamp:     time.Now().UTC(),
			Target:        rep.BaseURL,
			Duration:      rep.TotalDuration,
			VUs:           rep.VUs,
			Profile:       "scenario",
			Summary:       rep.Overall,
			Endpoints:     endpoints,
			Gates:         gates,
			Passed:        passed,
		}

		if err := handlePipelineExports(data, formats, *outputFile, *outputDir, *githubSummary); err != nil {
			fmt.Fprintf(os.Stderr, "Error exporting reports: %v\n", err)
			os.Exit(1)
		}
		if !passed {
			os.Exit(1)
		}
		return
	}

	if strings.TrimSpace(*openapiSource) != "" {
		if err := tui.RunOpenAPIPicker(*openapiSource, *token, *concurrency, *duration, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if strings.TrimSpace(*url) == "" {
		fmt.Fprintln(os.Stderr, "Error: target URL (-u) or --openapi is required.")
		fmt.Fprintln(os.Stderr, `Usage: tensgo -u https://example.com [-m POST] [-H "Content-Type: application/json"] [-b '{"ok":true}'] [-c 10] [-d 10s] [-t 5s] [--max-p95 300ms] [--max-error-rate 1.0] [--html report.html] [--no-tui]`)
		fmt.Fprintln(os.Stderr, `   or: tensgo --openapi http://localhost:5001/openapi.json [--token "Bearer ..."] [-c 10] [-d 10s]`)
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
	endpoints := []reporter.EndpointReport{
		{
			Method:  cfg.Method,
			Target:  cfg.URL,
			Summary: summary,
		},
	}
	gates, passed := reporter.BuildGateEvaluations(summary, endpoints, *maxP95, *maxErrorRate)
	baseGates, baseViolations, basePassed := reporter.EvaluateBaselineGates(baselineRep, summary, p95RegPct, rpsDropPct)
	gates = append(gates, baseGates...)
	passed = passed && basePassed

	hasThresholds := *maxP95 > 0 || *maxErrorRate >= 0 || len(baseGates) > 0
	violations := summary.CheckThresholds(*maxP95, *maxErrorRate)
	violations = append(violations, baseViolations...)

	if *outputFile != "-" {
		reporter.RenderTerminalReport(cfg, summary, violations, hasThresholds)
	}

	if *htmlPath != "" {
		if err := reporter.GenerateHTMLReport(*htmlPath, cfg, summary, violations, hasThresholds); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating HTML report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("HTML report written to %s\n", *htmlPath)
	}

	data := reporter.ReportData{
		SchemaVersion: reporter.SchemaVersion,
		Timestamp:     time.Now().UTC(),
		Target:        cfg.URL,
		Method:        cfg.Method,
		Duration:      totalDuration,
		VUs:           cfg.Concurrency,
		Profile:       "constant",
		Summary:       summary,
		Endpoints:     endpoints,
		Gates:         gates,
		Passed:        passed,
	}

	if err := handlePipelineExports(data, formats, *outputFile, *outputDir, *githubSummary); err != nil {
		fmt.Fprintf(os.Stderr, "Error exporting reports: %v\n", err)
		os.Exit(1)
	}

	if len(violations) > 0 || !passed {
		os.Exit(1)
	}
}

func runImportSubcommand(args []string) error {
	if len(args) == 0 || args[0] != "curl" {
		return fmt.Errorf("usage: tensgo import curl [--out tensgo-scenario.yaml|-] \"<curl-command>\"")
	}

	fs := flag.NewFlagSet("import curl", flag.ContinueOnError)
	outPath := fs.String("out", "tensgo-scenario.yaml", "Output YAML path (or '-' for stdout)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("missing cURL command or '-' for stdin")
	}

	var rawCurl string
	if len(rest) == 1 && rest[0] == "-" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		rawCurl = string(b)
	} else if len(rest) == 1 {
		rawCurl = rest[0]
	} else {
		quoted := make([]string, len(rest))
		for i, arg := range rest {
			if strings.ContainsAny(arg, " \t\"") && !strings.Contains(arg, "'") {
				quoted[i] = "'" + arg + "'"
			} else {
				quoted[i] = arg
			}
		}
		rawCurl = strings.Join(quoted, " ")
	}

	sc, err := scenario.ParseCurlCommand(rawCurl)
	if err != nil {
		return err
	}
	yamlBytes, err := scenario.MarshalScenarioYAML(sc)
	if err != nil {
		return fmt.Errorf("marshal scenario YAML: %w", err)
	}

	if *outPath == "-" {
		fmt.Print(string(yamlBytes))
		return nil
	}
	if err := os.WriteFile(*outPath, yamlBytes, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", *outPath, err)
	}
	fmt.Printf("Imported scenario written to %s\n", *outPath)
	return nil
}

func handlePipelineExports(data reporter.ReportData, formats []string, outputFile string, outputDir string, githubSummary bool) error {
	if len(formats) > 0 {
		written, err := reporter.ExportReports(data, formats, outputFile, outputDir, os.Stdout)
		if err != nil {
			return err
		}
		if outputFile != "-" {
			for _, dest := range written {
				fmt.Printf("Report written to %s\n", dest)
			}
		}
	}

	summaryEnv := strings.TrimSpace(os.Getenv("GITHUB_STEP_SUMMARY"))
	if githubSummary && summaryEnv == "" {
		return fmt.Errorf("--github-summary was set, but GITHUB_STEP_SUMMARY environment variable is empty")
	}
	if summaryEnv != "" {
		if err := reporter.AppendGitHubStepSummary(data, summaryEnv); err != nil {
			return err
		}
	}
	return nil
}
