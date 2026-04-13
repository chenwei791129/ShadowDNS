## Implementation Discipline

本 change 於 apply 階段套用以下 spectra 設定（`.spectra.yaml`）：

- **`tdd: true`** — 每個實作 task 以 Red-Green-Refactor 推進。spec 中每個 `#### Scenario` 即為 failing test 的驗收條件：**先依該 scenario 寫失敗測試，再寫最小實作令測試通過，最後重構**。Bug fix 情境需先以失敗測試重現問題。
- **`audit: true`** — 在設計 API、預設值、接受外部輸入時，以 sharp-edges discipline 審視：Scoundrel lens（惡意輸入能否觸發崩潰／放大攻擊？）、Lazy Developer lens（預設值是否安全？）、Confused Developer lens（型別混淆與無聲失敗風險？）。
- **`parallel_tasks: true`** — 同 group 內標註 `[P]` 的 task 可由 apply orchestrator 平行 dispatch，只要各自目標檔案互斥且不依賴同 group 內未完成的 task。

## 1. 專案初始化

- [x] 1.1 建立 Go module 與目錄骨架（`cmd/shadowdns`、`internal/{config,zone,view,alias,server,transfer}`、`examples/`、`testdata/`）
- [x] 1.2 宣告依賴：`github.com/miekg/dns`、`github.com/oschwald/maxminddb-golang`、`gopkg.in/yaml.v3`（對應 Decision：選用 miekg/dns 作為 DNS 協定層；選用 MaxMind mmdb 做 GeoIP Country + ASN）
- [x] 1.3 建立 `cmd/shadowdns/main.go`：flag 解析（`--named-conf`、`--aliases`、`--listen`）、SIGTERM/SIGINT 處理、log 初始化

## 2. Config loader

- [x] [P] 2.1 實作 Parse named.conf options block — 擷取 directory、geoip-directory、listen-on、allow-transfer、recursion、minimal-responses、version、hostname、transfer-format（目標檔：`internal/config/options.go`）
- [x] 2.2 實作 Parse view and zone declarations from master.zones — 保留 view 宣告順序與 view 內 zone、rule 順序；支援 `include` 指令（目標檔：`internal/config/zones.go`）
- [x] [P] 2.3 實作 Parse match-clients rule syntax — 支援 `geoip country`、`geoip asnum "AS#### ..."`、單 IP、CIDR、`any`；允許單行多規則（目標檔：`internal/config/match.go`）
- [x] 2.4 實作 Warn when non-last view uses `any` — 偵測 any 規則出現在非最後一個 view 時輸出 warning log（依賴 2.2，同檔 `internal/config/zones.go`）
- [x] [P] 2.5 實作 Parse aliases.yaml — 產出 `map[backup]root`；拒絕重複 backup、自我 alias；檔案不存在時 graceful fallback（目標檔：`internal/config/aliases.go`）
- [x] 2.6 實作 Reject unsupported named.conf directives at startup — 偵測 `type slave`、`type forward`、`dnssec-enable`、`allow-update` 等不支援指令並 fatal（依賴 2.2，同檔 `internal/config/zones.go`）

## 3. Zone parser

- [x] 3.1 實作 Parse RFC 1035 master zone files — 包裝 `dns.ZoneParser`；支援 `$TTL`、`$ORIGIN`、`@`、多行 `(...)`、`;` 註解；不以副檔名判斷（目標檔：`internal/zone/parser.go`）
- [x] 3.2 實作 Build an in-memory zone structure — zone origin、SOA、`map[ownerName][]dns.RR` 索引、預設 TTL 套用與 per-record 覆寫（目標檔：`internal/zone/zone.go`）
- [x] 3.3 實作 Classify zones as root or backup override at load time — 依 alias map 判定角色；backup 僅保留 TXT/MX/SRV 並 warn 丟棄其它型別；允許 backup 無對應檔案（目標檔：`internal/zone/classify.go`）
- [x] 3.4 實作 Fail loudly on malformed zone data — 拒絕 out-of-zone owner name 與未知 RR type，錯誤訊息含檔案路徑與行號（對應 Decision：Zone 資料結構：map-per-view-per-zone + alias 指標；同檔 `internal/zone/parser.go`）

## 4. View matcher

- [x] 4.1 實作 Resolve client IP to a view using first-match semantics — first-match short-circuit；無匹配時回傳明確 sentinel（目標檔：`internal/view/matcher.go`）
- [x] [P] 4.2 實作 Evaluate country match via MaxMind GeoLite2-Country — 載入 `.mmdb`、case-insensitive 比對、mmdb 無記錄視為 no-match（目標檔：`internal/view/geoip_country.go`）
- [x] [P] 4.3 實作 Evaluate ASN match via MaxMind GeoLite2-ASN — 從 rule 字串抽出 AS 數字比對（目標檔：`internal/view/geoip_asn.go`）
- [x] [P] 4.4 實作 Evaluate IP and CIDR rules without external lookup — 使用 `netip.Prefix` / `netip.Addr`，不經 GeoIP（對應 Decision：View match 規則引擎；目標檔：`internal/view/netmatch.go`）
- [x] 4.5 實作 Fail startup when GeoIP databases are missing or unreadable — 兩個 mmdb 任一缺失即 fatal 並印路徑（對應 Decision：外部 MaxMind DB 的路徑；跨 4.2、4.3 的 loader 整合）

## 5. Alias resolver 與 in-bailiwick rewrite

