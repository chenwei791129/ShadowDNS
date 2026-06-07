## Why

Production 環境的 BIND9 啟用了 queries log（`logging{}` 區塊的 `channel queries_log` + `category queries`），下游的營運流程與分析工具依賴該 log 的逐行格式。ShadowDNS 要能接手 BIND9 的部署，必須產出格式逐字相容的 query log，且直接吃既有的 named.conf `logging{}` 設定，讓切換時 named.conf 與下游 parser 都零改動。

## What Changes

- named.conf parser 新增 `logging{}` 區塊解析：`channel` 宣告（`file` 路徑、`severity`、`print-time`、`print-category`、`print-severity`）與 `category queries` 的 channel 對應。其餘 category 與 channel 參數照舊忽略。
- 新增 `internal/querylog` 套件：BIND9 query log 行格式的 formatter 與檔案寫入器（重用既有 `logging.ReopenSink`），效能取向（buffer pool、無多餘配置）。
- DNS handler 在 view 匹配成功後發出 query log（含之後被 REFUSED 的查詢）；no-view REFUSED 不進 query log。AXFR/IXFR 在 transfer 路徑的 view 解析成功後同樣發出（allow-transfer ACL REFUSED 的請求在 view 解析前返回，不記錄）。
- SIGHUP reload 不重新套用 `logging{}` 設定：既有 query log sink 原封不動，設定變更需重啟生效。
- SIGUSR1 同時 reopen main log（`--log-file`）與 queries log；沿用 logrotate + SIGUSR1 的 rotation 機制。
- 當 queries channel 的 `file` 子句帶有 `versions` 或 `size` 參數時，啟動時透過 main logger 印出 warning，告知 ShadowDNS 不實作 BIND 內建 rotation，需以 logrotate 接手。
- `--dry-run` 摘要納入 query log 狀態（啟用與否、檔案路徑、rotation warning）。

## Capabilities

### New Capabilities

- `query-logging`: BIND9 相容的 per-query log——行格式、發出點語意、flags 子集、SIGUSR1 reopen、停用情境。

### Modified Capabilities

- `config-loader`: 新增 `logging{}` 區塊解析需求（現行為靜默跳過整個區塊）。

## Impact

- Affected specs: 新增 `query-logging`；修改 `config-loader`。
- Affected code:
  - New:
    - internal/config/logging.go（`logging{}` 區塊解析）
    - internal/config/logging_test.go
    - internal/querylog/querylog.go（formatter 與寫入器）
    - internal/querylog/querylog_test.go
  - Modified:
    - internal/config/zones.go（top-level dispatch：`logging` 區塊改交給解析器，不再靜默跳過）
    - internal/config/zones_test.go（既有「logging 區塊靜默忽略」測試改為新行為）
    - internal/server/server.go（`Server` 持有 query logger）
    - internal/server/handler.go（`ServeDNS` 與 `handleTransfer` 的發出點）
    - internal/server/handler_test.go（發出點測試）
    - cmd/shadowdns/main.go（啟動佈線、SIGUSR1 同時 reopen 兩個 log、rotation warning、dry-run 摘要）
    - cmd/shadowdns/main_test.go（啟動失敗、reopen、dry-run 摘要測試）
    - README.md（feature 對照表的 query logging 由 Planned 改為已支援）
    - packaging/named.conf.example（補 `logging{}` 區塊範例）
  - Removed: （無）
