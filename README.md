# continuum-plugin-bookwarehouse-audio

Thin adapter exposing an external BookWarehouse instance to the [`continuum.audiobooks`](../continuum-plugin-audiobooks/) portal as an audiobook source. Calls upstream live on every request — no local catalog mirror, no SPA, no per-user state.

## Capabilities

| Capability | Notes |
|---|---|
| `http_routes.v1` (`backend`) | `/api/v1/*` — catalog, cover art, streaming (302 redirect to upstream), request forwarding. |
| `event_consumer.v1` (`request_handler`) | Subscribes to `plugin.continuum.audiobooks.request_submitted`; forwards each new request to BookWarehouse monitoring. |

## Configuration

| Key | Required | Description |
|---|---|---|
| `database_url` | yes | DSN for the `bookwarehouse_audio` schema. |
| `base_url` | yes | Upstream BookWarehouse base URL, no trailing slash. |
| `api_key` | yes | `X-API-Key` for upstream calls. |
| `default_cover_size` | no | One of `small \| medium \| large \| original` (default `large`). |
| `request_quality_profile` | no | BookWarehouse-side quality tier forwarded with new monitoring requests. |

## Layout

```
cmd/continuum-plugin-bookwarehouse-audio/   binary entrypoint + manifest.json
internal/
  bookwarehouse/    typed HTTP client for upstream BookWarehouse
  catalog/          audiobook_backend.v1 surface (list/search/detail/browse/cover)
  consumer/         event_consumer.v1 — subscribes to request_submitted
  event/            outbound publisher wrapper
  httproutes/       HttpRoutes.v1 adapter
  migrate/          0001 schema (forwarded_request)
  requesthandler/   business logic for request_submitted → BookWarehouse monitoring
  runtime/          Configure handler
  server/           chi mux composing all the above
  store/            forwarded_request DB wrapper
  stream/           302-redirect stream handler
  testutil/         Postgres testcontainers helper
```

## Dependencies

- Postgres role + `bookwarehouse_audio` schema (used for tracking forwarded requests).
- An external BookWarehouse instance with API access.

## Install

```sql
CREATE ROLE plugin_bookwarehouse_audio LOGIN PASSWORD '<chosen>';
CREATE SCHEMA bookwarehouse_audio AUTHORIZATION plugin_bookwarehouse_audio;
```

After configuring, sanity-check reachability:

```bash
curl -H "Authorization: Bearer <user-bearer>" \
  https://<continuum>/api/v1/plugins/continuum.bookwarehouse-audio/api/v1/health
```

Then select this plugin as the active backend from the audiobooks portal `/admin/settings`.

## What it explicitly doesn't do

- No SPA, no `navigable` route.
- No user state (progress, bookmarks, collections, sessions) — that's the portal's job.
- No ABS protocol surface — also the portal's job.
- No catalog mirror; every request hits upstream.

## Build & test

```bash
make build
make test
```

## Status

v0.1.0. Functional thin adapter.
