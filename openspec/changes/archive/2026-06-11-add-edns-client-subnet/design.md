## Context

ShadowDNS 的 view 選擇目前完全以來源 IP 驅動：`ServeDNS` 從 ResponseWriter 取出 client IP，交給 `view.Matcher.Resolve` 對五種規則（any/country/ASN/IP/CIDR）做 first-match。EDNS0 OPT 的處理採「單次解析」設計——`parseQueryOpt` 在一次 `opt.Option` 迭代中擷取所有欄位（含 COOKIE），`attachOPT` 是回應端 EDNS 內容的唯一組裝點（COOKIE 以 `respCookie` 欄位傳遞）。遷移來源 BIND 不支援 ECS（9.13.0 移除實驗功能），因此預設行為必須與 BIND 一致。產業調查（docs/ecs-implementation-survey.md）結論：ECS 定位為 source-IP GeoIP 之上的 opt-in 增強層，實際受益流量主要來自 Google Public DNS。

## Goals / Non-Goals

**Goals:**

- 啟用後，查詢中合法的 ECS 位址用於 geo 類規則（country/ASN）的 view 選擇，提升經 resolver 轉送之查詢的 geo 精準度
- 回應符合 RFC 7871 authoritative 端契約：原樣 echo FAMILY / SOURCE PREFIX-LENGTH / ADDRESS，並寫回 SCOPE PREFIX-LENGTH
- 預設關閉：完全忽略查詢中的 ECS 且回應不帶 ECS option（RFC 7871 對未啟用伺服器的 MUST NOT 要求），行為與 BIND 完全一致
- hot path 不增加第二次 OPT 迭代，維持單次解析設計

**Non-Goals:**

- gdnsd 式 supernet/scope 合併最佳化（需對 GeoIP mmdb 反向查詢，留作後續獨立 change）
- ECS 用於 IP/CIDR ACL 規則（刻意排除——ECS 為 client 可偽造欄位，用於 ACL 即開啟 view-spoofing）
- AXFR/IXFR 路徑的 ECS 處理（zone transfer 為 ACL 性質，維持純來源 IP）
- query log 與 Prometheus metrics 的 ECS 欄位（ECS subnet 屬 PII 邊緣資料，記錄/保留政策與未來 per-query log 變更一併設計)
- shadowdns.yaml 設定項與 SIGHUP 熱重載 ECS 開關（採 CLI flag，process 級設定）
- 多 NS 部署一致性的自動偵測（屬部署 gate：正式對外啟用前同 zone 所有 NS 須一致支援 ECS，否則 poison resolver cache；同一 gate 並含「啟用後觀察權威端 QPS 變化」——見 Risks 的 echo-scope 條目；實驗階段僅 ns2 不受影響）

## Decisions

### ECS 解析併入 queryOpt 單次 OPT 迭代

`parseQueryOpt` 的既有 option 迴圈中以 type assertion 擷取第一個 `dns.EDNS0_SUBNET`（仿 COOKIE 的 first-wins 處理），存入 `queryOpt` 新欄位。無論開關狀態都擷取（一個指標賦值，成本可忽略），是否使用由 handler 依開關決定——避免把設定狀態下沉到純解析函式。

替代方案：獨立的第二次迭代——違反現有單次解析設計，hot path 多一輪迴圈，否決。

### 雙位址 Resolve：geo 規則用 ECS、ACL 規則用來源 IP

`view.Matcher.Resolve` 簽名由單一位址改為 `(srcIP, geoIP netip.Addr)`：`CountryRule`/`ASNRule` 以 geoIP 查 mmdb，`IPRule`/`CIDRRule`/`AnyRule` 以 srcIP 評估。未啟用 ECS 或查詢無合法 ECS 時，呼叫端傳入 geoIP = srcIP，行為與現行完全一致。zone transfer 路徑兩個參數都傳來源 IP。

