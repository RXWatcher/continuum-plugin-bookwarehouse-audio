// Package stream implements the audiobook_backend.v1 byte-serving surface.
// It serves audio files directly from the local filesystem (the mount where
// BookWarehouse-managed audiobooks live inside this plugin runtime) using
// http.ServeContent for Range support.
//
// There is no upstream-redirect fallback: BookWarehouse's /stream endpoint
// is a 501 stub and /download has no Range support, so a redirect path
// dead-ends at a player that cannot seek. Resolution failures return a
// structured 502 so operators see "remap missing" in logs instead of a
// silent player-side stall.
package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/catalog"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/localfs"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/tokens"
)

// PathRemapping rewrites an absolute BookWarehouse storage path prefix to a
// local mount path prefix. Mirrors localfs.Remapping for back-compat with
// existing config plumbing.
type PathRemapping struct {
	SourcePath string
	TargetPath string
}

// Config controls byte serving.
type Config struct {
	LibraryRoot         string
	PathRemappings      []PathRemapping
	StreamSigningSecret string
}

// Handler serves audio bytes from the local filesystem mount.
type Handler struct {
	client   *bookwarehouse.Client
	resolver *localfs.Resolver
	secret   string
}

// NewHandler constructs a stream handler. A nil resolver still produces a
// handler — requests get a 503 explaining the configuration gap.
func NewHandler(c *bookwarehouse.Client, cfg Config) *Handler {
	remaps := make([]localfs.Remapping, 0, len(cfg.PathRemappings))
	for _, m := range cfg.PathRemappings {
		remaps = append(remaps, localfs.Remapping{SourcePath: m.SourcePath, TargetPath: m.TargetPath})
	}
	return &Handler{
		client:   c,
		resolver: localfs.New(cfg.LibraryRoot, remaps),
		secret:   cfg.StreamSigningSecret,
	}
}

// Stream serves /api/v1/stream/{book_id}/{file_idx}.
func (h *Handler) Stream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "book_id")
		idxStr := chi.URLParam(r, "file_idx")
		if bookID == "" || idxStr == "" {
			http.Error(w, "book_id and file_idx required", http.StatusBadRequest)
			return
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 {
			http.Error(w, "file_idx must be a non-negative integer", http.StatusBadRequest)
			return
		}
		if h.client == nil {
			writeServiceUnavailable(w, "upstream client not configured")
			return
		}
		if !h.resolver.Configured() {
			writeServiceUnavailable(w, "library_root not configured")
			return
		}
		// Signed media token validation. The route is declared public on the
		// host plugin proxy (it can't validate per-plugin tokens), so this
		// handler is the only place enforcing user-scoped, book-scoped, and
		// time-bounded access. Reject early on missing/invalid/expired tokens.
		if _, err := tokens.Verify(h.secret, r.URL.Query().Get("token"), bookID, idx); err != nil {
			writeTokenError(w, err)
			return
		}

		detail, err := h.client.GetBook(r.Context(), bookID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		file, ok := findFile(detail.Files, idx)
		if !ok {
			http.Error(w, "file index not found", http.StatusNotFound)
			return
		}
		sourceKey := file.StorageKey
		if sourceKey == "" {
			sourceKey = file.Filename
		}
		localPath, err := h.resolver.Resolve(sourceKey)
		if err != nil {
			writeResolveError(w, err)
			return
		}
		f, err := os.Open(localPath)
		if err != nil {
			http.Error(w, "file not readable", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil || stat.IsDir() {
			http.Error(w, "file not readable", http.StatusInternalServerError)
			return
		}

		filename := filepath.Base(file.Filename)
		if filename == "." || filename == string(filepath.Separator) || filename == "" {
			filename = detail.Title
		}
		if filename == "" {
			filename = bookID
		}
		w.Header().Set("Content-Type", catalog.CodecToMime(file.Codec))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, safeFilename(filename)))
		w.Header().Set("X-Stream-Source", "local-fs")
		http.ServeContent(w, r, filename, stat.ModTime(), f)
	}
}

func findFile(files []bookwarehouse.File, idx int) (bookwarehouse.File, bool) {
	for _, file := range files {
		if file.Index == idx {
			return file, true
		}
	}
	// No explicit-index match. Only fall back to positional indexing when the
	// upstream response carries no indices at all (every File.Index is the
	// zero value) — otherwise a request for a non-existent index would
	// silently serve a *different* track from this same book. When indices
	// are populated but none match, report not-found.
	for _, file := range files {
		if file.Index != 0 {
			return bookwarehouse.File{}, false
		}
	}
	if idx >= 0 && idx < len(files) {
		return files[idx], true
	}
	return bookwarehouse.File{}, false
}

func writeServiceUnavailable(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func writeTokenError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	if errors.Is(err, tokens.ErrSecretUnconfigured) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "media signing secret not configured"})
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}

func writeResolveError(w http.ResponseWriter, err error) {
	re := localfs.AsResolveError(err)
	w.Header().Set("Content-Type", "application/json")
	if re == nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":      "local filesystem resolve failed",
		"source_key": re.SourceKey,
		"reason":     re.Reason,
		"attempts":   re.Attempts,
	})
}

func safeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "\"", "_")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
}
