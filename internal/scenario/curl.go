package scenario

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// ParseCurlCommand parses a cURL command string into a ready-to-run tensgo Scenario.
func ParseCurlCommand(raw string) (*Scenario, error) {
	tokens, err := tokenizeShell(raw)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty cURL command")
	}
	if strings.EqualFold(tokens[0], "curl") {
		tokens = tokens[1:]
	}

	var (
		method    string
		targetURL string
		bodyStr   string
		hasBody   bool
		headers   = make(map[string]string)
	)

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok == "-X" || tok == "--request":
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("flag %s requires a value", tok)
			}
			i++
			method = strings.ToUpper(strings.TrimSpace(tokens[i]))

		case strings.HasPrefix(tok, "-X") && len(tok) > 2:
			method = strings.ToUpper(strings.TrimSpace(tok[2:]))

		case tok == "-H" || tok == "--header":
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("flag %s requires a value", tok)
			}
			i++
			k, v, found := strings.Cut(tokens[i], ":")
			if !found || strings.TrimSpace(k) == "" {
				return nil, fmt.Errorf("invalid header %q", tokens[i])
			}
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			// Handle shell-stripped quotes where "Authorization: Bearer token123" splits across tokens.
			if val == "" || (strings.EqualFold(key, "Authorization") && strings.EqualFold(val, "Bearer")) {
				var extra []string
				if val != "" {
					extra = append(extra, val)
				}
				for i+1 < len(tokens) &&
					!strings.HasPrefix(tokens[i+1], "-") &&
					!strings.HasPrefix(tokens[i+1], "http://") &&
					!strings.HasPrefix(tokens[i+1], "https://") {
					i++
					extra = append(extra, tokens[i])
				}
				val = strings.Join(extra, " ")
			}
			headers[key] = val

		case tok == "-d" || tok == "--data" || tok == "--data-raw" || tok == "--data-binary":
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("flag %s requires a value", tok)
			}
			i++
			bodyStr = tokens[i]
			hasBody = true

		case tok == "-u" || tok == "--user":
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("flag %s requires a value", tok)
			}
			i++
			encoded := base64.StdEncoding.EncodeToString([]byte(tokens[i]))
			headers["Authorization"] = "Basic " + encoded

		case tok == "--compressed":
			// Handled transparently by Go's net/http Transport.
			continue

		case strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://"):
			targetURL = tok

		case strings.HasPrefix(tok, "-"):
			// Skip unknown single/value flags gracefully.
			continue

		default:
			if targetURL == "" && (strings.Contains(tok, "://") || strings.Contains(tok, ".")) {
				targetURL = tok
			}
		}
	}

	if targetURL == "" {
		return nil, fmt.Errorf("no target URL found in cURL command")
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	u, err := url.ParseRequestURI(targetURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q in cURL command", targetURL)
	}

	if method == "" {
		if hasBody {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	baseURL := u.Scheme + "://" + u.Host
	reqPath := u.RequestURI()
	if reqPath == "" {
		reqPath = "/"
	}

	var bodyVal any
	if hasBody {
		var parsed any
		if err := json.Unmarshal([]byte(bodyStr), &parsed); err == nil {
			bodyVal = parsed
		} else {
			bodyVal = bodyStr
		}
	}

	if len(headers) == 0 {
		headers = nil
	}

	sc := &Scenario{
		Name:     fmt.Sprintf("Imported cURL (%s %s)", method, u.Path),
		BaseURL:  baseURL,
		VUs:      10,
		Duration: "10s",
		Steps: []Step{
			{
				Name:    fmt.Sprintf("%s %s", method, u.Path),
				Method:  method,
				Path:    reqPath,
				Headers: headers,
				Body:    bodyVal,
				Assert: &AssertConfig{
					Status: 200,
				},
				ExpectedStatuses: []int{200},
			},
		},
	}

	return sc, nil
}

// MarshalScenarioYAML serializes a Scenario struct to YAML bytes.
func MarshalScenarioYAML(sc *Scenario) ([]byte, error) {
	return yaml.Marshal(sc)
}

func tokenizeShell(input string) ([]string, error) {
	// Normalize backslash line continuations.
	s := strings.ReplaceAll(input, "\\\r\n", " ")
	s = strings.ReplaceAll(s, "\\\n", " ")

	var (
		tokens  []string
		cur     strings.Builder
		inToken bool
		quote   rune
		escaped bool
	)

	flush := func() {
		if inToken {
			tokens = append(tokens, cur.String())
			cur.Reset()
			inToken = false
		}
	}

	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}

		if quote != 0 {
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
			inToken = true
			continue
		}

		switch {
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == '\\':
			escaped = true
			inToken = true
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in cURL command")
	}
	if escaped {
		cur.WriteRune('\\')
	}
	flush()
	return tokens, nil
}
