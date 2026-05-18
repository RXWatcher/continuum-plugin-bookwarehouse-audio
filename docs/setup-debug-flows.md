# BookWarehouse Audio Setup, Debugging, And Flows

Plugin ID: `continuum.bookwarehouse-audio`
Version documented: `1.0.0`

## Purpose

audiobook backend connector for a BookWarehouse instance.

## Runtime Dependencies

- Continuum plugin host
- Reachable BookWarehouse API
- continuum.audiobooks for the user-facing portal

## Setup Checklist

1. Configure base_url, api_key, cover size, and direct streaming/path remapping options.
2. Install continuum.audiobooks and map a presentation library to this backend.
3. Test search/detail/streaming from the portal.

## Configuration Reference

- `base_url`
- `api_key`
- `default_cover_size`
- `direct_file_access`
- `path_remappings`

Use the plugin manifest/admin form as the source of truth for field validation and defaults. Keep database credentials scoped to the plugin schema unless a plugin explicitly needs read access to Continuum core tables.

## Exposed Routes

- `* /api/v1/* [authenticated]`

## Capabilities

- `http_routes.v1 (backend) - Catalog, cover, and streaming for a BookWarehouse instance.`
- `audiobook_backend.v1 (default) - Catalog, cover, and streaming source for the Audiobooks portal.`

## Operational Flows

### Catalog/playback

1. Audiobooks portal asks this backend for library/search/detail data.
2. The plugin proxies catalog and cover requests to BookWarehouse.
3. For playback, it either streams through BookWarehouse or uses direct file access when enabled.

## How This Plugin Communicates

- Serves as audiobook_backend.v1 for continuum.audiobooks.
- Talks outward to BookWarehouse for catalog, cover, and streaming data.

## Debugging Runbook

- Validate base_url from inside the plugin runtime.
- Check api_key permissions on BookWarehouse.
- If covers or streams fail, check default_cover_size, direct_file_access, and path_remappings.
- If request fulfillment is needed, configure a separate audiobook request-provider plugin.

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
