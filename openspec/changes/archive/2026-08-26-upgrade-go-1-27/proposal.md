## Summary

將 ShadowDNS 的最低 Go 工具鏈與 container builder 從 Go 1.26.4 升級至 Go 1.27.0，並以同一時段的升級前後基準測試確認 runtime 效能沒有顯著退化。

## Motivation

Go 1.27 已正式發布，包含小型物件配置最佳化、標準庫更新與新的工具鏈檢查。ShadowDNS 應採用受支援的新工具鏈，同時以可重現的驗證與跨主機 Perf-Guard 隔離並量測 compiler、runtime 與標準庫變更造成的效能影響。

## Proposed Solution

- 將 `go.mod` 的 Go directive 升級至 `1.27.0`，使用 Go 1.27 執行 module tidy，但不主動升級其他 dependencies。
- 將 `Dockerfile` 的 immutable-digest Go builder image 升級至對應的 Go 1.27.0 Alpine image，保持 builder 版本與 `go.mod` 一致。
- 將 `README.md` 與雙語 MkDocs 開發／安裝文件的最低 Go 版本更新為 1.27。
- 執行 lint、race-enabled tests、smoke test、strict docs build、Debian package build，以及 Docker 與 Podman container contract checks（可用 runtime 為準）。
- 在部署升級後 binary 前，以既有 Go 1.26.4 部署取得同時段 baseline；部署 Go 1.27.0 build 後，以相同 CNAME 與 A workload 執行 Perf-Guard 並套用既有 regression thresholds。

## Non-Goals

- 不將正式程式碼切換至 `encoding/json/v2`。
- 不採用 generic methods、portable SIMD 或其他 Go 1.27 新 API。
- 不新增 `goroutineleak` pprof handler。
- 不進行無關的 dependency upgrade、程式碼 modernize 或 hot-path refactor。
- 不因 benchmark 結果自動 rollback、commit 或發布。

## Alternatives Considered

- 僅更新 `go.mod` 並讓 container builder 留在 Go 1.26.4：違反現有 container-image contract，且會讓本機、CI 與 container builds 使用不同 compiler/runtime。
- 同時改用 `encoding/json/v2`：會混入 API 行為變更，使相容性與效能差異無法歸因於純工具鏈升級。
- 使用跨日期歷史 benchmark：環境變異會污染比較，因此拒絕，改採同一時段 back-to-back baseline 與 post-change measurements。

## Capabilities

### New Capabilities

- `go-toolchain`: 定義最低 Go 1.27.0 工具鏈，以及 module、CI 與 container builder 的版本同步要求。

### Modified Capabilities

- `container-image`: 將 builder image 的具體工具鏈基準升級至 Go 1.27.0，同時維持 immutable digest 與 `go.mod` 版本一致要求。

## Impact

- Affected specs: go-toolchain, container-image
- Affected code:
  - Modified: `go.mod`, `Dockerfile`, `README.md`, `docs/getting-started.md`, `docs/getting-started.zh.md`, `docs/installation.md`, `docs/installation.zh.md`
  - New: `openspec/changes/upgrade-go-1-27/specs/go-toolchain/spec.md`, `openspec/changes/upgrade-go-1-27/specs/container-image/spec.md`
  - Removed: none
- Affected systems: local development toolchain, GitHub Actions jobs that read `go.mod`, Debian package builds, linux/amd64 container builds, and the dedicated benchmark deployment target.
