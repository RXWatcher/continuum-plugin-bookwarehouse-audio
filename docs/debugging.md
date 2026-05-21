# Debugging Guide

Symptom-first runbook for `continuum.bookwarehouse-audio`. Pair with the
admin page Diagnostics tab — it pings the database and makes a real
upstream call. For setup and route maps, see
[`setup-debug-flows.md`](./setup-debug-flows.md).

## Quick triage

```
catalog fails  ─►  upstream connectivity, API key, base URL  (§1)
covers fail    ─►  resolver, sidecar/embedded fallback, cache (§4)
stream 401/403 ─►  token + signing secret mismatch           (§3)
stream 404     ─►  file_idx vs upstream's file.index         (§2, §5)
stream 502     ─►  path remap / library_root mismatch        (§5)
stream 500     ─►  file present in BW but not readable here  (§5, §6)
admin "Needs attention" ─►  /api/v1/admin/diagnostics output (§7)
```

## 1. BookWarehouse upstream connectivity

The typed client (`internal/bookwarehouse/client.go`) sends every request
with:

- `X-API-Key: <api_key>` header
- `Accept: application/json`
- 15-second timeout
- 10 MiB response cap (defence against runaway bodies)

Error messages bubble back as `upstream <status>: <truncated body up to
512 bytes>`. Common patterns:

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `upstream 401: ...` on every call | Missing/invalid API key | Re-enter `api_key`. The admin form preserves the existing key on empty input, so blank-Save is **not** how you rotate. Type the new key. |
| `upstream 403: ...` | API key valid but scope restricted | Confirm the key has read access to `/api/v1/audiobooks*` on BookWarehouse. |
| `upstream 404: ...` on a known book id | Book id from a *different* BookWarehouse instance, or deleted upstream | Verify with a direct curl: `curl -H "X-API-Key: $KEY" $BASE_URL/api/v1/audiobooks/<id>`. |
| `do: ... no such host` / `connection refused` | `base_url` wrong, or DNS/network from the plugin runtime is broken | Run the curl from *inside* the plugin container/host — operator-laptop checks lie. |
| Hangs ~15s then `context deadline exceeded` | Upstream slow or unreachable through a proxy | Check BookWarehouse logs; the client deadline is fixed. |
| `base_url: must use https except for localhost` on Save | Trying to use plain `http://` for a non-loopback host | Use `https://`. The validator allows `http://` only when the hostname is `localhost` or a loopback IP. |
| `base_url: must be an origin URL without credentials, query, or fragment` | Pasted full URL with path/query | Strip down to `scheme://host[:port]`. |

The Diagnostics tab's `upstream.ok=true` proves the plugin can reach BW and
authenticate — if that's green and catalog still fails, the problem is in
URL encoding or query shape, not auth.

The client snapshots `base_url`+`api_key` per request under an RWMutex, so a
config save reconfigures the live client in place. No plugin restart is
needed after Save; the BW client, path resolver, and cover service all
rebuild via the admin handler's `Refresh` callback.

## 2. Catalog/browse oddities

- **Pagination ends early or never** — BookWarehouse uses page numbers; the
  plugin translates the portal's cursor → `?page=`. `next_cursor` is empty
  when a returned page has fewer items than `limit`. If BW always returns
  full pages even at end-of-list, last page repeats. Lower `limit` to force
  a partial page, or rely on BW's own `next_cursor` when it provides one.
- **`limit` ignored** — the catalog handler caps it at 200. Anything larger
  silently clamps. `limit=0` or invalid falls back to BW's own default.
- **Narrator IDs differ in JSON shape** — BW returns numeric narrator IDs;
  the plugin stringifies them so portal URL params, React keys, and route
  segments stay string-typed. If you compare narrator IDs across sources,
  remember this conversion.
- **Sort/order not applied** — the catalog handler only forwards `sort` and
  `order` when set. BookWarehouse honours `added | title | duration |
  rating` × `asc | desc`. Anything else is silently passed through and
  ignored upstream.

## 3. HS256 stream-token verification

Both `/api/v1/stream/...` and `/api/v1/cover/...` are declared `public` in
the manifest because browsers cannot attach `Authorization` headers to
`<audio>` and `<img>` tag requests. The handler is the **only** auth gate.

A valid token (`internal/tokens/verify.go`) must satisfy:

| Claim | Required value |
| --- | --- |
| signing alg | `HS256` (anything else rejected) |
| signature | HMAC against `stream_signing_secret` |
| `aud` | exactly `audiobook_backend` |
| `exp` | present, not yet exceeded |
| `book_id` | equal to the path's book id (string) |
| `file_idx` | equal to the path's `file_idx` (or `-1` for covers) |
| `sub` | non-empty (the requesting user) |

`stream_signing_secret` is decoded by trying, in order: standard base64,
raw-std-base64, and finally the raw string bytes. The portal stores its
`media_signing_secret` base64-encoded; that's the preferred form.

### Failure mapping