- [x] [P] 5.1 實作 Detect whether a query target is a backup zone — 最長後綴 zone 匹配 + alias map 查詢，回傳角色與 root 目標（目標檔：`internal/alias/detect.go`）
- [x] 5.2 實作 Rewrite query name from backup to root before lookup — 依後綴替換且 case-insensitive（目標檔：`internal/alias/rewrite.go`）
- [x] 5.3 實作 Rewrite owner names in the answer to the original backup zone — apex 與子層均正確替換（同檔 `internal/alias/rewrite.go`）
- [x] 5.4 實作 Apply in-bailiwick rewrite to record values — 依 RDATA 型別處理 CNAME/NS/MX/PTR/SRV/SOA name 欄位；A/AAAA/TXT RDATA 不動（對應 Decision：in-bailiwick rewrite 規則；同檔 `internal/alias/rewrite.go`）
- [x] [P] 5.5 實作 Merge backup overrides for TXT, MX, and SRV — override 存在時覆蓋 root 繼承值，否則 fallback 至 rewrite 後 root 記錄（目標檔：`internal/alias/override.go`）
- [x] [P] 5.6 實作 Inherit SOA fields from root zone when serving backup SOA — serial/refresh/retry/expire/minimum verbatim，MNAME/RNAME 套 rewrite（對應 Decision：SOA serial 繼承策略；目標檔：`internal/alias/soa.go`）

## 6. DNS server

- [x] [P] 6.1 實作 Listen for DNS queries on UDP and TCP port 53 — 啟動 UDP + TCP 雙 listener；TCP 支援長度前綴（目標檔：`internal/server/listener.go`）
- [x] 6.2 實作 Operate in authoritative-only mode — AA=1、RA=0、不 recurse（目標檔：`internal/server/handler.go`）
- [x] 6.3 實作 Answer queries using view, alias, and zone data — 串接 view-matcher → alias-resolver → zone lookup → response encode（同檔 `internal/server/handler.go`）
- [x] 6.4 實作 Produce SOA in authority section for NXDOMAIN and NODATA — authority TTL 取 SOA TTL 與 SOA minimum 較小者（同檔 `internal/server/handler.go`）
- [x] 6.5 實作 Serve the zone SOA on explicit SOA query — apex SOA query 從 answer section 回應（同檔 `internal/server/handler.go`）
- [x] [P] 6.6 實作 Hide server identity — 對 `version.bind`、`hostname.bind`、`id.server` 回 REFUSED 或空 TXT（目標檔：`internal/server/chaos.go`）
- [x] 6.7 實作 Return minimal responses by default — 預設不填 additional section glue，除非 referral 需要（同檔 `internal/server/handler.go`）
- [x] 6.8 實作 Handle malformed or unsupported queries without crashing — FORMERR / NOTIMP / REFUSED 分類處理；recover panic 防止 process 退出（同檔 `internal/server/handler.go`）

## 7. Zone transfer

- [x] 7.1 實作 Serve AXFR over TCP for loaded zones — TCP only；SOA → records → SOA 格式（對應 Decision：AXFR 實作：stream rewrite；目標檔：`internal/transfer/axfr.go`）
- [x] 7.2 實作 Stream alias-zone AXFR with rewritten records — 即時讀 root zone + 套 rewrite；override 取代對應 (owner, type)（同檔 `internal/transfer/axfr.go`）
- [x] [P] 7.3 實作 Enforce allow-transfer ACL — 來源 IP 不在清單一律 REFUSED（目標檔：`internal/transfer/acl.go`）
- [x] [P] 7.4 實作 Send NOTIFY on zone content change — 啟動後與 reload 後送；退避重試 1s/2s/4s；排除 SOA MNAME target（目標檔：`internal/transfer/notify.go`）
- [x] 7.5 實作 Deny IXFR by responding with full AXFR — IXFR 查詢直接 fallback 到 AXFR stream 格式（同檔 `internal/transfer/axfr.go`）
- [x] 7.6 實作 Refuse unknown or unsupported transfer types — 未載入的 zone 一律 REFUSED（同檔 `internal/transfer/axfr.go`）

## 8. 整合測試

- [x] 8.1 建立 testdata fixtures：mini `named.conf`、`master.zones`、多個 `.fwd` 檔（含 root + backup）、`aliases.yaml`、mock mmdb（目標目錄：`testdata/`）
- [x] [P] 8.2 撰寫整合測試：對每個 view 發起代表性查詢（A / AAAA / CNAME / NS / MX / TXT / SOA），比對 answer/authority 完整欄位（目標檔：`test/integration/query_test.go`）
- [x] [P] 8.3 撰寫整合測試：對 backup zone 各種型別查詢，驗證 owner + in-bailiwick rewrite 正確；CNAME 指向第三方時 target 保留（目標檔：`test/integration/backup_test.go`）
- [x] [P] 8.4 撰寫整合測試：NXDOMAIN / NODATA authority section SOA 正確（含 backup zone）（目標檔：`test/integration/negative_test.go`）
- [x] [P] 8.5 撰寫整合測試：AXFR 對 root zone 與 backup zone 的 stream 內容；allow-transfer ACL 生效（目標檔：`test/integration/axfr_test.go`）

## 9. 文件與遷移準備（對應 Migration Plan、Risks / Trade-offs）

- [x] [P] 9.1 撰寫 `README.md`（英文）：安裝、啟動、設定檔對應、與 BIND 的差異清單
- [x] [P] 9.2 撰寫 `docs/migration.md`（正體中文）：四階段切換步驟（Phase 0–3）、rollback 策略、監控檢核清單
- [x] [P] 9.3 在 production 設定檔子集上執行啟動煙霧測試（加 `--dry-run` flag 僅載入不監聽），measure 記憶體使用量並紀錄於 `docs/benchmark.md`
- [x] 9.4 新增 `Context`、`Goals / Non-Goals`、`Open Questions` 對齊追蹤：在 README 對應段落連回 design.md，確保 `view-other` match-clients 順序、ASN 字串格式等 Open Questions 於部署前覆核（依賴 9.1）
