## Why

ShadowDNS 是對 latency 敏感的 authoritative DNS 服務，production 上偶發的 CPU 飆高、goroutine 洩漏、heap 成長等問題目前只能透過 metrics 的間接指標觀察，缺乏進一步向內窺探 runtime 行為的工具。加入 Go 官方 `net/http/pprof` endpoint 可讓 ops 在需要時即時採集 CPU profile、heap snapshot、goroutine dump，縮短疑難雜症的 MTTR。

為避免擴大預設攻擊面、避免 pprof endpoint 被濫用為 CPU DoS 或洩漏 stack trace，改動必須「預設停用、ops 明確 opt-in」。

## What Changes

- 新增 bool flag `-pprof-enable`（預設 `false`），用於控制是否暴露 pprof endpoints。
- 當 `-pprof-enable=true` 時，在既有的 metrics HTTP server（由 `-metrics-addr` 控制）的 `/debug/pprof/` path 下掛載 Go 標準 pprof handlers（index、cmdline、profile、symbol、trace、heap、goroutine、allocs、threadcreate、block、mutex）。
- pprof handlers 使用**手動逐條註冊**的方式（`pprof.Index / Cmdline / Profile / Symbol / Trace` + `pprof.Handler(name)`），不使用 `_ "net/http/pprof"` blank import，以避免污染 `http.DefaultServeMux`。
- 啟動期參數驗證：當 `-metrics-addr=""`（metrics server 停用）且 `-pprof-enable=true` 時，log fatal 並以非零 exit code 離開，避免 ops 以為 pprof 已啟用但實際沒有 server 可掛載。
- 不額外加 auth、不加 rate limit、不獨立 bind port：pprof 的存取控制完全依附於 metrics server 的 bind address 與外部網路隔離。
- 不主動 enable `runtime.SetBlockProfileRate` 或 `runtime.SetMutexProfileFraction`（避免對 DNS hot path 造成 always-on overhead）；block/mutex profile endpoint 存在但在未 opt-in 前會回傳空資料。

## Non-Goals (optional)

- 不新增認證（basic auth / token）— 存取控制由網路層負責。
- 不為 pprof 獨立一個 HTTP server 或 bind address — 與 metrics server 共用。
- 不改動既有 `-metrics-addr` 的預設值或預設 bind address。
- 不支援動態啟用／停用 pprof（flag 只在啟動時讀取，SIGHUP 不會 reload 此設定）。
- 不自動啟用 block/mutex profile sampling — 若 ops 需要，屬於另一個改動範圍。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `prometheus-metrics`: 新增一條 Requirement，定義 opt-in 的 pprof profiling endpoint 掛載在既有 metrics HTTP server 的 `/debug/pprof/` path 下，並定義 `-pprof-enable` 與 `-metrics-addr` 的衝突驗證行為。

## Impact

- Affected specs: `openspec/specs/prometheus-metrics/spec.md`（新增 Requirement）
- Affected code:
  - `cmd/shadowdns/main.go`：在 CLI opts struct（約 L155）新增 `PProfEnable bool` 欄位；新增 `-pprof-enable` flag 綁定（約 L229 附近，緊接 `-metrics-addr`）；新增啟動期衝突驗證；在 metrics mux 上條件式掛載 pprof routes（約 L403-405）
- Affected tests:
  - `test/integration/`：新增 integration test 覆蓋 enable/disable 兩情境與衝突 flag 的 fatal exit 行為
- 相依套件：僅使用 Go 標準函式庫 `net/http/pprof` 與 `runtime/pprof`，無新增外部依賴。
- 部署：ops 若要啟用 pprof，需在 systemd override.conf 加入 `-pprof-enable` flag 並重啟服務。
