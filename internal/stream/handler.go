// Package stream handles the audiobook_backend.v1 streaming surface. It
// issues 302 redirects to the upstream BookWarehouse stream URL; the portal's
// configured streaming mode decides whether to follow that redirect (proxy
// mode) or download via the URL into its local cache (cache mode).
package stream

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// Handler wraps the upstream client and serves the stream redirect route.
type Handler struct {
	client *bookwarehouse.Client
}

// NewHandler constructs a stream handler.
func NewHandler(c *bookwarehouse.Client) *Handler { return &Handler{client: c} }

// Stream issues a 302 redirect to the upstream's audio stream endpoint.
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
		http.Redirect(w, r, h.client.StreamURL(bookID, idx), http.StatusFound)
	}
}
