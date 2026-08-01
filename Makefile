.PHONY: build build-windows build-linux build-all build-harness-bench-image test test-unit test-smoke test-e2e test-contract test-go test-go-unit test-go-smoke test-python test-python-unit test-python-smoke test-python-e2e test-python-contract testbed lint vet release-local release-local-dry-run push install install-agent agent agent-prepare agent-write-gateway-config agent-run agent-stop clean

ifeq ($(OS),Windows_NT)
PATH := C:/Program Files/Git/usr/bin;C:/Program Files/Git/bin;$(PATH)
SHELL := C:/PROGRA~1/Git/usr/bin/bash.exe
ifeq ($(PROCESSOR_ARCHITECTURE),ARM64)
AGENT_WINDOWS_ARCH := ARM64
else
AGENT_WINDOWS_ARCH := AMD64
endif
endif

TABULA_HOME ?= .
VENV_PYTHON = .venv/bin/python3
HARNESS_BENCH_IMAGE ?= tabula-harness-bench:latest
HARNESS_BENCH_DOCKERFILE ?= docker/harness-bench.Dockerfile
TESTBED_VENV ?= $(CURDIR)/.venv-testbed
TESTBED_PYTHON ?= $(TESTBED_VENV)/bin/python
TESTBED_DIR ?= $(CURDIR)/../tabula-distrib/testbed
TESTBED_HOME ?=
TESTBED_KEEP ?= 0
TESTBED_EXTRA_ARGS ?=

PRIMARY_GOAL := $(firstword $(MAKECMDGOALS))
SECOND_GOAL := $(word 2,$(MAKECMDGOALS))
THIRD_GOAL := $(word 3,$(MAKECMDGOALS))
KNOWN_AGENT_ACTIONS := prepare run stop
KNOWN_AGENT_PROFILES := dev prod
AGENT_PROFILE := dev
AGENT_ACTION :=

ifeq ($(PRIMARY_GOAL),agent)
ifneq ($(filter $(SECOND_GOAL),$(KNOWN_AGENT_PROFILES)),)
AGENT_PROFILE := $(SECOND_GOAL)
AGENT_ACTION := $(THIRD_GOAL)
.PHONY: $(SECOND_GOAL)
$(SECOND_GOAL):
	@:
ifneq ($(THIRD_GOAL),)
.PHONY: $(THIRD_GOAL)
$(THIRD_GOAL):
	@:
endif
else
ifneq ($(filter $(SECOND_GOAL),$(KNOWN_AGENT_ACTIONS)),)
AGENT_ACTION := $(SECOND_GOAL)
.PHONY: $(SECOND_GOAL)
$(SECOND_GOAL):
	@:
endif
endif
endif

VERSION  := $(shell cat VERSION)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/tabula ./cmd/tabula/
	go build -ldflags "$(LDFLAGS)" -o bin/tabula-runtime ./cmd/tabula-runtime/

build-windows:
	GOOS=windows go build -ldflags "$(LDFLAGS)" -o bin/tabula.exe ./cmd/tabula/
	GOOS=windows go build -ldflags "$(LDFLAGS)" -o bin/tabula-runtime.exe ./cmd/tabula-runtime/

build-linux:
	GOOS=linux go build -ldflags "$(LDFLAGS)" -o bin/tabula-linux ./cmd/tabula/
	GOOS=linux go build -ldflags "$(LDFLAGS)" -o bin/tabula-runtime-linux ./cmd/tabula-runtime/

build-all: build build-windows build-linux

build-harness-bench-image:
	docker build -f "$(HARNESS_BENCH_DOCKERFILE)" -t "$(HARNESS_BENCH_IMAGE)" .

# Test

test: test-go test-python

test-unit: test-go-unit test-python-unit

test-smoke: test-go-smoke test-python-smoke

test-e2e: test-python-e2e

test-contract: test-python-contract

test-go:
	./scripts/test-go.sh all

test-go-unit:
	./scripts/test-go.sh unit

