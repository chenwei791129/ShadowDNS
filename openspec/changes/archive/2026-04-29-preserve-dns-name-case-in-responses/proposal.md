## Why

ShadowDNS 在組裝 DNS response 時把所有 name 強制 lowercase，跟現代 authoritative server（BIND9 ≥9.9.5、Knot、NSD、PowerDNS Auth）的 case-preserving 行為不一致。dnspyre 一致性檢查暴露了這個問題（`www.example.com` 案例：ns1 回 `service-host.Example.com.` vs ns2 回 `service-host.example.com.`），但更嚴重的影響是：採用 DNS-0x20 case randomization 的 resolver（Google Public DNS 自 2023-07 全球啟用、Unbound `use-caps-for-id`）會 drop case-mismatch 的 response，將 ShadowDNS 暴露在 cache-poisoning 風險與 SERVFAIL 中。

## What Changes

- `internal/dnsutil/dnsutil.go`：拆分 case-aware 與 case-fold helpers — `Canonicalize` 改為「保留原 case，只 normalize trailing dot」；新增 `LookupKey(name) string` 提供 lowercase fold key 給 zone / alias map lookup 使用。
- `internal/alias/rewrite.go`：`RewriteName` 與 `RewriteNameAnywhere` 的輸出規則改為「保留 input n 的原 case prefix + alias config 寫入的 backup case」；matching 仍以 lowercase 比對。`RewriteRR` 在改 owner / RDATA 時走新規則。
- `internal/config/aliases.go`、`internal/shadowdnscfg/config.go`：`AliasGroup.Members` 與 `AliasFlags.Roots` 保留 yaml 原 case；提供額外 lowercase index 供 lookup 使用。
- `internal/server/handler.go`：`qname` 用 lowercase 做 zone matching 不變；組 response 的 owner name 改為從 `req.Question[0].Name`（原 case）取得，RDATA 走 `RewriteRR` 新規則保留 case。
- `internal/zone/zone.go`：確認 `Lookup` / `LookupWildcard` 不 mutate RR header.Name 的 case；如需 lookup-key 與 stored-name 分離，調整 API。
- 新增 unit + integration test 覆蓋：mixed-case query 對 alias zone 的 owner / RDATA case；mixed-case query 對 root zone 的 case；alias config 保留 capital backup name。

## Non-Goals

- **不**實作 DNS-0x20 case randomization（resolver 端 anti-spoofing 機制，不在 authoritative server scope）。
- **不**改變 case-insensitive matching 行為（lookup / suffix 比對仍走 lowercase fold）。
- **不**處理 dnspyre 對某 backup zone 多級 CNAME chain flatten 不一致案例（俗稱 Case C） — 已記錄為非 ShadowDNS bug，詳見本機 `.local/` 內部報告（未進 git）。
- **不**改 zone storage 的索引設計（已用 lowercase key + 保留 RR case 是正確的）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `alias-resolver`：新增 case-preservation 規則 — owner name 與 RDATA name 在 rewrite 後保留 input 的 case prefix 與 alias config 的 backup case；matching 仍 case-insensitive。
- `dns-server`：新增 case-preservation 規則 — Question section QNAME 與 Answer section owner name 在 response 中保留 query 端 / zone-file 端原 case。
- `zone-parser`：明確化 zone storage 規則 — RR header.Name 保留 zone-file 原 case 儲存，lookup 用 lowercase fold key。

## Impact

- Affected specs：`alias-resolver`、`dns-server`、`zone-parser`
- Affected code：
  - Modified：
    - internal/dnsutil/dnsutil.go
    - internal/alias/rewrite.go
    - internal/alias/override.go
    - internal/config/aliases.go
    - internal/shadowdnscfg/config.go
    - internal/server/handler.go
    - internal/server/build.go
    - internal/zone/zone.go
  - New：
    - internal/dnsutil/dnsutil_test.go（若不存在）
    - test/integration/case_preservation_test.go
  - Removed：(none)
- 行為差異：所有 query 響應將保留 case — alias zone 的 backup 名（如 `Example.com`）在 RDATA 中保留 capital；mixed-case query 的 owner name 在 Answer section 保留 query case；Question section 也保留 query case（原本 miekg/dns 行為，但需確認 handler 沒 mutate）。
- Ops 動作：
  - alias yaml 中 backup 名稱的 case 變得有意義（之前任何 case 都被 lowercase 處理；之後寫 `Example.com` 跟寫 `example.com` response 不同）— 需在 release notes 提示，並建議 op 檢查 yaml case 是否符合 BIND zone-file 的 case。
  - 升級後對 Google Public DNS / Unbound caps-on resolver 的相容性恢復。
- 風險：跨多檔修改，需大量 mixed-case test 覆蓋；case 邏輯易錯，需嚴格 unit test 邊界。
