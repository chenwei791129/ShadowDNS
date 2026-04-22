BIN_DIR := bin
HOST_GOOS := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)
BINARY := $(BIN_DIR)/shadowdns-$(HOST_GOOS)-$(HOST_GOARCH)
LINUX_BINARY := $(BIN_DIR)/shadowdns-linux-amd64
CMD_PKG := ./cmd/shadowdns

.PHONY: all build build-linux test lint smoke deb test-deb

all: build

# `build` produces a binary for the host platform at
# `bin/shadowdns-<GOOS>-<GOARCH>` — used for local development and tests on
# macOS/Linux dev machines. For producing a Linux amd64 binary (e.g., for
# packaging), use `build-linux`.
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BINARY) $(CMD_PKG)

# `build-linux` cross-compiles for linux/amd64 at
# `bin/shadowdns-linux-amd64`, the target platform for the `.deb` package.
# On linux/amd64 hosts this produces the same artefact as `build`.
build-linux:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(LINUX_BINARY) $(CMD_PKG)

test:
	go test -race -count=1 ./...

lint:
	go tool golangci-lint run ./...

smoke: build
	@./scripts/smoke.sh

VERSION ?= 0.0.0-dev

deb: build-linux
	VERSION=$(VERSION) go tool nfpm package --packager deb

test-deb:
	@./scripts/test-deb.sh
