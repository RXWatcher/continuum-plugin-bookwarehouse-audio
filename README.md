# BookWarehouse Audio for Continuum

`continuum.bookwarehouse-audio` is an audiobook backend plugin that fronts an external BookWarehouse instance for the Continuum Audiobooks portal. It proxies BookWarehouse's catalog, browse, search, and detail data, serves cover artwork (with on-disk caching and embedded-tag extraction), and streams audio bytes directly from local mounts with HTTP Range support gated by HS256-signed media tokens minted by the portal.

## Category

Lives under **Books / Audiobooks** in the admin sidebar.

## Capabilities

| Type | ID | Purpose |
| --- | --- | --- |
| `http_routes.v1` | `backend` | Backend HTTP surface for catalog, covers, streaming, and the admin page. |
| `audiobook_backend.v1` | `default` | Presents this BookWarehouse instance to `continuum.audiobooks` as a `library_source` (catalog yes, requests no, auto-monitoring no). |

## Dependencies

- [`continuum-plugin-audiobooks`](https://github.com/RXWatcher/continuum-plugin-audiobooks) — the user-facing portal that mounts this plugin as an audiobook backend. The portal owns the UI and the Audiobookshelf-compatible surface; this plugin has no SPA of its own.
- An external **BookWarehouse** instance reachable over HTTP(S).
- A PostgreSQL database (DSN supplied via the `database_url` global config) for the small amount of state the plugin owns (app config snapshots).
- Optional sibling: [`continuum-plugin-audiobook-requests`](https://github.com/RXWatcher/continuum-plugin-audiobook-requests) when request fulfillment is needed — this backend is catalog/stream only.

Host app: [`ContinuumApp/continuum`](https://github.com/ContinuumApp/continuum). SDK: [`ContinuumApp/continuum-plugin-sdk`](https://github.com/ContinuumApp/continuum-plugin-sdk).

## External services

- **BookWarehouse REST API** — called via a typed client (`internal/bookwarehouse`) using `X-API-Key` authentication. Endpoints consumed: `/api/v1/audiobooks`, `/api/v1/audiobooks/search`, `/api/v1/audiobooks/{id}`, `/api/v1/audiobooks/authors|series|narrators`, and `/api/v1/audiobooks/{id}/cover`.
- **Local filesystem mounts** — the audio files BookWarehouse manages must also be reachable from inside this plugin's runtime, either under a single `library_root` or via per-prefix `path_remappings`. Streaming and cover extraction are file-system based; there is no upstream-redirect fallback for `/stream` because BookWarehouse's upstream stream endpoint does not support Range.

## Configuration

Set via the per-installation app config (admin page) on top of the single global key `database_url`.

| Key | Required | Purpose |
| --- | --- | --- |
| `database_url` | yes | PostgreSQL DSN for the plugin's own schema. |
| `base_url` | yes | BookWarehouse origin URL (no path/query/fragment; `http://` only for localhost). |
| `api_key` | yes | Sent to BookWarehouse as `X-API-Key`. |
| `default_cover_size` | no | `thumb`, `medium`, or `original`. |
| `library_root` | no | Absolute path where BookWarehouse audiobook files are mounted inside this plugin's runtime. Storage keys are joined to this root. |
| `cover_cache_dir` | no | Absolute path for cached cover variants. Defaults to a plugin-managed temp directory. |
| `stream_signing_secret` | yes (for playback) | HMAC key shared with the audiobooks portal for verifying signed media URLs. Base64 preferred; raw bytes accepted as fallback. Must match the portal's `media_signing_secret`. |
| `path_remappings` | no | List of `{source_path, target_path}` rewrites (both absolute) used when BookWarehouse returns absolute storage paths that live at a different absolute path inside the plugin runtime. |

## Signed streaming

The stream and cover byte routes are declared `public` in the manifest because browsers cannot attach `Authorization` headers to `<audio>` or `<img>` tag requests. The host plugin proxy therefore cannot authenticate these requests for us — this plugin verifies a short-TTL HS256 JWT supplied as `?token=...`.

Verification (`internal/tokens`) requires:

- HS256 signature against `stream_signing_secret` (base64-decoded if possible, else used as raw bytes).
- `aud` equal to `audiobook_backend`.
- `exp` claim present and not exceeded.
- `book_id` claim equal to the path's book id.
- `file_idx` claim equal to the path's file index (or the sentinel `-1` for cover tokens).
- Non-empty `sub` (the requesting user).

Verification failure short-circuits before any upstream call or filesystem access, so leaked tokens cannot be replayed for other books, files, or users.

## Path resolution

`internal/localfs` translates BookWarehouse storage keys to on-disk paths in this order:

1. Match a `path_remappings` entry as a prefix on absolute keys and rewrite `source_path` → `target_path`.
2. Otherwise treat the key as relative and join it to `library_root`.
3. Resolve symlinks and confirm the real path stays inside the configured root (or remap target), rejecting traversal.

## Cover caching

`internal/covers` serves covers from the local filesystem, preferring sidecar files (`cover.{jpg,jpeg,png}`, `folder.{jpg,jpeg,png}`) and falling back to embedded artwork extracted from the audio tags. Decoded images are resized (250 px thumb, 500 px medium) and cached on disk under `cover_cache_dir`, keyed by source `mtime+size`, with single-flight per cache key to avoid duplicate work under concurrent requests.

## Detailed docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Build and release

```bash
make build
make test
```

CI builds linux-amd64 binaries on push to main via the reusable workflow in [RXWatcher/continuum-plugin-repository](https://github.com/RXWatcher/continuum-plugin-repository) and publishes them to the catalog at [`./binaries/`](https://github.com/RXWatcher/continuum-plugin-repository/tree/main/binaries).
