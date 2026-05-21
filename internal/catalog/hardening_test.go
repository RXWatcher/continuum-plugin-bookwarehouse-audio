package catalog_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/catalog"
)

// The ?limit query parameter is attacker-controlled; it must be clamped before
// it is forwarded to the upstream catalog so a client cannot drive a giant
// upstream fetch (limit=999999999) or send garbage.
func TestCatalogLimit_ClampedBeforeUpstream(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string // expected forwarded ?limit ("" = not forwarded)
	}{
		{"huge clamped to max", "?limit=999999999", "200"},
		{"negative dropped", "?limit=-5", ""},
		{"garbage dropped", "?limit=abc", ""},
		{"zero dropped", "?limit=0", ""},
		{"valid passes through", "?limit=50", "50"},
		{"at cap passes through", "?limit=200", "200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLimit string
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/audiobooks" {
					gotLimit = r.URL.Query().Get("limit")
				}
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
			}))
			defer up.Close()

			c := bookwarehouse.NewClient(up.URL, "k")
			h := catalog.NewHandler(c, nil, "")
			router := chi.NewRouter()
			router.Get("/catalog", h.List())

			r := httptest.NewRequest("GET", "/catalog"+tc.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("code = %d", w.Code)
			}
			if gotLimit != tc.want {
				t.Fatalf("forwarded limit = %q, want %q", gotLimit, tc.want)
			}
		})
	}
}
