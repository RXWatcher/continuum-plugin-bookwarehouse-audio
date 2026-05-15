package catalog

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// Handler exposes the audiobook_backend.v1 contract over HTTP.
type Handler struct {
	client *bookwarehouse.Client
}

// NewHandler constructs a Handler bound to a typed upstream client.
func NewHandler(c *bookwarehouse.Client) *Handler { return &Handler{client: c} }

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
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				p.Limit = n
			}
		}
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
		q := r.URL.Query().Get("q")
		out, err := h.client.ListBooks(r.Context(), bookwarehouse.ListParams{
			Query:     q,
			LibraryID: parseLibraryID(r),
		})
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

// Cover handles GET /api/v1/cover/{book_id}/{size} → 302 to upstream cover URL.
func (h *Handler) Cover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bookID := chi.URLParam(r, "book_id")
		size := chi.URLParam(r, "size")
		if bookID == "" {
			http.Error(w, "book_id required", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, h.client.CoverURL(bookID, size), http.StatusFound)
	}
}

func readListParams(r *http.Request) bookwarehouse.ListParams {
	p := bookwarehouse.ListParams{
		Cursor:    r.URL.Query().Get("cursor"),
		LibraryID: parseLibraryID(r),
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			p.Limit = n
		}
	}
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
