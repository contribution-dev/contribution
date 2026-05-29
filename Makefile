LOCAL_TOOL_PATH := $(CURDIR)/.tools/go/bin:$(CURDIR)/.tools/bin:$(CURDIR)/node_modules/.bin
export PATH := $(LOCAL_TOOL_PATH):$(PATH)
export GOPATH ?= $(CURDIR)/.tools/go-path
export GOMODCACHE ?= $(CURDIR)/.tools/go/pkg/mod
export GOCACHE ?= $(CURDIR)/.tools/go-build
export GOBIN ?= $(CURDIR)/.tools/bin
export GOLANGCI_LINT_CACHE ?= $(CURDIR)/.tools/golangci-lint-cache

GO ?= go
PKG := ./...
BINARY := contribution

.PHONY: help build test test-race vet fmt fmt-check lint vuln ci clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  build      Build the CLI into ./bin' \
		'  test       Run unit tests' \
		'  test-race  Run tests with the race detector' \
		'  vet        Run go vet' \
		'  fmt        Format Go files' \
		'  fmt-check  Verify Go formatting' \
		'  lint       Run golangci-lint' \
		'  vuln       Run govulncheck' \
		'  ci         Run the local CI gate'

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/$(BINARY) ./cmd/contribution

test:
	$(GO) test $(PKG)

test-race:
	$(GO) test -race $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	scripts/go-format write

fmt-check:
	scripts/go-format check

lint:
	golangci-lint run

vuln:
	govulncheck $(PKG)

ci:
	pnpm ci:local

clean:
	rm -rf bin dist coverage.out
