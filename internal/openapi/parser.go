package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const sampleUUID = "550e8400-e29b-41d4-a716-446655440000"

var supportedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// EndpointSpec describes a single discovered API route and its generated sample payload.
type EndpointSpec struct {
	Method       string
	Path         string
	Summary      string
	SampleBody   string
	RequiresAuth bool
	AuthType     string
}

// Spec holds the parsed OpenAPI/Swagger metadata and discovered endpoints.
type Spec struct {
	Title         string
	BaseURL       string
	AuthType      string
	Endpoints     []EndpointSpec
	LoginEndpoint *EndpointSpec
	LoginFields   []string
}

// Parse reads an OpenAPI 3.0 or Swagger 2.0 JSON document from a local file path or HTTP URL.
func Parse(source string) (*Spec, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("openapi source cannot be empty")
	}

	raw, sourceBaseURL, err := loadSource(source)
	if err != nil {
		return nil, err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI/Swagger JSON: %w", err)
	}

	schemes := extractSecuritySchemes(doc)
	globalAuth, globalAuthType := resolveSecurityReq(doc["security"], schemes)

	spec := &Spec{
		Title:    extractTitle(doc),
		BaseURL:  extractBaseURL(doc, sourceBaseURL),
		AuthType: globalAuthType,
	}

	pathsObj, _ := doc["paths"].(map[string]any)
	if len(pathsObj) == 0 {
		return nil, fmt.Errorf("no paths found in OpenAPI/Swagger document")
	}

	pathKeys := make([]string, 0, len(pathsObj))
	for p := range pathsObj {
		pathKeys = append(pathKeys, p)
	}
	slices.Sort(pathKeys)

	for _, rawPath := range pathKeys {
		pathItem, ok := pathsObj[rawPath].(map[string]any)
		if !ok {
			continue
		}

		pathParams := extractParameters(pathItem["parameters"], doc)

		for _, method := range supportedMethods {
			opVal, exists := pathItem[strings.ToLower(method)]
			if !exists {
				continue
			}
			op, ok := opVal.(map[string]any)
			if !ok {
				continue
			}

			opParams := extractParameters(op["parameters"], doc)
			mergedParams := mergeParams(pathParams, opParams)
			resolvedPath := resolvePathParams(rawPath, mergedParams)

			summary, _ := op["summary"].(string)
			if summary == "" {
				summary, _ = op["operationId"].(string)
			}

			sampleBody := extractSampleBody(op, mergedParams, doc)

			requiresAuth := globalAuth
			authType := globalAuthType
			if opSec, hasOpSec := op["security"]; hasOpSec {
				requiresAuth, authType = resolveSecurityReq(opSec, schemes)
			}

			ep := EndpointSpec{
				Method:       method,
				Path:         resolvedPath,
				Summary:      summary,
				SampleBody:   sampleBody,
				RequiresAuth: requiresAuth,
				AuthType:     authType,
			}
			spec.Endpoints = append(spec.Endpoints, ep)

			if method == "POST" && isLoginPath(rawPath) && spec.LoginEndpoint == nil {
				epCopy := ep
				spec.LoginEndpoint = &epCopy
				spec.LoginFields = extractLoginFields(op, mergedParams, doc)
			}
		}
	}

	if len(spec.Endpoints) == 0 {
		return nil, fmt.Errorf("no supported HTTP endpoints found under paths")
	}

	return spec, nil
}

func loadSource(source string) ([]byte, string, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, "", fmt.Errorf("fetch OpenAPI spec %q: %w", source, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, "", fmt.Errorf("fetch OpenAPI spec %q: HTTP %d", source, resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return nil, "", fmt.Errorf("read OpenAPI spec %q: %w", source, err)
		}

		var baseURL string
		if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Host != "" {
			baseURL = u.Scheme + "://" + u.Host
		}
		return data, baseURL, nil
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return nil, "", fmt.Errorf("read OpenAPI file %q: %w", source, err)
	}
	return data, "", nil
}

func extractTitle(doc map[string]any) string {
	if info, ok := doc["info"].(map[string]any); ok {
		if title, ok := info["title"].(string); ok {
			return title
		}
	}
	return "API Specification"
}

func extractBaseURL(doc map[string]any, sourceBaseURL string) string {
	// OpenAPI 3.0 servers
	if servers, ok := doc["servers"].([]any); ok && len(servers) > 0 {
		if first, ok := servers[0].(map[string]any); ok {
			if srvURL, ok := first["url"].(string); ok && srvURL != "" {
				srvURL = strings.TrimRight(srvURL, "/")
				if strings.HasPrefix(srvURL, "http://") || strings.HasPrefix(srvURL, "https://") {
					return srvURL
				}
				if sourceBaseURL != "" && strings.HasPrefix(srvURL, "/") {
					return sourceBaseURL + srvURL
				}
			}
		}
	}

	// Swagger 2.0 host + basePath
	if host, ok := doc["host"].(string); ok && host != "" {
		scheme := "https"
		if schemes, ok := doc["schemes"].([]any); ok && len(schemes) > 0 {
			if s, ok := schemes[0].(string); ok && s != "" {
				scheme = s
			}
		}
		basePath, _ := doc["basePath"].(string)
		return strings.TrimRight(fmt.Sprintf("%s://%s%s", scheme, host, basePath), "/")
	}

	return sourceBaseURL
}