test-go-smoke:
	./scripts/test-go.sh smoke

test-python:
	TABULA_HOME=$(TABULA_HOME) ./scripts/test-python.sh all

test-python-unit:
	TABULA_HOME=$(TABULA_HOME) ./scripts/test-python.sh unit

test-python-smoke:
	TABULA_HOME=$(TABULA_HOME) ./scripts/test-python.sh smoke

test-python-e2e:
	TABULA_HOME=$(TABULA_HOME) ./scripts/test-python.sh e2e

test-python-contract:
	TABULA_HOME=$(TABULA_HOME) ./scripts/test-python.sh contract

testbed:
	@if [ ! -x "$(TESTBED_PYTHON)" ]; then \
		python3 -m venv "$(TESTBED_VENV)"; \
		"$(TESTBED_VENV)/bin/python" -m pip install -q --upgrade pip setuptools wheel; \
		"$(TESTBED_VENV)/bin/python" -m pip install -q -e "$(CURDIR)/tools/tabula-testbed"; \
	fi
	@if [ -n "$(TESTBED_HOME)" ]; then \
		rm -rf "$(TESTBED_HOME)"; \
	fi
	$(TESTBED_PYTHON) -m tabula_testbed_runner.cli run \
		--tabula-root "$(CURDIR)" \
		--testbed-dir "$(TESTBED_DIR)" \
		$(if $(SUITE),--suite "$(SUITE)",--all) \
		--source tabula-bundles="$(LOCAL_TABULA_BUNDLES)" \
		$(if $(TESTBED_HOME),--home "$(TESTBED_HOME)",) \
		$(if $(filter 1 true yes,$(TESTBED_KEEP)),--keep,) \
		$(TESTBED_EXTRA_ARGS)

# Lint

lint: lint-go

lint-go:
	golangci-lint run ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...

# Release

release-local:
	scripts/release-local.sh

release-local-dry-run:
	DRY_RUN=1 scripts/release-local.sh

push:
	git push --follow-tags
	git -C "$(LOCAL_TABULA_BUNDLES)" push
	git -C "$(LOCAL_TABULA_DISTRIB)" push

# Agent

agent:
	@ACTION="$(AGENT_ACTION)"; \
	if [ -z "$$ACTION" ]; then ACTION=start; fi; \
	case "$$ACTION" in \
	  prepare) $(MAKE) agent-prepare AGENT_PROFILE="$(AGENT_PROFILE)" ;; \
	  run) $(MAKE) agent-run AGENT_PROFILE="$(AGENT_PROFILE)" ;; \
	  stop) $(MAKE) agent-stop AGENT_PROFILE="$(AGENT_PROFILE)" ;; \
	  start) $(MAKE) agent-prepare AGENT_PROFILE="$(AGENT_PROFILE)" && $(MAKE) agent-run AGENT_PROFILE="$(AGENT_PROFILE)" ;; \
	  *) \
	    printf 'usage: make agent [dev|prod] [prepare|run|stop]\n'; \
	    printf '       make agent dev       # install/update dev agent, then run\n'; \
	    printf '       make agent dev stop  # stop dev agent\n'; \
	    printf '       make agent prod      # install/update prod agent, then run\n'; \
	    printf '       make agent prod stop # stop prod agent\n'; \
	    printf '       make agent-prepare\n'; \
	    printf '       make agent-run\n'; \
	    printf '       make agent-stop\n' >&2; \
	    exit 2; \
	    ;; \
	esac

LOCAL_TABULA_DISTRIB ?= $(CURDIR)/../tabula-distrib
LOCAL_TABULA_BUNDLES ?= $(CURDIR)/../tabula-bundles
PROD_VERSION ?=

