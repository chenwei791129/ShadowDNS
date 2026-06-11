# Zone Aliasing 原理

Zone aliasing 是 ShadowDNS 的核心機制：root domain 完整載入記憶體，備援網域只是一個指向 root 的指標，查詢時透過 in-bailiwick rewriting 即時產生「看起來像完整備援 zone」的回應。本頁說明查詢處理管線的四個階段與改寫規則。

## 查詢處理管線

```text
Client query
     |
     v
[ View Matcher ]
     |   依宣告順序評估 match-clients 規則（GeoIP country、
     |   GeoIP ASN、IP/CIDR、any）。First match wins，回傳 view 名稱。
     |
     v
[ Alias Resolver ]
     |   檢查被查詢的 zone 是否為備援 alias。是的話，先把查詢名稱
     |   從 backup.domain 改寫為 root.domain 再查詢，
     |   並記下原始備援名稱供回應使用。
     |
     v
[ Zone Lookup ]
     |   在選定 view 的記憶體 zone tree（map[ownerName][]RR）中
     |   尋找符合的 owner entry，每個 owner name O(1) 查詢。
     |   無精確命中時，依 RFC 4592 嘗試 wildcard 比對：由左而右
     |   逐層剝除 label，直到找到 `*.<parent>` entry，或被既有
     |   名稱阻擋（ENT 規則）。
     |
     v
[ In-Bailiwick Rewrite ]
     |   把 owner name 改寫回備援網域。RDATA 中含 DNS 名稱的欄位
     |   （CNAME target、NS、MX、SRV、SOA MNAME/RNAME）：若 target
     |   指向 root zone 內部，改寫為備援 zone；指向其他位置的 target
     |   （例如第三方 CDN 主機名）保持不變。
     |
     v
Response sent to client
```

## 各階段細節

### View Matcher

每個 view 的 `match-clients` 區塊在啟動時編譯為有序的規則 slice。規則由左至右評估，第一條命中 client 來源 IP 的規則決定 view；沒有任何 view 命中時回應 REFUSED。GeoIP 查詢使用直接讀入記憶體的 MaxMind mmdb；mmdb 檔案在每次 SIGHUP reload 時重新開啟，MaxMind 的每月更新不需重啟 process 即可生效。

### Alias Resolver

查詢時，resolver 對 alias map（啟動時從 `shadowdns.yaml` 的 `aliases` 區段建立）做 **longest-suffix match**。備援 zone entry 是一個薄指標 —— resolver 剝除備援後綴、替換為 root 後綴，把改寫後的名稱交給 zone lookup。原始備援名稱會保留下來，供改寫階段還原。

### Zone Lookup

Zone 資料以 `map[viewName]map[zoneName]*Zone` 儲存，每個 `Zone` 持有 `map[ownerName][]dns.RR`。所有結構在啟動後唯讀，讀取路徑不需要任何鎖。

精確比對無結果時，依 RFC 4592 退回 wildcard 比對：從查詢名稱逐一剝除 DNS label，探測 map 中是否存在 `*.<parent>` entry，直到抵達 zone origin，或被阻擋進一步遍歷的既有名稱擋下（empty non-terminal 規則）。支援 CNAME wildcard 合成與正確的回應 owner name 改寫。

備援覆寫紀錄（備援 zone 自有 zone file 提供的 TXT、MX、SRV）獨立儲存，在 root lookup 之後合併進結果。

### In-Bailiwick Rewrite

改寫規則刻意保守：

| 對象 | 改寫行為 |
|------|----------|
| Owner name | 一律改寫（依定義必然 in-bailiwick） |
| RDATA 中的 DNS 名稱（CNAME target、NS、MX、SRV、SOA MNAME/RNAME） | 只在指向 root zone 內部時改寫 —— 確保改寫後的名稱也能透過同一套 alias 機制正確解析 |
| 指向外部的 RDATA 名稱（如第三方 CDN 主機名） | 保持不變 |
| A / AAAA | 承載 IP 位址，永不改寫 |
| TXT | RDATA 視為不透明資料，永不改寫 —— 即使內容字串恰好等於 root domain 名稱 |

## SOA 繼承與 zone transfer

- 備援 zone 的 SOA 繼承自 root zone（serial 跟隨 root），因此 slave 能正確偵測變更。
- AXFR（TCP 全量 zone transfer）對 root zone 與 alias zone 都支援；既有 BIND slave 不需任何修改。
- NOTIFY 在啟動與 reload 後送往各 zone 的 NS record（可用 `--no-notify` 或 `options { notify no; };` 停用）。NOTIFY 目標 IP **只取自 in-zone glue record**，細節見[從 BIND 遷移](../migration.md)。

## 設定範例

```yaml
# shadowdns.yaml
aliases:
  example.com:          # root: fully loaded into memory
    - backup.example.com    # backup: a pointer to example.com
    - mirror.example.com
```

對 `www.backup.example.com` 的 A 查詢，回應與「載入了一份完整的 `backup.example.com` zone」完全相同 —— 但記憶體中只有 `example.com` 一份權威資料。

aliases 的完整規則（唯一性、self-alias 禁止、覆寫紀錄類型限制）見 [shadowdns.yaml](../configuration/shadowdns-yaml.md)。
