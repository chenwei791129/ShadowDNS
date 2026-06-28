## Context

DoH 端點（`internal/doh`）目前只接受 RFC 8484 wire-format：`GET /dns-query?dns=<base64url>` 或 `POST` 帶 `application/dns-message` body。查詢與回應都是 binary DNS 訊息，維運人員要用 curl 查一筆記錄必須先產生 wire query 再 base64url 編碼，腳本化成本高。

DoH handler 的查詢路徑是：HTTP 解碼 → 包一個 synthetic `dns.ResponseWriter`（`responseWriter`，持有 HTTP 連線的 peer TCP 位址）→ 呼叫共用的 `server.Server.ServeDNS` → 從 `responseWriter.msg`（完整 `*dns.Msg`）取回應寫出。關鍵在於既有 wire 路徑的 `serve()` 在 dispatch 前後做了三件 JSON 路徑同樣需要的事：（1）對 AXFR/IXFR 以 `isZoneTransferQuery` 判斷並回 REFUSED（避免單一 synthetic writer 只截到最後一個 transfer envelope）、（2）`ServeDNS` 後若 `responseWriter.packed` 為空則回 HTTP 500、（3）以最小 Answer TTL 設定 `Cache-Control`。view 選擇、ephemeral overlay、ratelimit、DNS cookies、ECS 全部發生在 `ServeDNS` 內，與傳輸協定無關。

既有 ECS 處理（`edns-client-subnet` capability）完全從 query 的 EDNS0 OPT 讀取 `dns.EDNS0_SUBNET` option；其分類器（`internal/dnsutil/ecs.go` 的 `ClassifyECS`）要求 SCOPE PREFIX-LENGTH 為 0、且 host bits 必須在 source prefix 之外全為 0，否則判為 ECSMalformed 並使 handler 回 FORMERR。ECS 只在 `--ecs-enable` 為真時生效；伺服器回填的 scope 等於 source netmask（authoritative 不縮放 scope）。回應由 `m.SetReply(req)` 組裝，會把 request 的 RD、CD bit 複製到回應。

本 change 在不動上述查詢路徑的前提下，於 HTTP 入口新增 `application/dns-json`（Google Public DNS / CloudFlare 事實格式，無 RFC）的解析與序列化，並比照 wire 路徑套用上述三項 dispatch 前後處理。

## Goals / Non-Goals

**Goals:**

- 讓維運人員用 `curl -H 'accept: application/dns-json' '.../dns-query?name=X&type=TXT'` + `jq` 零依賴查詢 ShadowDNS 權威記錄。
- JSON 路徑與 wire-format 路徑同源：重用 `ServeDNS`，回應內容（含 view 選擇、ephemeral、ECS、zone-transfer 拒絕、空回應 500、Cache-Control）與 wire / UDP / TCP 一致；name 大小寫亦比照 wire 保留以維持 case echo 一致。
- 支援 `edns_client_subnet` 查詢參數，讓單機可模擬任意網段以驗證 split-horizon / GeoIP。
- 純 additive：未要求 JSON 的請求行為與現狀完全相同。

**Non-Goals:**

- 不支援 JSON over POST（JSON 格式僅 `GET`，對齊 Google / CloudFlare）。
- 不支援 `do`（DNSSEC）—— ShadowDNS 非遞迴、不做 DNSSEC 驗證。
- 不支援 `ct`（content-type 覆寫參數）—— 以 `Accept` header 協商即可。
- 不新增任何 `shadowdns.yaml` 欄位或 CLI 旗標：JSON 格式隨 DoH 一併啟用。
- 不修改既有 ECS 邏輯；ECS 行為的真值來源仍是 `edns-client-subnet` capability。

## Decisions

### 以 Accept header 進行格式協商，?dns= 優先，JSON 僅支援 GET

`GET /dns-query` 的 handler 先判斷是否帶 `?dns=` 參數：若有，一律走現有 wire 路徑（不論 `Accept`），確保 wire 查詢不被誤導到 JSON 解析。若無 `?dns=` 且 `Accept` 列出 `application/dns-json`，走 JSON 路徑。`POST` 一律維持 wire（`application/dns-message`）；Accept 協商分支只存在於 GET handler，不加進 POST handler。

替代方案：以「出現 `?name=` 參數」作為觸發、或單純看 Accept 而不管 `?dns=`。否決——前者讓觸發與回應格式來源分裂；後者在 `Accept` 同時列出 wire 與 json（或帶 q-value）且帶 `?dns=` 時會把合法 wire 查詢誤判成 JSON 後 400。`?dns=` 優先的規則消除此歧義且實作單純。

### JSON 查詢參數解析：name 必填且保留大小寫、type 大小寫不敏感與 0–65535 數值範圍

