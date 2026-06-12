## Context

ShadowDNS 對 CNAME 鏈的現行處理：root 查詢路徑由 `zone.FollowCNAME` 在同一 zone 內追鏈並把整條鏈（CNAME 中間記錄 + 最終記錄）放入回應；backup 查詢路徑由 `alias.finalizeBackupRRs` 追同一條鏈後再做 in-bailiwick / `rewrite_rdata_labels` 改寫輸出。兩條路徑都會把 zone 內部的中間名稱完整暴露在回應中。

營運需求是機密性：中間跳點名稱（內部 LB、pool 命名）不得洩漏到外部回應。需求討論已收斂出「統一收合規則」：消耗 zone 內 CNAME 前綴，只輸出最終結果（記錄、單條合成 CNAME、或 NODATA），owner 一律為 qname、TTL 取被消耗鏈最小值。

既有約束：
- 預設行為必須與 BIND 完全一致（相容性優先），故新行為必須 opt-in、預設 false。
- 熱路徑效能受 perf-guard 把關（QPS -5% / p99 +15% 門檻），flag 關閉時不得新增可量測成本。
- 設定載入沿用 `shadowdns.yaml` 的 aliases section 與 `BuildAliasMap` 查表模式（missing key 即 false）。

## Goals / Non-Goals

**Goals:**

- per-alias-group 開關 `collapse_cname_chain`（root 宣告、backup 無條件繼承），預設 false。
- flag 開啟時，root 與 backup 查詢的 CNAME 鏈一律收合：隱藏所有 zone 內中間名稱，回應 owner = qname（保留 on-wire case）、TTL = min(被消耗鏈含最終記錄)。
- 三種鏈尾形態的確定性行為：zone 內取得記錄 → 僅回最終記錄；鏈離開 zone（含深度上限耗盡）→ 單條合成 CNAME；zone 內走到底無該 qtype → NODATA 且不落入 wildcard 合成。
- 直查 `qtype=CNAME` 與中間名稱直查均套用同一規則。

**Non-Goals:**

- 跨 zone（view scope）收合：鏈指向同 view 其他已載入 zone 時一律視為出境（Scope B 已於討論中因語意複雜度否決，效能差異在量測噪音以下）。
- 名稱枚舉防護：不隱藏中間名稱的存在性（直查仍正常回應，僅收合其鏈）。
- apex ANAME / 業界 CNAME Flattening：本功能不讓 apex 承載 CNAME，README 比較表的「CNAME Flattening (Planned)」狀態不變。
- 無 backup members 的 root 單獨啟用：aliases entry 仍要求非空 `members`（維持既有 schema 驗證）。
- AXFR 收合：zone transfer 照常輸出真實記錄。
- MkDocs 手冊新頁面：本次僅更新 `packaging/shadowdns.yaml.example` 的欄位註解。

## Decisions

### D1: 收合查表以 root origin 為 key，handler 以 `match.RootZone` 查表

`BuildAliasMap` 新增第三個輸出 `CollapseFlags`（與 `AliasFlags` 並列宣告的具名型別 `type CollapseFlags map[string]bool`，missing-key-即-false 語意的文件掛在型別上；key 為 root origin 的 lookup-fold FQDN）。handler 端統一以 `alias.Match.RootZone` 查表 — root 查詢時 `RootZone == MatchedZone`，backup 查詢時 `RootZone` 即所屬 root，一張表天然涵蓋「backup 繼承 root 設定」的語意，不需要像 `AliasFlags` 那樣攤平到每個 backup key。

查表時機釘死在 D6 的收合接入點內部（手上已有 CNAME 記錄時才讀），不得提升到查詢 prologue —— root 路徑的 exact 命中（最熱的路徑）今天零 alias map 讀取，這個約束讓 flag off 的部署與所有未命中 CNAME 的查詢維持零新增 map 讀取，是「flag 關閉時不得新增可量測成本」約束的具體操作化。

替代方案：比照 `AliasFlags` 以 backup 為 key 攤平 — 被否決，因為 root 自身查詢也要收合，攤平後仍需第二張 root key 表，徒增一份狀態。

### D2: zone 層新增獨立收合追蹤函式，`FollowCNAME` 熱路徑零修改

於 `internal/zone/collapse.go` 新增 `func (z *Zone) CollapseCNAME(initial []dns.RR, qtype uint16) CollapseResult`（與 `FollowCNAME` 相同的 `*Zone` method receiver——逐跳需要 `z.Lookup` / `z.LookupWildcard` / `z.Origin`），與 `FollowCNAME` 採相同的逐跳規則（exact qtype → exact CNAME → wildcard qtype → wildcard CNAME，深度上限 `MaxCNAMEDepth`），但不累積鏈，而是回傳 typed outcome。

