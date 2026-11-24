package scenario

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseCurlCommand(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantBase   string
		wantMethod string
		wantPath   string
		wantHeader map[string]string
		wantHasBody bool
		wantErr    bool
	}{
		{
			name:       "simple GET",
			raw:        `curl https://api.example.com/v1/health?verbose=1`,
			wantBase:   "https://api.example.com",
			wantMethod: "GET",
			wantPath:   "/v1/health?verbose=1",
		},
		{
			name: "multiline POST JSON with headers and compressed",
			raw: `curl -X POST https://api.example.com/orders \
				-H "Content-Type: application/json" \
				-H 'Authorization: Bearer tok-123' \
				--compressed \
				-d '{"item":"pro","qty":2}'`,
			wantBase:   "https://api.example.com",
			wantMethod: "POST",
			wantPath:   "/orders",
			wantHeader: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer tok-123",
			},
			wantHasBody: true,
		},
		{
			name:       "implicit POST from --data-raw and Basic Auth -u",
			raw:        `curl --user alice:secret123 --data-raw '{"ping":true}' http://localhost:8080/api/echo`,
			wantBase:   "http://localhost:8080",
			wantMethod: "POST",
			wantPath:   "/api/echo",
			wantHeader: map[string]string{
				"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret123")),
			},
			wantHasBody: true,
		},
		{
			name:       "powershell stripped quotes split header tokens",
			raw:        `curl -X POST https://api.example.com/v1/orders -H Authorization: Bearer token123 -H Content-Type: application/json -d {"item":"book"}`,
			wantBase:   "https://api.example.com",
			wantMethod: "POST",
			wantPath:   "/v1/orders",
			wantHeader: map[string]string{
				"Authorization": "Bearer token123",
				"Content-Type":  "application/json",
			},
			wantHasBody: true,
		},
		{
			name:    "missing URL",
			raw:     `curl -X GET -H "Accept: application/json"`,
			wantErr: true,
		},
		{
			name:    "unterminated quote",
			raw:     `curl -H "Accept: application/json http://localhost:8080`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc, err := ParseCurlCommand(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sc.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL=%q, want %q", sc.BaseURL, tc.wantBase)
			}
			if len(sc.Steps) != 1 {
				t.Fatalf("expected 1 step, got %d", len(sc.Steps))
			}
			step := sc.Steps[0]
			if step.Method != tc.wantMethod {
				t.Fatalf("Method=%q, want %q", step.Method, tc.wantMethod)
			}
			if step.Path != tc.wantPath {
				t.Fatalf("Path=%q, want %q", step.Path, tc.wantPath)
			}
			for k, v := range tc.wantHeader {
				if step.Headers[k] != v {
					t.Fatalf("Header[%q]=%q, want %q", k, step.Headers[k], v)
				}
			}
			if (step.Body != nil) != tc.wantHasBody {
				t.Fatalf("hasBody=%v, want %v (body=%v)", step.Body != nil, tc.wantHasBody, step.Body)
			}
			if step.Assert == nil || step.Assert.Status != 200 {
				t.Fatalf("expected default assert.status=200, got %+v", step.Assert)
			}

			yamlBytes, err := MarshalScenarioYAML(sc)
			if err != nil {
				t.Fatalf("MarshalScenarioYAML error: %v", err)
			}
			if !strings.Contains(string(yamlBytes), tc.wantBase) {
				t.Fatalf("YAML missing base_url %q:\n%s", tc.wantBase, string(yamlBytes))
			}
		})
	}
}
