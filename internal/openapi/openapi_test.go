package openapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseOpenAPI3AndSwagger2(t *testing.T) {
	tmp := t.TempDir()

	oa3 := `{
		"openapi": "3.0.3",
		"info": { "title": "Sample API" },
		"servers": [{ "url": "/api/v1" }],
		"security": [{ "bearerAuth": [] }],
		"components": {
			"securitySchemes": {
				"bearerAuth": { "type": "http", "scheme": "bearer" }
			},
			"schemas": {
				"LoginReq": {
					"type": "object",
					"properties": {
						"email": { "type": "string", "format": "email" },
						"password": { "type": "string" }
					}
				}
			}
		},
		"paths": {
			"/auth/login": {
				"post": {
					"summary": "Login",
					"security": [],
					"requestBody": {
						"content": {
							"application/json": {
								"schema": { "$ref": "#/components/schemas/LoginReq" }
							}
						}
					}
				}
			},
			"/items/{uuid}/{id}": {
				"get": {
					"summary": "Get item",
					"parameters": [
						{ "name": "uuid", "in": "path", "schema": { "type": "string", "format": "uuid" } },
						{ "name": "id", "in": "path", "schema": { "type": "integer" } }
					]
				}
			}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oa3))
	}))
	defer srv.Close()

	spec, err := Parse(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatalf("Parse URL error: %v", err)
	}
	if spec.Title != "Sample API" || !strings.HasSuffix(spec.BaseURL, "/api/v1") {
		t.Fatalf("unexpected spec metadata: %+v", spec)
	}
	if spec.LoginEndpoint == nil || len(spec.LoginFields) != 2 {
		t.Fatalf("expected LoginEndpoint and 2 fields, got %+v %v", spec.LoginEndpoint, spec.LoginFields)
	}
	if len(spec.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(spec.Endpoints))
	}
	if spec.Endpoints[0].RequiresAuth {
		t.Fatalf("expected /auth/login security: [] to override global auth")
	}
	if !spec.Endpoints[1].RequiresAuth || !strings.Contains(spec.Endpoints[1].Path, sampleUUID) || !strings.HasSuffix(spec.Endpoints[1].Path, "/1") {
		t.Fatalf("unexpected protected endpoint: %+v", spec.Endpoints[1])
	}

	// Swagger 2.0 from local file
	sw2Path := filepath.Join(tmp, "swagger.json")
	sw2 := `{
		"swagger": "2.0",
		"host": "api.example.com",
		"basePath": "/v2",
		"schemes": ["https"],
		"securityDefinitions": { "api_key": { "type": "apiKey", "in": "header", "name": "X-API-Key" } },
		"paths": {
			"/orders": {
				"post": {
					"security": [{ "api_key": [] }],
					"parameters": [{
						"in": "body",
						"schema": {
							"type": "object",
							"properties": {
								"qty": { "type": "integer" },
								"active": { "type": "boolean" },
								"tags": { "type": "array", "items": { "type": "string" } }
							}
						}
					}]
				}
			}
		}
	}`
	if err := os.WriteFile(sw2Path, []byte(sw2), 0o644); err != nil {
		t.Fatal(err)
	}
	swSpec, err := Parse(sw2Path)
	if err != nil {
		t.Fatalf("Parse Swagger 2.0 error: %v", err)
	}
	if swSpec.BaseURL != "https://api.example.com/v2" || swSpec.Endpoints[0].AuthType != "ApiKey" {
		t.Fatalf("unexpected Swagger 2.0 spec: %+v", swSpec)
	}

	if _, err := Parse(""); err == nil {
		t.Fatalf("expected error for empty source")
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"access_token":"tok-abc"}}`))
		case "/fail":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":"Invalid credentials"}`))
		case "/notoken":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	tok, err := Login(srv.URL, EndpointSpec{Path: "/ok"}, map[string]string{"email": "a@b.com"}, time.Second)
	if err != nil || tok != "Bearer tok-abc" {
		t.Fatalf("Login ok failed: tok=%q err=%v", tok, err)
	}

	if _, err := Login(srv.URL, EndpointSpec{Path: "/fail"}, map[string]string{}, time.Second); err == nil || !strings.Contains(err.Error(), "Invalid credentials") {
		t.Fatalf("expected clean error message, got %v", err)
	}

	if _, err := Login(srv.URL, EndpointSpec{Path: "/notoken"}, map[string]string{}, time.Second); err == nil {
		t.Fatalf("expected error when token missing")
	}
}
