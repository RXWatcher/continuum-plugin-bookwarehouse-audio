# BookWarehouse Audio for Continuum

`continuum.bookwarehouse-audio` connects the Continuum Audiobooks portal to an
external BookWarehouse audiobook instance. It exposes BookWarehouse catalog,
cover, and stream behavior through Continuum's `audiobook_backend.v1`
capability.

Use this plugin when BookWarehouse already owns your audiobook library and
Continuum should present that library through the Audiobooks portal.

## Detailed Operations Docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Features

- Catalog, search, detail, and cover routes for `continuum.audiobooks`.
- Stream redirects to BookWarehouse, or optional direct local file streaming
  through path remapping.
- Stateless operation: no plugin-owned database and no local request tracking.
- No user-facing SPA; the Audiobooks portal supplies the user interface and
  Audiobookshelf-compatible surface.

## Configuration

| Key | Required | Description |
|---|---|---|
| `base_url` | yes | BookWarehouse base URL, no trailing slash. |
| `api_key` | yes | API key sent to BookWarehouse as `X-API-Key`. |
| `default_cover_size` | no | `small`, `medium`, `large`, or `original`. Defaults to `large`. |
| `direct_file_access` | no | Stream files from local mounts before falling back to upstream redirects. |
| `path_remappings` | no | JSON array mapping BookWarehouse paths to paths mounted inside Continuum. |

Example path remapping:

```json
[
  {"source_path": "/media/books", "target_path": "/mnt/books"}
]
```

## Portal Integration

1. Install and configure `continuum.audiobooks`.
2. Install this plugin and configure BookWarehouse connection settings.
3. In the Audiobooks admin UI, add a presentation library backed by
   `continuum.bookwarehouse-audio`.
4. Use a separate audiobook request-provider plugin when request fulfillment is
   needed; this plugin is catalog/stream only.

## Build And Test

```bash
make build
make test
```
