## Why

ShadowDNS 送出 NOTIFY 時將 NS target 主機名直接丟給 `miekg/dns.Exchange(msg, "host:53")`，底層 `net.Dial` 走系統 resolver（`/etc/resolv.conf`）。這在只服務內網、對外 UDP:53 被防火牆阻擋的權威 DNS 部署上，會對每一個 NS target 噴出 `lookup <ns>: i/o timeout` 錯誤——即使該 NS 在自己正在載入的 zone 裡就有 A/AAAA glue record 可以直接取用。

BIND9 不會發生這個問題：依 RFC 1996 §3.3 暨 BIND9 實作慣例，NOTIFY 的 NS target IP **優先從自己的權威資料（in-zone glue）取得**，完全不走遞迴解析。ShadowDNS 目前缺這一層，導致在典型 production 部署下 NOTIFY 永遠失敗，即便內網 slave 其實可達。

## What Changes

- `NotifyTargets()` 改為回傳 `(hostname, []netip.Addr)` 對——IP 清單由 **loaded zone 資料** 中對應的 A / AAAA 記錄萃取
- `dispatchNotifies()` 以回傳的 IP 清單 **直接連線**（`ip:53`），不再把 hostname 交給系統 resolver
- 多 IP glue：對每一個 IP 各發一次 NOTIFY（例如 `ns21 A 10.0.0.21` 與 `ns21 AAAA 2001:db8::1`，兩者各發）
- 當 zone 中 **查不到該 NS 的 A/AAAA glue**（out-of-bailiwick NS 或缺 glue）時：跳過該 target 並記 `debug` log，**不 fallback 到系統 resolver**（避免在無外網環境重現原問題；有需要時由未來的 `also-notify` change 處理）
- NOTIFY log 多一個 `source` 欄位，值為 `"glue"` 或 `"skipped-no-glue"`，便於運維判斷為何某 target 沒被通知

## Non-Goals

- **不** 內建遞迴 resolver 或呼叫外部 resolver 來解析 out-of-bailiwick NS——那會把原本的 timeout 問題搬回來，應由 `also-notify` 顯式指定 IP 解決
- **不** 快取解析結果到跨重載——NOTIFY 只在啟動與 reload 後觸發，解析直接從 `*zone.Zone` 取，本來就不需要 cache
- **不** 改變 NOTIFY 的重試次數、退避時間、或整體 deadline（正交問題，由 `notify-toggle` 與後續 tuning change 處理）
- **不** 改變 WARN / DEBUG 的 log severity 策略（噪音量由 `notify-toggle` 的 disable 機制處理；本 change 只在 glue 缺失時多一筆 debug）
- **不** 實作 `also-notify` directive——列為 future work，與本 change 解耦

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `zone-transfer`: NOTIFY target 的 IP 解析行為由「走系統 resolver」改為「僅從 in-zone glue 取得；無 glue 則跳過」；新增多 IP glue 會對每個 IP 各發一次、以及 out-of-bailiwick NS 被跳過的 scenario

## Impact

- **Affected specs**: `zone-transfer`
- **Affected code**:
  - [internal/transfer/notify.go](internal/transfer/notify.go) — `NotifyTargets` 回傳型別改為帶 IP 的結構；新增 glue lookup helper
  - [internal/transfer/notify_test.go](internal/transfer/notify_test.go) — 新增 in-zone glue、多 IP glue、out-of-bailiwick NS（缺 glue）、IPv6 glue 的測試
  - [cmd/shadowdns/main.go](cmd/shadowdns/main.go) — `dispatchNotifies()` 改用 IP 直連、de-dup key 擴充為 `(origin, target-host, ip)`、log 欄位新增 `source`
  - [cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) — 若有既有 dispatch 測試則更新
  - [README.md](README.md) — 補 NOTIFY 章節說明 glue-only resolution 的行為與限制
