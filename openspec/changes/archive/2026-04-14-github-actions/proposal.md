## Why

ShadowDNS 目前沒有任何 CI/CD 自動化流程，所有品質檢查（test、lint、smoke）依賴開發者手動執行，版本發佈也需要手動 build 和打包。需要透過 GitHub Actions 建立自動化流程，確保每次提交都經過品質把關，並在發版時自動產出 binary 和 `.deb` 安裝包。

## What Changes

- 新增 `ci.yml` workflow：在非 main 分支 push 及 PR 到 main 時自動執行 `test`、`lint`、`smoke`
- 新增 `release-please.yml` workflow：push 到 main 時由 `googleapis/release-please-action@v4` 自動管理版本號，release 建立後自動 build binary、產生 `.deb` 包並上傳至 GitHub Release
- 使用 `gh secret set` 為 repo 設定 `MY_RELEASE_PLEASE_TOKEN`（PAT），供 release-please 在有 branch protection 的 main 分支上建立 release PR

## Non-Goals

- **不做 cross-compile**：目前只需 `linux/amd64`，但 workflow 配置保留 matrix 結構以便未來擴充
- **不做容器映像檔建置**：不建立 Docker image，僅產出 binary 和 `.deb`
- **不做自動部署**：Release 後不自動部署到任何環境，僅上傳 artifacts 到 GitHub Release
- **不使用 `pull_request_target`**：避免 fork PR 取得 secrets，CI workflow 僅使用 `pull_request` 事件

## Capabilities

### New Capabilities

- `ci-workflow`: 在非 main 分支 push 及 PR 到 main 時自動執行 test、lint、smoke 品質檢查
- `release-workflow`: push 到 main 時透過 release-please 自動管理版本號，並在 release 建立後自動 build、打包、上傳

### Modified Capabilities

（無）

## Impact

- 新增檔案：`.github/workflows/ci.yml`、`.github/workflows/release-please.yml`
- 依賴既有的 Makefile targets：`test`、`lint`、`smoke`、`build`、`deb`
- 需要設定 GitHub repo secret：`MY_RELEASE_PLEASE_TOKEN`
- 需要 `go-version-file: go.mod` 對應的 Go 版本在 GitHub Actions runner 上可用
