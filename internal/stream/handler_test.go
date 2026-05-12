package stream_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

func TestStream_Redirects302ToUpstream(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c)
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
	h := stream.NewHandler(c)
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/notanumber", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}