替代方案：ECS 位址整體取代來源 IP——實作最簡單，但外部 client 可塞偽造 ECS 冒充內網 CIDR 命中受限 view，安全上不可接受。gdnsd 同樣只將 ECS 用於 geo 選擇。

### SCOPE 寫回採 echo source prefix length

回應 ECS option 的 SCOPE PREFIX-LENGTH 直接等於查詢的 SOURCE PREFIX-LENGTH。RFC 7871 允許 scope 等於 source；偏窄的 scope 造成 resolver 端 cache 碎片化，成本是雙邊的——resolver 多佔快取、cache miss 率上升後增加的查詢量則回打 ShadowDNS（詳見 Risks），但絕不會發生「過寬 scope 把 geo 特化答案快取給錯誤網段」的正確性問題。opt-out 查詢（source prefix length = 0）寫回 scope 0。

替代方案：計算覆蓋同一 view 的最大網段（gdnsd supernet 合併）——需要 mmdb 反向範圍查詢，工程量數倍，列為 Non-Goal 留待後續。

### ECS 驗證與 FORMERR 邊界行為

驗證分兩層，每類違規只歸屬一層（依 miekg/dns v1.1.72 `EDNS0_SUBNET.unpack` 實測行為切分）：

**Library 層（unpack 時、不受 --ecs-enable 影響、handler 看不到）**：FAMILY 非 0/1/2、FAMILY 0 但 SOURCE PREFIX-LENGTH 非 0、SOURCE PREFIX-LENGTH 或 SCOPE 超過 family 上限（IPv4 32／IPv6 128）→ 訊息解包失敗，`dns.Server` 自行回 FORMERR 且清空所有 section（無 OPT）。本段為現況描述（已對照 go.mod 釘住的版本實測），不是分類函式可依賴的不變式：依專案測試原則，library 層拒絕不在本專案測試範圍，依賴升級後行為可能改變且無測試把關。因此 dnsutil 分類函式必須是 total function——對列舉情境之外的任何輸入（非預期 FAMILY、超界 prefix 等）一律 default 判 malformed，mask 運算前自行 bounds check，正確性不依賴 library 不變式。這不是為了重現 library 驗證，而是 default-deny 的防禦邊界。另外 unpack 會把 ADDRESS 一律 zero-pad／截斷成固定長度，wire 上的 octet 數不可還原——「octet 數與 prefix 不符」在 handler 層無從判定，由 library 行為承擔。

**Handler 層（僅 --ecs-enable 時，置於 `internal/dnsutil` 分類函式）**，malformed 檢查優先於 opt-out 分類：

- 查詢中 SCOPE PREFIX-LENGTH 非 0 → FORMERR（RFC 7871 規定查詢端 MUST 設 0；採嚴格解釋）
- SOURCE PREFIX-LENGTH 之外有非零位（unpack 不做 mask，此項可判；prefix = 0 時整個 ADDRESS 都算 prefix 之外）→ FORMERR（RFC 7871 SHOULD，採納）。此情境 wire 可達：unpack 不檢查 option 長度與 prefix 的關係，FAMILY 1/2 + prefix 0 + 非零 ADDRESS 會原樣送達 handler；FAMILY 0 不受影響（unpack 一律把 address 歸零）
- 通過上述檢查且 SOURCE PREFIX-LENGTH = 0（client opt-out，FAMILY 0/1/2 皆可——FAMILY 0 + prefix 0 是 `dig +subnet=0` 的標準形式且會送達 handler，必須視為 opt-out 而非 malformed）→ 不以 ECS 選 view，回應 echo 該 option（保留原 FAMILY）且 scope = 0

