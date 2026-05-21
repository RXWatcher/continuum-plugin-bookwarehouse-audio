package stream_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

// A symlink planted inside the configured library root (or an
// attacker-influenced upstream source path) must not let the handler resolve
// outside the root, even though it is lexically contained.
func TestStream_SymlinkEscapeBlocked(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	legit := filepath.Join(root, "audio", "book.m4b")
	if err := os.WriteFile(legit, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.m4b")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/audiobooks/legit":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "legit",
				"title": "OK",
				"files": []map[string]any{{
					"index":       0,
					"file_path":   "audio/book.m4b",
					"storage_key": "audio/book.m4b",
					"codec":       "m4b",
					"file_size":   2,
				}},
			})
		case "/api/v1/audiobooks/escape":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "escape",
				"title": "Escape",
				"files": []map[string]any{{
					"index":       0,
					"file_path":   "escape/secret.m4b",
					"storage_key": "escape/secret.m4b",
					"codec":       "m4b",
					"file_size":   10,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	c := bookwarehouse.NewClient(upstream.URL, "k")
	h := stream.NewHandler(c, stream.Config{LibraryRoot: root, StreamSigningSecret: testSecret})
	router := chi.NewRouter()
	router.Get("/stream/{book_id}/{file_idx}", h.Stream())

	// Legitimate file inside the root resolves.
	r := httptest.NewRequest("GET", "/stream/legit/0?token="+signTestToken(t, "legit", 0), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("legit code = %d body = %s", w.Code, w.Body.String())
	}

	// Path that lexically lives under root but resolves through a symlink to
	// outside must be rejected.
	r = httptest.NewRequest("GET", "/stream/escape/0?token="+signTestToken(t, "escape", 0), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("escape code = %d (want 502)", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "resolve failed") {
		t.Fatalf("escape body = %q", body)
	}
}

// findFile must not fall back to positional indexing when the upstream
// populates explicit indices — that would silently serve a different track.
func TestFindFile_NoSilentWrongTrack(t *testing.T) {
	indexed := []bookwarehouse.File{
		{Index: 0, Filename: "a"},
		{Index: 1, Filename: "b"},
		{Index: 2, Filename: "c"},
	}
	if f, ok := stream.FindFileForTesting(indexed, 5); ok {
		t.Fatalf("idx=5 absent but served %q", f.Filename)
	}
	if f, ok := stream.FindFileForTesting(indexed, 1); !ok || f.Filename != "b" {
		t.Fatalf("idx=1 should match the indexed file, got %q ok=%v", f.Filename, ok)
	}
	gapped := []bookwarehouse.File{{Index: 2, Filename: "x"}, {Index: 5, Filename: "y"}}
	if f, ok := stream.FindFileForTesting(gapped, 5); !ok || f.Filename != "y" {
		t.Fatalf("idx=5 should match Index:5, got %q ok=%v", f.Filename, ok)
	}
	unindexed := []bookwarehouse.File{{Filename: "t0"}, {Filename: "t1"}, {Filename: "t2"}}
	if f, ok := stream.FindFileForTesting(unindexed, 2); !ok || f.Filename != "t2" {
		t.Fatalf("positional fallback broken, got %q ok=%v", f.Filename, ok)
	}
	if _, ok := stream.FindFileForTesting(unindexed, 9); ok {
		t.Fatal("out-of-range positional request should be not-found")
	}
}
