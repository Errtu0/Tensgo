package reporter

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

type jsonMetadata struct {
	Timestamp  string `json:"timestamp"`
	Target     string `json:"target"`
	Method     string `json:"method,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	VUs        int    `json:"vus"`
	Profile    string `json:"profile"`
}

type jsonLatency struct {
	MinMs float64 `json:"min_ms"`
	AvgMs float64 `json:"avg_ms"`
	P50Ms float64 `json:"p50_ms"`
	P90Ms float64 `json:"p90_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	MaxMs float64 `json:"max_ms"`
}

type jsonMetrics struct {
	TotalRequests int         `json:"total_requests"`
	SuccessCount  int         `json:"success_count"`
	ErrorCount    int         `json:"error_count"`
	NetworkErrors int         `json:"network_errors"`
	ErrorRatePct  float64     `json:"error_rate_pct"`
	RPS           float64     `json:"rps"`
	StatusCodes   map[int]int `json:"status_codes"`
	Latency       jsonLatency `json:"latency"`
}

type jsonEndpoint struct {
	Name    string      `json:"name,omitempty"`
	Method  string      `json:"method"`
	Target  string      `json:"target"`
	Metrics jsonMetrics `json:"metrics"`
}

type jsonReport struct {
	SchemaVersion string           `json:"schema_version"`
	Passed        bool             `json:"passed"`
	Metadata      jsonMetadata     `json:"metadata"`
	Metrics       jsonMetrics      `json:"metrics"`
	Endpoints     []jsonEndpoint   `json:"endpoints"`
	Gates         []GateEvaluation `json:"gates"`
}

// RenderJSON serializes ReportData into a formatted, versioned JSON document.
func RenderJSON(data ReportData) ([]byte, error) {
	schemaVer := data.SchemaVersion
	if schemaVer == "" {
		schemaVer = SchemaVersion
	}
	ts := data.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	profile := data.Profile
	if profile == "" {
		profile = "constant"
	}

	endpoints := make([]jsonEndpoint, 0, len(data.Endpoints))
	for _, ep := range data.Endpoints {
		endpoints = append(endpoints, jsonEndpoint{
			Name:    ep.Name,
			Method:  ep.Method,
			Target:  ep.Target,
			Metrics: toJSONMetrics(ep.Summary),
		})
	}

	gates := data.Gates
	if gates == nil {
		gates = []GateEvaluation{}
	}

	doc := jsonReport{
		SchemaVersion: schemaVer,
		Passed:        data.Passed,
		Metadata: jsonMetadata{
			Timestamp:  ts.UTC().Format(time.RFC3339),
			Target:     data.Target,
			Method:     data.Method,
			DurationMs: data.Duration.Milliseconds(),
			VUs:        data.VUs,
			Profile:    profile,
		},
		Metrics:   toJSONMetrics(data.Summary),
		Endpoints: endpoints,
		Gates:     gates,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func toJSONMetrics(s metrics.Summary) jsonMetrics {
	codes := s.StatusCodes
	if codes == nil {
		codes = map[int]int{}
	}
	return jsonMetrics{
		TotalRequests: s.TotalRequests,
		SuccessCount:  s.SuccessCount,
		ErrorCount:    s.ErrorCount,
		NetworkErrors: s.NetworkErrors,
		ErrorRatePct:  round2(errorRatePct(s)),
		RPS:           round2(s.RPS),
		StatusCodes:   codes,
		Latency: jsonLatency{
			MinMs: durationToMs(s.Min),
			AvgMs: durationToMs(s.Avg),
			P50Ms: durationToMs(s.P50),
			P90Ms: durationToMs(s.P90),
			P95Ms: durationToMs(s.P95),
			P99Ms: durationToMs(s.P99),
			MaxMs: durationToMs(s.Max),
		},
	}
}

func durationToMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100.0
}
