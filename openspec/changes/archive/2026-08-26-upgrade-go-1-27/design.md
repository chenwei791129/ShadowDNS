## 情境

ShadowDNS 目前在 module metadata 宣告 Go 1.26.4，container builder 也固定使用相同版本且具 immutable digest 的 Alpine image。GitHub Actions 從 `go.mod` 取得 toolchain 版本，而 README 與雙語手冊則把 Go 1.26 列為最低開發需求。Go 1.27.0 改變 compiler allocation、runtime，以及包含 `encoding/json` 與 `net/http` 在內的標準庫實作，因此本次升級橫跨 build、test、packaging、container、documentation 與 performance verification。

專用 benchmark target 仍執行升級前的 Go 1.26.4 binary。Deployment 會覆寫該版本，因此必須在 bounded review 完成後、candidate deployment 前立即取得 baseline；跨日期或在中間插入完整驗證矩陣的 measurements 都不能取代 back-to-back 比較。

## 目標／非目標

**目標：**

- 讓 module metadata、container build、CI-derived setup 與開發文件一致採用 Go 1.27.0 作為最低 toolchain。
- 維持既有 application behavior 與公開 configuration／API contracts。
- 在 Go 1.27.0 下驗證 source、tests、packaging、documentation 與本機實際選用的 container runtime。
- 使用對稱且 back-to-back 的 CNAME 與 A benchmarks 量測 compiler、runtime 與標準庫造成的效能差異。
- 產生帶有 PASS 或 REGRESSION verdict 的 local comparison report。

**非目標：**

- 採用 Go 1.27 language features、experimental SIMD、新標準庫 API 或 `encoding/json/v2` import path。
- 升級 application／tool dependencies；除非 Go 1.27 compatibility 明確證明最低必要更新不可避免。
- Refactor DNS hot path、HTTP servers、pprof registration，或新增只驗證第三方／標準庫行為的 tests。
- 改變 release semantics、自動 commit、自動 rollback 或發布 release。

## 決策

### 維持純 toolchain 升級

只調整 Go directive 與明確 toolchain references，不重寫 application code。既有 `encoding/json` imports 保留 v1 semantics，即使 Go 1.27 的底層 implementation 已更新。這可把 compatibility failure 與效能差異主要歸因於 compiler、runtime 與標準庫，而不是混入新 API。

### 固定單一且精確的 Go 1.27.0 基準

將 module directive 設為 `go 1.27.0`，Docker builder 使用相符的 official Alpine image 並固定 immutable digest。GitHub Actions 已使用 `go-version-file: go.mod`，不新增第二個 workflow version source。Floating `1.27` tag 會降低 reproducibility 並違反既有 container-image contract，因此不採用。

### 除非 compatibility 要求，否則保留 dependency graph

使用 Go 1.27 執行 module tidy 並檢查結果。本次不主動升級任何 dependency。若既有 dependency 無法在 Go 1.27 build，必須先指出不相容 module、證據與最低必要版本，再決定是否擴大 scope，不可藏在廣泛 dependency upgrade 中。

### 重用既有驗證與部署介面

Formatting 使用 `go fmt ./cmd/... ./internal/...` 並檢查 working diff；其餘重用 repo Make targets。`make deb` 建立 local-change package，`make test-deb` 驗證 package contract；container checks 使用既有 runtime selector實際選到的單一可用 runtime，不虛構同時覆蓋 Docker 與 Podman。Deployment 重用 `release-shadowdns` local-change mode，cross-host benchmark 重用 `local-dnspyre-crosshost-benchmark`，避免複製 package、override、log scan、warm-up、measurement 與 report parsing logic。

### 使用對稱且緊密相連的 Perf-Guard protocol

所有 implementation、verification 與 bounded review 完成後，先用既有 benchmark skill對目前 deployment執行 discarded warm-up與三分鐘 CNAME/A baseline；接著只進行 candidate package build/deploy與health verification，再以相同 skill、相同參數執行 post-change measurements。任一 workload QPS下降超過5%或p99上升超過15%即為 REGRESSION，否則為 PASS。REGRESSION 只停止後續效能流程並交還使用者，不自動 rollback或commit；sanitization與handoff bookkeeping仍須完成。

### Benchmark evidence保持local且先行完成sanitization gate

Raw outputs與comparison report放在 `.local/dnspyre/report/`。Committed Spectra artifacts與documentation只使用synthetic hostnames、RFC-reserved examples與generic workload names。Sanitization直接掃change artifacts與完整working-tree diff，不依賴空的staged diff，並在任何可能因REGRESSION停止的handoff之前完成。

## 實作契約

**可觀察行為**

- Repository Go commands依 `go.mod`選擇Go 1.27.0或更新版本。
- Container build使用具immutable digest的official Go 1.27.0 Alpine builder，並維持既有Distroless nonroot runtime contract。
- 讀取 `go.mod` 的CI jobs不另行hard-code toolchain version。
- README與英文／正體中文手冊一致寫明Go 1.27或更新版本。
- ShadowDNS CLI、DNS、API、DoH、configuration、reload、packaging與container runtime behavior維持既有contracts。

**Failure modes與acceptance**

- Build、lint、test、documentation、package或實際可用container runtime的contract check失敗都會阻擋完成並保留診斷。
- 不可避免的dependency更新必須先明確記錄原因與最小scope，再交由使用者決定是否納入。
- Module、builder、CI-derived toolchain與所有developer prerequisites一致為Go 1.27。
- Formatting、lint、race-enabled tests、smoke、strict docs、Debian package與selected container runtime checks全部通過。
- Candidate binary回報Go 1.27.0 build information，service active且startup logs無錯誤訊號。
- Perf-Guard依相同workloads與參數產生PASS或REGRESSION verdict。

**Scope boundaries**

- In scope：toolchain metadata、immutable container builder pin、prerequisite documentation、既有build/test/package/container驗證與local Perf-Guard evidence。
- Out of scope：application features、新Go 1.27 APIs、無關dependency upgrades、benchmark harness redesign、release publication、自動rollback與自動commit。

## 風險／取捨

- [Go 1.27改變`encoding/json` internals與精確error wording] → 保留v1 import path並執行既有API／DoH tests，不替標準庫本身新增tests。
- [Small-allocation specialization可能改變DNS hot-path效能與binary size] → 執行對稱Perf-Guard；`GOEXPERIMENT=nosizespecializedmalloc`只作診斷，不成為shipped setting。
- [Official Go 1.27.0 Alpine digest可能解析到錯誤architecture或manifest] → 編輯Dockerfile前驗證linux/amd64 manifest metadata，再build與inspect image。
- [`go mod tidy`可能重排module metadata] → 檢查精確diff並拒絕無關version movement。
- [Tool modules可能尚未相容新Go release] → 在Go 1.27下執行repo-managed linter與packager，只在有具體證據時提出最低必要compatibility update。
- [Benchmark host variance可能偽裝改善或退化] → Baseline、deployment與post-change measurements在同一session緊密執行，使用相同warm-up、workloads、durations與source perspective。
- [最低macOS支援升至macOS 13] → 文件宣告Go 1.27 prerequisite；目前支援的project environment已高於此版本。

## Rollback

Rollback方式是重新部署先前已知package，或從升級前commit以Go 1.26.4重建。Rollback永不自動執行。

## Open Questions

無。Go 1.27.0、純toolchain scope、verification matrix、benchmark protocol、thresholds與no-auto-commit behavior均已由核准需求及project policy固定。
