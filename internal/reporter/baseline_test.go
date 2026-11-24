package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

func TestParsePercent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "empty", input: "", want: 0},
		{name: "plain int", input: "15", want: 15},
		{name: "with percent sign", input: " 15% ", want: 15},
		{name: "decimal percent", input: "12.5%", want: 12.5},
		{name: "negative invalid", input: "-5%", wantErr: true},
		{name: "non-numeric invalid", input: "abc%", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePercent(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParsePercent(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestLoadBaseline(t *testing.T) {
	tmp := t.TempDir()

	validPath := filepath.Join(tmp, "baseline.json")
	validJSON := `{
		"schema_version": "0.3.0",
		"metadata": { "target": "http://localhost:8080/health", "vus": 10 },
		"metrics": { "rps": 500.0, "latency": { "p95_ms": 100.0 } }
	}`
	if err := os.WriteFile(validPath, []byte(validJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := LoadBaseline(validPath)
	if err != nil {
		t.Fatalf("LoadBaseline valid error: %v", err)
	}
	if rep.Metrics.RPS != 500.0 || rep.Metrics.Latency.P95Ms != 100.0 {
		t.Fatalf("unexpected baseline metrics: %+v", rep.Metrics)
	}

	badPath := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{"metrics":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(badPath); err == nil {
		t.Fatalf("expected error for missing schema_version")
	}
}

func TestEvaluateBaselineGates(t *testing.T) {
	baseline := &BaselineReport{
		SchemaVersion: "0.3.0",
		Metrics: jsonMetrics{
			RPS: 1000.0,
			Latency: jsonLatency{
				P95Ms: 100.0,
			},
		},
	}

	tests := []struct {
		name           string
		currP95        time.Duration
		currRPS        float64
		maxP95RegPct   float64
		maxRPSDropPct  float64
		wantPassed     bool
		wantGates      int
		wantViolations int
	}{
		{
			name:           "within thresholds",
			currP95:        110 * time.Millisecond, // +10%
			currRPS:        900.0,                  // 10% drop
			maxP95RegPct:   15,
			maxRPSDropPct:  20,
			wantPassed:     true,
			wantGates:      2,
			wantViolations: 0,
		},
		{
			name:           "p95 regression breached",
			currP95:        130 * time.Millisecond, // +30%
			currRPS:        950.0,
			maxP95RegPct:   15,
			maxRPSDropPct:  20,
			wantPassed:     false,
			wantGates:      2,
			wantViolations: 1,
		},
		{
			name:           "rps drop breached",
			currP95:        100 * time.Millisecond,
			currRPS:        700.0, // 30% drop
			maxP95RegPct:   15,
			maxRPSDropPct:  20,
			wantPassed:     false,
			wantGates:      2,
			wantViolations: 1,
		},
		{
			name:           "both breached",
			currP95:        150 * time.Millisecond, // +50%
			currRPS:        600.0,                  // 40% drop
			maxP95RegPct:   15,
			maxRPSDropPct:  20,
			wantPassed:     false,
			wantGates:      2,
			wantViolations: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			curr := metrics.Summary{
				P95: tc.currP95,
				RPS: tc.currRPS,
			}
			gates, violations, passed := EvaluateBaselineGates(baseline, curr, tc.maxP95RegPct, tc.maxRPSDropPct)
			if passed != tc.wantPassed {
				t.Fatalf("passed=%v, want %v (violations=%v)", passed, tc.wantPassed, violations)
			}
			if len(gates) != tc.wantGates {
				t.Fatalf("len(gates)=%d, want %d", len(gates), tc.wantGates)
			}
			if len(violations) != tc.wantViolations {
				t.Fatalf("len(violations)=%d, want %d (%v)", len(violations), tc.wantViolations, violations)
			}
			if !tc.wantPassed && !strings.Contains(violations[0], "baseline") {
				t.Fatalf("expected violation message to mention baseline, got %q", violations[0])
			}
		})
	}
}
