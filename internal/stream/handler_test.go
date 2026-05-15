package stream_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

func TestStream_Redirects302ToUpstream(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/3", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d", w.Code)
	}
	loc := w.Header().Get("Location")
	want := "https://upstream.example/api/v1/books/bw-7/files/3/stream"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestStream_BadIndex(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/notanumber", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestStream_DirectFileAccessServesRange(t *testing.T) {
	dir := t.TempDir()
	localRoot := filepath.Join(dir, "local")
	if err := os.MkdirAll(filepath.Join(localRoot, "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(localRoot, "audio", "book.m4b")
	if err := os.WriteFile(localFile, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/books/bw-7" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "bw-7",
			"title": "Book",
			"files": []map[string]any{{
				"index":            0,
				"file_path":        "/warehouse/audio/book.m4b",
				"storage_key":      "/warehouse/audio/book.m4b",
				"codec":            "m4b",
				"file_size":        10,
				"duration_seconds": 100,
			}},
		})
	}))
	defer upstream.Close()

	c := bookwarehouse.NewClient(upstream.URL, "k")
	h := stream.NewHandler(c, stream.Config{
		DirectFileAccess: true,
		PathRemappings: []stream.PathRemapping{{
			SourcePath: "/warehouse",
			TargetPath: localRoot,
		}},
	})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/0", nil)
	r.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "2345" {
		t.Fatalf("body = %q", got)
	}
	if got := w.Header().Get("X-Stream-Source"); got != "direct" {
		t.Fatalf("X-Stream-Source = %q", got)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q", got)
	}
}
