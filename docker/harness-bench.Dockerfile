FROM golang:1.26-bookworm AS go-builder

WORKDIR /opt/src/tabula
COPY go.mod go.sum VERSION ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -ldflags "-X main.version=$(cat VERSION) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo docker) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /tmp/tabula ./cmd/tabula/ \
  && go build -ldflags "-X main.version=$(cat VERSION) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo docker) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /tmp/tabula-runtime ./cmd/tabula-runtime/

FROM python:3.13-slim AS python-deps

WORKDIR /opt/src/tabula
COPY scripts/requirements-runtime.txt ./scripts/requirements-runtime.txt
COPY tools/tabula-distro ./tools/tabula-distro
RUN python -m venv /opt/tabula-venv \
  && /opt/tabula-venv/bin/pip install -q --no-cache-dir --upgrade pip setuptools wheel \
  && /opt/tabula-venv/bin/pip install -q --no-cache-dir --no-compile -r /opt/src/tabula/scripts/requirements-runtime.txt \
  && /opt/tabula-venv/bin/pip install -q --no-cache-dir --no-compile --no-build-isolation /opt/src/tabula/tools/tabula-distro

FROM python:3.13-slim

ARG DEBIAN_FRONTEND=noninteractive

ENV TABULA_HOME=/var/lib/tabula \
    TABULA_WORKSPACE=/workspace \
    TABULA_APP_MANIFEST=/workspace/tabula.app.toml \
    TABULA_ASSETS_DIR=/opt/tabula-assets \
    TABULA_DOCKER_REINSTALL=0 \
    TABULA_RUNNER_STARTUP_TIMEOUT=60 \
    PATH=/var/lib/tabula/bin:/var/lib/tabula/.venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY apt-get update \
  && env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY apt-get install -y --no-install-recommends git openssh-client ca-certificates gosu passwd procps \
  && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /tmp/tabula /usr/local/bin/tabula
COPY --from=go-builder /tmp/tabula-runtime /usr/local/bin/tabula-runtime
COPY --from=python-deps /opt/tabula-venv /opt/tabula-venv
COPY config/global.toml /opt/tabula-assets/global.toml
COPY VERSION /opt/tabula-assets/VERSION
COPY bin/tabula-runner /opt/tabula-assets/tabula-runner
COPY bin/tabula-cli /opt/tabula-assets/tabula-cli
COPY docker/entrypoint-harness-bench.sh /opt/tabula-assets/entrypoint.sh

RUN chmod +x /opt/tabula-assets/entrypoint.sh /opt/tabula-assets/tabula-runner /opt/tabula-assets/tabula-cli \
  && mkdir -p /var/lib/tabula /workspace \
  && groupadd --gid 1000 tabula \
  && useradd --create-home --shell /bin/bash --uid 1000 --gid 1000 tabula

EXPOSE 8089 8765

ENTRYPOINT ["/opt/tabula-assets/entrypoint.sh"]
CMD ["run"]
