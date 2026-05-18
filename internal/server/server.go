// Package server constructs the chi-based HTTP handler. It is wrapped by
// internal/httproutes into the SDK's HttpRoutes.v1 RPC.
package server

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
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
		r.Get("/admin/diagnostics", s.handleDiagnostics)
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
<main class="shell">
<a class="back" href="/admin/plugins">&larr; Plugins</a>
<header><p class="eyebrow">Audiobook backend</p><h1>BookWarehouse Audio</h1><p>Audiobook catalog, cover, browse, and stream backend for the Audiobooks portal.</p></header>
<section class="grid">
<article class="panel"><h2>Setup status</h2><div id="status" class="stack muted">Loading diagnostics...</div></article>
<article class="panel"><h2>Catalog smoke test</h2><button id="test" type="button">Fetch libraries</button><pre id="output" class="output">No test run yet.</pre></article>
</section>
<section class="panel"><h2>Operations checklist</h2><ul><li>Configure BookWarehouse <code>base_url</code> and <code>api_key</code>.</li><li>Add this backend as a presentation library in the Audiobooks portal.</li><li>Run a library fetch test before troubleshooting the portal.</li><li>Validate stream path remapping if direct file access is enabled.</li></ul></section>
</main>
<script>
const statusEl=document.getElementById("status"), output=document.getElementById("output");
const hostToken=new URLSearchParams(location.search).get("token")||"";
function headers(){return hostToken?{Authorization:"Bearer "+hostToken}:{}}
function badge(ok){return '<span class="badge '+(ok?'ok':'bad')+'">'+(ok?'OK':'Needs attention')+'</span>'}
function esc(v){return String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
async function load(){try{const r=await fetch("./api/v1/admin/diagnostics",{headers:headers()});const d=await r.json();statusEl.innerHTML='<div>'+badge(d.configured)+' Configured</div><div>'+badge(d.upstream?.ok)+' BookWarehouse: '+esc(d.upstream?.message)+'</div><div>'+badge(d.catalog_routes)+' Catalog routes mounted</div><div>'+badge(d.stream_routes)+' Stream routes mounted</div>'}catch(e){statusEl.textContent=String(e)}} 
document.getElementById("test").addEventListener("click",async()=>{output.textContent="Loading...";try{const r=await fetch("./api/v1/catalog/libraries",{headers:headers()});output.textContent=JSON.stringify(await r.json(),null,2)}catch(err){output.textContent=String(err)}})
load();
</script>
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
	return `:root{--bg:#141417;--fg:#e8e8ec;--muted:#a1a1aa;--link:#93c5fd;--panel:#1c1c20;--border:#28282e;--ok:#22c55e;--bad:#fb7185;--input:#101014}[data-theme="cinema-light"]{--bg:#f7f3ed;--fg:#201c18;--muted:#756b60;--link:#9a3412;--panel:#fffaf3;--border:#ded1c0;--input:#fff}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#afc2e2;--link:#60a5fa;--panel:#172033;--border:#2d3f61;--input:#0d1422}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#f0a6b7;--link:#fb7185;--panel:#241018;--border:#4a2230;--input:#12070b}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bd6b4;--link:#6ee7b7;--panel:#14241b;--border:#2b4b39;--input:#08110d}body{font-family:system-ui,sans-serif;margin:0;line-height:1.5;background:var(--bg);color:var(--fg)}.shell{max-width:1120px;margin:0 auto;padding:28px}.back{color:var(--link);text-decoration:none}.eyebrow{color:var(--muted);text-transform:uppercase;font-size:12px;letter-spacing:.08em}h1{margin:.2rem 0}h2{font-size:16px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px}.panel{border:1px solid var(--border);background:var(--panel);border-radius:8px;padding:16px;margin-top:16px}.stack>*+*{margin-top:8px}button{background:var(--link);border:0;border-radius:6px;padding:9px 12px;color:#08111f;font-weight:700}.badge{display:inline-block;border:1px solid var(--border);border-radius:999px;padding:2px 8px;margin-right:6px;font-size:12px}.ok{color:var(--ok)}.bad{color:var(--bad)}.muted{color:var(--muted)}.output{overflow:auto;max-height:340px;background:var(--input);border:1px solid var(--border);border-radius:6px;padding:10px;color:var(--fg)}code{color:var(--link)}`
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	upstreamOK := false
	upstreamMessage := "not configured"
	if ok && cli != nil {
		if _, err := cli.Get(ctx, "/api/v1/audiobooks?limit=1"); err != nil {
			upstreamMessage = err.Error()
		} else {
			upstreamOK = true
			upstreamMessage = "upstream reachable"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugin_id":      "continuum.bookwarehouse-audio",
		"role":           "audiobook_library_source",
		"configured":     ok && cli != nil,
		"catalog_routes": ok && cli != nil,
		"stream_routes":  ok && cli != nil,
		"upstream":       map[string]any{"ok": upstreamOK, "message": upstreamMessage},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
