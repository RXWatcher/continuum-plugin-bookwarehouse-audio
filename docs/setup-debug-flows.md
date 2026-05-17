# BookWarehouse Audio Setup, Debugging, And Flows

Plugin ID: `continuum.bookwarehouse-audio`
Version documented: `0.1.0`

## Purpose

audiobook backend connector for a BookWarehouse instance.

## Runtime Dependencies

- Continuum plugin host
- Postgres schema for this plugin
- Reachable BookWarehouse API
- continuum.audiobooks for the user-facing portal

## Setup Checklist

1. Create schema and configure database_url.
2. Configure base_url, api_key, cover size, quality profile, and direct streaming/path remapping options.
3. Install continuum.audiobooks and map a presentation library to this backend.
4. Test search/detail from the portal.
5. Submit a request if request forwarding is enabled in BookWarehouse.

## Configuration Reference

- `database_url`
- `base_url`
- `api_key`
- `default_cover_size`
- `request_quality_profile`
- `direct_file_access`
- `path_remappings`

Use the plugin manifest/admin form as the source of truth for field validation and defaults. Keep database credentials scoped to the plugin schema unless a plugin explicitly needs read access to Continuum core tables.

## Exposed Routes

- `* /api/v1/* [authenticated]`

## Capabilities

- `http_routes.v1 (backend) - Catalog, streaming, and request forwarding to a BookWarehouse instance.`
- `audiobook_backend.v1 (default) - Catalog, cover, streaming, and request source for the Audiobooks portal.`
- `event_consumer.v1 (request_handler) - Forwards audiobook request_submitted events to BookWarehouse monitoring.`

## Operational Flows

### Catalog/playback

1. Audiobooks portal asks this backend for library/search/detail data.
2. The plugin proxies catalog and cover requests to BookWarehouse.
3. For playback, it either streams through BookWarehouse or uses direct file access when enabled.

### Requests

1. Audiobooks emits a request_submitted event targeted at this backend.
2. The plugin forwards the request to BookWarehouse and reports status back.

## How This Plugin Communicates

- Serves as audiobook_backend.v1 for continuum.audiobooks.
- Optionally consumes audiobook request events.
- Talks outward to BookWarehouse and publishes request status events.

## Debugging Runbook

- Validate base_url from inside the plugin runtime.
- Check api_key permissions on BookWarehouse.
- If covers or streams fail, check default_cover_size, direct_file_access, and path_remappings.
- If requests are accepted but never complete, inspect BookWarehouse queue state and plugin event logs.

## Log And Health Checks

- Start with Continuum Admin -> Plugins and confirm the installation is enabled.
- Check the plugin process logs around startup for manifest loading, migration, and route registration.
- Check scheduled task logs when a workflow depends on polling or reconciliation.
- Confirm the plugin routes are reachable through Continuum using the access level shown above.
- For database-backed plugins, verify the configured role can connect, create/migrate tables in its schema, and read/write expected rows.

## Common Failure Patterns

- Wrong installation ID selected in a portal or router setting after reinstalling a plugin.
- Plugin database URL points at the public schema instead of the dedicated plugin schema.
- Reverse proxy forwards the SPA route but not `/api/*`, `/api/v1/*`, `/assets/*`, or provider-specific public routes.
- Network checks are run from the operator laptop instead of from the Continuum/plugin runtime network.
- Secrets are regenerated during restart, invalidating signed URLs, encrypted fields, or login state.

## Verification After Changes

1. Restart or reload the plugin installation.
2. Open the plugin route or admin page in Continuum.
3. Exercise the smallest workflow that crosses a plugin boundary.
4. Confirm both the source plugin and destination plugin record the same request/session/login identifier.
5. Leave the scheduled reconciler enough time to run, then confirm terminal state or a useful error.
