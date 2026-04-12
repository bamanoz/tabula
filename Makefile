.PHONY: build test lint vet install clean

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

test-go:
	go test ./...

test-python:
	TABULA_HOME=$(TABULA_HOME) $(VENV_PYTHON) -m pytest tests/ -x -q

# Lint

lint: lint-go

lint-go:
	golangci-lint run ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...

# Install

install:
	bash install-dev.sh

# Clean

clean:
	rm -f bin/tabula bin/tabula.exe bin/tabula-linux
