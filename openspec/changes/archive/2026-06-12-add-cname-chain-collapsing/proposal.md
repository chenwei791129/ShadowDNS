## Why

查詢命中 CNAME 鏈時，ShadowDNS 現行回應會把鏈上所有中間記錄原樣送出（root 路徑的 `FollowCNAME`、backup 路徑的 in-bailiwick 改寫後輸出），導致 zone 內部的中間名稱（內部 load balancer、pool 命名等）洩漏給外部 client。營運上有機密性需求：對外只揭露查詢的最終結果與必要的出境目標，不揭露鏈的中間跳點。

## What Changes

- `shadowdns.yaml` 的 `aliases` group 新增 per-root 布林欄位 `collapse_cname_chain`（**預設 `false`**，未設定時回應行為與現行／BIND 完全一致），backup members 無條件繼承所屬 root 的設定，模式比照 `rewrite_rdata_labels`。
- flag 開啟時，該 root zone 與其所有 backup zone 的查詢一旦命中 CNAME 鏈，即套用**統一收合規則**（scope 限同一 zone，沿用既有深度上限 `MaxCNAMEDepth` = 8）：回應 owner 一律等於 qname（保留 on-wire case）、TTL 取被消耗鏈（含最終記錄）的最小值：
  - 鏈於 zone 內走到底取得目標 qtype 記錄 → 僅回最終記錄，回應中不含任何 CNAME。
  - 鏈指向 zone 外名稱（或深度上限耗盡）→ 回單條合成 CNAME，target 為第一個未解析的名稱。
  - 鏈於 zone 內走到底但目標名稱無該 qtype 資料 → NODATA（NOERROR + SOA），且不得 fall through 到 wildcard 合成。
- 直查 `qtype=CNAME` 同樣套用統一規則（出境 → 合成 CNAME；zone 內走到底 → NODATA）。中間名稱被直接查詢時不隱藏其存在性，但其回應同樣收合。
- backup 查詢：收合後的最終記錄與合成 CNAME 的 RDATA 仍套用既有 in-bailiwick / `rewrite_rdata_labels` 改寫規則，owner 為 backup namespace 的 on-wire qname。
- AXFR / 零變更：zone transfer 照常傳輸真實記錄（不收合），由既有 allow-transfer ACL 把關。
- SIGHUP reload：新欄位隨既有 unified config 原子 reload 流程生效。

## Capabilities

### New Capabilities

- `cname-chain-collapsing`: per-alias-group 開關的回應端 CNAME 鏈收合 — 在同一 zone 範圍內消耗 CNAME 鏈、隱藏中間名稱，依鏈尾狀態回最終記錄、單條合成 CNAME 或 NODATA。

### Modified Capabilities

- `shadowdns-config`: `aliases` section 接受新欄位 `collapse_cname_chain`（布林、預設 false、未知欄位拒絕清單同步更新），載入時輸出 per-root 的收合查表供 runtime 使用。

## Impact

- Affected specs: `cname-chain-collapsing`（新增）、`shadowdns-config`（修改）
- Affected code:
  - New:
    - `internal/zone/collapse.go`（CNAME 鏈收合追蹤：typed outcome + min-TTL）
    - `internal/zone/collapse_test.go`
    - `test/integration/cname_collapse_test.go`（端到端：root/backup、出境、NODATA、直查 CNAME）
  - Modified:
    - `internal/config/aliases.go`（`AliasGroup` 新欄位；`BuildAliasMap` 增加 per-root collapse 查表輸出）
    - `internal/config/aliases_test.go`
    - `internal/shadowdnscfg/config.go`（`rawAliasGroup` 新欄位、allowed-keys 與錯誤訊息、`Config` 新查表欄位）
    - `internal/shadowdnscfg/config_test.go`
    - `internal/server/server.go`（`ServerState` 新增 collapse 查表欄位）
    - `internal/server/build.go`（`BuildState` 簽章增加 collapse 查表參數並寫入 state）
    - `internal/server/build_test.go`
    - `internal/server/handler.go`（root 查詢路徑：exact CNAME 直查、CNAME fallback、wildcard CNAME 直查、wildcard CNAME fallback 四個收合接入點，全部路由到單一收合 helper；NODATA 短路不落入 wildcard）
    - `internal/server/handler_test.go`
    - `internal/alias/override.go`（backup 路徑：新增 collapse 專用解析入口；既有 `Resolve*` 函式簽章不變）
    - `internal/alias/rewrite.go`（自 `RewriteRR` 抽出 RDATA-only 改寫 primitive 供收合路徑共用）
    - `internal/alias/override_test.go`
    - `cmd/shadowdns/main.go`（`BuildState` 呼叫點傳遞新查表，啟動與 SIGHUP reload 兩處）
    - `internal/server/server_test.go`、`cmd/shadowdns/main_test.go`、`test/integration/helpers_test.go`、`test/integration/reload_diff_test.go`、`test/integration/axfr_test.go`、`test/integration/listenon_test.go`、`test/integration/case_preservation_test.go`（直接呼叫 `BuildState` 的測試檔，呼叫簽章機械同步）
    - `packaging/shadowdns.yaml.example`（新欄位範例與註解）
  - Removed: （無）
