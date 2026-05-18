// Package server constructs the chi-based HTTP handler. It is wrapped by
// internal/httproutes into the SDK's HttpRoutes.v1 RPC.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps wires the server's collaborators. Optional fields are nil-tolerated by
// each handler so partial wiring (during phased rollouts) doesn't break the
// health route.
type Deps struct {
	// Optional dependencies — handlers check for nil before use.
	BookwarehouseClient BookwarehouseClient
	StreamConfig        StreamConfig
}

// BookwarehouseClient is the subset of bookwarehouse.Client the handlers use.
// Defined as an interface so tests can substitute a fake.
type BookwarehouseClient interface{}

// StreamConfig narrows stream.Config without importing the stream package into
// this root server file.
type StreamConfig interface{}

// Server wraps the chi handler with the configured deps.
type Server struct {
	deps Deps
}

// New returns a server with the given dependencies.
func New(d Deps) *Server { return &Server{deps: d} }

// Handler returns a fully wired http.Handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/admin", s.handleAdminHome)
	r.Get("/admin/", s.handleAdminHome)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		s.mountCatalog(r)
		s.mountStream(r)
	})
	return r
}

func (s *Server) handleAdminHome(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>BookWarehouse Audio</title></head>
<body style="font-family:system-ui,sans-serif;margin:32px;line-height:1.5;background:#111;color:#eee">
<h1>BookWarehouse Audio</h1>
<p>Audiobook catalog, cover, and streaming backend for the Audiobooks portal.</p>
<ul>
<li><a style="color:#8ab4f8" href="./api/v1/health">Health</a></li>
</ul>
</body></html>`))
}