- `name`：必填且非空（缺或空回 HTTP 400），正規化為結尾點的 FQDN，但**保留 on-wire 大小寫**（不轉小寫），使 JSON `Question` 與 owner echo 與 wire DoH 對相同名稱一致（內部查詢比對本來就 case-fold，保留大小寫不影響解析）。
- `type`：選填，預設 `A`；mnemonic **大小寫不敏感**（查表前轉大寫，因 miekg/dns 的型別表以大寫為 key）；數值以 16-bit 範圍解析（`strconv.ParseUint(v, 10, 16)`），超出 0–65535 回 HTTP 400（避免 `Atoi` + uint16 cast 的靜默截斷）。
- 解析結果組成單一 question 的 `*dns.Msg`，設定 RD bit 後交給共用查詢路徑。

### AXFR/IXFR 在 JSON 路徑比照 wire 路徑一律 REFUSED

JSON 路徑在 dispatch 前比照 wire `serve()` 以 `isZoneTransferQuery` 判斷 AXFR/IXFR，命中則回 REFUSED（JSON `Status` 5、HTTP 200、空 Answer），不進 streaming transfer 路徑。否則 type 解析接受任意 mnemonic（含 AXFR=252），單一 synthetic writer 只會截到最後一個 transfer envelope，產生損毀回應並繞過 wire 路徑的 transfer 拒絕語意。

### edns_client_subnet 遮罩 host bits 後注入 EDNS0_SUBNET option 重用既有 ECS 路徑

JSON 路徑把 `edns_client_subnet` 解析為網段，**先以 source prefix 遮罩 host bits**（對齊 `ClassifyECS` 對 host bits 必須為 0 的要求），再建為 `SourceScope=0` 的 `dns.EDNS0_SUBNET`（`Family`/`SourceNetmask` 依參數）以 `SetEdns0` 掛到送入 `ServeDNS` 的 `*dns.Msg`。省略 prefix 時 IPv4 預設 /24、IPv6 預設 /56。未遮罩會使 `ClassifyECS` 判 ECSMalformed → handler 回 FORMERR（HTTP 200、Status 1），與使用者預期不符。應重用 `internal/dnsutil/ecs.go` 既有的 family/mask 邏輯（必要時將其匯出）以免重複實作。

當 binary 未開 `--ecs-enable` 時，注入的 option 由 `ServeDNS` 忽略（與 wire 查詢攜帶 ECS 但未啟用時行為相同），JSON 回應不回填 ECS scope。

替代方案：在 JSON 層另寫一套 view/geo 選擇。否決——會複製 ECS 邏輯、破壞「三種傳輸同源」不變量。

### JSON 回應對齊 Google Public DNS schema：RD 為真、CD 為偽、data 剝除 header、Cache-Control 與 ECS scope echo

由 `responseWriter.msg` 序列化為 JSON 物件：`Status`（DNS rcode 數值）、`TC`、`RD`、`RA`、`AD`、`CD`（取自回應 header bits）、`Question`（`name` + 數值 `type`）、`Answer`（每筆 `name`、數值 `type`、`TTL`、`data`）。`RD` 因 dispatch 的 query 已設 RD 而為真；`CD` 一律為偽（`cd` 參數不映射到 `req.CheckingDisabled`，故 `SetReply` 不會把 CD 帶進回應）。`data` 為 RDATA presentation format，**以剝除 record header 的方式取得**（非以空白切割，使 SOA/MX 等多欄位 RDATA 不被截斷）。當回應 OPT 含伺服器回填的 `EDNS0_SUBNET` 時附 `edns_client_subnet` 欄位，格式 `<network>/<source-prefix>/<scope-prefix>`，其中 scope-prefix 等於伺服器套用的 source prefix（此 authoritative server 不把 scope 縮放到 geo 邊界，故 scope 為 source 的 echo）。回應 `Content-Type` 為 `application/dns-json`，並比照 wire 路徑以最小 Answer TTL 設定 `Cache-Control: max-age=N`。JSON 欄位順序與空白不受約束，僅欄位名稱、型別與值具規範性。

### malformed 查詢回 HTTP 400、內部無回應回 HTTP 500、DNS 層結果一律 HTTP 200

請求本身無效（缺/空 `name`、`type` 或 `edns_client_subnet` 無法解析）回 HTTP 400 + 純文字錯誤。成功送入查詢路徑後，DNS 層結果（含 `REFUSED`、`NXDOMAIN`、空答）一律 HTTP 200，rcode 反映於 JSON `Status`。若 dispatch 後 `responseWriter` 未捕獲任何回應訊息（內部失敗），比照 wire `serve()` 的空捕獲守衛回 HTTP 500，而非輸出誤導性的空成功物件。

### cd 容忍忽略且不設 CD bit，do 與 ct 不支援

`cd` 參數若出現則接受但忽略（不影響查詢、不報錯，且**不**設定 `req.CheckingDisabled`，使回應 `CD` 維持為偽），因 ShadowDNS 非遞迴、無 DNSSEC 驗證可關閉。`do` 與 `ct` 不解析；出現時忽略，不觸發 400（與 Google 對不適用參數的寬容一致）。

