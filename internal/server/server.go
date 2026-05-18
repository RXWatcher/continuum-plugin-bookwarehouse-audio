// Package server constructs the chi-based HTTP handler. It is wrapped by
// internal/httproutes into the SDK's HttpRoutes.v1 RPC.
package server

import (
	"html"
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

func (s *Server) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en" data-theme="` + adminTheme(r) + `">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>BookWarehouse Audio</title><style>` + adminThemeCSS() + `</style></head>
<body>
<p><a href="/admin/plugins">&larr; Back to plugins</a></p>
<h1>BookWarehouse Audio</h1>
<p>Audiobook catalog, cover, and streaming backend for the Audiobooks portal.</p>
<ul>
<li><a href="./api/v1/health">Health</a></li>
</ul>
</body></html>`))
}

func adminTheme(r *http.Request) string {
	theme := r.Header.Get("X-Continuum-Theme")
	if theme == "" {
		theme = r.URL.Query().Get("theme")
	}
	if theme == "" {
		theme = "default"
	}
	return html.EscapeString(theme)
}

func adminThemeCSS() string {
	return `:root{--bg:#141417;--fg:#e8e8ec;--link:#93c5fd;--panel:#1c1c20;--border:#28282e}[data-theme="cinema-light"]{--bg:#f7f3ed;--fg:#201c18;--link:#9a3412;--panel:#fffaf3;--border:#ded1c0}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--link:#60a5fa;--panel:#172033;--border:#2d3f61}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--link:#fb7185;--panel:#241018;--border:#4a2230}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--link:#6ee7b7;--panel:#14241b;--border:#2b4b39}body{font-family:system-ui,sans-serif;margin:32px;line-height:1.5;background:var(--bg);color:var(--fg)}a{color:var(--link);text-decoration:none}li{margin:6px 0}ul{border:1px solid var(--border);background:var(--panel);border-radius:8px;padding:16px 16px 16px 34px;max-width:520px}`
}
