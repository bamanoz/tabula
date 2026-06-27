.PHONY: build test test-unit test-smoke test-e2e test-contract test-go test-go-unit test-go-smoke test-python test-python-unit test-python-smoke test-python-e2e test-python-contract lint vet release-local release-local-dry-run push install install-agent agent agent-prepare agent-run agent-connect clean

TABULA_HOME ?= .
VENV_PYTHON = .venv/bin/python3
AGENT_HOME ?= $(CURDIR)/.tabula

PRIMARY_GOAL := $(firstword $(MAKECMDGOALS))
SECOND_GOAL := $(word 2,$(MAKECMDGOALS))
THIRD_GOAL := $(word 3,$(MAKECMDGOALS))
KNOWN_AGENT_ACTIONS := prepare run connect
AGENT_ACTION :=

ifeq ($(PRIMARY_GOAL),agent)
ifneq ($(filter $(SECOND_GOAL),$(KNOWN_AGENT_ACTIONS)),)
AGENT_ACTION := $(SECOND_GOAL)
.PHONY: $(SECOND_GOAL)
$(SECOND_GOAL):
	@:
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
	git push
	git -C "$(LOCAL_TABULA_BUNDLES)" push
	git -C "$(LOCAL_TABULA_DISTRIB)" push

# Agent

agent:
	@ACTION="$(AGENT_ACTION)"; \
	if [ -z "$$ACTION" ]; then ACTION=connect; fi; \
	case "$$ACTION" in \
	  prepare) $(MAKE) agent-prepare AGENT_HOME="$(AGENT_HOME)" ;; \
	  run) $(MAKE) agent-run AGENT_HOME="$(AGENT_HOME)" ;; \
	  connect) $(MAKE) agent-connect SESSION="$(SESSION)" ;; \
	  *) \
	    printf 'usage: make agent {prepare|run|connect} [SESSION=id]\n'; \
	    printf '       make agent-prepare\n'; \
	    printf '       make agent-run\n'; \
	    printf '       make agent-connect [SESSION=id]\n' >&2; \
	    exit 2; \
	    ;; \
	esac

LOCAL_TABULA_DISTRIB ?= $(CURDIR)/../tabula-distrib
LOCAL_TABULA_BUNDLES ?= $(CURDIR)/../tabula-bundles

agent-prepare:
	TABULA_HOME="$(AGENT_HOME)" bash scripts/install-dev.sh
	TABULA_HOME="$(AGENT_HOME)" \
	TABULA_SOURCE_ALIAS_TABULA_DISTRIB="local:$(LOCAL_TABULA_DISTRIB)" \
	TABULA_SOURCE_ALIAS_TABULA_BUNDLES="local:$(LOCAL_TABULA_BUNDLES)" \
	"$(AGENT_HOME)/bin/tabula-install" app install "$(CURDIR)/tabula.app.toml" --workspace "$(CURDIR)" --update

agent-run:
	TABULA_HOME="$(AGENT_HOME)" TABULA_LOG_LEVEL="$${TABULA_LOG_LEVEL:-info}" "$(AGENT_HOME)/bin/tabula-runner"

agent-connect:
	TABULA_HOME="$(AGENT_HOME)" "$(AGENT_HOME)/bin/tabula-cli" $(if $(SESSION),--session $(SESSION),)

# Clean

clean:
	rm -f bin/tabula bin/tabula-runtime bin/tabula.exe bin/tabula-runtime.exe bin/tabula-linux bin/tabula-runtime-linux