**處理位置釘死**：ECS 驗證與 echo 設定緊接 COOKIE 區塊之後、CHAOS／AXFR dispatch／addrFromRemote 之前。意即：handler 可達的 malformed ECS 對所有通過前置檢查的查詢（含 AXFR/IXFR）一律 FORMERR；valid ECS 的 echo 適用於其後所有經 attachOPT 組裝的回應（NOERROR/NXDOMAIN/CHAOS 與 no-view 的 REFUSED）。更早的出口（NOTIMP、question-count FORMERR、BADVERS、malformed COOKIE 的 FORMERR）與 zone-transfer 串流回應、panic-recovery 的 SERVFAIL 不 echo——與 COOKIE 既有慣例一致（panic 冷路徑同樣丟棄 respCookie）。

Handler 層 FORMERR 沿用既有 `replyRcode` 路徑（帶 OPT echo、不帶 ECS option）。ECS 關閉時 handler 不做任何驗證——handler 可達的 malformed ECS 也照常忽略，等同 BIND（library 層拒絕則與開關無關，本來就存在）。

驗證與 echo-option 建構邏輯放 `internal/dnsutil`（新檔 ecs.go），不為此開新套件：邏輯量級約一個檔案，dnsutil 已是 DNS 雜項工具的歸屬地。

### 啟用開關 --ecs-enable flag、預設 false

cobra flag `--ecs-enable`（預設 `false`）→ `cmd/shadowdns/main.go` 既有的 CLI 選項 struct（--pprof-enable 所在的那個，注意不是 `internal/config` 的 named.conf OptionsBlock）→ 在 `server.NewServer` 之後設定 `Server` 的新導出布林欄位。與既有 server 行為開關（--pprof-enable 等）同慣例；部署上只需改 ns2 systemd override。啟動時記一行 info log 標明 ECS 啟用／停用狀態（兩種狀態都記），且該行記在 --dry-run 早退之前，使 dry-run 輸出也可確認 ECS 狀態。

替代方案：shadowdns.yaml 設定項——該檔目前僅放資料面設定（aliases、ephemeral_api），且引入熱重載語意問題；否決。

### 文件更新：功能比較表 Planned → 支援

README.md 與 docs/index.md、docs/index.zh.md 的 BIND 功能比較表中 ECS 列由 Planned 改為已支援（標注 opt-in，預設關閉），README.md 計畫功能清單中的 ECS 項目同步移除或改寫。

## Implementation Contract

**可觀察行為：**

- `--ecs-enable` 未指定（預設）：對任何查詢（含帶 ECS 者）的回應一律不含 ECS option，view 選擇純依來源 IP——與現行為位元級一致
- `--ecs-enable` 指定時：
  - 查詢帶合法 ECS（source prefix > 0）→ country/ASN 規則以 ECS 位址評估；回應 OPT 含 ECS option，FAMILY/SOURCE PREFIX-LENGTH/ADDRESS 與查詢相同、SCOPE = SOURCE PREFIX-LENGTH
  - 查詢帶 opt-out ECS（source prefix = 0）→ view 選擇用來源 IP；回應 echo ECS、SCOPE = 0
  - 查詢帶 handler 可達的 malformed ECS（非零 query SCOPE、prefix 外非零位）→ FORMERR，回應含 OPT 但不含 ECS option；wire 層違規（FAMILY 非 0/1/2、prefix/scope 超上限）由 library 在解包時拒絕（FORMERR、無 OPT、與開關無關）
  - 查詢不帶 ECS → 回應不含 ECS option，行為同預設
  - IP/CIDR 規則在任何情況下都以來源 IP 評估
  - AXFR/IXFR 與 REFUSED（無 view 命中）路徑不受 ECS 影響 view 判定；早於 ECS 處理點的出口（NOTIMP、question-count FORMERR、BADVERS、malformed COOKIE）與 panic-recovery SERVFAIL、transfer 串流回應不 echo ECS

**介面/資料形狀：**

