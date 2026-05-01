# pulumi-eos Makefile
# All Go commands run inside the Podman dev container.
# No host Go toolchain required.

COMPOSE_FILE := deployments/compose/compose.dev.yml
DC   := podman-compose -f $(COMPOSE_FILE)
EXEC := $(DC) exec -T dev

SEMGREP        ?= semgrep
SEMGREP_CONFIG ?= p/golang
SEMGREP_FLAGS  := --config $(SEMGREP_CONFIG) --metrics=off --disable-version-check --timeout 15 --no-git-ignore --include='*.go' .

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
  -X github.com/dantte-lp/pulumi-eos/internal/version.Version=$(VERSION) \
  -X github.com/dantte-lp/pulumi-eos/internal/version.GitCommit=$(GIT_COMMIT) \
  -X github.com/dantte-lp/pulumi-eos/internal/version.BuildDate=$(BUILD_DATE)

REPORT_DIR := reports

.PHONY: help all up down restart logs shell \
        build build-provider build-all \
        test test-v test-run test-integration test-acceptance fuzz coverage coverage-func test-report \
        lint lint-fix lint-md lint-mmd lint-yaml lint-spell lint-probes lint-docs \
        semgrep vulncheck vulncheck-strict osv-scan osv-scan-strict \
        sdks schema schema-validate \
        tidy download \
        clean versions

help:
	@echo "pulumi-eos Makefile targets"
	@echo "==========================="
	@echo "Lifecycle:   up down restart logs shell"
	@echo "Build:       build build-provider build-gen build-all sdks schema"
	@echo "Test:        test test-v test-integration test-acceptance fuzz coverage test-report"
	@echo "Quality:     lint lint-fix semgrep vulncheck"
	@echo "Docs:        lint-md lint-mmd lint-yaml lint-spell lint-docs"
	@echo "Deps:        tidy download"
	@echo "Misc:        clean versions"

# === Lifecycle ===

up:
	$(DC) up -d --build

down:
	$(DC) down

restart: down up

logs:
	$(DC) logs -f dev

shell:
	$(DC) exec dev bash

# === Build ===

all: build test lint lint-docs

# `verify` is the canonical pre-commit gate per docs/05-development.md
# "Mandatory per-resource verification rules": runs the full Go toolchain
# pass plus a live cEOS integration round-trip. Fails fast on the first
# broken gate.
verify: all test-integration-keep
	@echo "verify: all gates green"

build: build-provider

build-provider:
	$(EXEC) go build -ldflags='$(LDFLAGS)' -o bin/pulumi-resource-eos ./cmd/pulumi-resource-eos

build-all:
	$(EXEC) bash -c 'set -e; \
		for OS in linux darwin windows; do \
		  for ARCH in amd64 arm64; do \
		    ext=""; [ "$$OS" = "windows" ] && ext=".exe"; \
		    GOOS=$$OS GOARCH=$$ARCH go build -trimpath -ldflags="$(LDFLAGS)" \
		      -o dist/pulumi-resource-eos-$$OS-$$ARCH$$ext ./cmd/pulumi-resource-eos; \
		  done; \
		done'

sdks: build-provider
	$(EXEC) bash -c 'pulumi package gen-sdk bin/pulumi-resource-eos --language go,python,nodejs,dotnet,java --out sdk'

schema: build-provider
	$(EXEC) bash -c 'bin/pulumi-resource-eos -schema > schemas/schema.json'

schema-validate:
	$(EXEC) pulumi package validate schemas/schema.json

# === Test ===

test:
	$(EXEC) go test ./... -race -count=1

test-v:
	$(EXEC) go test ./... -race -count=1 -v

test-run:
	@test -n "$(RUN)" || (echo "Usage: make test-run RUN=TestX PKG=./internal/x"; exit 1)
	$(EXEC) go test -run '$(RUN)' $(PKG) -race -count=1 -v

fuzz:
	@test -n "$(FUNC)" || (echo "Usage: make fuzz FUNC=FuzzX PKG=./internal/x"; exit 1)
	$(EXEC) go test -fuzz=$(FUNC) $(PKG) -fuzztime=60s