## Implementation Contract

- **Behavior**：對啟用 DoH 的 ShadowDNS 送無 `?dns=`、`Accept: application/dns-json` 的 `GET /dns-query?name=<fqdn>&type=<type>`，回 HTTP 200 + `Content-Type: application/dns-json`，body 為含 `Status`/`Question`/`Answer[]` 的 JSON（`RD` 為真、`CD` 為偽），並帶以最小 Answer TTL 為界的 `Cache-Control`。`Answer[].data` 為剝除 header 的 RDATA presentation format。附 `edns_client_subnet=<ip>/<prefix>` 時，host bits 經遮罩後 view/geo 依該網段選擇，且當 ECS 啟用時回應含 `edns_client_subnet` scope（scope-prefix 等於 source-prefix）。帶 `?dns=` 或非 JSON `Accept` 的請求行為與現狀完全相同。`AXFR`/`IXFR` type 回 REFUSED。
- **Interface / data shape**：端點 `GET /dns-query`；查詢參數 `name`（必填非空）、`type`（預設 `A`，大小寫不敏感 mnemonic 或 0–65535 數值）、`edns_client_subnet`（`<ip>[/<prefix>]`）、`cd`（容忍忽略、不設 CD）。回應 JSON 欄位 `Status`(int)、`TC`/`RD`/`RA`/`AD`/`CD`(bool)、`Question`(`[{name,type}]`)、`Answer`(`[{name,type,TTL,data}]`)、選填 `edns_client_subnet`(string)；欄位順序不限。
- **Failure modes**：缺/空 `name` → 400；`type` 非 mnemonic 且非 0–65535 數值（如 `65537`、`notatype`）→ 400；`edns_client_subnet` 無法解析 → 400；`AXFR`/`IXFR` → HTTP 200 + Status 5（REFUSED）；DNS 層 `REFUSED`/`NXDOMAIN`/空答 → HTTP 200；dispatch 後無捕獲回應 → HTTP 500。
- **Acceptance criteria**：新增 `internal/doh/dnsjson_test.go`，涵蓋（1）Accept 協商與 `?dns=` 優先、（2）name 大小寫保留、type 大小寫不敏感與數值上界 400、（3）AXFR/IXFR → REFUSED、（4）`edns_client_subnet` host-bit 遮罩與 `EDNS0_SUBNET` 注入、（5）JSON schema 序列化（A/AAAA/TXT/CNAME/MX/SOA 的 `data`、RD=true、CD=false、Cache-Control、TXT example 以欄位語意而非 byte-exact 比對）、（6）malformed → 400 與空捕獲 → 500。既有 `internal/doh` wire-format 測試全部維持綠燈。手動驗證：對 ns2 以 curl + jq 查一筆 ephemeral TXT 取得預期值。
- **Scope boundaries**：**In scope** —— `/dns-query` 的 `GET` + `application/dns-json` 協商（`?dns=` 優先）、`name`/`type`/`edns_client_subnet` 解析、host-bit 遮罩與 ECS option 注入、AXFR/IXFR 拒絕、JSON 序列化（RD/CD/Cache-Control/scope echo）、400/500 錯誤語意、手冊更新。**Out of scope** —— JSON over POST、`do`/DNSSEC、`ct`、新增 config 欄位或 CLI 旗標、改動 `edns-client-subnet` 既有邏輯、wire-format 路徑行為。

## Risks / Trade-offs

- `application/dns-json` 無 RFC，Google 與 CloudFlare 有細節差異 → 以 Google Public DNS schema 為單一相容目標，並在 spec 用具體數值 example 鎖定欄位（欄位語意而非 byte-exact）；手冊註明相容對象與其非標準性。
- ECS 注入若未遮罩 host bits 會觸發 FORMERR（`ClassifyECS` 視為 malformed）→ 注入前一律以 source prefix 遮罩，並以測試覆蓋 host-bit-set 案例。
- RDATA presentation 格式化各型別易出錯（尤其多欄位的 SOA/MX 與 TXT 引號） → 以剝除 record header 的方式取得 RDATA（非空白切割），測試覆蓋 A/AAAA/TXT/CNAME/MX/SOA。
- Accept header 協商在多值 / q-value / 並存 `?dns=` 時易誤判 → 以「`?dns=` 優先、其餘看 Accept 是否列出 `application/dns-json`」固定行為並以測試鎖定。
- `edns_client_subnet` 與 `--ecs-enable` 的互動易被誤解（未開時靜默忽略） → 行為與 wire 查詢一致，design 與手冊明述「未啟用 ECS 時注入的 subnet 不生效、不回填 scope」。
- 回應 scope 等於 source（authoritative 不縮放）易被誤讀為 geo 邊界 → 手冊說明 scope-prefix 為 source 的 echo，僅證明 ECS 被接受並用於 view 選擇。
