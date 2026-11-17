package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// StepResult holds aggregated metrics for a single scenario step.
type StepResult struct {
	Name            string
	Method          string
	Path            string
	Summary         metrics.Summary
	ExtractWarnings map[string]int64
}

// Report contains overall and per-step metrics for a scenario run.
type Report struct {
	Name          string
	BaseURL       string
	VUs           int
	TotalDuration time.Duration
	Overall       metrics.Summary
	StepResults   []StepResult
}

// Run executes the multi-step scenario across VUs concurrently for the configured duration.
func Run(cfg *ParsedConfig, completed *atomic.Int64) Report {
	sc := cfg.Scenario

	transport := &http.Transport{
		MaxIdleConns:        sc.VUs,
		MaxIdleConnsPerHost: sc.VUs,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	var (
		mu              sync.Mutex
		allResults      []metrics.Result
		stepBuckets     = make([][]metrics.Result, len(sc.Steps))
		extractWarnings = make([]map[string]int64, len(sc.Steps))
		wg              sync.WaitGroup
	)
	for i := range extractWarnings {
		extractWarnings[i] = make(map[string]int64)
	}

	testStart := time.Now()
	baseURL := strings.TrimRight(sc.BaseURL, "/")

	for range sc.VUs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vars := make(map[string]string)

			for {
				if ctx.Err() != nil {
					return
				}

				for stepIdx, step := range sc.Steps {
					if ctx.Err() != nil {
						return
					}

					resolvedPath := interpolate(step.Path, vars)
					if !strings.HasPrefix(resolvedPath, "/") {
						resolvedPath = "/" + resolvedPath
					}
					targetURL := baseURL + resolvedPath

					var bodyReader io.Reader
					hasBody := false
					if step.Body != nil {
						bodyStr := buildBodyString(step.Body, vars)
						if bodyStr != "" {
							bodyReader = strings.NewReader(bodyStr)
							hasBody = true
						}
					}

					req, err := http.NewRequestWithContext(ctx, step.Method, targetURL, bodyReader)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						res := metrics.Result{Err: err}
						mu.Lock()
						allResults = append(allResults, res)
						stepBuckets[stepIdx] = append(stepBuckets[stepIdx], res)
						mu.Unlock()
						if completed != nil {
							completed.Add(1)
						}
						break
					}

					if hasBody {
						req.Header.Set("Content-Type", "application/json")
					}
					for hk, hv := range step.Headers {
						req.Header.Set(hk, interpolate(hv, vars))
					}

					reqStart := time.Now()
					resp, err := client.Do(req)
					elapsed := time.Since(reqStart)

					if err != nil {
						if ctx.Err() != nil {
							return
						}
						res := metrics.Result{Duration: elapsed, Err: err}
						mu.Lock()
						allResults = append(allResults, res)
						stepBuckets[stepIdx] = append(stepBuckets[stepIdx], res)
						mu.Unlock()
						if completed != nil {
							completed.Add(1)
						}
						if len(step.Extract) > 0 {
							break
						}
						continue
					}

					var respBytes []byte
					if len(step.Extract) > 0 && resp.StatusCode < 400 {
						respBytes, _ = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
					} else {
						_, _ = io.Copy(io.Discard, resp.Body)
					}
					_ = resp.Body.Close()

					res := metrics.Result{
						Duration:   elapsed,
						StatusCode: resp.StatusCode,
					}
					mu.Lock()
					allResults = append(allResults, res)
					stepBuckets[stepIdx] = append(stepBuckets[stepIdx], res)
					mu.Unlock()
					if completed != nil {
						completed.Add(1)
					}

					if resp.StatusCode >= 400 && len(step.Extract) > 0 {
						break
					}

					if len(step.Extract) > 0 && len(respBytes) > 0 {
						var parsed any
						if err := json.Unmarshal(respBytes, &parsed); err == nil {
							for varName, jsonPath := range step.Extract {
								val := extractJSONPath(parsed, jsonPath)
								if val == "" && isTokenVarName(varName) {
									val = extractTokenRecursive(parsed)
								}
								if val != "" {
									vars[varName] = val
								} else {
									mu.Lock()
									extractWarnings[stepIdx][varName]++
									mu.Unlock()
								}
							}
						} else {
							mu.Lock()
							for varName := range step.Extract {
								extractWarnings[stepIdx][varName]++
							}
							mu.Unlock()
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	totalDur := time.Since(testStart)

	stepResults := make([]StepResult, len(sc.Steps))
	for i, step := range sc.Steps {
		stepResults[i] = StepResult{
			Name:            step.Name,
			Method:          step.Method,
			Path:            step.Path,
			Summary:         metrics.Calculate(stepBuckets[i], totalDur),
			ExtractWarnings: extractWarnings[i],
		}
	}

	return Report{
		Name:          sc.Name,
		BaseURL:       sc.BaseURL,
		VUs:           sc.VUs,
		TotalDuration: totalDur,
		Overall:       metrics.Calculate(allResults, totalDur),
		StepResults:   stepResults,
	}
}

func interpolate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func buildBodyString(body any, vars map[string]string) string {
	switch v := body.(type) {
	case string:
		return interpolate(v, vars)
	default:
		interpolated := interpolateAny(v, vars)
		b, err := json.Marshal(interpolated)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func interpolateAny(v any, vars map[string]string) any {
	switch val := v.(type) {
	case string:
		return interpolate(val, vars)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = interpolateAny(item, vars)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = interpolateAny(item, vars)
		}
		return out
	default:
		return v
	}
}

func extractJSONPath(doc any, path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "$.")
	p = strings.TrimPrefix(p, "$")
	p = strings.TrimPrefix(p, ".")
	if p == "" {
		return ""
	}

	parts := strings.Split(p, ".")
	current := doc
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		val, exists := m[part]
		if !exists {
			return ""
		}
		current = val
	}

	switch v := current.(type) {
	case string:
		return v
	case float64, bool, int, int64:
		return fmt.Sprintf("%v", v)
	default:
		return ""
	}
}

func isTokenVarName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "token" || lower == "accesstoken" || lower == "access_token" || lower == "jwt" || lower == "id_token"
}

func extractTokenRecursive(v any) string {
	tokenKeys := []string{"accessToken", "access_token", "token", "jwt", "id_token"}
	switch val := v.(type) {
	case map[string]any:
		for _, want := range tokenKeys {
			for k, raw := range val {
				if strings.EqualFold(k, want) {
					if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
		}
		for _, raw := range val {
			if tok := extractTokenRecursive(raw); tok != "" {
				return tok
			}
		}
	case []any:
		for _, item := range val {
			if tok := extractTokenRecursive(item); tok != "" {
				return tok
			}
		}
	}
	return ""
}
