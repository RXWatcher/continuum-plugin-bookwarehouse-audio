package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/server"
)

func TestHealthOK(t *testing.T) {
	h := server.New(server.Deps{})
	r := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["ok"] != true {
		t.Errorf("ok = %v, want true", body["ok"])
	}
}

func TestAdminPageIncludesStreamDiagnosticsGuidance(t *testing.T) {
	h := server.New(server.Deps{})
	r := httptest.NewRequest("GET", "/admin?theme=midnight-cinema", nil)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Stream diagnostics", "Direct file access", "Range support", "Path remapping"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}
	if !strings.Contains(body, `data-theme="midnight-cinema"`) {
		t.Fatalf("admin page should preserve theme")
	}
}

func TestAdminPageIncludesStatelessOperatorConsole(t *testing.T) {
	h := server.New(server.Deps{})
	r := httptest.NewRequest("GET", "/admin?theme=midnight-cinema", nil)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-tab-target="readiness"`,
		`data-tab-target="config"`,
		`data-tab-target="browser"`,
		`data-tab-target="stream-test"`,
		`data-tab-target="diagnostics"`,
		`id="search-form"`,
		`id="config-form"`,
		`id="stream-form"`,
		`Redirect fallback`,
		`plugin database`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}
}
