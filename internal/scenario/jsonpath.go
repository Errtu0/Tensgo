package scenario

import (
	"fmt"
	"strconv"
	"strings"
)

type pathSegment struct {
	key     string
	index   int
	isIndex bool
}

// EvalJSONPath traverses a decoded JSON document using dot/bracket notation
// (e.g. "$.token", "$.data.user.id", "$.items[0].id", "$.items[-1].code").
func EvalJSONPath(doc any, path string) (any, bool) {
	segments, ok := parseJSONPath(path)
	if !ok {
		return nil, false
	}

	current := doc
	for _, seg := range segments {
		if seg.isIndex {
			arr, ok := current.([]any)
			if !ok {
				return nil, false
			}
			idx := seg.index
			if idx < 0 {
				idx = len(arr) + idx
			}
			if idx < 0 || idx >= len(arr) {
				return nil, false
			}
			current = arr[idx]
		} else {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			val, exists := m[seg.key]
			if !exists {
				return nil, false
			}
			current = val
		}
	}
	return current, true
}

// FormatJSONValue converts a scalar JSON value into its string representation.
func FormatJSONValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// MatchRule evaluates an assertion rule (exact match, "!empty", or "contains:<substr>").
func MatchRule(actual string, exists bool, rule string) (bool, string) {
	if rule == "!empty" {
		if !exists || strings.TrimSpace(actual) == "" {
			return false, "expected non-empty value"
		}
		return true, ""
	}
	if strings.HasPrefix(rule, "contains:") {
		substr := strings.TrimPrefix(rule, "contains:")
		if !exists || !strings.Contains(actual, substr) {
			return false, fmt.Sprintf("expected value containing %q, got %q", substr, actual)
		}
		return true, ""
	}
	if !exists || actual != rule {
		return false, fmt.Sprintf("expected %q, got %q", rule, actual)
	}
	return true, ""
}

func parseJSONPath(path string) ([]pathSegment, bool) {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil, false
	}
	if strings.HasPrefix(p, "$.") {
		p = p[2:]
	} else if strings.HasPrefix(p, "$[") {
		p = p[1:]
	} else if p == "$" {
		return nil, false
	} else {
		p = strings.TrimPrefix(p, ".")
	}
	if p == "" {
		return nil, false
	}

	var segments []pathSegment
	i := 0
	for i < len(p) {
		switch p[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(p[i:], ']')
			if end == -1 {
				return nil, false
			}
			inner := strings.TrimSpace(p[i+1 : i+end])
			if (strings.HasPrefix(inner, "'") && strings.HasSuffix(inner, "'")) ||
				(strings.HasPrefix(inner, "\"") && strings.HasSuffix(inner, "\"")) {
				if len(inner) < 2 {
					return nil, false
				}
				segments = append(segments, pathSegment{key: inner[1 : len(inner)-1]})
			} else {
				idx, err := strconv.Atoi(inner)
				if err != nil {
					return nil, false
				}
				segments = append(segments, pathSegment{index: idx, isIndex: true})
			}
			i += end + 1
		default:
			start := i
			for i < len(p) && p[i] != '.' && p[i] != '[' {
				i++
			}
			key := p[start:i]
			if key != "" {
				segments = append(segments, pathSegment{key: key})
			}
		}
	}
	return segments, len(segments) > 0
}
