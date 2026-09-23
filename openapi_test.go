package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpecHandlerServesYAML(t *testing.T) {
	rec := httptest.NewRecorder()
	SpecHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/yaml") {
		t.Fatalf("content-type = %q, want application/yaml", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"openapi: 3.0.3", "/process:", "ProcessRequest", "payload_id", "result"} {
		if !strings.Contains(body, want) {
			t.Fatalf("spec missing %q", want)
		}
	}
}

func TestProcessAPIYAMLAlias(t *testing.T) {
	rec := httptest.NewRecorder()
	SpecHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/process_api.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openapi: 3.0.3") {
		t.Fatal("alias did not serve the spec")
	}
}

func TestDocsHandlerServesSwaggerUI(t *testing.T) {
	rec := httptest.NewRecorder()
	DocsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"swagger-ui", "/openapi.yaml", "tryItOutEnabled"} {
		if !strings.Contains(body, want) {
			t.Fatalf("docs page missing %q", want)
		}
	}
}
