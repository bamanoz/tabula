# Docker Single-Container Runtime

This mode runs Tabula kernel, local runtime, one project-scoped tenant, drivers,
plugins, and gateway-web inside one container. Host repository mounts at
`/workspace`; `TABULA_DISTRO_SOURCE` selects distro and `TABULA_TENANT_ID`
selects container-local tenant.

This is a development/runtime convenience, not a security sandbox. Do not mount
the Docker socket or your full home directory unless you explicitly want the
agent to control them.

## Layout

Inside the container:

```text
/opt/src/tabula   # core source copied into the image
/workspace/.tabula # TABULA_HOME runtime/config/state by default
/workspace        # mounted host repository
/tmp/tabula-runtime-<tenant-id>/runtime.sock  # local runtime socket
```

`TABULA_HOME` is runtime state. It is not a workspace.

## Build And Run

From the `tabula` checkout:

```bash
UID=$(id -u) GID=$(id -g) docker compose up --build
```

By default, compose mounts the current checkout as `/workspace` and uses
`/workspace/.tabula` as `TABULA_HOME`. This keeps config, secrets, and runtime
state next to the repository, matching the normal per-repo local install shape.

Ports:

- `18089` - host port mapped to container kernel `8089`
- `18765` - host port mapped to container gateway-web `8765`

The gateway URL includes a local token. Get it from inside the container:

```bash
docker compose exec tabula bash -lc 'cat "$TABULA_HOME/run/plugins/gateway-web/ready.json"'
```

## Run Another Repository

Any repository can mount at `/workspace`; pass distro source and stable local
tenant ID explicitly:

```bash
UID=$(id -u) GID=$(id -g) \
docker compose run --rm \
  -e TABULA_WORKSPACE=/workspace \
  -e TABULA_DISTRO_SOURCE='git+https://github.com/owner/distros.git@main#path=my-distro' \
  -e TABULA_TENANT_ID=my-project \
  -v /absolute/path/to/my-project:/workspace \
  --service-ports tabula
```

Entrypoint installs core when needed, then runs:

```bash
tabula-agent install --distro "$TABULA_DISTRO_SOURCE" --tenant "$TABULA_TENANT_ID" \
  --bind /workspace --update --no-start --non-interactive
tabula serve --runtime-mode managed
```

Image includes Python 3.13 and `git`, so git distro sources work in clean
repositories. Core compose file also mounts sibling `../tabula-distrib` and
`../tabula-bundles` for local `code-immune` development.

The default compose mapping uses non-standard host ports to avoid collisions
with a locally running Tabula instance.

## Config And Provider Secrets

Tabula reads user-owned config from `$TABULA_HOME/config/global.toml` and secrets
from `$TABULA_HOME/secrets.json`. The Docker entrypoint creates minimal defaults
only when these files are missing. It does not overwrite existing user config or
secrets.

Default Docker `global.toml` references secret store ids:

```toml
[plugins.driver]
provider = "openai"

[plugins.driver.providers.openai]
api_key = { source = "store", id = "driver.openai.api_key" }
```

Write secrets into the chosen `TABULA_HOME` before starting the agent, or mount an
existing per-repo `.tabula` directory. Example for a fresh repo-local home:

```bash
docker compose run --rm \
  -e TABULA_HOME=/workspace/.tabula \
  -v /absolute/path/to/my-project:/workspace \
  tabula prepare
docker compose run --rm \
  -e TABULA_HOME=/workspace/.tabula \
  -v /absolute/path/to/my-project:/workspace \
  tabula bash -lc 'cat > "$TABULA_HOME/secrets.json" <<EOF
{
  "driver.openai.api_key": "sk-...",
  "driver.anthropic.api_key": "sk-ant-..."
}
EOF'
```

To switch providers, edit `$TABULA_HOME/config/global.toml` inside the runtime
volume and change `[plugins.driver].provider`.

Avoid putting secrets in compose `environment` or `env_file`; `docker compose
config` prints resolved environment values.

## User IDs And File Ownership

Compose runs the container as your host uid/gid. Use your host ids so files
edited in mounted workspaces are not owned by root:

```bash
UID=$(id -u) GID=$(id -g) docker compose up
```

## Useful Commands

Prepare/install without starting the runner:

```bash
docker compose run --rm tabula prepare
```

Open a shell after prepare:

```bash
docker compose run --rm tabula shell
```

Force reinstall on next start:

```bash
TABULA_DOCKER_REINSTALL=1 UID=$(id -u) GID=$(id -g) docker compose up
```

Reset runtime state:

```bash
docker compose down -v
```

## Current Scope

The first Docker target is intentionally single-container:

- one kernel process;
- one local runtime process;
- distro plugins and drivers inside the same container;
- gateway-web exposed on `0.0.0.0:8765` with token auth;
- one mounted workspace path.

Future work can add multi-container runtimes, per-task containers, browser/MCP
profiles, and image publishing.

## Verified Example

The `yaml-parsing` repository includes a `docker-compose.yml` that builds the
Tabula image from sibling `../tabula`, mounts the repository at `/workspace`, and
uses `/workspace/.tabula` as `TABULA_HOME`.

Verified flow:

```bash
docker context use desktop-linux
UID=$(id -u) GID=$(id -g) docker compose build
UID=$(id -u) GID=$(id -g) docker compose up -d
```

Gateway health:

```bash
TOKEN=$(cat .tabula/run/plugins/gateway-web/token)
curl "http://127.0.0.1:18765/api/health?token=$TOKEN"
```

Live agent verification sent a websocket prompt through gateway-web in session
`docker-live` and received the streamed response `DOCKER_OK`.
