# Operator Runbook

Operator-focused setup, flows, and verification steps for
`silo.bookwarehouse-audio`. The README covers the *what*; this document
covers *how to bring it up and confirm it works*. Runtime debugging,
authentication failures, and remap pitfalls live in
[`debugging.md`](./debugging.md).

## Where this plugin sits

Two plugins cooperate to give end-users an audiobook portal:

```
browser ── silo.audiobooks (portal UI + Audiobookshelf API)
                    │
                    │ HTTP (plugin-to-plugin via host proxy)
                    │ Signed token minted with media_signing_secret
                    ▼
            silo.bookwarehouse-audio  ◀── this plugin
                    │
                    ├── HTTP  ─► external BookWarehouse  (X-API-Key)
                    └── disk  ─► local mount with audiobook files
```

The portal owns the user-facing UX. This plugin owns:

- the BookWarehouse REST client,
- the local filesystem byte path for `/stream` and `/cover`,
- the on-disk cover cache,
- HS256 stream-token verification.

## First-install checklist

1. Set the `database_url` global config. The plugin auto-creates the
   `app_config` row on first read.
2. Open the plugin admin page (`/admin` under the plugin route). Set:
   - **Base URL** — BookWarehouse origin, no trailing path. `http://` is
     rejected unless the host is `localhost` or a loopback IP.
   - **API key** — sent as `X-API-Key`. Empty input on Save keeps the
     previously stored key (the API key field is intentionally write-only).
   - **Default cover size** — `thumb` | `medium` | `original`. Older labels
     `thumbnail`, `small`, `large` are normalised by the catalog handler.
   - **Library root** — absolute path inside *this plugin's* runtime where
     BookWarehouse-managed audio files appear. Must be absolute.
   - **Cover cache dir** — absolute path; if empty the plugin uses
     `os.TempDir()/silo-bw-audio-covers`. Operator-managed paths are
     preferred for persistence across container restarts.
   - **Path remappings** — only needed if BookWarehouse returns absolute
     storage paths that differ from this plugin's view of the same files.
     Both `source_path` and `target_path` must be absolute.
3. In the audiobooks portal:
   - Register this backend as a `library_source`.
   - Set the portal's `media_signing_secret` equal to this plugin's
     `stream_signing_secret`. Base64 is preferred; raw bytes are also
     accepted by the verifier as a fallback.
4. Use the admin page tabs (Readiness, Browser, Stream test, Diagnostics) to
   verify connectivity before exercising the portal.

## Capabilities snapshot

| Capability | Role | Notes |
| --- | --- | --- |
| `http_routes.v1` `backend` | catalog + cover + stream + admin | All routes mounted under `/api/v1/*` plus `/admin`. |
| `audiobook_backend.v1` `default` | `library_source` | `supports_catalog: true`, `supports_requests: false`, `supports_auto_monitoring: false`. |

## Route map (operator view)

| Route | Access | Behaviour |
| --- | --- | --- |
| `GET /api/v1/health` | authenticated | Always-on liveness JSON. |
| `GET /api/v1/admin/diagnostics` | authenticated | DB ping + a real upstream call to `GET /api/v1/audiobooks?limit=1`. Powers the admin Readiness tab. |
| `GET /api/v1/admin/config` | authenticated | Returns the persisted config with the API key redacted. |
| `PATCH /api/v1/admin/config` | authenticated | Writes config and triggers an in-process refresh of the BW client + path resolver + covers service. No restart needed. |
| `GET /api/v1/catalog{,/libraries,/search,/{id}}` | authenticated | Proxies to BookWarehouse `audiobooks` endpoints. |
| `GET /api/v1/browse/{authors,series,narrators}` | authenticated | Browse facet endpoints. |
| `GET /api/v1/cover/{book_id}/{size}` | **public** | Token-gated. Reads from local FS (sidecar → embedded tag). |
| `GET /api/v1/stream/{book_id}/{file_idx}` | **public** | Token-gated. `http.ServeContent` with Range. |
| `GET /admin`, `/admin/*` | admin | Server-rendered single-file admin UI. |

