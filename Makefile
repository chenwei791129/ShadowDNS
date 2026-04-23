BIN_DIR := bin
HOST_GOOS := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)
BINARY := $(BIN_DIR)/shadowdns-$(HOST_GOOS)-$(HOST_GOARCH)
LINUX_BINARY := $(BIN_DIR)/shadowdns-linux-amd64
CMD_PKG := ./cmd/shadowdns

.PHONY: all build build-linux test lint smoke completions deb test-deb

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

# `completions` generates shell completion files into $(BIN_DIR) for bash, zsh,
# and fish. Completion output depends only on the cobra command tree, not the
# target architecture, so `go run` on the host works even when cross-compiling
# for linux/amd64. The generated files are picked up by nfpm.yaml `contents:`
# and installed under /usr/share/{bash-completion,zsh/vendor-completions,fish/vendor_completions.d}/.
# This target is the single source of truth for which shells are supported —
# scripts/test-deb.sh also depends on it so the completion set never drifts.
completions:
	@mkdir -p $(BIN_DIR)
	go run $(CMD_PKG) completion bash > $(BIN_DIR)/shadowdns.bash
	go run $(CMD_PKG) completion zsh > $(BIN_DIR)/shadowdns.zsh
	go run $(CMD_PKG) completion fish > $(BIN_DIR)/shadowdns.fish

deb: build-linux completions
	VERSION=$(VERSION) go tool nfpm package --packager deb

test-deb:
	@./scripts/test-deb.sh
