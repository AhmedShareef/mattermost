# Custom production image

[`Dockerfile.production`](./Dockerfile.production) builds a Mattermost runtime image from this source tree instead of downloading an official release tarball.

It preserves the container contract expected by the standard single-host Docker deployment:

- `/mattermost/config`
- `/mattermost/data`
- `/mattermost/logs`
- `/mattermost/plugins`
- `/mattermost/client/plugins`
- `/mattermost/bleve-indexes`

## What it packages

- `webapp/channels/dist`
- `mattermost` server binary built from this checkout
- `mmctl`
- the packaged client bundle, templates, fonts, i18n files, and config skeleton produced by `make package-prep`
- runtime document conversion dependencies used by Mattermost

## Important constraint

This repository alone does not contain Mattermost's private peer `enterprise` repository, so builds from this Dockerfile report:

```text
Build Enterprise Ready: false
```

That is acceptable only if the target deployment does not rely on enterprise-only behavior. If your production instance currently uses enterprise-only settings or UI paths, treat that as a hard stop until your build process includes the matching enterprise source and validation.

## External compose usage

Keep your deployment-specific compose files outside this repo. Replace the Mattermost service image line with a build block similar to this:

```yaml
services:
  mattermost:
    image: mattermost-custom-enterprise:11.5-custom
    build:
      context: /path/to/mattermost
      dockerfile: server/build/Dockerfile.production
      args:
        MM_BUILD_NUMBER: 11.5-custom
        MM_BUILD_HASH: <commit-sha>
```

Leave the rest of the runtime contract unchanged:

- existing Postgres service
- existing bind mounts
- existing `MM_SQLSETTINGS_DATASOURCE`
- existing `MM_SERVICESETTINGS_SITEURL`
- existing Bleve path
- existing `127.0.0.1:8065` reverse-proxy target

## Pre-cutover audit

Before downtime, export and compare the effective running config:

```bash
docker compose exec -T mattermost /mattermost/bin/mattermost version
docker compose exec -T mattermost /mattermost/bin/mmctl --local config get > mmctl-config.json
docker compose config --environment | grep '^MM_' | sort > env-overrides.env
```

Review at minimum:

- `ServiceSettings.SiteURL`
- `SqlSettings.DataSource`
- `BleveSettings.IndexDir`
- `PluginSettings`
- `AuthSettings`
- `ClusterSettings`
- `ComplianceSettings`
- `DataRetentionSettings`
- `CallsConfig`

## Validation

After building the image but before cutover:

```bash
docker compose config
docker image inspect mattermost-custom-enterprise:11.5-custom >/dev/null
docker run --rm --entrypoint /mattermost/bin/mattermost mattermost-custom-enterprise:11.5-custom version
```

After cutover:

```bash
curl -fsS http://127.0.0.1:8065/api/v4/system/ping
curl -fsSI https://<your-domain>
```

Then verify in the browser:

- admin login
- System Console
- OpenID Connect admin page
- `OpenID Connect (Other)` fields
- existing channels, users, plugins, and attachments
- websocket connectivity through Nginx
