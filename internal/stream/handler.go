// Package stream handles the audiobook_backend.v1 streaming surface. It can
// serve large audio files directly from a local filesystem mount using
// BookWarehouse file paths plus admin-configured remapping rules, falling back
// to upstream BookWarehouse redirects when direct access is unavailable.
package stream

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/catalog"
)

// PathRemapping maps a BookWarehouse source path prefix to a local target path
// prefix available inside the plugin runtime.
type PathRemapping struct {
	SourcePath string
	TargetPath string
}

// Config controls direct filesystem streaming.
type Config struct {
	DirectFileAccess bool
	PathRemappings   []PathRemapping
}

// Handler wraps the upstream client and serves the stream redirect route.
type Handler struct {
	client *bookwarehouse.Client
	config Config
}

// NewHandler constructs a stream handler.
func NewHandler(c *bookwarehouse.Client, cfg Config) *Handler {
	return &Handler{client: c, config: cfg}
}

// Stream serves a local file with range support when direct file access is
// configured. Otherwise it issues a 302 redirect to the upstream audio stream.
func (h *Handler) Stream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "book_id")
		idxStr := chi.URLParam(r, "file_idx")
		if bookID == "" || idxStr == "" {
			http.Error(w, "book_id and file_idx required", http.StatusBadRequest)
			return
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			http.Error(w, "file_idx must be int", http.StatusBadRequest)
			return
		}

		if h.config.DirectFileAccess && len(h.config.PathRemappings) > 0 {
			if served := h.tryServeDirect(w, r, bookID, idx); served {
				return
			}
		}
		http.Redirect(w, r, h.client.StreamURL(bookID, idx), http.StatusFound)
	}
}

func (h *Handler) tryServeDirect(w http.ResponseWriter, r *http.Request, bookID string, idx int) bool {
	detail, err := h.client.GetBook(r.Context(), bookID)
	if err != nil {
		return false
	}
	file, ok := findFile(detail.Files, idx)
	if !ok {
		return false
	}
	sourcePath := file.StorageKey
	if sourcePath == "" {
		sourcePath = file.Filename
	}
	localPath, ok := remapPath(sourcePath, h.config.PathRemappings)
	if !ok {
		return false
	}
	f, err := os.Open(localPath)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}
	filename := filepath.Base(file.Filename)
	if filename == "." || filename == string(filepath.Separator) {
		filename = detail.Title
	}
	if filename == "" {
		filename = bookID
	}
	w.Header().Set("Content-Type", catalog.CodecToMime(file.Codec))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, safeFilename(filename)))
	w.Header().Set("X-Stream-Source", "direct")
	http.ServeContent(w, r, filename, stat.ModTime(), f)
	return true
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
	// are populated but none match, report not-found so the caller defers to
	// the upstream redirect, which interprets the index authoritatively.
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

func remapPath(source string, remappings []PathRemapping) (string, bool) {
	if source == "" {
		return "", false
	}
	sourceClean := filepath.Clean(source)
	for _, mapping := range remappings {
		sourcePrefix := filepath.Clean(mapping.SourcePath)
		targetPrefix := filepath.Clean(mapping.TargetPath)
		if sourcePrefix == "." || targetPrefix == "." {
			continue
		}
		if sourceClean != sourcePrefix && !strings.HasPrefix(sourceClean, sourcePrefix+string(filepath.Separator)) {
			continue
		}
		rel, err := filepath.Rel(sourcePrefix, sourceClean)
		if err != nil || rel == "." {
			rel = ""
		}
		if strings.HasPrefix(rel, "..") {
			continue
		}
		localPath := filepath.Clean(filepath.Join(targetPrefix, rel))
		if localPath != targetPrefix && !strings.HasPrefix(localPath, targetPrefix+string(filepath.Separator)) {
			continue
		}
		// Lexical containment above blocks "../" traversal, but a symlink
		// planted inside the target mount (or an attacker-influenced upstream
		// source path) can still resolve outside the admin-configured root.
		// Resolve symlinks on both sides and re-verify containment against
		// the real paths before trusting localPath.
		if resolved, ok := resolveWithin(targetPrefix, localPath); ok {
			return resolved, true
		}
	}
	return "", false
}

// resolveWithin resolves symlinks in candidate and confirms the real path is
// still inside the real targetPrefix. It returns false when either path cannot
// be resolved (e.g. the file does not exist — the caller then falls back to
// the upstream redirect) or when the resolved path escapes the root.
func resolveWithin(targetPrefix, candidate string) (string, bool) {
	realTarget, err := filepath.EvalSymlinks(targetPrefix)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	if realPath != realTarget && !strings.HasPrefix(realPath, realTarget+string(filepath.Separator)) {
		return "", false
	}
	return realPath, true
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