| Response | Cause |
| --- | --- |
| `503 {"error":"media signing secret not configured"}` | `stream_signing_secret` is blank in this plugin's config. The portal never gets a chance to authenticate. Set the secret to match `media_signing_secret`. |
| `401 {"error":"media token missing"}` | No `?token=` query param. Either the portal isn't signing yet, or the URL was rewritten by a proxy that stripped the query. |
| `401 {"error":"verify: signature is invalid"}` | Secrets do not match between portal and this plugin. Treat base64-vs-raw carefully — the portal almost always sends base64, so paste the same string in both places. |
| `401 {"error":"verify: token has invalid claims: token is expired"}` | Clock skew between portal host and this plugin, or the portal is minting tokens with a too-short TTL. NTP both ends; check the portal's TTL setting. |
| `401 {"error":"book_id mismatch"}` | Portal signed a token for one book and the player requested another — usually a stale cached URL after a portal restart or library re-index. Hard refresh the portal. |
| `401 {"error":"file_idx mismatch"}` | Same as above but for the file index. Often happens if the upstream's file indices shifted (re-import). |
| `401 {"error":"sub required"}` | Portal token has no `sub` claim. Audiobooks portal version mismatch — upgrade the portal. |
| `401 {"error":"unexpected signing method: ..."}` | Portal signed with something other than HS256. Misconfiguration on the portal; only HS256 is accepted. |

When verification fails the handler short-circuits before any upstream call
or filesystem access — leaked tokens cannot be replayed for other resources
or users, but they also can't be used to enumerate book existence.

### Rotating the secret

1. Change `stream_signing_secret` here.
2. Change `media_signing_secret` on the portal to match.
3. Save in both places. No restart needed; the next portal request will
   mint with the new key and this plugin will verify with it. In-flight
   `<audio>` tags using already-issued tokens will 401 until reload.

## 4. Cover failures and clearing the cache

Resolution order in `internal/covers/covers.go`:

1. Fetch book detail from BW; need at least one `Files` entry.
2. Resolve `Files[0].StorageKey` to a local path. Fall back to
   `Files[0].Filename` if the storage key fails to resolve.
3. Look in that file's directory for `cover.{jpg,jpeg,png}` then
   `folder.{jpg,jpeg,png}` in that order.
4. If no sidecar exists, open the audio file, parse tags with `dhowden/tag`,
   extract the embedded picture.
5. Resize for `thumb` (250 px long edge) / `medium` (500 px long edge).
   `original` returns the source bytes unchanged.

### Common cover failures

| Response | Cause | Fix |
| --- | --- | --- |
| `503 {"error":"cover service not configured: set library_root"}` | `library_root` blank and no path remappings | Set `library_root` to the absolute path of the audiobooks mount inside this plugin. |
| `404 no cover available` | No sidecar, no embedded artwork. Or the book has zero files in BW. | Confirm the audio file has an embedded picture (`ffprobe -show_streams …` looks for `attached_pic`). Or drop a `cover.jpg` next to it. |
| `502 {"error":"local filesystem resolve failed", ...}` | Resolver couldn't translate the storage key. Body includes `source_key`, `reason`, and `attempts` so you can see what paths were tried. | Adjust `library_root` or add a path remap. See §5. |
| `cover dimensions exceed limit (NxM)` | Source image exceeds the 40 megapixel decompression-bomb guard | Replace the sidecar with a sane-sized image, or rely on embedded artwork. |
| `read tag: …` errors | The audio file's tag parser failed | The container/codec may not carry tags `dhowden/tag` understands. Add a sidecar `cover.jpg`. |

### Clearing the cache

The cache key is a SHA-256 of
`book_id ∥ source_path ∥ size ∥ source_mtime ∥ source_size ∥ kind`. Touching
the audio file (`touch <path>` updates mtime) invalidates *both* the
extracted-original entry and any resized variants derived from it on the
next request. To wipe completely:

```bash
# Default location when cover_cache_dir is empty:
rm -rf "$TMPDIR/continuum-bw-audio-covers"

# Or the operator-configured location:
rm -rf <cover_cache_dir>
```

The cover service creates the directory on next request. Single-flight via
an in-memory map prevents thundering herd when many clients ask for the same
cover after a wipe.

### Cache layout

- Files written as `<cover_cache_dir>/<aa>/<sha256>.bin` (2-character
  fanout).
- Writes are atomic via `.part` rename, so a crash mid-extract doesn't leave
  a corrupted entry.
- No TTL — entries are content-addressed; stale entries simply stop being
  read once their key changes. Periodic cleanup is the operator's
  responsibility if disk is constrained.

## 5. Path remapping and `library_root`

The resolver (`internal/localfs/resolve.go`):

1. **Absolute key** → walk `path_remappings`; first prefix match (with
   `source_path` as a path prefix on segment boundaries) rewrites
   `source_path` → `target_path`. Verify the resolved real path stays
   inside `target_path`. If no remap matches but `library_root` is set, try
   joining `library_root + key`.
2. **Relative key** → join to `library_root`. Reject if `library_root` is
   empty.
