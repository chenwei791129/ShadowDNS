## Context

目標情境：既有以 BIND 為權威 DNS server 的部署，zone 檔與 `named.conf` 由上游管理系統產生並下發。典型架構如下：

```
                    ┌─────────────────────┐
                    │   BIND Master       │  ← 管理系統產生 zone files
                    │                     │    + named.conf 後 reload
                    │   大量 zone         │
                    └──────────┬──────────┘
                               │ AXFR + NOTIFY
         ┌─────────────────────┼─────────────────────┐
         ▼                     ▼                     ▼
   BIND Slave #1          BIND Slave #2          BIND Slave #N
```

**現況測量（以實際部署樣本估算）**：
- `master.zones` 規模可達 12 萬行以上
- 7 個 view 基於 GeoIP country + ASN + CIDR 判斷（view 名稱由部署者定義）
- 每個 view 對同一 domain 都有獨立 zone 檔 → 約 3,600 unique domain × 7 view ≈ 25,200 份
- 管理系統將 domain 分為 `root domain`（~600）與 `backup domain`（~3,000）；backup 的 zone 內容基本複製 root
- 以 10 KB 平均 zone 大小估算，其中 ~80%（約 3,000 × 7 × 10 KB ≈ 210 MB）是冗餘 backup 資料常駐記憶體

**限制**：
- 既有 BIND slave 無法立即替換，必須以標準 AXFR + NOTIFY 維持同步
- zone file 格式與 `named.conf` 語法必須 100% 相容（管理系統不改變輸出）
- 必須在所有 view 下都能正確回應

**利害關係人**：
- DNS Ops（部署、監控 slave 同步狀態）
- 產生 zone 檔的管理系統（需新增 `aliases.yaml` 輸出）
- 依賴 DNS 正確解析的下游業務系統

## Goals / Non-Goals

**Goals:**

- **記憶體效率**：僅載入 root domain 的完整 zone 資料；backup domain 透過 alias 指標解析，記憶體使用量相對 BIND 下降 ~80%
- **透明相容**：client、slave、管理系統三端皆無感切換——client 查詢 backup domain 看到的 response 完全不顯露 alias 機制；slave 看到的 AXFR 內容與原本從 BIND 拉到的等價
- **設定格式 100% 相容**：直接讀取現有 `named.conf`、`master.zones`、所有 zone 檔，不需要改寫
- **first-match view 語意**：保留 BIND view 的 first-match 行為，match-clients 規則順序敏感
- **最小依賴**：僅依賴 `miekg/dns`、`oschwald/maxminddb-golang`、`yaml.v3`；不引入 heavy framework

**Non-Goals（v1 不做）:**

- **IXFR（incremental transfer）**：slave 每次 NOTIFY 後全量 AXFR，多付一次頻寬但實作成本歸零；後續版本可加
- **DNSSEC**：簽章、NSEC/NSEC3、DS 皆不實作；業務未要求
- **IPv6 listener**：現行 BIND `listen-on-v6 {none;}` 與之對齊
- **Dynamic Update / DDNS**：不支援 RFC 2136 UPDATE
- **熱更新 / SIGHUP / admin API**：v1 以「重啟 process」達成 reload；後續加入 inotify 或 admin HTTP endpoint
- **Recursion**：純 authoritative，`recursion no`
- **Prometheus metrics / structured logging**：v1 僅輸出標準 log；observability 在後續 change 補
- **slave 節點改用 ShadowDNS**：v1 僅取代 master；slave 維持 BIND
- **成為 BIND 功能超集**：只實作本文列出的項目，其他 BIND 功能（views with different recursion settings、RPZ、catalog zones、etc.）不支援

## Decisions

### 選用 miekg/dns 作為 DNS 協定層

**選擇**：`github.com/miekg/dns` v1.1.x

