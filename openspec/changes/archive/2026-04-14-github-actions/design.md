## Context

ShadowDNS 目前所有品質檢查（`make test`、`make lint`、`make smoke`）和版本發佈皆為手動操作。專案託管於 GitHub private repo（`chenwei791129/ShadowDNS`），main 分支啟用 branch protection。已有一個同帳號的 repo（`rancher-kubeconfig-updater`）使用 `release-please-action@v4` 管理版本，可復用相同的 PAT。

## Goals / Non-Goals

**Goals:**

- 每次 push 和 PR 自動執行 test、lint、smoke，確保程式碼品質
- 透過 release-please 自動管理語意化版本號和 changelog
- Release 建立時自動產出 binary 和 `.deb` 包並上傳至 GitHub Release
- 確保 CI workflow 對外部 fork PR 安全無虞

**Non-Goals:**

- 不做 cross-compile（僅 `linux/amd64`，但保留 matrix 結構）
- 不建立 Docker image
- 不做自動部署到任何環境
- 不使用 `pull_request_target` 事件

## Decisions

### CI 觸發策略

CI workflow 使用 `push`（排除 main）和 `pull_request`（目標 main）兩個事件觸發。

選擇 `pull_request` 而非 `pull_request_target` 的原因：`pull_request` 事件來自 fork 時不會暴露 repo secrets，而 `pull_request_target` 會在 base repo 的 context 下執行並可存取所有 secrets，攻擊者可透過修改 workflow 檔案竊取 PAT。

CI workflow 不引用任何 secrets，permissions 設為 `contents: read` 最小權限。

### Release 觸發與版本管理

使用 `googleapis/release-please-action@v4`，`release-type: go`。僅在 push 到 main 時觸發，由 release-please 自動建立 release PR 並管理版本號。

使用 `secrets.MY_RELEASE_PLEASE_TOKEN`（PAT）而非預設 `GITHUB_TOKEN`，因為 main 分支有 branch protection，預設 token 無法建立 release PR。PAT scope 最小化為 `contents: write` 和 `pull-requests: write`。

備選方案：使用 GitHub App token（`actions/create-github-app-token`）可避免 PAT 與個人帳號綁定的問題，但目前單人維護，PAT 方案較簡單。

### Build Matrix 結構

雖然目前只需 `linux/amd64`，但使用 `strategy.matrix.include` 結構，未來新增架構只需追加一組 entry：

```yaml
strategy:
  matrix:
    include:
      - goos: linux
        goarch: amd64
        output: shadowdns-linux-amd64
```

### Deb Packaging 整合

在 build job 中，binary build 完成後執行 `make deb` 產生 `.deb` 包，兩個產物（binary 和 `.deb`）一併上傳至 GitHub Release。`make deb` 依賴 nfpm，需在 workflow 中安裝。

### Secret 設定方式

使用 `gh secret set` CLI 命令為 repo 設定 `MY_RELEASE_PLEASE_TOKEN`，避免手動進入 GitHub Web UI。PAT 值可與 `rancher-kubeconfig-updater` 共用同一把（前提是 fine-grained token 的 repository access 涵蓋 ShadowDNS），但 repo secret 需各自建立。

## Risks / Trade-offs

- **PAT 綁定個人帳號** → 帳號停用時 release 流程中斷。緩解：目前單人維護，未來可遷移至 GitHub App token
- **`make deb` 依賴 nfpm** → CI 環境需額外安裝 nfpm。緩解：在 workflow 中加入 nfpm 安裝步驟
- **Go 版本相容性** → 使用 `go-version-file: go.mod` 確保 CI 與本地一致，但 GitHub Actions runner 可能尚未支援最新版。緩解：`setup-go` action 會自動下載指定版本
- **Smoke test 可能需要特殊環境** → `make smoke` 若依賴本地 DNS 環境或特定 port，CI 中可能失敗。緩解：先納入 CI，若有問題再調整
