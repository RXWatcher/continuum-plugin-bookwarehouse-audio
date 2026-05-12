package server

import (
	"encoding/json"
	"net/http"
)

// handleHealth returns 200 with {"ok": true}. Suitable as a liveness probe.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true,
	})
}