**理由**：Go 生態事實標準，CoreDNS、PowerDNS Go 元件、Consul DNS interface 均用它。提供 DNS message parse/pack、zone file parser（RFC 1035）、UDP/TCP server、AXFR client/server primitives。自刻工作量可省 2-3 人月。

**替代方案**：
- 自刻 DNS protocol：可避免外部依賴，但需重製 message encoding、compression pointer、EDNS0、zone parser；不划算。
- `golang.org/x/net/dns/dnsmessage`：低階得多，只 encode/decode，沒有 server 框架，等於半自刻。

### 選用 MaxMind mmdb 做 GeoIP Country + ASN

**選擇**：`github.com/oschwald/maxminddb-golang` + MaxMind GeoLite2-Country / GeoLite2-ASN `.mmdb` 檔

**理由**：BIND 現況使用 MaxMind `geoip-directory`，我們用相同資料來源能保持比對結果一致；mmdb 格式在記憶體中以 trie 組織，查詢 O(log n) 且不會每次 fork process。`oschwald` library 是 MaxMind 官方推薦 Go client。

**替代方案**：
- 預先將 mmdb 展平為 CIDR list 存記憶體：啟動慢、記憶體大，且失去定期更新能力。
- 呼叫外部 `geoiplookup` command：每 query fork 一次，致命的效能成本。

### Zone 資料結構：map-per-view-per-zone + alias 指標

**選擇**：

```
type Server struct {
    // view-name → zone-name → *Zone（完整 zone data）
    views map[string]map[string]*Zone

    // backup-zone-name → root-zone-name（全 view 共用）
    aliases map[string]string

    // backup-zone-name → view-name → override records (TXT/MX/SRV only)
    overrides map[string]map[string]*OverrideSet
}

type Zone struct {
    Origin  string      // "root.com."
    SOA     *dns.SOA
    Records map[string][]dns.RR  // owner name → records
}
```

查詢 `www.backup.com A?` from view-th 流程：

```
1. server.views["view-th"]["backup.com"]   → nil
2. server.aliases["backup.com"]            → "root.com"
3. server.views["view-th"]["root.com"]     → *Zone
4. 計算 suffix: strip("www.backup.com", "backup.com") = "www"
5. 拼接:       join("www", "root.com") = "www.root.com"
6. zone.Records["www.root.com."]           → [A 1.2.3.4]
7. rewrite owner: www.root.com → www.backup.com
8. 回應
```

**理由**：查詢是熱路徑，`map[string]*Zone` O(1) lookup。alias map 啟動時建立一次，read-only。override records 與 root 分離儲存避免污染 root zone，查詢時在 root zone 查完後 merge。

**替代方案**：
- 把 backup zone 完整展開載入（類似 BIND）：失去記憶體效益，專案意義歸零。
- 用 persistent DB（BoltDB / SQLite）：增加磁碟 I/O 延遲，熱資料 DNS 伺服器不適用。

### in-bailiwick rewrite 規則

**選擇**：owner name 一律改寫；record value 中的 DNS name 僅在「指向 root zone 自身」時才改寫。

演算法：

```
rewriteName(name, rootZone, backupZone):
    若 name 等於 rootZone            → 回 backupZone
    若 name 以 "." + rootZone 結尾    → 回 strip + "." + backupZone
    否則                             → 回 name（原樣）

處理的 record 型別（RDATA 中含 name 者）：
    CNAME.Target
    NS.Ns
    MX.Mx
    PTR.Ptr
    SRV.Target
    SOA.Ns（MNAME）
    SOA.Mbox（RNAME）

不處理 name rewrite 的：
    A.A, AAAA.AAAA                 → 單純 IP，不變
    TXT.Txt                         → 文字內容不解析；若含 root domain 字串視為資料，不改寫

Record 型別特別規則：
    TXT / MX / SRV                 → 若 backup zone 有 override，用 override；否則 fallback root
    SOA.Serial                     → 繼承 root zone SOA serial（slave 檢查一致性）
```