IT_COMPOSE := deployments/compose/compose.integration.yml
IT_DC      := podman-compose -f $(IT_COMPOSE)

test-integration:
	$(MAKE) test-integration-up
	$(EXEC) env EOS_HOST=host.containers.internal GNMI_HOST=host.containers.internal go test -tags integration ./test/integration/... -race -count=1 -v
	$(MAKE) test-integration-down

# Same as test-integration but keeps the cEOS container running for the
# next iteration. Used by `make verify` and by the daily local loop —
# bringing cEOS up takes ~60s, so re-using it across iterations is the
# default for interactive work.
test-integration-keep:
	$(MAKE) test-integration-up
	$(EXEC) env EOS_HOST=host.containers.internal GNMI_HOST=host.containers.internal go test -tags integration ./test/integration/... -race -count=1 -v

test-integration-up:
	$(IT_DC) up -d
	bash scripts/integration-bootstrap.sh pulumi-eos-it-ceos

test-integration-down:
	$(IT_DC) down --volumes --remove-orphans

test-integration-logs:
	$(IT_DC) logs -f ceos

test-acceptance:
	$(EXEC) go test -tags acceptance ./test/acceptance/... -count=1 -v

coverage:
	$(EXEC) go test -buildvcs=false ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic
	$(EXEC) go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

coverage-func:
	$(EXEC) go test -buildvcs=false ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic
	$(EXEC) go tool cover -func=coverage.out

test-report:
	@mkdir -p $(REPORT_DIR)/tests
	$(EXEC) gotestsum \
		--junitfile $(REPORT_DIR)/tests/unit-report.xml \
		--jsonfile $(REPORT_DIR)/tests/unit-report.json \
		--format short-verbose \
		-- -buildvcs=false ./... -race -count=1
	$(EXEC) junit2html $(REPORT_DIR)/tests/unit-report.xml $(REPORT_DIR)/tests/unit-report.html
	@echo "Test report: $(REPORT_DIR)/tests/unit-report.html"

# === Quality ===

lint:
	$(EXEC) golangci-lint run ./...

lint-fix:
	$(EXEC) golangci-lint run --fix ./...

semgrep:
	$(SEMGREP) scan $(SEMGREP_FLAGS)

vulncheck:
	$(EXEC) go run ./scripts/vuln-audit.go

vulncheck-strict:
	$(EXEC) govulncheck ./...

osv-scan: vulncheck

osv-scan-strict:
	$(EXEC) osv-scanner scan -r .

# === Docs ===

lint-md:
	markdownlint-cli2 "**/*.md" "#node_modules" "#vendor" "#sdk" "#reports" "#dist"

lint-mmd:
	bash scripts/lint-mermaid.sh

lint-yaml:
	yamllint -c .yamllint.yaml .

lint-spell:
	$(EXEC) cspell --no-progress --no-summary --config .cspell.json "**/*.md" "**/*.go" "**/*.yaml" "**/*.yml"

# lint-probes enforces docs/05-development.md rule 2b on probe files
# (`probe_<x>_test.go` under //go:build integration && probe). Every
# probe must terminate with commit, not abort — abort-only probes
# silently mark hardware-platform-unsupported commands as OK and ship
# them into resources that then fail at runtime (cEOSLab `tunnel
# dont-fragment` was caught this way; see commit `d2ee58a`).
lint-probes:
	bash scripts/lint-probes.sh

lint-docs: lint-md lint-mmd lint-yaml lint-spell lint-probes

# === Deps ===

tidy:
	$(EXEC) go mod tidy

download:
	$(EXEC) go mod download

# === Clean ===

clean:
	rm -rf bin/ dist/ sdk/ reports/ coverage.out coverage.html

# === Info ===

versions:
	@echo "=== Go ==="          && $(EXEC) go version
	@echo "=== Pulumi ==="      && $(EXEC) pulumi version
	@echo "=== golangci-lint ===" && $(EXEC) golangci-lint version --short
	@echo "=== govulncheck ===" && $(EXEC) govulncheck -version 2>/dev/null || echo "installed"
