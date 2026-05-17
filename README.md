# BookWarehouse Audio for Continuum

`continuum.bookwarehouse-audio` connects the Continuum Audiobooks portal to an
external BookWarehouse audiobook instance. It exposes BookWarehouse catalog,
cover, stream, and request-monitoring behavior through Continuum's
`audiobook_backend.v1` capability.

Use this plugin when BookWarehouse already owns your audiobook library and
Continuum should present that library through the Audiobooks portal.

## Detailed Operations Docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Features

- Catalog, search, detail, and cover routes for `continuum.audiobooks`.
- Stream redirects to BookWarehouse, or optional direct local file streaming
  through path remapping.
- Request forwarding from the Audiobooks portal to BookWarehouse monitoring.
- Local request tracking so Continuum can reconcile request state.
- No user-facing SPA; the Audiobooks portal supplies the user interface and
  Audiobookshelf-compatible surface.

## Configuration

| Key | Required | Description |
|---|---|---|
| `database_url` | yes | Postgres DSN for the `bookwarehouse_audio` schema. |
| `base_url` | yes | BookWarehouse base URL, no trailing slash. |
| `api_key` | yes | API key sent to BookWarehouse as `X-API-Key`. |
| `default_cover_size` | no | `small`, `medium`, `large`, or `original`. Defaults to `large`. |
| `request_quality_profile` | no | BookWarehouse-side quality tier for new requests. |
| `direct_file_access` | no | Stream files from local mounts before falling back to upstream redirects. |
| `path_remappings` | no | JSON array mapping BookWarehouse paths to paths mounted inside Continuum. |

Example path remapping:

```json
[
  {"source_path": "/media/books", "target_path": "/mnt/books"}
]
```

## Database Setup

```sql
CREATE ROLE plugin_bookwarehouse_audio WITH LOGIN PASSWORD '<chosen>';
CREATE SCHEMA bookwarehouse_audio AUTHORIZATION plugin_bookwarehouse_audio;
GRANT CONNECT ON DATABASE continuum TO plugin_bookwarehouse_audio;
```

Example DSN:

```text
postgres://plugin_bookwarehouse_audio:password@postgres:5432/continuum?search_path=bookwarehouse_audio&sslmode=disable
```

## Portal Integration

1. Install and configure `continuum.audiobooks`.
2. Install this plugin and configure BookWarehouse connection settings.
3. In the Audiobooks admin UI, add a presentation library backed by
   `continuum.bookwarehouse-audio`.
4. Optionally select this plugin as the Audiobooks request provider if
   BookWarehouse should monitor new requests.

## Build And Test

```bash
make build
make test
```