**理由**：這是最小侵入且自我一致的規則——凡 rewrite 出的名字都會在 root zone 存在對應記錄，backup 對這些名字的查詢會透過同一套 alias 機制解到正確資料。

**替代方案**：
- 全不改寫 value：SOA MNAME 會暴露 `ns1.root.com`，違背「透明相容」Goal。
- 全部改寫 value：指向 AWS ALB、externaldns.net 等第三方的 CNAME 會壞掉。
- 改寫 TXT 字串內的 root 字樣：SPF/DKIM 內容是 opaque data，改寫風險高收益低。

### SOA serial 繼承策略

**選擇**：backup zone 的 SOA response 完全取自 root zone SOA，包含 serial、refresh、retry、expire、minimum，僅 owner name 與 MNAME / RNAME 依 rewrite 規則調整。

**理由**：root 更新 → serial bump → alias 同步 bump → slave 對 backup 的 SOA query 發現 serial 變動 → 拉 AXFR → 得到 rewrite 後新資料。無需額外 version 追蹤，且行為語意一致。

**替代方案**：
- backup 獨立 serial（每次 reload +1）：slave 重啟會全面強制 re-AXFR，浪費頻寬。
- 以 `root.serial + N`：需維護 offset，複雜度增加無收益。

### AXFR 實作：stream rewrite

**選擇**：slave 請求 `AXFR backup.com` 時，ShadowDNS 從 root zone 動態產生記錄流（不預先展開儲存），依序 stream：`SOA → NS → 其它記錄 → SOA`，逐筆套用 rewrite。

**理由**：保持 alias 機制的記憶體優勢；AXFR 是低頻操作，即時 rewrite 的 CPU 成本可接受。

**替代方案**：預先展開 backup zone 記錄到另一份結構，犧牲記憶體優勢。拒絕。

### View match 規則引擎

**選擇**：每個 view 的 match-clients 按宣告順序編譯成 rule slice，逐條求值，first-match short-circuit。規則類型：

```
type MatchRule interface {
    Match(clientIP net.IP) bool
}

type GeoIPCountryRule   { countryCode string }  // e.g. "TH"
type GeoIPASNumRule     { asn uint32 }           // e.g. 4134
type CIDRRule           { prefix netip.Prefix }
type IPRule             { ip netip.Addr }
type AnyRule            {}
```

view 之間依 `master.zones` 宣告順序排列，first-match 決定 view。

**理由**：與 BIND 行為嚴格對齊；逐條求值適合短 rule list（每 view 數十條以下）。

### 外部 MaxMind DB 的路徑

**選擇**：由 `named.conf` 的 `geoip-directory` 指定（既有欄位），預設 `/usr/local/share/GeoIP/`，讀取 `GeoLite2-Country.mmdb` 與 `GeoLite2-ASN.mmdb`。

**理由**：沿用既有 ops 習慣。若檔案缺失啟動時直接 fatal，而非 fallback（避免 view 判斷悄悄錯誤）。

## Risks / Trade-offs

- **[Risk] BIND `geoip asnum "AS#### Description"` 語法需解析 description 字串**：MaxMind mmdb 只儲存 ASN 整數，我們必須從字串中抽出數字部分（regex `^AS(\d+)\s`）。若字串格式略有變化可能漏抓。
  → **Mitigation**：parser 對無法抽出 ASN 的規則直接拒絕啟動並指出行號；加 unit test 涵蓋 `master.zones` 現有所有 asnum 字串格式。

- **[Risk] alias rewrite 與 glue records 互動**：若 root zone 有 `ns1.root.com IN A 1.2.3.4`（NS glue），backup zone 的 NS `ns1.backup.com` 需要對應 glue 才能解析。
  → **Mitigation**：查詢 `ns1.backup.com A` 時走同一套 alias 機制解到 root zone 的 A → 自動補 glue，不需特殊處理。Integration test 驗證。

