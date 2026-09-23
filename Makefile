# NetScope – build, test and deployment targets.
# Go and Node are used from the host if installed, otherwise from Docker images.

SHELL        := /bin/bash
.SHELLFLAGS  := -e -o pipefail -c
VERSION      ?= $(shell git describe --tags --always 2>/dev/null || date +%Y.%m.%d)
IMAGE        ?= netscope:latest
BASE_URL     ?= http://127.0.0.1:8080
GO_IMAGE     ?= golang:1.26-alpine
NODE_IMAGE   ?= node:22-alpine
LINT_IMAGE   ?= golangci/golangci-lint:v2.13.2

HAVE_GO  := $(shell command -v go 2>/dev/null)
HAVE_NPM := $(shell command -v npm 2>/dev/null)

DOCKER_GO   = docker run --rm -v "$(CURDIR)":/src -v netscope-gomod:/go/pkg/mod -v netscope-gocache:/root/.cache/go-build \
              -w /src -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false $(GO_IMAGE)
DOCKER_NODE = docker run --rm -v "$(CURDIR)":/src -w /src/web $(NODE_IMAGE)

ifeq ($(HAVE_GO),)
GO    = $(DOCKER_GO) go
GOFMT = $(DOCKER_GO) gofmt
else
GO    = go
GOFMT = gofmt
endif

ifeq ($(HAVE_NPM),)
NPM = $(DOCKER_NODE) npm
else
NPM = npm --prefix web
endif

.PHONY: help web build dev test lint docker-build docker-up docker-down smoke clean

help:
	@echo "make build | dev | test | lint | docker-build | docker-up | docker-down | smoke"

## web: build the SvelteKit UI into internal/webui/dist
web:
	$(NPM) ci
	$(NPM) run build
	@touch internal/webui/dist/.keep

## build: web UI + Go binary (bin/netscope)
build: web
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/netscope ./cmd/netscope

## dev: Go backend on 127.0.0.1:18080 plus the Vite dev server (hot reload) on :5173
dev:
	@command -v go >/dev/null && command -v npm >/dev/null || { echo "make dev braucht go und npm lokal"; exit 1; }
	@mkdir -p .devdata
	@trap 'kill 0' EXIT; \
	NETSCOPE_DATA_DIR="$(CURDIR)/.devdata" NETSCOPE_LISTEN=127.0.0.1:18080 NETSCOPE_LOG_FORMAT=text go run ./cmd/netscope serve & \
	cd web && npm install && npm run dev

## test: Go unit tests (in parallel), then the performance targets one package at a
## time – timings measured next to 40 other test binaries would say nothing
PERF_PKGS := ./internal/inventory ./internal/plugins/cve ./internal/plugins/topology
test:
	$(GO) test -count=1 -short ./...
	$(GO) test -count=1 -p 1 -run 'Performance' -v $(PERF_PKGS)

## lint: gofmt, go vet, golangci-lint, prettier + svelte-check
lint:
	@out="$$($(GOFMT) -l cmd internal)"; if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	docker run --rm -v "$(CURDIR)":/app -v netscope-gomod:/go/pkg/mod -v netscope-lintcache:/root/.cache -w /app \
		-e GOFLAGS=-buildvcs=false $(LINT_IMAGE) golangci-lint run ./...
	$(NPM) ci
	$(NPM) run lint

## docker-build: build the container image
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

## docker-up: (re)start the container and wait until it is healthy
docker-up:
	@mkdir -p data
	NETSCOPE_VERSION=$(VERSION) docker compose up -d --build
	@for i in $$(seq 1 60); do \
		if curl -fsS $(BASE_URL)/api/v1/health >/dev/null 2>&1; then echo "NetScope läuft: $(BASE_URL)"; exit 0; fi; sleep 2; \
	done; echo "NetScope wurde nicht gesund"; docker compose logs --tail 100; exit 1

## docker-down: stop the container (data stays in ./data)
docker-down:
	docker compose down

## smoke: end-to-end checks against a running instance (incl. real scans)
smoke:
	BASE_URL=$(BASE_URL) ./scripts/smoke.sh

clean:
	rm -rf bin .devdata web/.svelte-kit
