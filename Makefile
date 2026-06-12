BIN_DIR := bin
HOST_GOOS := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)
BINARY := $(BIN_DIR)/shadowdns-$(HOST_GOOS)-$(HOST_GOARCH)
LINUX_BINARY := $(BIN_DIR)/shadowdns-linux-amd64
CMD_PKG := ./cmd/shadowdns

.PHONY: all help build build-linux test lint smoke completions deb test-deb docs-serve docs-build

all: build

# `help` lists every annotated target. Descriptions come from the trailing
# `## ...` comment on each target line, so adding a new target with a `##`
# comment makes it show up here automatically.
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*## "; printf "\033[1mUsage:\033[0m make \033[36m<target>\033[0m\n\n\033[1mTargets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# `build` produces a binary for the host platform at
# `bin/shadowdns-<GOOS>-<GOARCH>` — used for local development and tests on
# macOS/Linux dev machines. For producing a Linux amd64 binary (e.g., for
# packaging), use `build-linux`.
build: ## Build the binary for the host platform into bin/
	@mkdir -p $(BIN_DIR)
	go build -o $(BINARY) $(CMD_PKG)

# `build-linux` cross-compiles for linux/amd64 at
# `bin/shadowdns-linux-amd64`, the target platform for the `.deb` package.
# On linux/amd64 hosts this produces the same artefact as `build`.
build-linux: ## Cross-compile a linux/amd64 binary into bin/
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(LINUX_BINARY) $(CMD_PKG)

test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

lint: ## Run golangci-lint
	go tool golangci-lint run ./...

smoke: build ## Smoke test the binary with --dry-run
	@./scripts/smoke.sh

VERSION ?= 0.0.0-dev

# `completions` generates shell completion files into $(BIN_DIR) for bash, zsh,
# and fish. Completion output depends only on the cobra command tree, not the
# target architecture, so `go run` on the host works even when cross-compiling
# for linux/amd64. The generated files are picked up by nfpm.yaml `contents:`
# and installed under /usr/share/{bash-completion,zsh/vendor-completions,fish/vendor_completions.d}/.
# This target is the single source of truth for which shells are supported —
# scripts/test-deb.sh also depends on it so the completion set never drifts.
completions: ## Generate bash/zsh/fish completion files into bin/
	@mkdir -p $(BIN_DIR)
	go run $(CMD_PKG) completion bash > $(BIN_DIR)/shadowdns.bash
	go run $(CMD_PKG) completion zsh > $(BIN_DIR)/shadowdns.zsh
	go run $(CMD_PKG) completion fish > $(BIN_DIR)/shadowdns.fish

deb: build-linux completions ## Build the .deb package via nfpm
	VERSION=$(VERSION) go tool nfpm package --packager deb

test-deb: ## End-to-end container test of the .deb package
	@./scripts/test-deb.sh

# `docs-serve` runs a live-reload MkDocs preview of the manual site at
# http://127.0.0.1:8000. Requires uv; the mkdocs-material toolchain is
# fetched into uv's cache on demand — nothing is installed globally.
# Keep MKDOCS_DEPS in sync with .github/workflows/docs.yml.
MKDOCS_DEPS := --with mkdocs-material --with mkdocs-static-i18n
docs-serve: ## Live-reload preview of the MkDocs manual site
	uvx $(MKDOCS_DEPS) mkdocs serve

# `docs-build` renders the static site into ./site (gitignored).
docs-build: ## Render the manual site into site/ with --strict
	uvx $(MKDOCS_DEPS) mkdocs build --strict
