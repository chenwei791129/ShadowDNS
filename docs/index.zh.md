# ShadowDNS

<p align="center">
  <!-- Raw HTML src is not rewritten by the i18n plugin; this page is served
       under /zh/, so step up one level to reach the shared root assets/. -->
  <img src="../assets/logo.png" alt="ShadowDNS Logo" width="480">
</p>

ShadowDNS 是一套權威 DNS 伺服器（authoritative DNS server），核心特色是 **zone aliasing**：以極低的記憶體成本服務大量備援網域（backup domain），同時對 client、BIND slave 與既有管理系統保持完全透明的相容性。

## 為什麼需要 ShadowDNS？

在 BIND 上服務大量備援網域時，每個備援網域在每個 view 都需要載入一份完整的 zone 副本。以典型的 split-horizon 部署為例 —— 3,000 個備援網域 × 7 個 view —— 記憶體中就存在約 21,000 份近乎相同的 zone 副本，彼此只差在 zone 名稱。以平均 zone 大小 10 KB 計算，這代表約 210 MB 不帶任何有效資訊的記憶體開銷。

ShadowDNS 透過 zone aliasing 消除這項浪費：

- 只有 root domain 會完整載入記憶體。
- 備援網域只是一個指向 root 的指標。
- 對備援 zone 的查詢透過 **in-bailiwick rewriting** 即時改寫：回應看起來與載入完整備援 zone 完全相同，但伺服器只保留一份權威資料。

在參考部署中，相較於同等的 BIND master，記憶體用量約減少 **80%**。

## 設計目標：透明相容

- 查詢備援網域的 client 看不到任何回應差異。
- 既有 BIND slave 照常透過 AXFR 接收 zone transfer。
- 產生 `named.conf` 與 zone file 的管理系統不需任何修改 —— ShadowDNS 直接讀取既有設定檔。

## 與 BIND 的功能比較

| 功能                               | BIND (master) | ShadowDNS  |
|------------------------------------|---------------|------------|
| RFC 1035 zone file 解析            | Yes           | Yes        |
| Split-horizon views                | Yes           | Yes        |
| GeoIP country 比對                 | Yes           | Yes        |
| GeoIP ASN 比對                     | Yes           | Yes        |
| IP / CIDR 比對                     | Yes           | Yes        |
| AXFR                               | Yes           | Yes        |
| NOTIFY                             | Yes           | Yes        |
| Wildcard records (RFC 4592)        | Yes           | Yes        |
| Zone aliasing（備援網域）          | No            | Yes        |
| Hot reload (SIGHUP)                | Yes           | Yes        |
| Prometheus metrics                 | No            | Yes        |
| IXFR                               | Yes           | No         |
| DNSSEC                             | Yes           | No         |
| IPv6 listener                      | Yes           | Yes        |
| DNS Cookies (RFC 7873)             | Yes           | Yes        |
| Response Rate Limiting (RRL)       | Yes           | Yes        |
| EDNS Client Subnet (ECS, RFC 7871) | No            | Yes（opt-in，`--ecs-enable`，預設關閉） |
| Query logging（BIND 格式）         | Yes           | Yes        |
| CNAME Flattening（外部 target）    | No            | No         |
| In-bailiwick CNAME Flattening      | No            | Planned    |
| 回應端 CNAME 鏈收合                 | No            | Yes（per alias group opt-in，預設關閉） |
| Dynamic Update                     | Yes           | No         |
| Recursion                          | 可設定        | 永遠關閉   |

!!! note "專案狀態"
    ShadowDNS 目前為 v0.x 實驗階段，尚未部署到正式環境。切換正式流量前，計劃先以正式環境規模的資料集（7 個 view、12,000+ 個 zone file）完成整合測試。

## 下一步

- [快速開始](getting-started.md) — 從 build 到啟動的最短路徑
- [安裝](installation.md) — 原始碼編譯與 `.deb` 套件安裝
- [Zone Aliasing 原理](guides/zone-aliasing.md) — 查詢處理管線與改寫規則
- [從 BIND 遷移](migration.md) — 四階段切換步驟與 rollback 策略