func extractParameters(raw any, doc map[string]any) []map[string]any {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := m["$ref"].(string); ok {
			if resolved := resolveRef(ref, doc); resolved != nil {
				m = resolved
			}
		}
		out = append(out, m)
	}
	return out
}

func mergeParams(base, override []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(base)+len(override))
	index := make(map[string]int)
	for _, p := range base {
		name, _ := p["name"].(string)
		in, _ := p["in"].(string)
		key := in + ":" + name
		index[key] = len(merged)
		merged = append(merged, p)
	}
	for _, p := range override {
		name, _ := p["name"].(string)
		in, _ := p["in"].(string)
		key := in + ":" + name
		if idx, exists := index[key]; exists {
			merged[idx] = p
		} else {
			merged = append(merged, p)
		}
	}
	return merged
}

func resolvePathParams(rawPath string, params []map[string]any) string {
	paramSchemas := make(map[string]map[string]any)
	for _, p := range params {
		in, _ := p["in"].(string)
		name, _ := p["name"].(string)
		if in == "path" && name != "" {
			if schema, ok := p["schema"].(map[string]any); ok {
				paramSchemas[name] = schema
			} else {
				paramSchemas[name] = p
			}
		}
	}

	resolved := rawPath
	for {
		start := strings.IndexByte(resolved, '{')
		if start == -1 {
			break
		}
		end := strings.IndexByte(resolved[start:], '}')
		if end == -1 {
			break
		}
		end += start
		paramName := resolved[start+1 : end]
		replacement := samplePathValue(paramName, paramSchemas[paramName])
		resolved = resolved[:start] + replacement + resolved[end+1:]
	}
	return resolved
}

func samplePathValue(name string, schema map[string]any) string {
	lowerName := strings.ToLower(name)
	if schema != nil {
		if ex, ok := schema["example"]; ok && ex != nil {
			return fmt.Sprintf("%v", ex)
		}
		format, _ := schema["format"].(string)
		typ, _ := schema["type"].(string)
		if format == "uuid" || strings.Contains(lowerName, "uuid") {
			return sampleUUID
		}
		switch typ {
		case "integer", "number":
			return "1"
		case "boolean":
			return "true"
		}
	}

	switch {
	case strings.Contains(lowerName, "uuid"):
		return sampleUUID
	case lowerName == "id" || strings.HasSuffix(lowerName, "_id") || strings.HasSuffix(lowerName, "id"):
		return "1"
	default:
		return "sample"
	}
}

func extractSampleBody(op map[string]any, params []map[string]any, doc map[string]any) string {
	// OpenAPI 3.0 requestBody -> content -> application/json
	if rb, ok := op["requestBody"].(map[string]any); ok {
		if ref, ok := rb["$ref"].(string); ok {
			if resolved := resolveRef(ref, doc); resolved != nil {
				rb = resolved
			}
		}
		if content, ok := rb["content"].(map[string]any); ok {
			if jsonMedia, ok := content["application/json"].(map[string]any); ok {
				if ex, ok := jsonMedia["example"]; ok && ex != nil {
					if b, err := json.Marshal(ex); err == nil {
						return string(b)
					}
				}
				if schema, ok := jsonMedia["schema"].(map[string]any); ok {
					val := mockValueFromSchema(schema, doc, 0)
					if b, err := json.Marshal(val); err == nil {
						return string(b)
					}
				}
			}
		}
	}

	// Swagger 2.0 body parameter
	for _, p := range params {
		in, _ := p["in"].(string)
		if in == "body" {
			if schema, ok := p["schema"].(map[string]any); ok {
				val := mockValueFromSchema(schema, doc, 0)
				if b, err := json.Marshal(val); err == nil {
					return string(b)
				}
			}
		}
	}

	return ""
}

