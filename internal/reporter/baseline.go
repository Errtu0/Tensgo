package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// BaselineReport represents a previously exported tensgo JSON report used for regression checks.
type BaselineReport struct {
	SchemaVersion string       `json:"schema_version"`
	Metadata      jsonMetadata `json:"metadata"`
	Metrics       jsonMetrics  `json:"metrics"`
}

// LoadBaseline reads and parses a tensgo JSON report file.
func LoadBaseline(path string) (*BaselineReport, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("baseline path cannot be empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline %q: %w", path, err)
	}

	var rep BaselineReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("parse baseline JSON %q: %w", path, err)
	}
	if rep.SchemaVersion == "" {
		return nil, fmt.Errorf("baseline %q is missing schema_version", path)
	}
	return &rep, nil
}

// ParsePercent parses a percentage string such as "15", "15%", or "12.5%".
// An empty string returns 0, nil (disabled).
func ParsePercent(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid percentage %q: expected non-negative number (e.g. 15 or 15%%)", raw)
	}
	return v, nil
}

// EvaluateBaselineGates compares current metrics against a baseline report and returns
// GateEvaluation entries, human-readable violation strings, and overall pass status.
func EvaluateBaselineGates(baseline *BaselineReport, current metrics.Summary, maxP95RegPct, maxRPSDropPct float64) ([]GateEvaluation, []string, bool) {
	if baseline == nil {
		return nil, nil, true
	}

	var (
		gates      []GateEvaluation
		violations []string
		allPassed  = true
	)

	if maxP95RegPct > 0 {
		baseP95Ms := baseline.Metrics.Latency.P95Ms
		currP95Ms := durationToMs(current.P95)
		var deltaPct float64
		if baseP95Ms > 0 {
			deltaPct = (currP95Ms - baseP95Ms) / baseP95Ms * 100.0
		}
		passed := deltaPct <= maxP95RegPct
		if !passed {
			allPassed = false
		}
		actual := fmt.Sprintf("%.2fms (baseline %.2fms, %+.1f%%)", currP95Ms, baseP95Ms, deltaPct)
		expected := fmt.Sprintf("<= +%.1f%%", maxP95RegPct)
		var msg string
		if !passed {
			msg = fmt.Sprintf("P95 regression %+.1f%% exceeded max +%.1f%% (baseline %.2fms -> current %.2fms)",
				deltaPct, maxP95RegPct, baseP95Ms, currP95Ms)
			violations = append(violations, msg)
		}
		gates = append(gates, GateEvaluation{
			Name:     "Baseline P95 Regression Gate",
			Passed:   passed,
			Expected: expected,
			Actual:   actual,
			Message:  msg,
		})
	}

	if maxRPSDropPct > 0 {
		baseRPS := baseline.Metrics.RPS
		currRPS := current.RPS
		var dropPct float64
		if baseRPS > 0 {
			dropPct = (baseRPS - currRPS) / baseRPS * 100.0
		}
		passed := dropPct <= maxRPSDropPct
		if !passed {
			allPassed = false
		}
		actual := fmt.Sprintf("%.2f req/s (baseline %.2f, %.1f%% drop)", currRPS, baseRPS, dropPct)
		expected := fmt.Sprintf("<= %.1f%% drop", maxRPSDropPct)
		var msg string
		if !passed {
			msg = fmt.Sprintf("RPS drop %.1f%% exceeded max %.1f%% (baseline %.2f -> current %.2f)",
				dropPct, maxRPSDropPct, baseRPS, currRPS)
			violations = append(violations, msg)
		}
		gates = append(gates, GateEvaluation{
			Name:     "Baseline RPS Drop Gate",
			Passed:   passed,
			Expected: expected,
			Actual:   actual,
			Message:  msg,
		})
	}

	return gates, violations, allPassed
}