`public` routes are still verified inside the handler — see
[`debugging.md`](./debugging.md) for token gotchas.

## Operational flows

### Catalog browse

Portal → plugin `/api/v1/catalog?cursor=…&limit=…` → plugin client builds
`GET {base_url}/api/v1/audiobooks?page=<cursor>&limit=<n>` with `X-API-Key`
→ plugin transforms the response into the portal envelope (`items`,
`next_cursor`, `total`). Cursor is BookWarehouse's page number; pagination
terminates when a partial page is returned.

### Cover request

Browser `<img src=".../cover/{id}/{size}?token=…">` → plugin verifies the
HS256 token (`file_idx=-1` sentinel) → fetches book detail via the BW client
→ resolves the first file's `storage_key` to a local path via
`internal/localfs` → looks for `cover.{jpg,jpeg,png}` or
`folder.{jpg,jpeg,png}` in that directory → falls back to embedded artwork
extracted with `github.com/dhowden/tag` → resizes (thumb=250px,
medium=500px, original=unchanged) → caches under `cover_cache_dir`, keyed by
`sha256(book_id || source_path || size || mtime || file size || kind)`.

### Stream request

Browser `<audio src=".../stream/{id}/{idx}?token=…">` → token verify (must
match `book_id` and `file_idx` exactly; `aud=audiobook_backend`; `sub`
non-empty; `exp` present and not exceeded) → fetch book detail → look up
the file by `index`, falling back to positional indexing only when *every*
file has `index==0` in the upstream response → resolve to local path → serve
via `http.ServeContent`. Range requests are answered with `206 Partial
Content`. Response carries `X-Stream-Source: local-fs`.

### Config save

Admin saves config → `PATCH /api/v1/admin/config` writes to `app_config`,
validates `base_url`, normalises empty defaults → calls
`bookwarehouse.Client.Reconfigure(base_url, api_key)` in place → runs the
`Refresh` callback (from `cmd/.../main.go`) which rebuilds the path resolver
and covers service so the new `library_root`, `cover_cache_dir`, and
`path_remappings` take effect without a plugin restart.

## Verifying a change

1. Save in the admin page; expect a green Readiness strip.
2. Diagnostics tab → confirm `database.ok` and `upstream.ok`. Upstream check
   makes a real `GET /api/v1/audiobooks?limit=1` call with a 5s deadline.
3. Browser tab → "Fetch libraries" returns one library
   (`Book Warehouse Audiobooks`, id 1). "Search" exercises the catalog
   transform.
4. Stream test tab → enter a known `book_id` + `file_idx=0` and follow the
   built link. The portal must be the one issuing the token; this admin
   builder does **not** sign one, so a direct click will return 401 with
   `media token missing` — that's expected. Use it to confirm the route is
   present and that the diagnostic body shape is correct.
5. In the audiobooks portal: open a book, hit play, scrub. Confirm Range
   support (the network panel should show 206s with `X-Stream-Source:
   local-fs`).

## Database

- Single `app_config` row (`id=1`, `data jsonb`). Singleton enforced by a
  CHECK constraint.
- Migrations live in `internal/migrate/files/`. The runner is invoked at
  bootstrap; failures abort startup so misconfigured DSNs surface
  immediately.
- The plugin schema is whichever schema the `database_url` DSN selects; the
  host doesn't manage it. Use a dedicated schema or role; the plugin does
  not read core Silo tables.

## What this plugin does *not* do

- Audiobook requests / monitoring (delegate to a request-provider plugin).
- Multi-library presentation — exposes a single library
  (`Book Warehouse Audiobooks`, id 1) for portal mapping.
- Upstream-redirect fallback for `/stream`. BookWarehouse's stream endpoint
  does not support Range, so a redirect would break seeking. The plugin
  always serves bytes directly.
- Cover redirect to upstream. The byte path stays inside this plugin so
  browser-issued `<img>` requests don't follow URLs they cannot
  authenticate to.

## See also

- [Runtime debugging guide](./debugging.md)
- README at the repository root for capability/manifest summary
