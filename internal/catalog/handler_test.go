package catalog_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/catalog"
)

func upstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/audiobooks":
			_, _ = w.Write([]byte(`{"items":[{"id":"a","title":"A","duration_seconds":100}],"total":1}`))
		case "/api/v1/audiobooks/search":
			_, _ = w.Write([]byte(`{"items":[{"id":"b","title":"B"}]}`))
		case "/api/v1/audiobooks/a":
			_, _ = w.Write([]byte(`{"id":"a","title":"A","files":[{"index":0,"file_path":"f.m4b","codec":"m4b"}]}`))
		case "/api/v1/audiobooks/authors":
			_, _ = w.Write([]byte(`{"items":[{"id":"a1","name":"Andy Weir","count":3}]}`))
		case "/api/v1/audiobooks/series":
			_, _ = w.Write([]byte(`{"items":[{"id":"s1","name":"Hyperion","count":4}]}`))
		case "/api/v1/audiobooks/narrators":
			_, _ = w.Write([]byte(`{"items":[{"id":"n1","name":"Ray Porter","count":2}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

func mountHandler(c *bookwarehouse.Client) http.Handler {
	h := catalog.NewHandler(c, nil, "")
	r := chi.NewRouter()
	r.Get("/catalog", h.List())
	r.Get("/catalog/libraries", h.Libraries())
	r.Get("/catalog/search", h.Search())
	r.Get("/catalog/{id}", h.Detail())
	r.Get("/browse/authors", h.BrowseAuthors())
	r.Get("/browse/series", h.BrowseSeries())
	r.Get("/browse/narrators", h.BrowseNarrators())
	r.Get("/cover/{book_id}/{size}", h.Cover())
	return r
}

func TestCatalogLibraries_ReturnsDefaultLibrary(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/catalog/libraries", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	var env catalog.PageEnvelope[catalog.LibraryInfo]
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Items) != 1 || env.Items[0].Name != "Book Warehouse Audiobooks" {
		t.Errorf("env = %+v", env)
	}
}

func TestCatalogList_Returns200WithItems(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	c := bookwarehouse.NewClient(up.URL, "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/catalog?limit=10", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	var env catalog.PageEnvelope[catalog.AudiobookSummary]
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Items) != 1 || env.Items[0].ID != "a" {
		t.Errorf("env = %+v", env)
	}
}

func TestCatalogSearch_Returns200WithItems(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	c := bookwarehouse.NewClient(up.URL, "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/catalog/search?q=x", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	var env catalog.PageEnvelope[catalog.AudiobookSummary]
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Items) != 1 || env.Items[0].ID != "b" {
		t.Errorf("env = %+v", env)
	}
}

// Search forwarded only ?q=, dropping cursor/limit/sort/order, so it always
// returned upstream page 1 and infinite scroll never advanced.
func TestCatalogSearch_PassesPaginationParams(t *testing.T) {
	var gotQuery string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/audiobooks/search" {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"items":[{"id":"b","title":"B"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer up.Close()
	c := bookwarehouse.NewClient(up.URL, "k")
	srv := mountHandler(c)
	r := httptest.NewRequest("GET", "/catalog/search?q=x&cursor=c2&limit=5&sort=title&order=desc", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{"q=x", "cursor=c2", "limit=5", "sort=title", "order=desc"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("upstream query %q missing %q", gotQuery, want)
		}
	}
}

func TestCatalogDetail_Returns200(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	c := bookwarehouse.NewClient(up.URL, "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/catalog/a", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	var d catalog.AudiobookDetail
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d.ID != "a" || len(d.Files) != 1 {
		t.Errorf("d = %+v", d)
	}
}

func TestBrowseAuthors(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	c := bookwarehouse.NewClient(up.URL, "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/browse/authors", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	var env catalog.PageEnvelope[catalog.AuthorSummary]
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Items[0].Name != "Andy Weir" {
		t.Errorf("env = %+v", env)
	}
}

// Cover() with no covers.Service wired returns 503 — covers are now served
// from the local filesystem, so a 302 redirect would just dead-end on the
// browser side.
func TestCover_ReturnsServiceUnavailableWhenUnconfigured(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	srv := mountHandler(c)

	r := httptest.NewRequest("GET", "/cover/bw-42/large", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", w.Code)
	}
}