func mockValueFromSchema(schema map[string]any, doc map[string]any, depth int) any {
	if schema == nil || depth > 5 {
		return map[string]any{}
	}

	if ref, ok := schema["$ref"].(string); ok {
		if resolved := resolveRef(ref, doc); resolved != nil {
			return mockValueFromSchema(resolved, doc, depth+1)
		}
	}

	if ex, ok := schema["example"]; ok && ex != nil {
		return ex
	}
	if def, ok := schema["default"]; ok && def != nil {
		return def
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}

	if allOf, ok := schema["allOf"].([]any); ok && len(allOf) > 0 {
		merged := map[string]any{}
		for _, sub := range allOf {
			if subMap, ok := sub.(map[string]any); ok {
				if v, ok := mockValueFromSchema(subMap, doc, depth+1).(map[string]any); ok {
					for k, val := range v {
						merged[k] = val
					}
				}
			}
		}
		return merged
	}

	typ, _ := schema["type"].(string)
	format, _ := schema["format"].(string)

	if _, hasProps := schema["properties"].(map[string]any); hasProps && typ == "" {
		typ = "object"
	}

	switch typ {
	case "object":
		props, _ := schema["properties"].(map[string]any)
		obj := make(map[string]any, len(props))
		for k, propRaw := range props {
			propMap, ok := propRaw.(map[string]any)
			if !ok {
				continue
			}
			obj[k] = mockValueFromSchema(propMap, doc, depth+1)
		}
		return obj
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{mockValueFromSchema(items, doc, depth+1)}
		}
		return []any{}
	case "integer":
		return 1
	case "number":
		return 1.5
	case "boolean":
		return true
	case "string":
		switch format {
		case "email":
			return "user@example.com"
		case "uuid":
			return sampleUUID
		case "date-time":
			return "2026-01-01T00:00:00Z"
		case "date":
			return "2026-01-01"
		default:
			return "sample"
		}
	default:
		return "sample"
	}
}

func resolveRef(ref string, doc map[string]any) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var current any = doc
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	res, _ := current.(map[string]any)
	return res
}

func extractSecuritySchemes(doc map[string]any) map[string]string {
	schemes := make(map[string]string)

	// OpenAPI 3.0: components.securitySchemes
	if comp, ok := doc["components"].(map[string]any); ok {
		if secSchemes, ok := comp["securitySchemes"].(map[string]any); ok {
			for name, raw := range secSchemes {
				if m, ok := raw.(map[string]any); ok {
					schemes[name] = classifySecurityScheme(m)
				}
			}
		}
	}

	// Swagger 2.0: securityDefinitions
	if secDefs, ok := doc["securityDefinitions"].(map[string]any); ok {
		for name, raw := range secDefs {
			if m, ok := raw.(map[string]any); ok {
				schemes[name] = classifySecurityScheme(m)
			}
		}
	}

	return schemes
}

func classifySecurityScheme(m map[string]any) string {
	typ, _ := m["type"].(string)
	scheme, _ := m["scheme"].(string)
	switch strings.ToLower(typ) {
	case "http":
		if strings.EqualFold(scheme, "basic") {
			return "Basic"
		}
		return "Bearer"
	case "oauth2", "openidconnect":
		return "OAuth2"
	case "apikey":
		return "ApiKey"
	case "basic":
		return "Basic"
	default:
		return "Auth"
	}
}

func resolveSecurityReq(raw any, schemes map[string]string) (bool, string) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return false, ""
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok || len(m) == 0 {
			continue
		}
		for name := range m {
			if t, exists := schemes[name]; exists {
				return true, t
			}
			return true, "Auth"
		}
	}
	return false, ""
}

func isLoginPath(rawPath string) bool {
	lower := strings.ToLower(rawPath)
	return strings.Contains(lower, "/login") ||
		strings.Contains(lower, "/signin") ||
		strings.Contains(lower, "/token")
}

func extractLoginFields(op map[string]any, params []map[string]any, doc map[string]any) []string {
	var schema map[string]any

	if rb, ok := op["requestBody"].(map[string]any); ok {
		if ref, ok := rb["$ref"].(string); ok {
			if resolved := resolveRef(ref, doc); resolved != nil {
				rb = resolved
			}
		}
		if content, ok := rb["content"].(map[string]any); ok {
			if jsonMedia, ok := content["application/json"].(map[string]any); ok {
				schema, _ = jsonMedia["schema"].(map[string]any)
			}
		}
	}

	if schema == nil {
		for _, p := range params {
			if in, _ := p["in"].(string); in == "body" {
				schema, _ = p["schema"].(map[string]any)
				break
			}
		}
	}

	if schema != nil {
		if ref, ok := schema["$ref"].(string); ok {
			if resolved := resolveRef(ref, doc); resolved != nil {
				schema = resolved
			}
		}
		if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
			var identityFields, secretFields, otherFields []string
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			slices.Sort(keys)

			for _, k := range keys {
				lower := strings.ToLower(k)
				switch {
				case strings.Contains(lower, "pass") || strings.Contains(lower, "secret"):
					secretFields = append(secretFields, k)
				case strings.Contains(lower, "email") || strings.Contains(lower, "user") || strings.Contains(lower, "phone") || strings.Contains(lower, "login"):
					identityFields = append(identityFields, k)
				default:
					otherFields = append(otherFields, k)
				}
			}

			fields := make([]string, 0, len(keys))
			fields = append(fields, identityFields...)
			fields = append(fields, secretFields...)
			fields = append(fields, otherFields...)
			return fields
		}
	}

	return []string{"email", "password"}
}
