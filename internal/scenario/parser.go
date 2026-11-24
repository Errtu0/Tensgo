package scenario

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario defines a multi-step stateful user journey loaded from YAML.
type Scenario struct {
	Name     string `yaml:"name"`
	BaseURL  string `yaml:"base_url"`
	VUs      int    `yaml:"vus"`
	Duration string `yaml:"duration"`
	Timeout  string `yaml:"timeout,omitempty"`
	Steps    []Step `yaml:"steps"`
}

// AssertConfig defines declarative response validation rules for a scenario step.
type AssertConfig struct {
	Status       any               `yaml:"status,omitempty"`
	JSON         map[string]string `yaml:"json,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	BodyContains string            `yaml:"body_contains,omitempty"`
}

// Step describes a single HTTP action and optional variable extractions and assertions.
type Step struct {
	Name             string            `yaml:"name"`
	Method           string            `yaml:"method"`
	Path             string            `yaml:"path"`
	Headers          map[string]string `yaml:"headers,omitempty"`
	Body             any               `yaml:"body,omitempty"`
	Extract          map[string]string `yaml:"extract,omitempty"`
	Assert           *AssertConfig     `yaml:"assert,omitempty"`
	ExpectedStatuses []int             `yaml:"-"`
}

// HasAssertions reports whether the step has any response assertion rules configured.
func (s Step) HasAssertions() bool {
	if s.Assert == nil {
		return false
	}
	return len(s.ExpectedStatuses) > 0 ||
		len(s.Assert.JSON) > 0 ||
		len(s.Assert.Headers) > 0 ||
		s.Assert.BodyContains != ""
}

// ParsedConfig holds validated scenario parameters and parsed durations.
type ParsedConfig struct {
	Scenario Scenario
	Duration time.Duration
	Timeout  time.Duration
}

// ParseFile reads and validates a scenario YAML file.
func ParseFile(path string) (*ParsedConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("scenario file path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario file %q: %w", path, err)
	}

	var sc Scenario
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		return nil, fmt.Errorf("parse YAML %q: %w", path, err)
	}

	sc.BaseURL = strings.TrimSpace(sc.BaseURL)
	if sc.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required in scenario")
	}
	u, err := url.ParseRequestURI(sc.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid base_url %q: expected http:// or https:// URL", sc.BaseURL)
	}

	if sc.Name == "" {
		sc.Name = "Scenario Flow"
	}
	if sc.VUs <= 0 {
		sc.VUs = 10
	}

	dur := 10 * time.Second
	if strings.TrimSpace(sc.Duration) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(sc.Duration))
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("invalid duration %q: %w", sc.Duration, err)
		}
		dur = d
	}

	timeout := 5 * time.Second
	if strings.TrimSpace(sc.Timeout) != "" {
		t, err := time.ParseDuration(strings.TrimSpace(sc.Timeout))
		if err != nil || t <= 0 {
			return nil, fmt.Errorf("invalid timeout %q: %w", sc.Timeout, err)
		}
		timeout = t
	}

	if len(sc.Steps) == 0 {
		return nil, fmt.Errorf("scenario must define at least one step")
	}

	for i := range sc.Steps {
		sc.Steps[i].Method = strings.ToUpper(strings.TrimSpace(sc.Steps[i].Method))
		if sc.Steps[i].Method == "" {
			sc.Steps[i].Method = "GET"
		}
		sc.Steps[i].Path = strings.TrimSpace(sc.Steps[i].Path)
		if sc.Steps[i].Path == "" {
			return nil, fmt.Errorf("step %d (%s) is missing path", i+1, sc.Steps[i].Name)
		}
		if sc.Steps[i].Name == "" {
			sc.Steps[i].Name = fmt.Sprintf("Step %d", i+1)
		}
		if sc.Steps[i].Assert != nil && sc.Steps[i].Assert.Status != nil {
			statuses, err := parseExpectedStatuses(sc.Steps[i].Assert.Status)
			if err != nil {
				return nil, fmt.Errorf("step %d (%s) invalid assert.status: %w", i+1, sc.Steps[i].Name, err)
			}
			sc.Steps[i].ExpectedStatuses = statuses
		}
	}

	return &ParsedConfig{
		Scenario: sc,
		Duration: dur,
		Timeout:  timeout,
	}, nil
}

func parseExpectedStatuses(raw any) ([]int, error) {
	switch v := raw.(type) {
	case int:
		return []int{v}, nil
	case int64:
		return []int{int(v)}, nil
	case float64:
		return []int{int(v)}, nil
	case []any:
		out := make([]int, 0, len(v))
		for _, item := range v {
			n, ok := item.(int)
			if !ok {
				if f, okF := item.(float64); okF {
					n = int(f)
				} else {
					return nil, fmt.Errorf("expected integer status code, got %T", item)
				}
			}
			out = append(out, n)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("status list cannot be empty")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected int or list of ints, got %T", raw)
	}
}

// DefaultTemplateYAML returns a starter tensgo.yaml scenario template.
func DefaultTemplateYAML() string {
	return `name: "Authenticated User Workflow"
base_url: "http://localhost:5001"
vus: 20
duration: "15s"
steps:
  - name: "Login"
    method: "POST"
    path: "/api/v1/auth/login"
    body:
      email: "test@larch.dev"
      password: "secret"
    extract:
      # Alternative JSONPath examples: $.token or $.data.accessToken
      token: "$.data.accessToken"
  - name: "Fetch Profile"
    method: "GET"
    path: "/api/v1/auth/me"
    headers:
      Authorization: "Bearer {{token}}"
`
}
