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
<nav class="tabs" aria-label="BookWarehouse Audio admin sections">
<button class="tab active" data-tab-target="readiness" type="button">Readiness</button>
<button class="tab" data-tab-target="browser" type="button">Browser</button>
<button class="tab" data-tab-target="stream-test" type="button">Stream test</button>
<button class="tab" data-tab-target="diagnostics" type="button">Diagnostics</button>
</nav>
<section class="tab-panel active" id="readiness">
<article class="panel"><div class="panel-head"><div><h2>Setup status</h2><p class="muted">This plugin is stateless: it proxies BookWarehouse catalog, cover, and stream operations and does not own request reconciliation.</p></div><span id="ready-badge" class="badge">Loading</span></div><div id="status" class="cards muted">Loading diagnostics...</div></article>
</section>
<section class="tab-panel" id="browser">
<article class="panel"><div class="panel-head"><div><h2>Backend browser</h2><p class="muted">Fetch libraries or search upstream titles without leaving the plugin admin page.</p></div></div><form id="search-form" class="row"><input id="q" placeholder="Title, author, narrator" aria-label="Search query"><button type="submit">Search</button><button id="test" type="button">Fetch libraries</button></form><pre id="output" class="output">No browser test run yet.</pre></article>
</section>
<section class="tab-panel" id="stream-test">
<article class="panel"><div class="panel-head"><div><h2>Stream diagnostics</h2><p class="muted">Direct file access serves ranged local files when remapping succeeds. Redirect fallback sends the client to BookWarehouse when direct access is unavailable.</p></div></div><form id="stream-form" class="row"><input id="book-id" placeholder="Book ID" aria-label="Book ID"><input id="file-idx" value="0" placeholder="File index" aria-label="File index"><button type="submit">Build stream URL</button></form><pre id="stream-output" class="output">Enter a book id to inspect the route Continuum will call.</pre><div class="triage-grid"><div><h3>Direct file access</h3><p>When enabled, the plugin tries local filesystem streaming before falling back to upstream redirects.</p></div><div><h3>Range support</h3><p>Direct files should include <code>Accept-Ranges: bytes</code> and answer ranged requests with partial content.</p></div><div><h3>Path remapping</h3><p>Every BookWarehouse source path must map to a readable container path without escaping the configured target root.</p></div></div></article>
</section>
<section class="tab-panel" id="diagnostics">
<article class="panel"><div class="panel-head"><div><h2>Raw diagnostics</h2><p class="muted">Use this payload when debugging upstream authentication, route mounting, or portal provisioning.</p></div></div><pre id="diagnostics-output" class="output">Loading diagnostics...</pre></article>
</section>
<section class="panel"><h2>Operations checklist</h2><ul><li>Configure BookWarehouse <code>base_url</code> and <code>api_key</code>.</li><li>Add this backend as a presentation library in the Audiobooks portal.</li><li>Run a library fetch test before troubleshooting the portal.</li><li>Validate stream path remapping if direct file access is enabled.</li></ul></section>
</main>
<script>
const statusEl=document.getElementById("status"), output=document.getElementById("output"), diagnosticsOutput=document.getElementById("diagnostics-output"), streamOutput=document.getElementById("stream-output");
const hostToken=new URLSearchParams(location.search).get("token")||"";
function headers(){return hostToken?{Authorization:"Bearer "+hostToken}:{}}
function badge(ok){return '<span class="badge '+(ok?'ok':'bad')+'">'+(ok?'OK':'Needs attention')+'</span>'}
function esc(v){return String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
function activateTab(id){document.querySelectorAll(".tab").forEach(b=>b.classList.toggle("active",b.dataset.tabTarget===id));document.querySelectorAll(".tab-panel").forEach(p=>p.classList.toggle("active",p.id===id))}
document.querySelectorAll(".tab").forEach(b=>b.addEventListener("click",()=>activateTab(b.dataset.tabTarget)))
async function load(){try{const r=await fetch("./api/v1/admin/diagnostics",{headers:headers()});const d=await r.json();document.getElementById("ready-badge").textContent=d.configured&&d.upstream?.ok?"Ready":"Needs attention";statusEl.innerHTML='<div class="diag">'+badge(d.configured)+'<strong>Configured</strong><span>base_url and api_key are applied</span></div><div class="diag">'+badge(d.upstream?.ok)+'<strong>BookWarehouse</strong><span>'+esc(d.upstream?.message)+'</span></div><div class="diag">'+badge(d.catalog_routes)+'<strong>Catalog routes</strong><span>libraries, list, search, detail, browse</span></div><div class="diag">'+badge(d.stream_routes)+'<strong>Stream routes</strong><span>direct local file or Redirect fallback</span></div>';diagnosticsOutput.textContent=JSON.stringify(d,null,2)}catch(e){statusEl.textContent=String(e);diagnosticsOutput.textContent=String(e)}} 
document.getElementById("test").addEventListener("click",async()=>{output.textContent="Loading libraries...";try{const r=await fetch("./api/v1/catalog/libraries",{headers:headers()});output.textContent=JSON.stringify(await r.json(),null,2)}catch(err){output.textContent=String(err)}})
document.getElementById("search-form").addEventListener("submit",async e=>{e.preventDefault();output.textContent="Searching...";try{const q=encodeURIComponent(document.getElementById("q").value||"");const path=q?"./api/v1/catalog/search?q="+q+"&limit=10":"./api/v1/catalog?limit=10";const r=await fetch(path,{headers:headers()});output.textContent=JSON.stringify(await r.json(),null,2)}catch(err){output.textContent=String(err)}})
document.getElementById("stream-form").addEventListener("submit",e=>{e.preventDefault();const id=document.getElementById("book-id").value.trim();const idx=document.getElementById("file-idx").value.trim()||"0";if(!id){streamOutput.textContent="Book ID required.";return}const url=new URL("./api/v1/stream/"+encodeURIComponent(id)+"/"+encodeURIComponent(idx),location.href).toString();streamOutput.textContent=JSON.stringify({route:url,expected:"200/206 with X-Stream-Source: direct when remapping succeeds, otherwise 302 Redirect fallback to BookWarehouse"},null,2)})
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
	return `:root{--bg:#141417;--fg:#e8e8ec;--muted:#a1a1aa;--link:#93c5fd;--panel:#1c1c20;--border:#28282e;--ok:#22c55e;--bad:#fb7185;--input:#101014}[data-theme="cinema-light"]{--bg:#f7f3ed;--fg:#201c18;--muted:#756b60;--link:#9a3412;--panel:#fffaf3;--border:#ded1c0;--input:#fff}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#afc2e2;--link:#60a5fa;--panel:#172033;--border:#2d3f61;--input:#0d1422}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#f0a6b7;--link:#fb7185;--panel:#241018;--border:#4a2230;--input:#12070b}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bd6b4;--link:#6ee7b7;--panel:#14241b;--border:#2b4b39;--input:#08110d}*{box-sizing:border-box}body{font-family:system-ui,sans-serif;margin:0;line-height:1.5;background:var(--bg);color:var(--fg)}.shell{max-width:1120px;margin:0 auto;padding:28px}.back{display:inline-flex;margin-bottom:12px;color:var(--link);text-decoration:none}.eyebrow{color:var(--muted);text-transform:uppercase;font-size:12px;letter-spacing:.08em}h1{margin:.2rem 0}h2{font-size:16px;margin:0}.tabs{display:flex;gap:8px;flex-wrap:wrap;margin:18px 0}.tab{background:transparent;color:var(--fg);border:1px solid var(--border)}.tab.active{background:var(--link);color:#08111f}.tab-panel{display:none}.tab-panel.active{display:block}.grid,.triage-grid,.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px}.panel{border:1px solid var(--border);background:var(--panel);border-radius:8px;padding:16px;margin-top:16px}.panel-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.triage-grid{margin-top:14px}.triage-grid h3{font-size:14px;margin:.2rem 0}.triage-grid p{color:var(--muted);margin:.25rem 0}.row{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:8px}.stack>*+*{margin-top:8px}input{min-width:0;background:var(--input);color:var(--fg);border:1px solid var(--border);border-radius:6px;padding:9px}button{background:var(--link);border:0;border-radius:6px;padding:9px 12px;color:#08111f;font-weight:700;cursor:pointer}.badge{display:inline-block;border:1px solid var(--border);border-radius:999px;padding:2px 8px;margin-right:6px;font-size:12px;white-space:nowrap}.ok{color:var(--ok)}.bad{color:var(--bad)}.muted{color:var(--muted)}.diag{display:grid;gap:4px;border:1px solid var(--border);border-radius:6px;background:var(--input);padding:12px}.diag strong{color:var(--fg)}.diag span{color:var(--muted);font-size:12px}.output{overflow:auto;max-height:340px;background:var(--input);border:1px solid var(--border);border-radius:6px;padding:10px;color:var(--fg)}code{color:var(--link)}@media(max-width:760px){.row,.panel-head{grid-template-columns:1fr;display:grid}}`
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
