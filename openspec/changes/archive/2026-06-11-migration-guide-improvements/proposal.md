## Why

`docs/migration.md` 目前僅涵蓋 BIND9 → ShadowDNS「切換階段」的遷移指引（四階段切換、平行驗證、Rollback 策略、監控檢核清單），但缺乏切換完成後「Day 2 穩態維運」所需的操作指引。維運人員在正式取代 BIND 後面臨多個無文件可循的盲點：reload 靜默失敗、GeoIP DB 過期、ephemeral 記錄揮發性、rolling restart 必要性、答案一致性回歸、query log 磁碟管理、升級 / 回滾 SOP、latency 監控。

## What Changes

- 在 `docs/migration.md` 新增「Day 2 維運」章節，涵蓋以下可操作指引：
  - Reload 靜默失敗偵測與因應（reload metrics 告警 + serial probe + log-based 輔助檢查）
  - GeoIP DB 過期監控與月度輪換 SOP（逐台 SIGHUP reload）
  - Ephemeral DNS-01 記錄揮發性警示與重啟排程策略
  - 重啟成本說明與 rolling restart 操作流程
  - 持續性答案一致性回歸驗證（常態化 answer-diff）
  - Query log 磁碟管理（logrotate 設定查核與容量估算）
  - 升級 / 回滾 SOP（`--dry-run` 驗證 + rolling 套用 + 回滾）
  - Latency 監控指引（`shadowdns_dns_request_duration_seconds`）

## Non-Goals

- 不修改任何程式碼；本 change 為純文件改善。
- 不涵蓋 AXFR/NOTIFY slave 同步狀態監控（生產環境四台皆為 master，未使用 AXFR/NOTIFY）。
- 文件描述**現行版本**行為。原先引用的兩個平行 change 均已於本 change 實作前落地（`dns-latency-histogram-buckets` 於 2026-06-10、`reload-coverage-and-metrics` 於 2026-06-11 archive），故受影響的四個小節（reload 失敗偵測、GeoIP 過期監控、rolling restart 清單、latency 監控）直接描述落地後的行為（reload metrics 告警、mmdb 隨 SIGHUP 重載、GeoIP / RRL / query log 不需重啟、細化後的 bucket 邊界），不再使用前瞻註記。
- 不新增獨立的維運 runbook 文件；優先增補進現有 `docs/migration.md`。

## Capabilities

### New Capabilities

- `day2-operations-guide`：定義 ShadowDNS 穩態維運的操作規範，涵蓋 reload 失敗偵測、GeoIP DB 過期監控、ephemeral 記錄揮發性、rolling restart、答案一致性回歸、query log 磁碟管理、升級回滾 SOP、latency 監控等八項維運需求。

### Modified Capabilities

（none）

## Impact

- Affected specs: `day2-operations-guide` (new capability spec)
- Affected code:
  - Modified: `docs/migration.md`
  - New: `openspec/changes/migration-guide-improvements/specs/day2-operations-guide/spec.md`
  - Removed: 無