3. Symlinks are resolved with `EvalSymlinks`; the real path must remain
   under the configured root (traversal defence).

Errors return `ResolveError` with `source_key`, `reason`, and `attempts`
(every candidate path the resolver tried). The 502 JSON body surfaces all
three so an operator can grep the logs:

```json
{
  "error": "local filesystem resolve failed",
  "source_key": "/media/audiobooks/Author/Title/01.m4b",
  "reason": "no path_remapping matched the absolute storage key",
  "attempts": ["/mnt/books/Author/Title/01.m4b"]
}
```

### Diagnosing remap mismatches

- BookWarehouse returns absolute paths (`/media/...`) but this plugin sees
  `/mnt/books/...`? Add a remap `{source_path: "/media", target_path:
  "/mnt/books"}`.
- BookWarehouse returns *relative* keys (`Author/Title/01.m4b`)? Don't add a
  remap; set `library_root` to wherever those relative keys join to a real
  file.
- Resolved path escapes the root (symlink to outside the mount)? `attempts`
  will show the candidate but the response stays 502 because
  `resolveWithin` rejected it. Move the file or adjust roots.
- `library_root` and `cover_cache_dir` both must be **absolute paths** —
  validated on Configure, and rejected by the runtime with
  `library_root: must be an absolute path`.

### Operator-side verification

```bash
# Step into the plugin runtime (container, ssh, etc.), then:
ls -ld "$LIBRARY_ROOT"            # exists, readable by plugin user
stat "$LIBRARY_ROOT/<some-key>"   # for a known relative key
```

If the plugin runs as a non-root user (it should), every directory on the
path needs `x` and the file needs `r`. Containerised mounts that look fine
from the host can still be unreadable inside the namespace.

## 6. File readable from BookWarehouse but not from here

BookWarehouse and this plugin are separate processes and almost certainly
see the filesystem differently. If catalog/detail work but `/stream` returns
`file not readable` (500) after a successful resolve:

- Inside the plugin's runtime, run `cat <resolved_path> > /dev/null` as the
  plugin user. EACCES on a parent directory is the usual cause.
- Watch for read-only bind mounts that the plugin process cannot stat
  through (Docker `:ro` mounts work for read; `noexec`/`nosuid` are fine).
- SELinux or AppArmor labels — a mount that BW can read because it inherits
  one profile, this plugin cannot because it inherits another.

`/stream` only opens the file *after* resolve succeeds, so a 502 means the
path didn't translate and a 500 means the path translated but `os.Open`
failed.

## 7. Reading the Diagnostics endpoint

`GET /api/v1/admin/diagnostics` returns:

```json
{
  "plugin_id": "continuum.bookwarehouse-audio",
  "role": "audiobook_library_source",
  "configured": true|false,
  "catalog_routes": true|false,
  "stream_routes": true|false,
  "database": {"ok": true|false, "message": "..."},
  "upstream": {"ok": true|false, "message": "..."}
}
```

- `configured=false` → BookWarehouse client never built. Usually empty
  `base_url`/`api_key`, or the host hasn't pushed config yet.
- `database.ok=false` with a `pgx` error → DSN wrong, role can't connect,
  or migrations failed at boot.
- `upstream.ok=false` with `upstream <status>: …` → see §1.
- `catalog_routes`/`stream_routes` `false` → BW client wasn't built, so the
  router skipped those mounts. Fix `configured` first.

The endpoint enforces a 5-second deadline on the upstream check; that means
the slowest a `Diagnostics` page load can ever wait is ~5s + DB ping.

## 8. Logs and what to grep

`Config.LogValue()` and `Config.String()` redact `database_url`, `api_key`,
and `stream_signing_secret`. Look for:

- `library_root=...` — operator-set value
- `path_remappings=N` — count only (full entries are intentionally not
  logged at info level)
- `do: ... ` errors — BW client HTTP failures
- `local filesystem resolve failed` — wrapped resolver errors
- `read tag:` / `decode:` — cover extraction failures

If you need to confirm the running config matches what's on disk, save the
admin page; the next `Refresh` callback log line records the load.

## 9. Sanity smoke test

Inside the plugin runtime, with `$TOKEN` set to a valid signed JWT minted
by the portal (or any valid one — the file_idx and book_id must match):

```bash
# Catalog page 1, 5 items
curl -fsS "$BASE/api/v1/catalog?limit=5" | jq '.items | length'

# Cover for a known book
curl -fsS -o cover.bin "$BASE/api/v1/cover/<book_id>/medium?token=$TOKEN"
file cover.bin    # expect: JPEG image data or PNG

# Stream byte range 0-1023
curl -fsS -r 0-1023 -o slice.bin "$BASE/api/v1/stream/<book_id>/0?token=$TOKEN"
ls -l slice.bin   # expect: 1024 bytes
```

A 206 response with `Content-Range: bytes 0-1023/<total>` confirms Range
support is working end-to-end through whatever reverse proxy and host
plugin proxy are in front of this plugin.