例外規則：當 `qtype == dns.TypeCNAME` 時，逐跳的 exact / wildcard **qtype 查詢步驟必須跳過**——CNAME 記錄永遠視為跳點、絕不作為 terminal records 回傳（否則直查 CNAME 時第二條 CNAME 會被當成 terminal 回傳，洩漏中間名稱），因此 qtype=CNAME 的 outcome 只可能是 `CollapseTail` 或 `CollapseNoData`。

```go
type CollapseOutcome int
const (
    CollapseRecords CollapseOutcome = iota // 鏈於 zone 內走到底取得記錄
    CollapseTail                           // 鏈離開 zone 或深度耗盡，Target 為第一個未解析名稱
    CollapseNoData                         // 鏈於 zone 內走到底但無該 qtype 資料
)
type CollapseResult struct {
    Outcome CollapseOutcome
    RRs     []dns.RR // Outcome==CollapseRecords 時的最終記錄。為 zone 內存放的原始切片，
                     // 呼叫端 MUST NOT 就地修改；owner 與 TTL 改寫一律發生在 dns.Copy 副本上
    Target  string   // Outcome==CollapseTail 時的合成 CNAME target（保留 zone 檔原始 case）
    MinTTL  uint32   // 被消耗鏈（含起點 CNAME 與最終記錄）的最小 TTL
}
```

理由：後處理 `FollowCNAME` 的回傳切片無法區分「NODATA 鏈尾」與「深度耗盡」（兩者都以 zone 內 CNAME 結尾），且 `FollowCNAME` 為效能敏感熱路徑，獨立函式讓 flag 關閉時的程式路徑一個 byte 都不變。中間跳點即使命中 wildcard 也不需要合成 owner（收合只取 RDATA 與 TTL），比 `FollowCNAME` 少一次 `copyWithOwner`。

替代方案：將 `FollowCNAME` 重構為共用 step 函式 — 被否決，動到熱路徑換取的重用量太小（追蹤迴圈約 30 行）。

### D3: 深度上限耗盡視同出境（`CollapseTail`）

深度預算定義：**消耗的 CNAME 記錄數上限為 `MaxCNAMEDepth`（8，含起點 CNAME）**；把 CNAME 的 target 解析成 terminal records 不計入預算。恰好消耗 8 條 CNAME 且第 8 條的 target 於 zone 內解析出記錄 → `CollapseRecords`；消耗完 8 條後 target 仍是未解析名稱（即存在第 9 條 CNAME 或迴圈）→ `CollapseTail`，合成 target 即該未解析名稱（第 8 條 CNAME 的 target），與出境形態統一為「未解析名稱成為 target」一條規則。

**與 `FollowCNAME` 的已知預算語意差異**：`FollowCNAME` 的迴圈是 `for range MaxCNAMEDepth - len(initial)`（迭代次數預算，每次迭代解析一個 target），鏈長 ≤ 7 條 CNAME 才會把 terminal records 追進回應；恰好 8 條的鏈在 flag-off 時回 8 條裸 CNAME、不解析 terminal。因此鏈長恰為 8 時 flag-on 比 flag-off 多解析一跳（回 terminal records 而非裸鏈）——此差異無害（仍符合隱藏中間名稱的目標，且 client 拿到的是更完整的答案），明文接受、不改 `FollowCNAME` 對齊。連帶地，2.1 的 `FollowCNAME` ↔ `CollapseCNAME` 終點一致性 parity 測試**只適用於鏈長 ≤ 7 的鏈**；鏈長 ≥ 8 由深度預算邊界測試單獨覆蓋，不納入 parity 不變式。

**迴圈的自指合成 CNAME**：zone 內迴圈於預算耗盡時，第 8 條消耗的 CNAME 的 target 可能正好等於 qname 自身（例：`a → b → a` 迴圈直查 `a`，消耗序列 a,b,a,…,b 的第 8 條 target 為 `a`），此時合成記錄為 owner == target 的自指 CNAME。這是迴圈配置錯誤下的已知 wire artifact，記載於 spec 邊界表；resolver 端自有追鏈上限會終止它，不另做特判。

