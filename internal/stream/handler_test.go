package stream_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/stream"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/tokens"
)

const testSecret = "test-secret-with-enough-entropy-32"

// signTestToken mints an HS256 token shaped like what the portal would
// produce. Returns the URL-encoded query value.
func signTestToken(t *testing.T, bookID string, fileIdx int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"aud":      tokens.Audience,
		"sub":      "1",
		"book_id":  bookID,
		"file_idx": fileIdx,
		"exp":      time.Now().Add(5 * time.Minute).Unix(),
		"iat":      time.Now().Unix(),
	})
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return url.QueryEscape(s)
}

func TestStream_ReturnsServiceUnavailableWhenLibraryRootUnset(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/3?token="+signTestToken(t, "bw-7", 3), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", w.Code)
	}
}

func TestStream_BadIndex(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: "/tmp", StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/notanumber", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestStream_RejectsMissingToken(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: "/tmp", StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", w.Code)
	}
}

func TestStream_RejectsTokenForWrongBook(t *testing.T) {
	c := bookwarehouse.NewClient("https://upstream.example", "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: "/tmp", StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/0?token="+signTestToken(t, "other-book", 0), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", w.Code)
	}
}

func TestStream_LocalFSServesRange(t *testing.T) {
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
		if r.URL.Path != "/api/v1/audiobooks/bw-7" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "bw-7",
			"title": "Book",
			"files": []map[string]any{{
				"index":            0,
				"file_path":        "audio/book.m4b",
				"storage_key":      "audio/book.m4b",
				"codec":            "m4b",
				"file_size":        10,
				"duration_seconds": 100,
			}},
		})
	}))
	defer upstream.Close()

	c := bookwarehouse.NewClient(upstream.URL, "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: localRoot, StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-7/0?token="+signTestToken(t, "bw-7", 0), nil)
	r.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "2345" {
		t.Fatalf("body = %q", got)
	}
	if got := w.Header().Get("X-Stream-Source"); got != "local-fs" {
		t.Fatalf("X-Stream-Source = %q", got)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q", got)
	}
}

func TestStream_ResolveMissReturnsStructuredError(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/bw-x" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "bw-x",
			"title": "Missing",
			"files": []map[string]any{{
				"index":       0,
				"file_path":   "nonexistent/file.m4b",
				"storage_key": "nonexistent/file.m4b",
				"codec":       "m4b",
			}},
		})
	}))
	defer upstream.Close()

	c := bookwarehouse.NewClient(upstream.URL, "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: dir, StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	r := httptest.NewRequest("GET", "/stream/bw-x/0?token="+signTestToken(t, "bw-x", 0), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, w.Body.String())
	}
	if got, _ := body["source_key"].(string); !strings.Contains(got, "nonexistent") {
		t.Fatalf("source_key = %q", got)
	}
}
