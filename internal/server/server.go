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

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/runtime"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/store"
)

// Deps wires the server's collaborators. Optional fields are nil-tolerated by
// each handler so partial wiring (during phased rollouts) doesn't break the
// health route.
type Deps struct {
	// Optional dependencies — handlers check for nil before use.
	BookwarehouseClient BookwarehouseClient
	StreamConfig        StreamConfig
	Covers              CoversService
	Store               *store.Store
	Config              runtime.Config
	// Refresh re-reads the persisted app config and rebuilds the byte-serving
	// stack (path resolver, cover service, BW client). The admin handler
	// calls it after a successful config save so changes to library_root,
	// path_remappings, etc., take effect without a plugin re-upload.
	Refresh func(context.Context) error
}

// BookwarehouseClient is the subset of bookwarehouse.Client the handlers use.
// Defined as an interface so tests can substitute a fake.
type BookwarehouseClient interface{}

// StreamConfig narrows stream.Config without importing the stream package into
// this root server file.
type StreamConfig interface{}

// CoversService narrows *covers.Service for the same reason as StreamConfig.
type CoversService interface{}

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
		r.Get("/admin/config", s.handleGetConfig)
		r.Patch("/admin/config", s.handleUpdateConfig)
		s.mountCatalog(r)
		s.mountStream(r)
	})
	return r
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not configured"})
		return
	}
	cfg, err := s.deps.Store.GetAppConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	cfg.APIKey = ""
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not configured"})
		return
	}
	cur, err := s.deps.Store.GetAppConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	var next runtime.Config
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if next.APIKey == "" {
		next.APIKey = cur.APIKey
	}
	if err := s.deps.Store.UpdateAppConfig(r.Context(), next); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	next.DatabaseURL = s.deps.Config.DatabaseURL
	s.deps.Config = next
	if cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client); ok && cli != nil {
		cli.Reconfigure(next.BaseURL, next.APIKey)
	}
	// Rebuild the resolver + covers service so library_root and
	// path_remappings changes apply without re-uploading the binary. Wired
	// in by main.go; nil in early-dev or tests, in which case the next
	// host Configure RPC picks up the change.
	if s.deps.Refresh != nil {
		if err := s.deps.Refresh(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "refresh failed: " + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
<div id="status-strip" class="status-strip" aria-label="Plugin health"><div class="strip-cell"><span class="strip-dot"></span><strong>Loading…</strong></div></div>
<nav class="tabs" aria-label="BookWarehouse Audio admin sections">
<button class="tab active" data-tab-target="readiness" type="button">Readiness</button>
<button class="tab" data-tab-target="config" type="button">Config</button>
<button class="tab" data-tab-target="browser" type="button">Browser</button>
<button class="tab" data-tab-target="stream-test" type="button">Stream test</button>
<button class="tab" data-tab-target="diagnostics" type="button">Diagnostics</button>
</nav>
<section class="tab-panel active" id="readiness">
<article class="panel"><div class="panel-head"><div><h2>Setup status</h2><p class="muted">This plugin owns BookWarehouse connection settings and proxies catalog, cover, and stream operations.</p></div><span id="ready-badge" class="badge">Loading</span></div><div id="status" class="cards muted">Loading diagnostics...</div></article>
</section>
<section class="tab-panel" id="config">
<article class="panel"><div class="panel-head"><div><h2>Plugin config</h2><p class="muted">BookWarehouse connection plus the local filesystem mount where the plugin can read audiobook files and covers.</p></div><span id="config-save-state" class="badge">Loading</span></div><form id="config-form" class="config-grid"><label>Base URL<input id="cfg-base-url" name="base_url" placeholder="https://bookwarehouse.domain.com"></label><label>API key<input id="cfg-api-key" name="api_key" type="password" placeholder="Leave blank to keep current key"></label><label>Default cover size<select id="cfg-cover-size" name="default_cover_size"><option value="medium">Medium</option><option value="original">Original</option><option value="thumbnail">Thumbnail</option></select></label><label>Library root<input id="cfg-library-root" name="library_root" placeholder="/srv/audiobooks"></label><label>Cover cache dir<input id="cfg-cover-cache-dir" name="cover_cache_dir" placeholder="/var/cache/continuum-bw-audio-covers"></label><div class="span-all remap-block"><div class="remap-head"><strong>Path remappings (advanced)</strong><span id="remap-help" class="muted">Only needed when BookWarehouse returns absolute paths that differ from this plugin's library root.</span></div><div id="remap-list" class="remap-list"></div><button id="add-remap" type="button" class="secondary">Add remapping</button></div><button type="submit">Save config</button></form></article>
</section>
<section class="tab-panel" id="browser">
<article class="panel"><div class="panel-head"><div><h2>Backend browser</h2><p class="muted">Fetch libraries or search upstream titles without leaving the plugin admin page.</p></div><span id="browser-state" class="badge">Idle</span></div><form id="search-form" class="row"><input id="q" placeholder="Title, author, narrator" aria-label="Search query"><button type="submit">Search</button><button id="test" type="button" class="secondary">Fetch libraries</button></form><div class="browser-shell"><div id="browser-summary" class="info-grid"><div class="diag"><strong>Ready</strong><span>Run a library fetch or search to inspect upstream responses in a readable layout.</span></div></div><div id="browser-results" class="result-grid"><div class="empty-state">No browser test run yet.</div></div></div></article>
</section>
<section class="tab-panel" id="stream-test">
<article class="panel"><div class="panel-head"><div><h2>Stream diagnostics</h2><p class="muted">Audio bytes are served from the configured library root via http.ServeContent with HTTP Range support. Resolution failures return 502 with a structured diagnostic body so misconfigured remaps surface immediately instead of silently dead-ending.</p></div></div><form id="stream-form" class="row"><input id="book-id" placeholder="Book ID" aria-label="Book ID"><input id="file-idx" value="0" placeholder="File index" aria-label="File index"><button type="submit">Build stream URL</button></form><div class="info-grid"><div class="diag"><strong>Resolved route</strong><span><a id="stream-route" class="inline-link" href="#">Enter a book id to build the route.</a></span></div><div class="diag"><strong>Expected behavior</strong><span id="stream-behavior">200 (or 206) with X-Stream-Source: local-fs when the storage_key resolves; 502 with a JSON body when no library_root or path_remappings match.</span></div></div><div class="triage-grid"><div><h3>Direct file access</h3><p>The plugin always reads from the library root mount; there is no upstream-redirect fallback (BookWarehouse's /stream is a stub).</p></div><div><h3>Range support</h3><p>Direct files include <code>Accept-Ranges: bytes</code> and answer ranged requests with partial content via <code>http.ServeContent</code>.</p></div><div><h3>Path remapping</h3><p>Relative storage keys join to library_root. Absolute paths use path_remappings when the upstream's storage root differs from the mount.</p></div></div></article>
</section>
<section class="tab-panel" id="diagnostics">
<article class="panel"><div class="panel-head"><div><h2>Diagnostics</h2><p class="muted">Configuration, route, and upstream status rendered as operator-facing checks instead of a raw payload dump.</p></div></div><div id="diagnostics-grid" class="cards muted">Loading diagnostics...</div></article>
</section>
<section class="panel"><h2>Operations checklist</h2><ul><li>Configure BookWarehouse <code>base_url</code> and <code>api_key</code>.</li><li>Add this backend as a presentation library in the Audiobooks portal.</li><li>Run a library fetch test before troubleshooting the portal.</li><li>Validate stream path remapping if direct file access is enabled.</li></ul></section>
</main>
<script>
const statusEl=document.getElementById("status"), browserState=document.getElementById("browser-state"), browserSummary=document.getElementById("browser-summary"), browserResults=document.getElementById("browser-results"), diagnosticsGrid=document.getElementById("diagnostics-grid"), streamRoute=document.getElementById("stream-route"), streamBehavior=document.getElementById("stream-behavior"), configState=document.getElementById("config-save-state"), libraryRootInput=document.getElementById("cfg-library-root"), coverCacheDirInput=document.getElementById("cfg-cover-cache-dir"), remapList=document.getElementById("remap-list"), addRemapButton=document.getElementById("add-remap"), remapHelp=document.getElementById("remap-help");
const hostToken=new URLSearchParams(location.search).get("token")||"";
function headers(){return hostToken?{Authorization:"Bearer "+hostToken}:{}}
function badge(ok){return '<span class="badge '+(ok?'ok':'bad')+'">'+(ok?'OK':'Needs attention')+'</span>'}
function esc(v){return String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
function activateTab(id){document.querySelectorAll(".tab").forEach(b=>b.classList.toggle("active",b.dataset.tabTarget===id));document.querySelectorAll(".tab-panel").forEach(p=>p.classList.toggle("active",p.id===id))}
document.querySelectorAll(".tab").forEach(b=>b.addEventListener("click",()=>activateTab(b.dataset.tabTarget)))
async function readMaybeJSON(r){const text=await r.text();const type=r.headers.get("content-type")||"";if(type.includes("application/json")){try{return text?JSON.parse(text):{}}catch{}}return {error:text||r.statusText,raw:text}}
function normalizeCoverSize(v){switch(String(v||"").trim().toLowerCase()){case"thumbnail":case"small":return"thumbnail";case"original":case"large":return"original";case"medium":return"medium";default:return"medium"}}
function formatDuration(total){const seconds=Number(total||0);if(!seconds)return"Unknown duration";const hours=Math.floor(seconds/3600);const minutes=Math.floor((seconds%3600)/60);if(hours)return hours+'h '+minutes+'m';if(minutes)return minutes+'m';return seconds+'s'}
function formatList(list){return Array.isArray(list)&&list.length?list.join(', '):'Not provided'}
function coverHTML(src,label,kind){if(src){return '<img src="'+esc(src)+'" alt="'+esc(label)+'">'}return '<div class="cover-fallback">'+esc(kind)+'</div>'}
function renderInfoGrid(items){return items.map(item=>'<div class="diag"><strong>'+esc(item.label)+'</strong><span>'+esc(item.value)+'</span></div>').join('')}
function renderLibraries(data){const items=Array.isArray(data.items)?data.items:[];browserState.textContent=items.length?items.length+' libraries':'No libraries';browserSummary.innerHTML=renderInfoGrid([{label:'Library roots',value:items.length||0},{label:'Media type',value:items.map(item=>item.media_type||'audiobook').filter(Boolean).join(', ')||'audiobook'},{label:'Source',value:'BookWarehouse upstream'}]);browserResults.innerHTML=items.length?items.map(item=>'<article class="result-card compact"><div class="cover-fallback small">LIB</div><div class="result-body"><strong>'+esc(item.name||'Unnamed library')+'</strong><span class="muted">Library ID '+esc(item.id)+'</span><div class="chip-row"><span class="chip">'+esc(item.media_type||'audiobook')+'</span></div></div></article>').join(''):'<div class="empty-state">No libraries returned by the upstream.</div>'}
function renderSearchResults(data,query){const items=Array.isArray(data.items)?data.items:[];browserState.textContent=items.length?items.length+' results':'No matches';browserSummary.innerHTML=renderInfoGrid([{label:'Query',value:query||'Recent catalog slice'},{label:'Results shown',value:items.length},{label:'Reported total',value:data.total||items.length||0},{label:'Next cursor',value:data.next_cursor||'None'}]);browserResults.innerHTML=items.length?items.map(item=>'<article class="result-card">'+coverHTML(item.cover_url,item.title||'Cover','AUDIO')+'<div class="result-body"><strong>'+esc(item.title||'Untitled')+'</strong><span class="muted">'+esc(formatList(item.authors))+'</span><div class="meta-line">'+esc(formatList(item.narrators))+'</div><div class="chip-row"><span class="chip">'+esc(formatDuration(item.duration_seconds))+'</span>'+(item.year?'<span class="chip">'+esc(item.year)+'</span>':'')+(item.rating?'<span class="chip">Rating '+esc(item.rating)+'</span>':'')+'</div></div></article>').join(''):'<div class="empty-state">No audiobooks matched this search.</div>'}
function renderDiagnostics(d){diagnosticsGrid.innerHTML=renderInfoGrid([{label:'Plugin id',value:d.plugin_id||'continuum.bookwarehouse-audio'},{label:'Role',value:d.role||'audiobook_library_source'},{label:'Database',value:d.database?.message||'Unknown'},{label:'Upstream',value:d.upstream?.message||'Unknown'},{label:'Catalog routes',value:d.catalog_routes?'Mounted':'Unavailable'},{label:'Stream routes',value:d.stream_routes?'Mounted':'Unavailable'},{label:'Configured',value:d.configured?'Plugin DB config loaded':'Waiting for operator config'}])}
function renderStrip(cells){const strip=document.getElementById("status-strip");if(!strip)return;strip.replaceChildren();for(const c of cells){const cell=document.createElement("div");cell.className="strip-cell "+(c.ok?"ok":"bad");if(c.detail)cell.title=c.detail;const dot=document.createElement("span");dot.className="strip-dot";const label=document.createElement("strong");label.textContent=c.label;const note=document.createElement("span");note.textContent=c.ok?"OK":"Check";cell.append(dot,label,note);strip.appendChild(cell)}}
function createRemapRow(source,target){const row=document.createElement("div");row.className="remap-row";const sourceInput=document.createElement("input");sourceInput.className="remap-source";sourceInput.placeholder="/media/books";sourceInput.value=source||"";const arrow=document.createElement("span");arrow.className="remap-arrow";arrow.textContent="→";const targetInput=document.createElement("input");targetInput.className="remap-target";targetInput.placeholder="/mnt/books";targetInput.value=target||"";const removeButton=document.createElement("button");removeButton.type="button";removeButton.className="secondary remap-remove";removeButton.textContent="Remove";removeButton.addEventListener("click",()=>{row.remove();if(!remapList.children.length)renderRemappings([])});row.append(sourceInput,arrow,targetInput,removeButton);return row}
function renderRemappings(remaps){remapList.replaceChildren();if(!remaps.length){const empty=document.createElement("div");empty.className="remap-empty";empty.textContent="No remappings added. Most installs only need a library root.";remapList.appendChild(empty);return}for(const remap of remaps){remapList.appendChild(createRemapRow(remap.source_path||"",remap.target_path||""))}}
function addRemapRow(source,target){const empty=remapList.querySelector(".remap-empty");if(empty)empty.remove();remapList.appendChild(createRemapRow(source,target))}
function collectRemappings(){const remaps=[];for(const row of remapList.querySelectorAll(".remap-row")){const source=row.querySelector(".remap-source").value.trim();const target=row.querySelector(".remap-target").value.trim();if(!source&&!target)continue;if(!source||!target)throw new Error("Each path remapping needs both a source path and a target path.");remaps.push({source_path:source,target_path:target})}return remaps}
async function loadConfig(){try{const r=await fetch("./api/v1/admin/config",{headers:headers()});const d=await readMaybeJSON(r);if(!r.ok)throw new Error(d.error||r.statusText);document.getElementById("cfg-base-url").value=d.base_url||"https://bookwarehouse.domain.com";document.getElementById("cfg-cover-size").value=normalizeCoverSize(d.default_cover_size);libraryRootInput.value=d.library_root||"";coverCacheDirInput.value=d.cover_cache_dir||"";renderRemappings(d.path_remappings||[]);configState.textContent="Loaded"}catch(e){configState.textContent="Unavailable"}}
async function load(){try{const r=await fetch("./api/v1/admin/diagnostics",{headers:headers()});const d=await r.json();document.getElementById("ready-badge").textContent=d.configured&&d.upstream?.ok?"Ready":"Needs attention";statusEl.innerHTML='<div class="diag">'+badge(d.database?.ok)+'<strong>Database</strong><span>'+esc(d.database?.message)+'</span></div><div class="diag">'+badge(d.configured)+'<strong>Configured</strong><span>base_url and api_key are applied</span></div><div class="diag">'+badge(d.upstream?.ok)+'<strong>BookWarehouse</strong><span>'+esc(d.upstream?.message)+'</span></div><div class="diag">'+badge(d.catalog_routes)+'<strong>Catalog routes</strong><span>libraries, list, search, detail, browse</span></div><div class="diag">'+badge(d.stream_routes)+'<strong>Stream routes</strong><span>direct local file or redirect fallback</span></div>';renderDiagnostics(d);renderStrip([{label:'DB',ok:!!d.database?.ok,detail:d.database?.message},{label:'Configured',ok:!!d.configured,detail:'base_url + api_key applied'},{label:'BookWarehouse',ok:!!d.upstream?.ok,detail:d.upstream?.message},{label:'Catalog',ok:!!d.catalog_routes,detail:'catalog routes mounted'},{label:'Stream',ok:!!d.stream_routes,detail:'stream routes mounted'}])}catch(e){statusEl.textContent=String(e);diagnosticsGrid.innerHTML='<div class="empty-state">'+esc(String(e))+'</div>';renderStrip([{label:'Diagnostics',ok:false,detail:String(e)}])}}
addRemapButton.addEventListener("click",()=>addRemapRow("",""))
document.getElementById("config-form").addEventListener("submit",async e=>{e.preventDefault();configState.textContent="Saving";try{const remaps=collectRemappings();const body={base_url:document.getElementById("cfg-base-url").value.trim(),api_key:document.getElementById("cfg-api-key").value,default_cover_size:document.getElementById("cfg-cover-size").value||"medium",library_root:libraryRootInput.value.trim(),cover_cache_dir:coverCacheDirInput.value.trim(),path_remappings:remaps};const r=await fetch("./api/v1/admin/config",{method:"PATCH",headers:{...headers(),"Content-Type":"application/json"},body:JSON.stringify(body)});const d=await readMaybeJSON(r);if(!r.ok)throw new Error(d.error||r.statusText);document.getElementById("cfg-api-key").value="";configState.textContent="Saved";await loadConfig()}catch(err){configState.textContent="Error"}})
document.getElementById("test").addEventListener("click",async()=>{browserState.textContent="Loading";browserResults.innerHTML='<div class="empty-state">Loading libraries...</div>';try{const r=await fetch("./api/v1/catalog/libraries",{headers:headers()});const d=await readMaybeJSON(r);if(!r.ok)throw new Error(d.error||r.statusText);renderLibraries(d)}catch(err){browserState.textContent="Error";browserSummary.innerHTML='';browserResults.innerHTML='<div class="empty-state">'+esc(String(err))+'</div>'}})
document.getElementById("search-form").addEventListener("submit",async e=>{e.preventDefault();const rawQuery=document.getElementById("q").value||"";browserState.textContent="Searching";browserResults.innerHTML='<div class="empty-state">Searching upstream titles...</div>';try{const q=encodeURIComponent(rawQuery);const path=q?"./api/v1/catalog/search?q="+q+"&limit=10":"./api/v1/catalog?limit=10";const r=await fetch(path,{headers:headers()});const d=await readMaybeJSON(r);if(!r.ok)throw new Error(d.error||r.statusText);renderSearchResults(d,rawQuery)}catch(err){browserState.textContent="Error";browserSummary.innerHTML='';browserResults.innerHTML='<div class="empty-state">'+esc(String(err))+'</div>'}})
document.getElementById("stream-form").addEventListener("submit",e=>{e.preventDefault();const id=document.getElementById("book-id").value.trim();const idx=document.getElementById("file-idx").value.trim()||"0";if(!id){streamRoute.removeAttribute("href");streamRoute.textContent="Book ID required.";streamBehavior.textContent="Enter a BookWarehouse audiobook id and an optional file index.";return}const url=new URL("./api/v1/stream/"+encodeURIComponent(id)+"/"+encodeURIComponent(idx),location.href).toString();streamRoute.href=url;streamRoute.textContent=url;streamBehavior.textContent="200/206 with X-Stream-Source: local-fs when the storage_key resolves; 502 with a JSON diagnostic when no library_root or path_remapping matches."})
load();loadConfig();
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
	return `:root{--bg:#141417;--fg:#e8e8ec;--muted:#a1a1aa;--link:#93c5fd;--panel:#1c1c20;--border:#28282e;--ok:#22c55e;--bad:#fb7185;--input:#101014}[data-theme="cinema-light"]{--bg:#f7f3ed;--fg:#201c18;--muted:#756b60;--link:#9a3412;--panel:#fffaf3;--border:#ded1c0;--input:#fff}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#afc2e2;--link:#60a5fa;--panel:#172033;--border:#2d3f61;--input:#0d1422}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#f0a6b7;--link:#fb7185;--panel:#241018;--border:#4a2230;--input:#12070b}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bd6b4;--link:#6ee7b7;--panel:#14241b;--border:#2b4b39;--input:#08110d}*{box-sizing:border-box}body{font-family:system-ui,sans-serif;margin:0;line-height:1.5;background:var(--bg);color:var(--fg)}.shell{max-width:1120px;margin:0 auto;padding:28px}.back{display:inline-flex;margin-bottom:12px;color:var(--link);text-decoration:none}.eyebrow{color:var(--muted);text-transform:uppercase;font-size:12px;letter-spacing:.08em}h1{margin:.2rem 0}h2{font-size:16px;margin:0}.tabs{display:flex;gap:8px;flex-wrap:wrap;margin:18px 0}.tab{background:transparent;color:var(--fg);border:1px solid var(--border)}.tab.active{background:var(--link);color:#08111f}.tab-panel{display:none}.tab-panel.active{display:block}.grid,.triage-grid,.cards,.info-grid,.result-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px}.panel{border:1px solid var(--border);background:var(--panel);border-radius:8px;padding:16px;margin-top:16px}.panel-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.triage-grid{margin-top:14px}.triage-grid h3{font-size:14px;margin:.2rem 0}.triage-grid p{color:var(--muted);margin:.25rem 0}.row{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:8px}.config-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:12px}.config-grid label{display:grid;gap:6px;color:var(--muted);font-size:13px}.config-grid .span-all{grid-column:1/-1}.check{display:flex!important;align-items:center;gap:8px}.remap-block,.remap-head,.remap-list,.browser-shell{display:grid;gap:10px}.remap-head strong{font-size:13px}.remap-row{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr) auto;gap:8px;align-items:center}.remap-arrow{color:var(--muted);font-size:16px}.remap-empty,.empty-state{border:1px dashed var(--border);border-radius:6px;padding:12px;background:var(--input);color:var(--muted);font-size:13px}.remap-list.disabled{opacity:.72}.secondary{background:transparent;border:1px solid var(--border);color:var(--fg);font-weight:600}.secondary:disabled{opacity:.5;cursor:not-allowed}select,input{min-width:0;background:var(--input);color:var(--fg);border:1px solid var(--border);border-radius:6px;padding:9px}input[type="checkbox"]{width:auto}button{background:var(--link);border:0;border-radius:6px;padding:9px 12px;color:#08111f;font-weight:700;cursor:pointer}.badge{display:inline-block;border:1px solid var(--border);border-radius:999px;padding:2px 8px;margin-right:6px;font-size:12px;white-space:nowrap}.ok{color:var(--ok)}.bad{color:var(--bad)}.muted{color:var(--muted)}.diag{display:grid;gap:4px;border:1px solid var(--border);border-radius:6px;background:var(--input);padding:12px}.diag strong{color:var(--fg)}.diag span{color:var(--muted);font-size:12px}.result-card{display:grid;grid-template-columns:72px minmax(0,1fr);gap:12px;border:1px solid var(--border);border-radius:8px;background:var(--input);padding:12px}.result-card.compact{grid-template-columns:56px minmax(0,1fr)}.result-card img,.cover-fallback{width:72px;height:96px;border-radius:6px;object-fit:cover;background:#0b0f17}.result-card.compact .cover-fallback{width:56px;height:56px}.cover-fallback{display:grid;place-items:center;color:var(--muted);font-size:12px;font-weight:700;letter-spacing:.06em}.cover-fallback.small{height:56px}.result-body{display:grid;gap:6px;min-width:0}.result-body strong{font-size:14px}.meta-line{color:var(--muted);font-size:12px}.chip-row{display:flex;flex-wrap:wrap;gap:6px}.chip{border:1px solid var(--border);border-radius:999px;padding:3px 8px;font-size:12px;color:var(--muted)}.inline-link{color:var(--link);text-decoration:none;word-break:break-all}code{color:var(--link)}.status-strip{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:8px;margin-top:14px}.strip-cell{display:flex;align-items:center;gap:8px;border:1px solid var(--border);background:var(--panel);border-radius:6px;padding:8px 10px;font-size:12px}.strip-dot{flex:0 0 8px;width:8px;height:8px;border-radius:999px;background:var(--muted)}.strip-cell.ok .strip-dot{background:var(--ok)}.strip-cell.bad .strip-dot{background:var(--bad)}.strip-cell strong{font-weight:600;color:var(--fg)}.strip-cell span{color:var(--muted);margin-left:auto;font-size:11px}@media(max-width:760px){.row,.panel-head,.config-grid,.remap-row,.result-card{grid-template-columns:1fr;display:grid}.remap-arrow{display:none}.result-card img,.cover-fallback{width:100%;max-width:120px}}`
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	dbOK := false
	dbMessage := "not configured"
	if s.deps.Store != nil {
		if err := s.deps.Store.Pool().Ping(ctx); err != nil {
			dbMessage = err.Error()
		} else {
			dbOK = true
			dbMessage = "database reachable"
		}
	}
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
		"database":       map[string]any{"ok": dbOK, "message": dbMessage},
		"stream_routes":  ok && cli != nil,
		"upstream":       map[string]any{"ok": upstreamOK, "message": upstreamMessage},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
