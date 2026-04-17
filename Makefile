.PHONY: build test test-unit test-smoke test-e2e test-contract test-go test-go-unit test-go-smoke test-python test-python-unit test-python-smoke test-python-e2e test-python-contract lint vet install install-dev clean

TABULA_HOME ?= .
VENV_PYTHON = .venv/bin/python3

# Build

build:
	go build -o bin/tabula ./cmd/tabula/

build-windows:
	GOOS=windows go build -o bin/tabula.exe ./cmd/tabula/

build-linux:
	GOOS=linux go build -o bin/tabula-linux ./cmd/tabula/

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

install-dev: install

# Clean

clean:
	rm -f bin/tabula bin/tabula.exe bin/tabula-linux