AGENT_DEV_HOME ?= $(CURDIR)/.tabula-dev
AGENT_PROD_HOME ?= $(CURDIR)/.tabula-prod
ifeq ($(OS),Windows_NT)
AGENT_DEV_VENV ?= $(AGENT_DEV_HOME)/.venv-Windows-$(AGENT_WINDOWS_ARCH)
else
AGENT_DEV_VENV ?= $(AGENT_DEV_HOME)/.venv-$(shell uname -s)-$(shell uname -m)
endif
AGENT_PROD_VENV ?= $(AGENT_PROD_HOME)/.venv
AGENT_DEV_DISTRO ?= $(LOCAL_TABULA_DISTRIB)/code-immune
AGENT_PROD_DISTRO ?= git+https://github.com/bamanoz/tabula-distrib.git@main#path=code-immune
AGENT_DEV_VALUES ?= $(CURDIR)/agent-profiles/dev/values.toml
AGENT_PROD_VALUES ?= $(CURDIR)/agent-profiles/prod/values.toml
AGENT_DEV_TENANT ?= code-immune-tabula-dev
AGENT_PROD_TENANT ?= code-immune-tabula-prod
AGENT_DEV_KERNEL_URL ?= ws://127.0.0.1:8189/ws
AGENT_PROD_KERNEL_URL ?= ws://127.0.0.1:8089/ws
AGENT_DEV_GATEWAY_WEB_PORT ?= 8865
AGENT_PROD_GATEWAY_WEB_PORT ?= 8765

ifeq ($(AGENT_PROFILE),prod)
AGENT_HOME ?= $(AGENT_PROD_HOME)
AGENT_VENV ?= $(AGENT_PROD_VENV)
AGENT_DISTRO ?= $(AGENT_PROD_DISTRO)
AGENT_VALUES ?= $(AGENT_PROD_VALUES)
AGENT_TENANT ?= $(AGENT_PROD_TENANT)
AGENT_KERNEL_URL ?= $(AGENT_PROD_KERNEL_URL)
AGENT_GATEWAY_WEB_PORT ?= $(AGENT_PROD_GATEWAY_WEB_PORT)
else
AGENT_HOME ?= $(AGENT_DEV_HOME)
AGENT_VENV ?= $(AGENT_DEV_VENV)
AGENT_DISTRO ?= $(AGENT_DEV_DISTRO)
AGENT_VALUES ?= $(AGENT_DEV_VALUES)
AGENT_TENANT ?= $(AGENT_DEV_TENANT)
AGENT_KERNEL_URL ?= $(AGENT_DEV_KERNEL_URL)
AGENT_GATEWAY_WEB_PORT ?= $(AGENT_DEV_GATEWAY_WEB_PORT)
endif

