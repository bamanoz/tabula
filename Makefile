.PHONY: build test test-unit test-smoke test-e2e test-contract test-go test-go-unit test-go-smoke test-python test-python-unit test-python-smoke test-python-e2e test-python-contract lint vet install clean

TABULA_HOME ?= .
VENV_PYTHON = .venv/bin/python3

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

# Install

install:
	bash scripts/install-dev.sh

# Clean

clean:
	rm -f bin/tabula bin/tabula-runtime bin/tabula.exe bin/tabula-runtime.exe bin/tabula-linux bin/tabula-runtime-linux