- **[Risk] 切換時 slave 一次全量 AXFR 25,200 個 zone**：可能對網路與 slave 造成瞬時壓力。
  → **Mitigation**：切換計畫分階段——(1) 先跑 ShadowDNS 在非 production IP，(2) 對一組測試 slave 驗證 AXFR 正確，(3) 管理系統加產 `aliases.yaml`，(4) production slave DNS 設定切換 master；期間 BIND 可留作 rollback。

- **[Risk] backup domain 實際可能有獨立 A record 需求而非 alias**：未來若某個「原本是 backup」的 domain 需要獨立 IP，純 alias 模型無法表達。
  → **Mitigation**：`aliases.yaml` 僅定義明確要 alias 的 domain，不在 yaml 裡列的 domain 走正常 root zone 模式；管理系統可隨時將某個 domain 從 alias 關係中移除，回到完整 zone 檔。

- **[Risk] in-memory view map 在 25,200 zone × 7 view 下啟動時間**：全量解析 + mmap mmdb 檔可能數十秒。
  → **Mitigation**：可平行解析各個 zone 檔（`errgroup`）；measure 後若仍過慢，再考慮 lazy-load（首次查到時才解析）。v1 先跑簡單版本並 benchmark。

- **[Trade-off] AXFR stream rewrite 每次都即時計算 vs 預先 cache rewrite 後記錄**：v1 選擇即時計算，CPU 成本可能在 slave 大量 AXFR 時拉高。若事後 benchmark 顯示瓶頸，可加一層 rewrite cache（以 SOA serial 為 key）。

- **[Trade-off] 無熱更新導致 reload 要重啟 process**：切換期間 slave 會看到 TCP connection 斷開、UDP query 可能 drop。可接受：reload 頻率低（每日數次），且 slave 端 BIND 會自動重試。

## Migration Plan

**Phase 0 — 開發與測試（本 change 範圍）**
1. 實作 ShadowDNS v1 所有 capabilities
2. 單元測試 + integration test（pytest 式 .fwd fixture → dig 結果斷言）
3. 載入 production `named.conf` + 全量 zone 檔，驗證啟動成功、記憶體 < 預期上限（~50 MB）

**Phase 1 — 並行驗證（本 change 之外）**
1. 在現行 BIND master 主機旁部署一個 ShadowDNS instance 綁不同 IP
2. 複製管理系統輸出到該 instance
3. 用既有監控系統對比：對同一組 query，BIND 與 ShadowDNS response 應完全一致（除了 SOA serial 可能不同步時刻）
4. 連續跑 7 天無異常

**Phase 2 — Slave 切換（本 change 之外）**
1. 管理系統開始產出 `aliases.yaml`
2. 準備一台 staging BIND slave，設定其 master 為 ShadowDNS instance
3. 驗證 AXFR 成功、記錄完整、alias zone rewrite 正確
4. 生產 slave 逐一切換 master

**Phase 3 — BIND Master 退場（本 change 之外）**
1. 舊 BIND master 保留 1-2 週作為熱備援
2. 無異常後下線

**Rollback**：任一階段若發現問題，slave 的 master 設定可立即切回舊 BIND master，ShadowDNS 僅為新增 instance 不影響既有服務。

## Open Questions

- **NOTIFY 送出的目標清單**：是從 `allow-transfer` ACL 還是 zone 的 NS records 推斷？BIND 預設是 NS records；我們選擇對齊 BIND 預設（`also-notify` 指令後續若需要再加）。
- **ASN description 字串格式變化**：`master.zones` 現況使用 `"AS#### description text"`，未來若 MaxMind 更新 ASN 描述，字串會改變。我們的 parser 只取數字部分，不比對描述，應不受影響。需加測試。
- **`view-other` match-clients 是 `any`**：確認其必須是 view 宣告順序的最後一個才有 fallback 效果；若順序錯誤會 shadow 掉其它 view。parser 應在解析期輸出 warning。