agent-prepare:
	@set -e; \
	if [ "$(OS)" = "Windows_NT" ] && [ "$(AGENT_PROFILE)" != "prod" ]; then \
		TABULA_HOME="$(AGENT_HOME)" powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/install-dev.ps1; \
		$(MAKE) agent-write-gateway-config AGENT_PROFILE="$(AGENT_PROFILE)" AGENT_HOME="$(AGENT_HOME)" AGENT_KERNEL_URL="$(AGENT_KERNEL_URL)" AGENT_GATEWAY_WEB_PORT="$(AGENT_GATEWAY_WEB_PORT)"; \
		if [ -d "$(AGENT_HOME)/tenants/$(AGENT_TENANT)" ] && [ ! -e "$(AGENT_HOME)/tenants/$(AGENT_TENANT)/install.lock.json" ]; then rm -rf "$(AGENT_HOME)/tenants/$(AGENT_TENANT)"; fi; \
		TABULA_HOME="$(AGENT_HOME)" TABULA_URL="$(AGENT_KERNEL_URL)" TABULA_SOURCE_ALIAS_TABULA_DISTRIB="local:$(LOCAL_TABULA_DISTRIB)" TABULA_SOURCE_ALIAS_TABULA_BUNDLES="local:$(LOCAL_TABULA_BUNDLES)" \
		"$(AGENT_VENV)/Scripts/python.exe" -m tabula_distro.agent_cli --home "$(AGENT_HOME)" install --distro "$(AGENT_DISTRO)" --tenant "$(AGENT_TENANT)" --bind "$(CURDIR)" --values "$(AGENT_VALUES)" --update --replace-binding --no-start --non-interactive; \
	elif [ "$(AGENT_PROFILE)" = "prod" ]; then \
		TABULA_HOME="$(AGENT_HOME)" VERSION="$(PROD_VERSION)" bash scripts/install.sh --distro "$(AGENT_DISTRO)" --tenant "$(AGENT_TENANT)" --bind "$(CURDIR)" --values "$(AGENT_VALUES)" --update --no-start --non-interactive; \
		$(MAKE) agent-write-gateway-config AGENT_PROFILE="$(AGENT_PROFILE)" AGENT_HOME="$(AGENT_HOME)" AGENT_KERNEL_URL="$(AGENT_KERNEL_URL)" AGENT_GATEWAY_WEB_PORT="$(AGENT_GATEWAY_WEB_PORT)"; \
	else \
		TABULA_HOME="$(AGENT_HOME)" bash scripts/install-dev.sh; \
		$(MAKE) agent-write-gateway-config AGENT_PROFILE="$(AGENT_PROFILE)" AGENT_HOME="$(AGENT_HOME)" AGENT_KERNEL_URL="$(AGENT_KERNEL_URL)" AGENT_GATEWAY_WEB_PORT="$(AGENT_GATEWAY_WEB_PORT)"; \
		if [ -d "$(AGENT_HOME)/tenants/$(AGENT_TENANT)" ] && ! "$(AGENT_VENV)/bin/python" -c 'import json, sys; value = json.load(open(sys.argv[1], encoding="utf-8")); raise SystemExit(0 if isinstance(value, dict) and value.get("version") == 2 else 1)' "$(AGENT_HOME)/tenants/$(AGENT_TENANT)/install.lock.json" 2>/dev/null; then rm -rf "$(AGENT_HOME)/tenants/$(AGENT_TENANT)"; fi; \
		TABULA_HOME="$(AGENT_HOME)" TABULA_URL="$(AGENT_KERNEL_URL)" TABULA_SOURCE_ALIAS_TABULA_DISTRIB="local:$(LOCAL_TABULA_DISTRIB)" TABULA_SOURCE_ALIAS_TABULA_BUNDLES="local:$(LOCAL_TABULA_BUNDLES)" \
		"$(AGENT_HOME)/bin/tabula-agent" --home "$(AGENT_HOME)" install --distro "$(AGENT_DISTRO)" --tenant "$(AGENT_TENANT)" --bind "$(CURDIR)" --values "$(AGENT_VALUES)" --update --replace-binding --no-start --non-interactive; \
	fi

agent-write-gateway-config:
	@mkdir -p "$(AGENT_HOME)/config/plugins/gateway-web"
	@if [ ! -e "$(AGENT_HOME)/config/plugins/gateway-web/config.toml" ]; then { \
		printf 'host = "127.0.0.1"\n'; \
		printf 'port = %s\n' "$(AGENT_GATEWAY_WEB_PORT)"; \
		printf 'kernel_url = "$(AGENT_KERNEL_URL)"\n'; \
	} > "$(AGENT_HOME)/config/plugins/gateway-web/config.toml"; fi

agent-run:
	@TABULA_HOME="$(AGENT_HOME)" TABULA_VENV="$(AGENT_VENV)" TABULA_URL="$(AGENT_KERNEL_URL)" TABULA_LOG_LEVEL="$${TABULA_LOG_LEVEL:-info}" \
		"$(AGENT_HOME)/bin/tabula-agent" --home "$(AGENT_HOME)" --tenant "$(AGENT_TENANT)"

agent-stop:
	@TABULA_HOME="$(AGENT_HOME)" "$(AGENT_HOME)/bin/tabula-agent" --home "$(AGENT_HOME)" stop

# Clean

clean:
	rm -f bin/tabula bin/tabula-runtime bin/tabula.exe bin/tabula-runtime.exe bin/tabula-linux bin/tabula-runtime-linux
