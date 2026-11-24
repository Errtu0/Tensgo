package scenario

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvalJSONPath(t *testing.T) {
	raw := `{
		"token": "abc-123",
		"data": {
			"user": { "id": 42, "active": true },
			"items": [
				{ "id": "item-1", "code": "A1" },
				{ "id": "item-2", "code": "Z9" }
			],
			"matrix": [[10, 20], [30, 40]]
		}
	}`
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		want     string
		wantOK   bool
	}{
		{name: "top-level string", path: "$.token", want: "abc-123", wantOK: true},
		{name: "nested int", path: "$.data.user.id", want: "42", wantOK: true},
		{name: "nested bool", path: "$.data.user.active", want: "true", wantOK: true},
		{name: "array first element", path: "$.data.items[0].id", want: "item-1", wantOK: true},
		{name: "array negative index", path: "$.data.items[-1].code", want: "Z9", wantOK: true},
		{name: "nested matrix", path: "$.data.matrix[1][0]", want: "30", wantOK: true},
		{name: "out of bounds", path: "$.data.items[5].id", wantOK: false},
		{name: "missing key", path: "$.data.missing", wantOK: false},
		{name: "empty path", path: "$", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, ok := EvalJSONPath(doc, tc.path)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (val=%v)", ok, tc.wantOK, val)
			}
			if ok {
				got := FormatJSONValue(val)
				if got != tc.want {
					t.Fatalf("got=%q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestMatchRule(t *testing.T) {
	tests := []struct {
		name   string
		actual string
		exists bool
		rule   string
		wantOK bool
	}{
		{name: "exact match pass", actual: "created", exists: true, rule: "created", wantOK: true},
		{name: "exact match fail", actual: "failed", exists: true, rule: "created", wantOK: false},
		{name: "not empty pass", actual: "ord-1", exists: true, rule: "!empty", wantOK: true},
		{name: "not empty fail blank", actual: "   ", exists: true, rule: "!empty", wantOK: false},
		{name: "not empty fail missing", actual: "", exists: false, rule: "!empty", wantOK: false},
		{name: "contains pass", actual: "application/json; charset=utf-8", exists: true, rule: "contains:application/json", wantOK: true},
		{name: "contains fail", actual: "text/plain", exists: true, rule: "contains:application/json", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, _ := MatchRule(tc.actual, tc.exists, tc.rule)
			if ok != tc.wantOK {
				t.Fatalf("MatchRule(%q, %v, %q) = %v, want %v", tc.actual, tc.exists, tc.rule, ok, tc.wantOK)
			}
		})
	}
}

func TestParseFileWithAssertions(t *testing.T) {
	tmp := t.TempDir()

	validPath := filepath.Join(tmp, "valid.yaml")
	validYAML := `
name: "Test Flow"
base_url: "http://localhost:8080"
vus: 2
duration: "1s"
steps:
  - name: "Create"
    method: "POST"
    path: "/items"
    assert:
      status: [200, 201]
      json:
        "$.status": "ok"
`
	if err := os.WriteFile(validPath, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseFile(validPath)
	if err != nil {
		t.Fatalf("ParseFile valid error: %v", err)
	}
	if len(cfg.Scenario.Steps[0].ExpectedStatuses) != 2 {
		t.Fatalf("expected 2 statuses, got %v", cfg.Scenario.Steps[0].ExpectedStatuses)
	}

	invalidPath := filepath.Join(tmp, "invalid_status.yaml")
	invalidYAML := `
base_url: "http://localhost:8080"
steps:
  - name: "Bad"
    method: "GET"
    path: "/health"
    assert:
      status: "not-an-int"
`
	if err := os.WriteFile(invalidPath, []byte(invalidYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(invalidPath); err == nil {
		t.Fatalf("expected error for invalid assert.status type")
	}
}

func TestRunExtractAndAssertionsPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", "req-999")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"access_token":"tok-xyz","items":[{"id":"ord-77"}]}}`))
		case "/orders/ord-77":
			if r.Header.Get("Authorization") != "Bearer tok-xyz" || r.Header.Get("X-Trace") != "req-999" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created","order_id":"ord-77"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &ParsedConfig{
		Duration: 250 * time.Millisecond,
		Timeout:  2 * time.Second,
		Scenario: Scenario{
			Name:    "Pass Journey",
			BaseURL: srv.URL,
			VUs:     2,
			Steps: []Step{
				{
					Name:   "Login",
					Method: "POST",
					Path:   "/login",
					Body:   map[string]any{"nonce": "{{random_int}}", "ts": "{{timestamp}}"},
					Extract: map[string]string{
						"auth_token": "$.data.access_token",
						"order_id":   "$.data.items[-1].id",
						"req_id":     "header:X-Request-Id",
					},
					Assert: &AssertConfig{
						Status: 200,
						JSON: map[string]string{
							"$.data.access_token": "!empty",
						},
						Headers: map[string]string{
							"Content-Type": "contains:application/json",
						},
					},
					ExpectedStatuses: []int{200},
				},
				{
					Name:   "Verify Order",
					Method: "GET",
					Path:   "/orders/{{order_id}}",
					Headers: map[string]string{
						"Authorization": "Bearer {{auth_token}}",
						"X-Trace":       "{{req_id}}",
					},
					Assert: &AssertConfig{
						JSON: map[string]string{
							"$.status":   "created",
							"$.order_id": "ord-77",
						},
						BodyContains: "order_id",
					},
					ExpectedStatuses: []int{200, 201},
				},
			},
		},
	}

	var completed atomic.Int64
	rep := Run(cfg, &completed)

	if rep.Overall.TotalRequests == 0 {
		t.Fatalf("expected >0 requests")
	}
	if rep.Overall.ErrorCount != 0 || rep.AssertionFailures != 0 {
		t.Fatalf("expected 0 errors and 0 assertion failures, got errors=%d assertions=%d",
			rep.Overall.ErrorCount, rep.AssertionFailures)
	}
	if len(rep.Overall.CheckThresholds(0, 0.0)) > 0 {
		t.Fatalf("expected 0 threshold violations")
	}
}

func TestRunAssertionsFailuresPropagateToErrorRate(t *testing.T) {
	tests := []struct {
		name       string
		assertCfg  *AssertConfig
		statuses   []int
		wantReason string
	}{
		{
			name:       "status mismatch",
			assertCfg:  &AssertConfig{},
			statuses:   []int{201},
			wantReason: "status: expected [201], got 200",
		},
		{
			name:       "json exact mismatch",
			assertCfg:  &AssertConfig{JSON: map[string]string{"$.status": "created"}},
			wantReason: `json $.status: expected "created", got "error"`,
		},
		{
			name:       "json not empty failure",
			assertCfg:  &AssertConfig{JSON: map[string]string{"$.data.id": "!empty"}},
			wantReason: "json $.data.id: expected non-empty value",
		},
		{
			name:       "header contains failure",
			assertCfg:  &AssertConfig{Headers: map[string]string{"Content-Type": "contains:xml"}},
			wantReason: "header Content-Type: expected value containing",
		},
		{
			name:       "body_contains failure",
			assertCfg:  &AssertConfig{BodyContains: "order_id"},
			wantReason: `body_contains: missing "order_id"`,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // silent error with HTTP 200
		_, _ = w.Write([]byte(`{"status":"error","data":{"id":""}}`))
	}))
	defer srv.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &ParsedConfig{
				Duration: 150 * time.Millisecond,
				Timeout:  time.Second,
				Scenario: Scenario{
					Name:    tc.name,
					BaseURL: srv.URL,
					VUs:     1,
					Steps: []Step{
						{
							Name:             "Check",
							Method:           "GET",
							Path:             "/check",
							Assert:           tc.assertCfg,
							ExpectedStatuses: tc.statuses,
						},
					},
				},
			}

			rep := Run(cfg, nil)
			if rep.Overall.TotalRequests == 0 {
				t.Fatalf("expected >0 requests")
			}
			if rep.Overall.ErrorCount != rep.Overall.TotalRequests || rep.AssertionFailures == 0 {
				t.Fatalf("expected all requests to fail assertions: total=%d err=%d assertFails=%d",
					rep.Overall.TotalRequests, rep.Overall.ErrorCount, rep.AssertionFailures)
			}
			// Verify quality gate catches 100% error rate even on HTTP 200.
			violations := rep.Overall.CheckThresholds(0, 1.0)
			if len(violations) == 0 {
				t.Fatalf("expected error-rate threshold violation, got none")
			}

			foundReason := false
			for reason := range rep.StepResults[0].FailureReasons {
				if strings.Contains(reason, tc.wantReason) {
					foundReason = true
					break
				}
			}
			if !foundReason {
				t.Fatalf("expected failure reason containing %q, got %v", tc.wantReason, rep.StepResults[0].FailureReasons)
			}
		})
	}
}
