// Package requesthandler serves the audiobook_backend.v1 request status
// snapshot endpoint. The portal calls this from its reconciler when an event
// might have been missed.
package requesthandler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/store"
)

// Handler exposes GET /api/v1/requests/{external_id}.
type Handler struct {
	store  *store.Store
	client *bookwarehouse.Client
}

// NewHandler builds a request snapshot handler.
func NewHandler(s *store.Store, c *bookwarehouse.Client) *Handler {
	return &Handler{store: s, client: c}
}

// Snapshot returns the row for an external_id, optionally refreshing from
// upstream when the client is configured.
func (h *Handler) Snapshot() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		externalID := chi.URLParam(r, "external_id")
		if externalID == "" {
			http.Error(w, "external_id required", http.StatusBadRequest)
			return
		}
		row, err := h.store.GetByExternalID(r.Context(), externalID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Best-effort: refresh from upstream so the reconciler picks up
		// any state transitions we missed.
		if h.client != nil {
			if snap, err := h.client.GetMonitoring(r.Context(), externalID); err == nil && snap.Status != "" {
				row.Status = snap.Status
			}
		}
		writeJSON(w, map[string]any{
			"request_id":  row.RequestID,
			"external_id": row.ExternalID,
			"status":      row.Status,
			"error":       row.ErrorText,
			"updated_at":  row.UpdatedAt,
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