理由：(a) 合法的超長鏈仍可由 client 續查解析，NODATA 則會默默斷線；(b) zone 內迴圈時 client 端的 resolver 自有 CNAME 追蹤上限會 SERVFAIL，不比現行（直接回 8 條迴圈記錄）更差；(c) 規則統一可減少 spec 與測試的分支。代價：深度耗盡時第 8 條 CNAME 的 target 名稱會暴露 — 8 跳以上的鏈屬病態配置，於 spec 中記載此邊界。

### D4: 收合的 NODATA 必須短路，不得 fall through 到 wildcard 合成

現行 NODATA 鏈尾會回傳非空鏈（呼叫端視為已答覆），收合後改回 NODATA 信號時，若依現行控制流會錯誤地繼續嘗試 wildcard 合成（qname 實際存在 CNAME，RFC 4592 不應套 wildcard）。因此：

- root 路徑：`handleRootQuery` 在收合點收到 `CollapseNoData` 時直接走 `negativeReply`。NODATA 的推導機制依起點而異但結論相同：exact CNAME 起點時 qname 的 `Records` entry 存在 → NODATA；wildcard CNAME 起點時 qname 無 entry，由 `negativeReply` 的 `HasWildcard` 分支推導出 NODATA。
- backup 路徑：既有 `ResolveExactNoCNAME` / `ResolveCNAMEFallback` / `ResolveWildcard`（及 wrapper `Resolve` / `ResolveExact`）**簽章一律不變**。新增 collapse 專用解析入口，鏡射三個既有階段（exact / CNAME fallback / wildcard），內部重用同一批 building blocks（`RewriteQName`、backup override 查詢、`(*zone.Zone).CollapseCNAME`、D5 的 RDATA-only 改寫 primitive），回傳 `(rrs []dns.RR, nodata bool)`，不變式：`nodata=true ⇒ len(rrs)==0`。`handleBackupQuery` 在 flag on 時各階段改走 collapse 入口（保持 ephemeral overlay 的插入順序），`nodata=true` 時跳過後續 fallback 直接 `negativeReply`（`backupZoneHasName` 既有推導同樣自然得出 NODATA）。

替代方案：直接擴充三個既有函式簽章（加 collapse 參數＋第二回傳值）— 被否決：wrapper `Resolve` / `ResolveExact` 連帶要改、二十餘個既有測試呼叫點機械翻修，且函式將有 8 個位置參數含兩個相鄰 bool（`rewriteRDATALabels`、`collapse`），誤置風險編譯器無法攔截。雙套路徑漂移的風險改由「collapse 入口重用既有 building blocks」與 2.1 的 `FollowCNAME` ↔ `CollapseCNAME` 終點一致性 parity 測試（鏈長 ≤ 7，見 D3）把關。

### D5: 回應組裝 — owner、case、TTL 與 backup 改寫的組合

- **Records**：每筆 terminal record **恰好一次** `dns.Copy`，owner=qnameOrig（on-wire case）與 TTL=`MinTTL` 都寫在副本上（`CollapseResult.RRs` 是 zone 存放切片，禁止就地修改）。root 路徑即既有 `rewriteWildcardOwner`（`internal/server/handler.go`）的 copy+owner 形狀再加 TTL 覆寫。backup 路徑**不走完整 `RewriteRR`**——其內部已做一次 `dns.Copy` 且 owner 改寫會被後續覆蓋成死工：改為自 `RewriteRR` 抽出 RDATA-only 改寫 primitive（共用同一個 RR type-switch，`RewriteRR` 改為呼叫該 primitive），對收合副本套用 RDATA 改寫後直接設 owner 為 backup namespace 的 qnameOrig。
- **Tail**：合成 `dns.CNAME{Hdr: {Name: qnameOrig, Ttl: MinTTL}, Target: result.Target}`。backup 路徑的 Target 直接用既有 `RewriteNameAnywhere`（`rewrite_rdata_labels=true`）/ `RewriteName`（false）改寫——與 `RewriteRR` CNAME 分支同一 dispatch；出境名稱通常為 no-op，templated CNAME 場景則維持與現行 stored-CNAME 改寫一致的結果。
- **TTL 例值**：`www 300 CNAME lb`、`lb 60 CNAME pool-a`、`pool-a 600 A 192.0.2.10` → 回 `www 60 A 192.0.2.10`（min(300,60,600)）。
- **meta-qtype**：ANY 等 meta-qtype 依 `Zone.Lookup` 既有語意在鏈尾查無記錄 → 統一規則自然得出 NODATA，不特判、不洩漏。

### D6: root 路徑的四個收合接入點

