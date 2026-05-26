FROM golang:1.26-bookworm AS builder

WORKDIR /opt/src/tabula
COPY . .
RUN go build -ldflags "-X main.version=$(cat VERSION) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo docker) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /tmp/tabula ./cmd/tabula/ \
  && go build -ldflags "-X main.version=$(cat VERSION) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo docker) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /tmp/tabula-runtime ./cmd/tabula-runtime/

FROM python:3.13-slim

ARG DEBIAN_FRONTEND=noninteractive

ENV TABULA_HOME=/var/lib/tabula \
    TABULA_WORKSPACE=/workspace \
    TABULA_APP_MANIFEST=/workspace/tabula.app.toml \
    TABULA_DOCKER_REINSTALL=0 \
    TABULA_RUNNER_STARTUP_TIMEOUT=60 \
    PATH=/var/lib/tabula/bin:/var/lib/tabula/.venv/bin:/usr/local/bin:/usr/bin:/bin

RUN env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY apt-get update \
  && env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY apt-get install -y --no-install-recommends git ca-certificates \
  && rm -rf /var/lib/apt/lists/*

COPY . /opt/src/tabula
COPY --from=builder /tmp/tabula /usr/local/bin/tabula
COPY --from=builder /tmp/tabula-runtime /usr/local/bin/tabula-runtime

RUN chmod +x /opt/src/tabula/docker/entrypoint.sh \
  && rm -rf /opt/src/tabula/tools/tabula-distro/src/*.egg-info \
  && mkdir -p /var/lib/tabula /workspace

EXPOSE 8089 8765

ENTRYPOINT ["/opt/src/tabula/docker/entrypoint.sh"]
CMD ["run"]
