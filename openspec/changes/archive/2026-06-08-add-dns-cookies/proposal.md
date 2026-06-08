## Why

ShadowDNS 目前對帶 EDNS0 的查詢完全不在回應中攜帶 OPT record（RFC 6891 合規缺口），也未實作 DNS Cookies（RFC 7873）。DNS Cookies 以極低成本提供來源位址驗證能力，可緩解 off-path 偽造來源攻擊（反射/放大、偽造回應），是 README 既列的 Planned 項目；BIND 9.11+ 預設啟用同等行為，補上後可消除與 BIND 的行為差異。

## What Changes

- 所有查詢回應路徑：對帶 EDNS0 OPT 的查詢，回應 SHALL 附上 OPT record（echo EDNS、版本 0、伺服器 UDP buffer size），修復 RFC 6891 合規缺口；UDP 截斷預算把 OPT record 的線上尺寸計入；EDNS 版本 > 0 的查詢回 BADVERS（RFC 6891 §6.1.3）
- 新增 DNS Cookies（RFC 7873）Phase 1「只回應、不強制」：查詢帶 COOKIE option 時，回應附上完整 cookie（client cookie echo + 伺服器計算的 server cookie）；畸形 COOKIE option（長度非 8 且非 16–40 bytes）回 FORMERR（RFC 7873 §5.2.2）；不帶 cookie 的查詢行為不變，不回 BADCOOKIE
- Server cookie 採 RFC 9018 interoperable 格式：version(1B) + reserved(3B) + timestamp(4B) + SipHash-2-4 雜湊(8B)；secret 於程序啟動時以 crypto/rand 產生，僅存於記憶體，無設定檔項目
- 新增 `internal/cookie` 套件作為 cookie 產生／驗證的單一 seam
- OPT 解析統一為每查詢單次：handler 解析一次 OPT 後同時供 cookie 處理與 query log 欄位使用（query log 輸出格式與內容不變）
- 效能驗收：Go 微基準覆蓋「無 EDNS / 有 EDNS 無 cookie / 有 cookie」三種 handler 路徑；dnspyre 壓測拓撲為獨立的 load-generating client 主機跨網路對 test nameserver（ShadowDNS）施測——改動前由 client → server 實測取得基準值，改動後以完全相同參數重測比較，QPS 退化 < 2% 且 p99 不超過 baseline p99 + run-to-run 雜訊幅度（雜訊幅度 = baseline 多輪實測 p99 的 max − min）
- README 功能表更新：DNS Cookies 由 Planned 改為 Yes（含查詢日誌已有的 K 旗標銜接說明不變）

## Capabilities

### New Capabilities

- `dns-cookies`: RFC 7873 DNS Cookies 的伺服器端行為——COOKIE option 解析、server cookie 產生與驗證（RFC 9018 格式）、回應附帶規則、secret 生命週期

### Modified Capabilities

- `dns-server`: 回應 SHALL 對帶 EDNS0 的查詢 echo OPT record（目前回應不含 OPT）；UDP 截斷預算 SHALL 計入 OPT record 尺寸；EDNS 版本 > 0 SHALL 回 BADVERS

## Impact

- Affected specs: 新增 `dns-cookies`；修改 `dns-server`（OPT echo 與截斷預算）
- Affected code:
  - New: `internal/cookie/cookie.go`、`internal/cookie/cookie_test.go`、`internal/server/handler_bench_test.go`（三路徑微基準）
  - Modified: `internal/server/handler.go`（OPT echo、cookie 整合、OPT 解析統一；涵蓋 replyWithAnswer、replyRcode、negativeReply 與 panic-recovery SERVFAIL 四個回應組裝點）、`internal/server/server.go`（secret 初始化與持有）、`go.mod` 與 `go.sum`（新增 SipHash-2-4 相依，選型於 design.md 決定）、`README.md`（功能表與 Planned 清單）
  - Removed: 無
- 相依系統：query-logging 的 EDNS/COOKIE 欄位改由統一解析結果餵入，輸出格式不變；prometheus-metrics 不新增指標（Phase 1 不強制驗證，無拒絕事件可計數）
- 效能風險面：每回應新增一個 OPT RR 的組裝成本與 miekg/dns `EDNS0_COOKIE` hex 編解碼成本，以微基準與 client → server 跨網路 dnspyre 前後壓測把關
