package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

// StepResult holds aggregated metrics for a single scenario step.
type StepResult struct {
	Name              string
	Method            string
	Path              string
	Summary           metrics.Summary
	ExtractWarnings   map[string]int64
	AssertionFailures int64
	FailureReasons    map[string]int64
}

// Report contains overall and per-step metrics for a scenario run.
type Report struct {
	Name              string
	BaseURL           string
	VUs               int
	TotalDuration     time.Duration
	Overall           metrics.Summary
	StepResults       []StepResult
	AssertionFailures int64
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
		mu                sync.Mutex
		allResults        []metrics.Result
		stepBuckets       = make([][]metrics.Result, len(sc.Steps))
		extractWarnings   = make([]map[string]int64, len(sc.Steps))
		assertionFailures = make([]int64, len(sc.Steps))
		failureReasons    = make([]map[string]int64, len(sc.Steps))
		wg                sync.WaitGroup
	)
	for i := range extractWarnings {
		extractWarnings[i] = make(map[string]int64)
		failureReasons[i] = make(map[string]int64)
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

					needBody := len(step.Extract) > 0 || step.HasAssertions()
					var respBytes []byte
					if needBody {
						respBytes, _ = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
					} else {
						_, _ = io.Copy(io.Discard, resp.Body)
					}
					_ = resp.Body.Close()

					var parsedJSON any
					hasJSON := false
					if needBody && len(respBytes) > 0 {
						if err := json.Unmarshal(respBytes, &parsedJSON); err == nil {
							hasJSON = true
						}
					}

					assertErr := evaluateAssertions(step, resp.StatusCode, resp.Header, respBytes, parsedJSON, hasJSON)

					res := metrics.Result{
						Duration:   elapsed,
						StatusCode: resp.StatusCode,
					}
					if assertErr != "" {
						res.Err = fmt.Errorf("assertion failed: %s", assertErr)
					}

					mu.Lock()
					allResults = append(allResults, res)
					stepBuckets[stepIdx] = append(stepBuckets[stepIdx], res)
					if assertErr != "" {
						assertionFailures[stepIdx]++
						failureReasons[stepIdx][assertErr]++
					}
					mu.Unlock()
					if completed != nil {
						completed.Add(1)
					}

					if (resp.StatusCode >= 400 || assertErr != "") && len(step.Extract) > 0 {
						break
					}

					if len(step.Extract) > 0 {
						for varName, expr := range step.Extract {
							var extracted string
							if strings.HasPrefix(strings.ToLower(strings.TrimSpace(expr)), "header:") {
								hdrName := strings.TrimSpace(expr[len("header:"):])
								extracted = resp.Header.Get(hdrName)
							} else if hasJSON {
								extracted = extractJSONPath(parsedJSON, expr)
								if extracted == "" && isTokenVarName(varName) {
									extracted = extractTokenRecursive(parsedJSON)
								}
							}
							if extracted != "" {
								vars[varName] = extracted
							} else {
								mu.Lock()
								extractWarnings[stepIdx][varName]++
								mu.Unlock()
							}
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	totalDur := time.Since(testStart)

	var totalAssertFails int64
	stepResults := make([]StepResult, len(sc.Steps))
	for i, step := range sc.Steps {
		totalAssertFails += assertionFailures[i]
		stepResults[i] = StepResult{
			Name:              step.Name,
			Method:            step.Method,
			Path:              step.Path,
			Summary:           metrics.Calculate(stepBuckets[i], totalDur),
			ExtractWarnings:   extractWarnings[i],
			AssertionFailures: assertionFailures[i],
			FailureReasons:    failureReasons[i],
		}
	}

	return Report{
		Name:              sc.Name,
		BaseURL:           sc.BaseURL,
		VUs:               sc.VUs,
		TotalDuration:     totalDur,
		Overall:           metrics.Calculate(allResults, totalDur),
		StepResults:       stepResults,
		AssertionFailures: totalAssertFails,
	}
}

func interpolate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	if strings.Contains(s, "{{random_int}}") {
		s = strings.ReplaceAll(s, "{{random_int}}", strconv.Itoa(rand.IntN(1_000_000)))
	}
	if strings.Contains(s, "{{timestamp}}") {
		s = strings.ReplaceAll(s, "{{timestamp}}", strconv.FormatInt(time.Now().Unix(), 10))
	}
	return s
}

func evaluateAssertions(step Step, statusCode int, headers http.Header, bodyBytes []byte, parsedJSON any, hasJSON bool) string {
	if step.Assert == nil {
		return ""
	}
	if len(step.ExpectedStatuses) > 0 && !slices.Contains(step.ExpectedStatuses, statusCode) {
		return fmt.Sprintf("status: expected %v, got %d", step.ExpectedStatuses, statusCode)
	}
	if step.Assert.BodyContains != "" && !strings.Contains(string(bodyBytes), step.Assert.BodyContains) {
		return fmt.Sprintf("body_contains: missing %q", step.Assert.BodyContains)
	}

	if len(step.Assert.Headers) > 0 {
		keys := make([]string, 0, len(step.Assert.Headers))
		for k := range step.Assert.Headers {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			actual := headers.Get(k)
			if ok, reason := MatchRule(actual, actual != "", step.Assert.Headers[k]); !ok {
				return fmt.Sprintf("header %s: %s", k, reason)
			}
		}
	}

	if len(step.Assert.JSON) > 0 {
		if !hasJSON {
			return "json: response body is not valid JSON"
		}
		keys := make([]string, 0, len(step.Assert.JSON))
		for k := range step.Assert.JSON {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, jp := range keys {
			rawVal, exists := EvalJSONPath(parsedJSON, jp)
			actual := FormatJSONValue(rawVal)
			if ok, reason := MatchRule(actual, exists, step.Assert.JSON[jp]); !ok {
				return fmt.Sprintf("json %s: %s", jp, reason)
			}
		}
	}

	return ""
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
	val, ok := EvalJSONPath(doc, path)
	if !ok {
		return ""
	}
	switch val.(type) {
	case map[string]any, []any:
		return ""
	default:
		return FormatJSONValue(val)
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
