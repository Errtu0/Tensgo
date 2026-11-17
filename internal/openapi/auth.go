package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var tokenKeys = []string{"access_token", "accesstoken", "token", "jwt", "id_token"}

// Login sends a POST request with credentials to the discovered login endpoint
// and recursively extracts a Bearer token from the JSON response.
func Login(baseURL string, loginEP EndpointSpec, credentials map[string]string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	targetURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(loginEP.Path, "/")

	payload, err := json.Marshal(credentials)
	if err != nil {
		return "", fmt.Errorf("marshal credentials: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("read login response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Login failed (HTTP %d): %s", resp.StatusCode, extractErrorMessage(body))
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("login response is not valid JSON: %w", err)
	}

	tok := extractTokenRecursive(parsed)
	if tok == "" {
		return "", fmt.Errorf("no token field (access_token/token/jwt) found in login response")
	}

	if strings.HasPrefix(strings.ToLower(tok), "bearer ") {
		return tok, nil
	}
	return "Bearer " + tok, nil
}

func extractTokenRecursive(v any) string {
	switch val := v.(type) {
	case map[string]any:
		// Check known token keys at this level first in priority order.
		for _, want := range tokenKeys {
			for k, raw := range val {
				if strings.EqualFold(k, want) {
					if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
		}
		// Recurse into nested objects/arrays.
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

func extractErrorMessage(body []byte) string {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if msg := findErrorField(parsed); msg != "" {
			return cleanSnippet(msg, 120)
		}
	}
	return cleanSnippet(string(body), 120)
}

func findErrorField(v any) string {
	switch val := v.(type) {
	case map[string]any:
		for _, key := range []string{"message", "error", "detail"} {
			for k, raw := range val {
				if strings.EqualFold(k, key) {
					switch s := raw.(type) {
					case string:
						if strings.TrimSpace(s) != "" {
							return s
						}
					case map[string]any, []any:
						if nested := findErrorField(s); nested != "" {
							return nested
						}
					}
				}
			}
		}
		for _, raw := range val {
			if nested := findErrorField(raw); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range val {
			if msg := findErrorField(item); msg != "" {
				return msg
			}
		}
	}
	return ""
}

func cleanSnippet(raw string, maxRunes int) string {
	s := strings.ReplaceAll(raw, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "Unknown error"
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-3]) + "..."
	}
	return s
}