- `internal/dnsutil` 新增 ECS 驗證函式：輸入 `*dns.EDNS0_SUBNET`，輸出分類（valid / opt-out / malformed）與 geo 查詢用 `netip.Addr`；以及回應 echo option 建構函式（輸入查詢 option 與 scope 值）
- `view.Matcher.Resolve(srcIP, geoIP netip.Addr) string`——兩個呼叫點（一般查詢、zone transfer）同步更新
- `queryOpt` 結構新增 ECS 查詢指標欄位與回應 ECS 欄位（仿 respCookie 模式），`attachOPT` 在回應 OPT 中 append
- `cmd/shadowdns/main.go` 註冊 `--ecs-enable` flag 於既有 CLI 選項 struct，並於 `server.NewServer` 之後設定 `server.Server` 的新導出布林欄位（不動 `internal/config` 的 OptionsBlock，也不經 `internal/server/build.go`）

**失敗模式：**

- handler 可達的 malformed ECS → FORMERR（僅啟用時）；ECS 關閉時 handler 對其沉默忽略（刻意，等同 BIND 行為）。library 層 unpack 拒絕不受開關影響，屬既有行為
- ECS 位址查 mmdb 無結果 → country/ASN 規則 no-match（沿用現有 no-match-not-error 語意），不 fallback 回來源 IP 重查 geo 規則

**驗收標準：**

- `make test` 全綠（race detector）；新增單元測試覆蓋：dnsutil 驗證分類（合法 v4/v6、opt-out、各 malformed 類型）、Matcher 雙位址規則路由、handler 端到端（啟用/停用 × 合法/opt-out/malformed/無 ECS 的回應 option 與 rcode 斷言）
- `make lint`、`make smoke` 通過
- 手動驗證（使用者執行）：對部署於測試主機的實例以 dig +subnet 確認啟用前後行為

**範圍邊界：**

- in scope：上述 handler/view/dnsutil/flag/文件變更
- out of scope：Non-Goals 全部項目；既有 COOKIE、RRL、metrics、querylog 行為不得改變

## Risks / Trade-offs

- [echo-scope 策略造成 resolver 端 cache 碎片化（每個 /24 各自快取），且成本不只在 resolver 端：cache miss 率上升代表增加的查詢量回打 ShadowDNS，加上 Google 一旦偵測到正確 echo 就會對其流量普遍帶 ECS，正式啟用後權威端 QPS 可能明顯放大（量級約與活躍 client /24 數成正比）] → 實驗階段僅 ns2、風險可控；正式對外啟用前的部署 gate 除多 NS 一致性外，須加入「啟用後觀察權威端 QPS 變化」；後續可用 supernet 合併 change 降低碎片化，回應格式不變、可獨立演進
- [雙位址 Resolve 的兩個參數同為 netip.Addr，編譯器只擋 arity 不擋引數對調——呼叫端寫成 Resolve(geoIP, srcIP) 會讓 ACL 規則改吃 ECS 位址，靜默打穿防 view-spoofing 的核心目標且 matcher 單元測試照樣全綠] → handler 端到端的「偽造 ECS 不得命中 CIDR 受限 view」測試是對調的 load-bearing 防線，必須存在且不可刪；matcher 測試另覆蓋兩種位址分流
- [現行 openspec/specs/view-matcher/spec.md 含重複的 requirement 標題區塊（歷史 archive 殘留），heading-keyed 的 MODIFIED 合併可能只改到其中一份] → archive 階段執行 sync 時人工確認重複區塊一併更新／去重，避免合併後主 spec 自相矛盾
- [非零 query scope 採嚴格 FORMERR，極少數不合規 client 可能被拒] → 該行為僅在 opt-in 啟用後出現；如實際互通性出問題可降級為「視為 0」，屬一行決策變更
- [同 zone 多 NS 不一致支援 ECS 會 poison resolver cache] → 實驗階段僅 ns2 部署無此問題；已列為正式啟用前的部署 gate（Non-Goal 註明）
- [ECS 為 PII 邊緣資料] → 本變更不記錄 ECS 至任何 log；未來 per-query log 設計時一併處理保留政策
- [hot path 效能回歸] → 解析僅多一個 type assertion 分支；依 Perf-Guard 流程於實作後做 baseline vs post-change 基準比對
