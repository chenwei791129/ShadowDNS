BIN_DIR := bin
BINARY := $(BIN_DIR)/shadowdns
CMD_PKG := ./cmd/shadowdns

.PHONY: all build test lint smoke deb test-deb

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BINARY) $(CMD_PKG)

test:
	go test ./...

lint:
	go tool golangci-lint run ./...

smoke: build
	@./scripts/smoke.sh

VERSION ?= 0.0.0-dev

deb: build
	VERSION=$(VERSION) go tool nfpm package --packager deb

test-deb:
	@./scripts/test-deb.sh
