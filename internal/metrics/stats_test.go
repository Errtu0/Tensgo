package metrics

import (
	"errors"
	"testing"
	"time"
)

func TestCalculate(t *testing.T) {
	empty := Calculate(nil, 2*time.Second)
	if empty.TotalRequests != 0 || empty.RPS != 0 || len(empty.StatusCodes) != 0 {
		t.Fatalf("expected zeroed summary for empty results, got %+v", empty)
	}

	results := []Result{
		{Duration: 10 * time.Millisecond, StatusCode: 200},
		{Duration: 20 * time.Millisecond, StatusCode: 201},
		{Duration: 30 * time.Millisecond, StatusCode: 404},
		{Duration: 40 * time.Millisecond, StatusCode: 500},
		{Duration: 50 * time.Millisecond, Err: errors.New("dial timeout")},
	}

	s := Calculate(results, 2*time.Second)
	if s.TotalRequests != 5 || s.SuccessCount != 2 || s.ErrorCount != 3 || s.NetworkErrors != 1 {
		t.Fatalf("unexpected counts: %+v", s)
	}
	if s.Min != 10*time.Millisecond || s.Max != 50*time.Millisecond || s.Avg != 30*time.Millisecond {
		t.Fatalf("unexpected min/avg/max: min=%s avg=%s max=%s", s.Min, s.Avg, s.Max)
	}
	if s.P50 != 30*time.Millisecond || s.P90 != 50*time.Millisecond || s.P95 != 50*time.Millisecond || s.P99 != 50*time.Millisecond {
		t.Fatalf("unexpected percentiles: p50=%s p90=%s p95=%s p99=%s", s.P50, s.P90, s.P95, s.P99)
	}
	if s.RPS != 2.5 {
		t.Fatalf("RPS=%v, want 2.5", s.RPS)
	}
	s.Print()
}

func TestCheckThresholdsAndFormatDuration(t *testing.T) {
	s := Summary{
		TotalRequests: 100,
		ErrorCount:    5,
		P95:           250 * time.Millisecond,
	}

	if v := s.CheckThresholds(0, -1); len(v) != 0 {
		t.Fatalf("expected 0 violations when disabled, got %v", v)
	}
	if v := s.CheckThresholds(300*time.Millisecond, 5.0); len(v) != 0 {
		t.Fatalf("expected 0 violations within limits, got %v", v)
	}
	if v := s.CheckThresholds(100*time.Millisecond, 1.0); len(v) != 2 {
		t.Fatalf("expected 2 violations, got %v", v)
	}

	if got := FormatDuration(450 * time.Microsecond); got != "450µs" {
		t.Fatalf("FormatDuration(450µs) = %q", got)
	}
	if got := FormatDuration(12500 * time.Microsecond); got != "12.50ms" {
		t.Fatalf("FormatDuration(12.5ms) = %q", got)
	}
}
