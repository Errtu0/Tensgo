package metrics

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// Result holds the outcome and latency of a single HTTP request.
type Result struct {
	Duration   time.Duration
	StatusCode int
	Err        error
}

// Summary contains aggregated load test statistics.
type Summary struct {
	TotalRequests int
	SuccessCount  int
	ErrorCount    int
	NetworkErrors int
	StatusCodes   map[int]int
	TotalDuration time.Duration
	RPS           float64
	P50           time.Duration
	P95           time.Duration
	P99           time.Duration
	Min           time.Duration
	Max           time.Duration
}

// Calculate computes aggregated metrics from raw request results and total test duration.
func Calculate(results []Result, totalDuration time.Duration) Summary {
	total := len(results)
	summary := Summary{
		TotalRequests: total,
		TotalDuration: totalDuration,
		StatusCodes:   make(map[int]int),
	}

	if total == 0 {
		return summary
	}

	durations := make([]time.Duration, 0, total)
	for _, r := range results {
		if r.Err != nil {
			summary.ErrorCount++
			summary.NetworkErrors++
		} else {
			summary.StatusCodes[r.StatusCode]++
			if r.StatusCode >= 400 {
				summary.ErrorCount++
			} else {
				summary.SuccessCount++
			}
		}
		durations = append(durations, r.Duration)
	}

	slices.Sort(durations)

	summary.Min = durations[0]
	summary.Max = durations[total-1]
	summary.P50 = percentile(durations, 50)
	summary.P95 = percentile(durations, 95)
	summary.P99 = percentile(durations, 99)

	if totalDuration > 0 {
		summary.RPS = float64(total) / totalDuration.Seconds()
	}

	return summary
}

// CheckThresholds evaluates summary metrics against CI/CD quality gate thresholds
// and returns a slice of human-readable violation messages.
func (s Summary) CheckThresholds(maxP95 time.Duration, maxErrorRate float64) []string {
	var violations []string

	if maxP95 > 0 && s.P95 > maxP95 {
		violations = append(violations, fmt.Sprintf(
			"P95 latency %s exceeded max threshold %s",
			s.P95.Round(time.Microsecond), maxP95,
		))
	}

	if maxErrorRate >= 0 {
		var errorRate float64
		if s.TotalRequests > 0 {
			errorRate = float64(s.ErrorCount) / float64(s.TotalRequests) * 100.0
		}
		if errorRate > maxErrorRate {
			violations = append(violations, fmt.Sprintf(
				"Error rate %.2f%% exceeded max threshold %.2f%%",
				errorRate, maxErrorRate,
			))
		}
	}

	return violations
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100.0*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// FormatDuration formats sub-millisecond durations in microseconds and longer ones in milliseconds.
func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}

// Print writes a formatted load test report to standard output.
func (s Summary) Print() {
	fmt.Println("========================================")
	fmt.Println("           tensgo Load Report           ")
	fmt.Println("========================================")
	fmt.Printf("Total Duration : %s\n", s.TotalDuration.Round(time.Millisecond))
	fmt.Printf("Total Requests : %d\n", s.TotalRequests)
	fmt.Printf("Success        : %d\n", s.SuccessCount)
	fmt.Printf("Errors         : %d\n", s.ErrorCount)
	fmt.Printf("RPS            : %.2f req/s\n", s.RPS)
	fmt.Println("----------------------------------------")
	fmt.Println("Latency:")
	fmt.Printf("  Min          : %s\n", s.Min.Round(time.Microsecond))
	fmt.Printf("  P50          : %s\n", s.P50.Round(time.Microsecond))
	fmt.Printf("  P95          : %s\n", s.P95.Round(time.Microsecond))
	fmt.Printf("  P99          : %s\n", s.P99.Round(time.Microsecond))
	fmt.Printf("  Max          : %s\n", s.Max.Round(time.Microsecond))
	if len(s.StatusCodes) > 0 || s.NetworkErrors > 0 {
		fmt.Println("----------------------------------------")
		fmt.Println("Status Code Distribution:")
		codes := make([]int, 0, len(s.StatusCodes))
		for code := range s.StatusCodes {
			codes = append(codes, code)
		}
		slices.Sort(codes)
		for _, code := range codes {
			fmt.Printf("  [%d]: %d\n", code, s.StatusCodes[code])
		}
		if s.NetworkErrors > 0 {
			fmt.Printf("  [Network Error]: %d\n", s.NetworkErrors)
		}
	}
	fmt.Println("========================================")
}
