# continuum-plugin-bookwarehouse-audio

Continuum plugin: thin adapter exposing an external BookWarehouse instance to
the `continuum.audiobooks` portal via the `audiobook_backend.v1` capability.

See the design spec at
`/opt/worktrees/continuum-rh/docs/superpowers/specs/2026-05-11-audiobooks-portal-and-bookwarehouse-backend-design.md`.

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

## Build

```bash
make build
make test
```

## Configuration

Global config keys (all required at install time):

- `database_url`            — DSN for the `bookwarehouse_audio` schema.
- `base_url`                — Upstream BookWarehouse base URL.
- `api_key`                 — Upstream BookWarehouse API key (secret).
- `default_cover_size`      — Optional default cover size (e.g. `large`).
- `request_quality_profile` — Optional quality profile name forwarded with
                              new monitoring requests.

## Admin runbook

1. Provision the postgres role + schema:
   ```sql
   CREATE ROLE plugin_bookwarehouse_audio LOGIN PASSWORD '<…>';
   CREATE SCHEMA bookwarehouse_audio AUTHORIZATION plugin_bookwarehouse_audio;
   ```
2. Install the plugin and configure the five globals above.
3. Verify reachability:
   ```bash
   curl -H "Authorization: Bearer <user-bearer>" \
        https://<continuum>/api/v1/plugins/continuum.bookwarehouse-audio/api/v1/health
   ```
4. The portal (`continuum.audiobooks`) picks this plugin via its
   `/admin/settings` page.

## What it doesn't do

- No SPA. No `navigable` route.
- No user state (progress, bookmarks, collections, sessions).
- No ABS protocol surface — that's in the portal.
- No catalog mirror; calls upstream live on every request.
