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

## Published image

The verified `linux/amd64` image built from commit `df6e6a5669ca8f8d20b58680be926d0b08c97303` is:

```text
ahmedshareef/mattermost-custom:11.7-df6e6a5
```

For production, pin the registry digest so a mutable tag cannot change the deployed artifact:

```yaml
services:
  mattermost:
    image: ahmedshareef/mattermost-custom:11.7-df6e6a5@sha256:0738413dcd0882a37e1e4e840c1184df94e2db4740e5da66721d0ada8608ed94
```

This is a source-available build, not an Enterprise build.

## Build another revision

`MM_BUILD_HASH` is image provenance metadata. Set it to the full Git commit SHA for the source being packaged:

```bash
git rev-parse HEAD
```

Build another `linux/amd64` image with:

```bash
docker buildx build \
  --load \
  --platform linux/amd64 \
  --file server/build/Dockerfile.production \
  --tag <docker-hub-user>/mattermost-custom:<version>-<short-sha> \
  --build-arg MM_BUILD_NUMBER=<version>-custom \
  --build-arg MM_BUILD_HASH="$(git rev-parse HEAD)" \
  --build-arg WEBAPP_NODE_OPTIONS=--max-old-space-size=12288 \
  --build-arg WEBAPP_WEBPACK_PARALLELISM=2 \
  .
```

Use `--network=host` only when the builder's Docker bridge cannot reliably reach package registries. It reduces build-network isolation and should not be the default.

## External compose usage

Keep deployment-specific compose files outside this repo. Replace only the Mattermost service image while preserving its existing environment, ports, and mounts:

```yaml
services:
  mattermost:
    image: ahmedshareef/mattermost-custom:11.7-df6e6a5@sha256:0738413dcd0882a37e1e4e840c1184df94e2db4740e5da66721d0ada8608ed94
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
docker image inspect ahmedshareef/mattermost-custom:11.7-df6e6a5 >/dev/null
docker run --rm --entrypoint /mattermost/bin/mattermost ahmedshareef/mattermost-custom:11.7-df6e6a5 version
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
