## 1. Repo Secret 設定

- [x] 1.1 依照 "Secret 設定方式" 決策，使用 `gh secret set MY_RELEASE_PLEASE_TOKEN --repo chenwei791129/ShadowDNS` 設定 repo secret（PAT scope：`contents: write`、`pull-requests: write`），滿足 "Repo secret set via gh CLI" 需求

## 2. CI Workflow

- [x] [P] 2.1 建立 `.github/workflows/ci.yml`，依照 "CI 觸發策略" 決策，設定 "CI triggers on non-main push and pull request to main"：`push`（`branches-ignore: [main]`）和 `pull_request`（`branches: [main]`），確保使用 `pull_request` 事件而非 `pull_request_target`（"CI uses pull_request event not pull_request_target"）
- [x] [P] 2.2 在 ci.yml 設定 "CI has minimal permissions and no secrets"：`permissions: contents: read`，不引用任何 secrets
- [x] [P] 2.3 在 ci.yml 加入 Go setup step，使用 `actions/setup-go@v5` 搭配 `go-version-file: go.mod`（"CI uses Go version from go.mod"），以及 `actions/cache@v4` 快取 Go modules
- [x] [P] 2.4 在 ci.yml 加入 "CI runs test, lint, and smoke in sequence" 的三個 step：依序執行 `make test`、`make lint`、`make smoke`，任一失敗即中止

## 3. Release Workflow

- [x] [P] 3.1 建立 `.github/workflows/release-please.yml`，設定 "Release workflow triggers only on push to main"：`on: push: branches: [main]`
- [x] [P] 3.2 設定 "Release workflow permissions are minimal"：`permissions` 包含 `contents: write`、`issues: write`、`pull-requests: write`
- [x] [P] 3.3 依照 "Release 觸發與版本管理" 決策，建立 release-please job，使用 `googleapis/release-please-action@v4`、`release-type: go`、`token: ${{ secrets.MY_RELEASE_PLEASE_TOKEN }}`，實現 "Release-please manages version and changelog"，並 expose `release_created` 和 `tag_name` outputs（"Release-please job outputs gate the build job"）
- [x] 3.4 依照 "Build Matrix 結構" 決策，建立 build-and-upload job，設定 `needs: release-please` 和 `if: ${{ needs.release-please.outputs.release_created }}`，使用 "Build matrix supports future architecture expansion" 的 `strategy.matrix.include` 結構（初始僅 `linux/amd64`）
- [x] 3.5 在 build job 加入 Go setup 和 binary build step，使用 `-ldflags="-s -w -X main.version=<tag_name>"` 和 `CGO_ENABLED=0`，output 命名為 `shadowdns-<goos>-<goarch>`（"Build produces binary with version and ldflags"）
- [x] 3.6 依照 "Deb Packaging 整合" 決策，在 build job 安裝 nfpm 並執行 `make deb` 產生 `.deb` 包（"Build produces deb package"）
- [x] 3.7 在 build job 使用 `gh release upload <tag_name> --clobber` 上傳 binary 和 `.deb` 至 GitHub Release（"Binary and deb uploaded to GitHub Release"）

## 4. 驗證

- [x] 4.1 確認 `version` 變數存在於 `cmd/shadowdns/main.go` 中供 ldflags 注入，若不存在則新增
- [x] 4.2 推送 feature branch 驗證 CI workflow 觸發並通過 test、lint、smoke