`handleRootQuery` 中 collapse flag 開啟時改道的位置（均為既有命中點，無新增查詢順序）：

1. exact match 且 `qtype==CNAME` 命中 CNAME 記錄（統一規則適用於直查 CNAME）。
2. CNAME fallback（`qtype!=CNAME`，exact CNAME 命中後原呼叫 `FollowCNAME` 處）。
3. wildcard exact 且 `qtype==CNAME` 命中 wildcard CNAME。
4. wildcard CNAME fallback。

四個接入點**全部路由到單一 server 層收合 helper**（讀 flag → `z.CollapseCNAME` → 三態回應組裝），三態邏輯只存在一份，避免四份手展開複本漂移。wildcard 起點（3、4）直接以原始 wildcard CNAME 切片餵入 helper——不先經 `rewriteWildcardOwner` 預複製，因為合成 owner 會被收合丟棄（收合只消耗 RDATA 與 TTL）。

Ephemeral TXT overlay 的查詢順序與優先權完全不變（overlay 仍在 exact 與 CNAME fallback 之間）。注意 overlay 只攔截「store 中有 live entry」的 TXT 查詢；無 entry 的 TXT 查詢照常進入 CNAME fallback 並收合——**TXT 不豁免於收合**。

### D7: 設定載入 — `collapse_cname_chain` 欄位與既有驗證鏈整合

`rawAliasGroup` 新增 `collapse_cname_chain` yaml 欄位；`UnmarshalYAML` 的 allowed-keys 集合與錯誤訊息同步加入該欄位名。`config.AliasGroup` 新增 `CollapseCNAMEChain bool`；`BuildAliasMap` 簽章改為回傳 `(AliasMap, AliasFlags, CollapseFlags, error)`，`CollapseFlags` 為 D1 所述的具名型別。`shadowdnscfg.Config` 與 `server.ServerState` 各新增 `CollapseFlags` 欄位，`server.BuildState` 簽章增加對應參數；production 呼叫點僅 `cmd/shadowdns/main.go` 兩處（啟動與 SIGHUP reload）——`cmd/shadowdns/prune_backup.go` 不呼叫 `BuildState`，無需修改。另有直接呼叫 `BuildState` 的測試檔需機械同步簽章：`internal/server/server_test.go`、`internal/server/build_test.go`、`cmd/shadowdns/main_test.go`、`test/integration/helpers_test.go`、`test/integration/reload_diff_test.go`、`test/integration/axfr_test.go`、`test/integration/listenon_test.go`、`test/integration/case_preservation_test.go`。SIGHUP 原子 reload 語意由既有 state snapshot 機制自然涵蓋。

## Implementation Contract

**可觀察行為**（以 `dig` 對照，zone 內容：`www 300 CNAME lb`、`lb 60 CNAME pool-a`、`pool-a 600 A 192.0.2.10`，alias group `example.com: {members: [example.net], collapse_cname_chain: true}`）：

| 查詢 | flag off（現行） | flag on |
|---|---|---|
| `www.example.com. A` | 3 條記錄（兩條 CNAME + A） | `www.example.com. 60 A 192.0.2.10` 單條 |
| `www.example.net. A`（backup） | 改寫後 3 條 | `www.example.net. 60 A 192.0.2.10` 單條 |
| `www.example.com. AAAA` | 兩條 CNAME（裸鏈） | NODATA（NOERROR + SOA） |
| `www.example.com. CNAME` | `www CNAME lb` | NODATA（鏈於 zone 內走到底） |
| 若 `pool-a 600 CNAME cdn.external-vendor.example.org.`，查 `www.example.com. A` | 3 條 CNAME 裸鏈 | `www.example.com. 60 CNAME cdn.external-vendor.example.org.` 單條 |
| `lb.example.com. A`（中間名稱直查） | CNAME + A | 收合後單條（owner = lb） |

**介面 / 資料形狀**：

- YAML：`aliases.<root>.collapse_cname_chain`（bool，預設 false；未知欄位錯誤訊息更新為 `expected one of: members, rewrite_rdata_labels, collapse_cname_chain`）。
- `config.BuildAliasMap` 回傳新增具名型別 `config.CollapseFlags`（`map[string]bool`，key = root origin lookup-fold，與 `AliasFlags` 並列宣告）。
- `func (z *zone.Zone) CollapseCNAME(initial []dns.RR, qtype uint16) CollapseResult`（形狀見 D2），MUST NOT panic；`qtype==CNAME` 時 outcome 僅 `CollapseTail` / `CollapseNoData`。
- `internal/alias` 既有 `Resolve*` 簽章不變；新增 collapse 專用解析入口（exact / CNAME fallback / wildcard 三階段），回傳 `(rrs []dns.RR, nodata bool)`，不變式 `nodata=true ⇒ len(rrs)==0`；`internal/alias/rewrite.go` 抽出 RDATA-only 改寫 primitive，`RewriteRR` 改為呼叫同一 primitive（外部行為不變）。
- `server.BuildState` 增加 collapse 查表參數；`ServerState.CollapseFlags` 供 handler 於收合接入點內以 `match.RootZone` 查詢（不提升至查詢 prologue）。

