package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/covers"
	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/localfs"
	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/tokens"
)

// Handler exposes the audiobook_backend.v1 contract over HTTP.
type Handler struct {
	client *bookwarehouse.Client
	covers *covers.Service
	secret string
}

// NewHandler constructs a Handler bound to a typed upstream client. covers
// may be nil when the cover service hasn't been wired (e.g. early dev); the
// Cover() route returns 503 in that case. secret is the HMAC key shared with
// the portal — Cover() requires a valid signed ?token= matching the book id.
func NewHandler(c *bookwarehouse.Client, cv *covers.Service, secret string) *Handler {
	return &Handler{client: c, covers: cv, secret: secret}
}

// maxCatalogLimit caps the page size forwarded upstream. The limit is
// attacker-controlled query input; without a ceiling a client asking for
// limit=999999999 turns into a giant upstream fetch. 0 (absent/invalid/<=0)
// falls back to the upstream default.
const maxCatalogLimit = 200

// parseLimit reads, validates and clamps the ?limit query parameter.
func parseLimit(r *http.Request) int {
	l := r.URL.Query().Get("limit")
	if l == "" {
		return 0
	}
	n, err := strconv.Atoi(l)
	if err != nil || n <= 0 {
		return 0
	}
	if n > maxCatalogLimit {
		return maxCatalogLimit
	}
	return n
}

// Libraries handles GET /api/v1/catalog/libraries. Book Warehouse currently
// exposes one audiobook catalog, but publishing it through the same contract as
// multi-root backends lets the Audiobooks portal configure it explicitly.
func (h *Handler) Libraries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, struct {
			Items []LibraryInfo `json:"items"`
		}{
			Items: []LibraryInfo{{
				ID:        1,
				Name:      "Book Warehouse Audiobooks",
				MediaType: "audiobook",
			}},
		})
	}
}

// List handles GET /api/v1/catalog
func (h *Handler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := bookwarehouse.ListParams{
			Cursor:    r.URL.Query().Get("cursor"),
			Sort:      r.URL.Query().Get("sort"),
			Order:     r.URL.Query().Get("order"),
			LibraryID: parseLibraryID(r),
		}
		p.Limit = parseLimit(r)
		out, err := h.client.ListBooks(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeBookEnvelope(w, out)
	}
}

// Search handles GET /api/v1/catalog/search?q=
func (h *Handler) Search() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := bookwarehouse.ListParams{
			Query:     r.URL.Query().Get("q"),
			Cursor:    r.URL.Query().Get("cursor"),
			Sort:      r.URL.Query().Get("sort"),
			Order:     r.URL.Query().Get("order"),
			LibraryID: parseLibraryID(r),
		}
		p.Limit = parseLimit(r)
		out, err := h.client.ListBooks(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeBookEnvelope(w, out)
	}
}

// Detail handles GET /api/v1/catalog/{id}
func (h *Handler) Detail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			id = r.PathValue("id")
		}
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		d, err := h.client.GetBook(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, ToDetail(d))
	}
}

// BrowseAuthors handles GET /api/v1/browse/authors.
func (h *Handler) BrowseAuthors() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := readListParams(r)
		out, err := h.client.ListAuthors(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		env := PageEnvelope[AuthorSummary]{NextCursor: out.NextCursor, Total: out.Total}
		env.Items = make([]AuthorSummary, len(out.Items))
		for i, a := range out.Items {
			env.Items[i] = AuthorSummary{ID: a.ID, Name: a.Name, Count: a.Count}
		}
		writeJSON(w, env)
	}
}

// BrowseSeries handles GET /api/v1/browse/series.
func (h *Handler) BrowseSeries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := readListParams(r)
		out, err := h.client.ListSeries(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		env := PageEnvelope[SeriesSummary]{NextCursor: out.NextCursor, Total: out.Total}
		env.Items = make([]SeriesSummary, len(out.Items))
		for i, a := range out.Items {
			env.Items[i] = SeriesSummary{ID: a.ID, Name: a.Name, Count: a.Count}
		}
		writeJSON(w, env)
	}
}

// BrowseNarrators handles GET /api/v1/browse/narrators.
func (h *Handler) BrowseNarrators() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := readListParams(r)
		out, err := h.client.ListNarrators(r.Context(), p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		env := PageEnvelope[NarratorSummary]{NextCursor: out.NextCursor, Total: out.Total}
		env.Items = make([]NarratorSummary, len(out.Items))
		for i, a := range out.Items {
			env.Items[i] = NarratorSummary{ID: a.ID, Name: a.Name, Count: a.Count}
		}
		writeJSON(w, env)
	}
}

// Cover handles GET /api/v1/cover/{book_id}/{size}. Cover bytes are served
// from the local filesystem mount: a sidecar file in the book's directory
// when present, otherwise the embedded picture extracted from the first
// audio file. No redirect to BookWarehouse — the byte path stays inside the
// plugin so browsers don't follow URLs they can't reach.
func (h *Handler) Cover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "book_id")
		sizeParam := chi.URLParam(r, "size")
		if bookID == "" {
			http.Error(w, "book_id required", http.StatusBadRequest)
			return
		}
		if h.covers == nil {
			writeServiceUnavailable(w, "cover service not configured: set library_root")
			return
		}
		// The cover route is declared public on the host plugin proxy
		// (proxy can't validate per-plugin tokens), so this handler is the
		// only auth gate. file_idx=-1 is the sentinel for cover tokens.
		if _, err := tokens.Verify(h.secret, r.URL.Query().Get("token"), bookID, tokens.CoverFileIdx); err != nil {
			writeTokenError(w, err)
			return
		}
		res, err := h.covers.Get(r.Context(), bookID, normalizeSize(sizeParam))
		if err != nil {
			writeCoverError(w, err)
			return
		}
		defer res.Close()
		w.Header().Set("Content-Type", res.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Header().Set("X-Cover-Source", "local-fs")
		http.ServeContent(w, r, "cover", res.ModTime, res.Reader)
	}
}

// normalizeSize maps the URL {size} segment to a covers.Size. Accepts
// historical names from the older API (large, small, thumbnail) so existing
// portal URLs keep working through the rewrite.
func normalizeSize(raw string) covers.Size {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "thumb", "thumbnail", "small":
		return covers.SizeThumb
	case "medium":
		return covers.SizeMedium
	default:
		return covers.SizeOriginal
	}
}

func writeCoverError(w http.ResponseWriter, err error) {
	if errors.Is(err, covers.ErrNoCover) {
		http.Error(w, "no cover available", http.StatusNotFound)
		return
	}
	if re := localfs.AsResolveError(err); re != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":      "local filesystem resolve failed",
			"source_key": re.SourceKey,
			"reason":     re.Reason,
			"attempts":   re.Attempts,
		})
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
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

func readListParams(r *http.Request) bookwarehouse.ListParams {
	p := bookwarehouse.ListParams{
		Cursor:    r.URL.Query().Get("cursor"),
		LibraryID: parseLibraryID(r),
	}
	p.Limit = parseLimit(r)
	return p
}

func parseLibraryID(r *http.Request) int64 {
	raw := r.URL.Query().Get("library_id")
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func writeBookEnvelope(w http.ResponseWriter, p bookwarehouse.Paged[bookwarehouse.Book]) {
	out := PageEnvelope[AudiobookSummary]{
		NextCursor: p.NextCursor,
		Total:      p.Total,
	}
	out.Items = make([]AudiobookSummary, len(p.Items))
	for i, b := range p.Items {
		out.Items[i] = ToSummary(b)
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