**失敗模式**：

- flag 未宣告 / false → 與現行行為 byte-level 一致（含回應記錄順序與 TTL）。
- 收合過程不產生新錯誤路徑：`CollapseCNAME` 對任何輸入不 panic；深度耗盡與出境同樣回 `CollapseTail`，不回 SERVFAIL。
- YAML 含未知欄位或型別錯誤 → 載入失敗（沿用既有 strict decoding 行為），啟動 fail-fast、SIGHUP reload 保留舊設定。

**驗收準則**：

- `make test`（race detector）與 `make lint` 全綠。
- 單元測試覆蓋：`internal/zone/collapse_test.go`（三種 outcome、min-TTL、深度預算邊界、qtype=CNAME 跳點規則、wildcard 中間跳點、迴圈、與 `FollowCNAME` 的終點一致性 parity）、`internal/shadowdnscfg/config_test.go`（欄位解析、未知欄位錯誤訊息）、`internal/config/aliases_test.go`（CollapseFlags 輸出）、`internal/server/handler_test.go`（root 四接入點 + NODATA 短路）、`internal/alias/override_test.go`（backup collapse 入口三階段 + 改寫組合）。
- 整合測試 `test/integration/cname_collapse_test.go` 重現上表全部六種查詢形態。
- flag off 回歸：既有測試全數不修改即通過（預設行為不變的機械證明）。
- 實作完成後依 CLAUDE.local.md 跑 perf-guard（touched `internal/server` / `internal/zone` / `internal/alias` 熱路徑，屬 must-run 類）。

**範圍邊界**：in scope = 上述查表、收合追蹤、root/backup 回應組裝、設定載入與接線、example config 註解；out of scope = Non-Goals 全項、metrics 新指標、querylog 欄位、transfer package 任何修改。

## Risks / Trade-offs

- [深度耗盡暴露第 8 跳名稱] → 8 跳以上屬病態配置；spec 明文記載此邊界，營運上以鏈長 < 8 為前提。
- [zone 內 CNAME 迴圈時 client 收到合成 CNAME 後反覆續查] → resolver 自身的鏈追蹤上限會終止；行為不比現行（回傳迴圈鏈）差；zone 檔迴圈本身是配置錯誤。
- [收合後回應 TTL 取 min，下游快取壽命縮短] → 與 Cloudflare/Google 收斂後的業界慣例一致（取 min 是正確語意），且僅影響 opt-in 的 group。
- [collapse 專用入口與既有解析路徑語意漂移（雙套路徑）] → collapse 入口強制重用既有 building blocks（`RewriteQName` / `CollapseCNAME` / RDATA primitive），並以 `FollowCNAME` ↔ `CollapseCNAME` 終點一致性 parity 測試把關（鏈長 ≤ 7 且於 zone 內收尾的鏈，flag-on 的 terminal 必須等於 flag-off 鏈尾；鏈長 ≥ 8 因兩者預算語意不同排除於 parity 之外，見 D3）。
- [`BuildState` 簽章變更牽動 8 個測試檔] → 純機械性呼叫形狀調整；以「flag off 時既有測試斷言零修改」為回歸底線。
- [flag 查表的熱路徑成本] → 查表只發生在收合接入點內（命中 CNAME 的查詢才讀一次 O(1) map）；flag off 部署與未命中 CNAME 的查詢零新增讀取，並以 perf-guard 實測背書。

## Migration Plan

純 opt-in 功能：部署新版後未改 YAML 的安裝行為不變。啟用 = 在 alias group 加 `collapse_cname_chain: true` 後 SIGHUP；回滾 = 移除欄位後 SIGHUP（舊版二進位看到未知欄位會拒載，故降版前須先移除欄位）。

## Open Questions

（無 — scope、鏈尾三形態、直查 CNAME、中間名稱、深度耗盡與 TTL 規則均已於需求討論定案。）
