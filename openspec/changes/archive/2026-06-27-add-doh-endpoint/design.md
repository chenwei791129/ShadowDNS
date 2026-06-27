## Context

ShadowDNS 是以 miekg/dns 建構的權威 DNS 伺服器，目前只在 UDP/TCP port 53 提供服務。維運團隊在受限內網（無 internet access、僅經 squid proxy 出網）修改 zone 並 reload 後，需要一條可走標準 HTTPS（TCP/443）的查詢驗證管道。

現況關鍵事實：

- 核心查詢處理函式為 `Server.ServeDNS(w dns.ResponseWriter, req *dns.Msg)`，透過 `dns.ResponseWriter` 回寫結果，並以 `w.RemoteAddr()` 推導來源 IP（驅動 view 選擇與 DNS cookies）。它不是 `*dns.Msg -> *dns.Msg` 的純函式。
- 既有 HTTP 服務（ephemeral TXT API、Prometheus metrics）皆使用標準函式庫 net/http，無任何框架。
- 全專案目前沒有任何 TLS 程式碼，本 change 是第一個 TLS 使用者。
- 既有 SIGHUP reload 涵蓋 config／zones／GeoIP／rate-limit／query-log，但 runOptions（CLI flag）不重讀。
- Let's Encrypt 對 IP 位址的憑證強制為 shortlived profile（160 小時／約 6 天），驗證僅允許 HTTP-01 或 TLS-ALPN-01（排除 DNS-01）。
- Go 內建 golang.org/x/crypto/acme 支援 IP identifier 與 HTTP-01，但不支援 ACME profile 選擇（OrderOption 僅 WithOrderNotBefore/NotAfter；提案 golang/go#73101 未進 release），因此無法取得 LE IP 憑證。

## Goals / Non-Goals

**Goals:**

- 提供符合 RFC 8484 的 DoH endpoint（路徑 /dns-query，GET 與 POST），讓通用 DoH client 查詢 ShadowDNS 權威 zone。
- 完整複用既有權威查詢路徑，DoH 僅作為新傳輸層，不改 UDP/TCP 行為與回應內容。
- 透過內嵌 ACME client 對 IP 位址自動取得與續簽 Let's Encrypt 憑證，免人工維護。
- DoH 設定納入 shadowdns.yaml 與 SIGHUP reload。

**Non-Goals:**

- 不提供遞迴解析。DoH 與 UDP/TCP 一致，只回本機 host 的 zone；非本機 zone 查詢回 REFUSED/SERVFAIL。
- 不實作 app 層的來源 IP ACL（443 的存取控制交由防火牆；維運團隊已決定不在應用層做白名單）。
- 不實作 TLS-ALPN-01 驗證（採 HTTP-01，使 443 不需對 LE 開放）。
- 不支援以網域（DNS-01）取得憑證；本 change 聚焦 IP 憑證情境。
- 不支援憑證以外部工具提供（手動 CertFile/KeyFile）的模式；本 change 採內嵌 ACME。
- 不支援以 X-Forwarded-For／Forwarded 標頭做 view 選擇（除非未來在 443 前引入受信任反向代理）；view 來源 IP 一律取 TCP 連線對端，client 子網以 ECS 表達。

## Decisions

### 以 synthetic dns.ResponseWriter 橋接 HTTP 並複用 ServeDNS

新增 internal/doh 套件，HTTP handler 將請求解碼為 `*dns.Msg` 後，包一個實作 `dns.ResponseWriter` 介面的合成物件，呼叫既有 `server.Server.ServeDNS`。合成 writer 的 `RemoteAddr()` 回傳 HTTP client 的真實 TCP 位址（使 view 選擇與 DNS cookies 正確運作），`WriteMsg` 與 `Write` 將封包擷取至緩衝區，再由 handler 寫回 HTTP body。

理由：ServeDNS 是 view 選擇、CNAME 合成、ephemeral 覆寫、cookies 等所有權威邏輯的單一入口。複用它可確保 DoH 回應與 UDP/TCP 完全一致，且不需改動 query path。合成 writer 因非 UDP，`dnsutil.IsUDP` 會回 false，自然套用 TCP 風格的「不截斷」行為，符合 DoH（HTTP/TCP framing）需求。

替代方案：將 ServeDNS 重構為純 `*dns.Msg -> *dns.Msg`。否決——影響面大、風險高，且 UDP 截斷與 cookies 等行為仍與傳輸耦合。參考實作為 CoreDNS 的 plugin/pkg/doh helper。

view 來源 IP 取 TCP RemoteAddr、不採信 X-Forwarded-For：合成 writer 的 `RemoteAddr()` 一律回傳 HTTP 連線的對端 TCP 位址，**不**解析 `X-Forwarded-For`／`Forwarded` 標頭。理由：(1) RFC 8484 未規範 client IP 傳遞，XFF 是正交的 HTTP 反向代理慣例；(2) 本 change scope 下 DoH client 直連 443、前方無受信任反向代理，RemoteAddr 即真實 client；(3) view 在 ShadowDNS 可能是資訊邊界（split-horizon），而 XFF 為 client 可控標頭，無條件採信會讓任何能連 443 者偽造來源、選擇任意 view，繞過防火牆這道唯一邊界。client 若要表達自身子網，原生途徑是既有的 ECS（EDNS Client Subnet，RFC 7871、`--ecs-enable`），跨 UDP/TCP/DoH 一致。僅當未來在 443 前引入受信任反向代理時，才以「trusted-proxy 白名單 + 取最右側不可信跳點」的方式加入 XFF 支援。

### RFC 8484 請求解碼與回應編碼

GET 解析 `?dns=` query 參數（base64url、無 padding）；POST 讀取 body。兩者 Content-Type 與回應 Content-Type 皆為 application/dns-message。回應的 HTTP cache 標頭（max-age）SHALL 不超過 Answer 區所有 RR 的最小 TTL。非 /dns-query 路徑或不支援的方法回對應 4xx。

理由：符合 RFC 8484 才能與通用 DoH client（curl、瀏覽器）互通。

### DoH 設定置於 shadowdns.yaml 的 doh 區塊並納入 SIGHUP reload

在 internal/shadowdnscfg 新增 `doh:` 區塊（比照既有 EphemeralAPIConfig 的 Listen + 驗證器模式），正規化欄位為：`doh.listen`（DoH HTTPS 綁定的 host:port）、`doh.acme.directory_url`（ACME 目錄 URL）、`doh.acme.ip`（要簽發的 IP）、`doh.acme.http01_listen`（HTTP-01 challenge responder 綁定的 host:port，須以 port 80 對公網可達）。沿用 rawConfig + KnownFields(true) 嚴格解析與 buildEphemeralAPI 風格的 buildDoH 驗證器，缺必要欄位即載入失敗並指名欄位。

reload 語意（重要，須與程式碼現況一致）：既有 `reload()` 會重新呼叫 shadowdnscfg.Load 重新解析整份 YAML，但**並未把 ephemeral_api 之類的服務設定在 reload 套用**（API server 在 run() 啟動一次、為 process-lifetime），且 DNS listener 在 reload 刻意不重綁（main.go 的 listen-address drift 明文「requiring a process restart」）。因此 DoH 採相同語意：SIGHUP 會重新驗證 `doh` 區塊並回報錯誤，但 `doh.listen` 與 `doh.acme.*` 的變更不在 reload 即時套用，而是記錄「需重啟」並維持啟動時的 listener。

理由：與既有 reload 的「重新解析但不重綁 listener」設計一致，避免在 reload 重綁造成停機；且不會建立 codebase 沒有先例的熱重綁機制。憑證輪替走獨立的 ACME 熱抽換（見下），與 SIGHUP 無關。不保留 app 層 Allow IP-ACL，443 存取由防火牆負責（已決定）。

### 不設定 ACME 帳號 email（無 contact）

`doh.acme` 不提供 email 欄位，ACME 帳號以「無 contact」註冊。

理由：(1) RFC 8555 §7.3 的 account `contact` 為選填，Let's Encrypt 接受無 contact 的帳號註冊；lego 的 `Registrar.Register` 預設送空 `Contact`，僅在 `GetEmail() != ""` 時才加 `mailto:`，故空 email 完全可行。(2) email 的唯一用途是憑證到期/政策通知信，而本情境為 ~6 天 shortlived 憑證、自動續簽，到期通知無實質意義。(3) 少一個必填欄位即少一處設定負擔與驗證面（符合 audit 的「移除非必要必填欄位」傾向）。

實作：`acmeUser` 仍需實作 lego `registration.User` 介面，故保留 `GetEmail()` 方法但回傳空字串；移除 `acmeUser.email` 欄位與對 `cfg.Email` 的依賴。設定層移除 `DoHACMEConfig.Email`、`rawDoHACME.Email` 與 `buildDoHACME` 的 email 必填檢查；因 `KnownFields(true)` 嚴格解析，設定若仍寫 `doh.acme.email` 會以「未知欄位」被拒（v0.x 實驗階段，可接受的 breaking config 變更）。

### TLS 憑證經 Let's Encrypt HTTP-01 與 shortlived profile 對 IP 自動簽發

採 HTTP-01 驗證：port 80 對公網開放、僅回應 /.well-known/acme-challenge/ 路徑的 challenge token（其餘 404）；port 443 提供 DoH，由防火牆限制來源。order 須帶 shortlived profile 並以 IP identifier 簽發。

理由：LE IP 憑證僅允許 HTTP-01 或 TLS-ALPN-01；採 HTTP-01 可讓 443 完全不需對 LE 的多視角驗證來源開放（LE 驗證來源 IP 不固定、不可白名單），維運團隊得以僅在防火牆放行企業出網 IP 至 443。

替代方案：TLS-ALPN-01（佔用 443、須對 LE 開放，與防火牆目標衝突）；DNS-01（IP 憑證不支援）；內部 CA／手動憑證（使用者已否決，要求公開信任的 IP 憑證）。

### 採用 go-acme/lego 作為 ACME 函式庫

內嵌第三方 ACME 函式庫，首選 go-acme/lego，需支援 (a) RFC 8738 IP identifier、(b) Let's Encrypt profile 選擇（shortlived）、(c) HTTP-01。實作前先以 spike 驗證所選 lego 版本三項皆支援；若不支援則評估 certmagic（caddy 的 IP+shortlived 支援尚未成熟）或 fork patch x/crypto。

理由：Go 內建 x/crypto/acme 不支援 profile 選擇，無法簽 LE IP 憑證。lego 為最成熟、功能最完整的 Go ACME client。

### 憑證熱輪替以 GetCertificate callback 搭配 atomic 指標

TLS 設定使用 `tls.Config.GetCertificate` callback，指向一個以 atomic 指標持有的目前憑證。ACME client 在背景以約 1/3 效期（~2 天）為節點自動續簽，成功後原子替換指標，無需重啟 listener 或斷線。續簽失敗須記錄錯誤並可由 metrics/log 觀察（避免在 6 天效期內無聲過期）。

理由：6 天短效憑證需頻繁續簽，熱輪替避免服務中斷；atomic 指標與既有 ServerState 的 lock-free swap 風格一致。

### Port 80 challenge responder 與 port 443 DoH 服務分離

port 80 與 port 443 為兩個獨立的 net/http server，皆比照 internal/api 的 graceful shutdown 模式，於主 context 取消時關閉。port 80 server 僅處理 ACME challenge，常駐執行（因續簽時程不可預測且 LE 為多視角驗證）。

理由：將公開的驗證面（80）與受防火牆保護的服務面（443）在程式與部署上清楚分離。

### DoH 查詢在 Prometheus metrics 以獨立 proto 標籤呈現

`proto` 標籤字串實際是在 internal/server/handler.go 內，以 `dnsutil.IsUDP(w)`（檢查 `w.LocalAddr()` 是否為 *net.UDPAddr）硬決定為 `"udp"`/`"tcp"` 後傳給 metrics.RecordRequest；internal/metrics 只存放被傳入的字串、本身不需改動。合成 DoH writer 因 LocalAddr 非 UDP，對 IsUDP 看起來與 TCP 相同，故**無法只靠 IsUDP 區分 DoH 與 TCP**。解法：讓合成 writer 暴露其傳輸（例如實作一個 `Protocol() string` 介面），handler 先以型別/介面斷言取得 `"doh"`，否則退回 IsUDP 的 udp/tcp 判定。

理由：DoH 與 TCP 是不同傳輸，分開計數才有運維意義；修改點在 handler.go 而非 metrics.go。

## Implementation Contract

**Behavior（運維者可觀察）:**

- 啟用 `doh:` 設定後，`curl` 對 `https://<IP>/dns-query` 以 GET（`?dns=<base64url>`）或 POST（`application/dns-message`）查詢本機 zone 的記錄，會得到與相同查詢走 UDP/TCP 53 一致的 DNS 回應。
- 查詢非本機 zone 時，DoH 回應的 RCODE 與 UDP/TCP 相同（REFUSED 或 SERVFAIL），不做遞迴。
- 未設定 `doh:` 區塊時，不啟動任何 DoH／ACME／port-80 listener。
- 首次啟動會經 ACME HTTP-01 對設定的 IP 取得 LE 憑證；憑證於接近到期前自動續簽且不中斷服務。

**Interface / data shape:**

- HTTP：`GET /dns-query?dns=<base64url-no-pad>` 與 `POST /dns-query`（body 為 wire-format DNS）。請求與回應 Content-Type 皆為 `application/dns-message`。回應含 `Cache-Control: max-age=<n>`，n ≤ Answer 區最小 TTL。
- ACME challenge：`GET http://<IP>/.well-known/acme-challenge/<token>` 回 challenge 回應；port 80 其他路徑回 404。
- 設定：shadowdns.yaml 新增 `doh:` 區塊；缺少必要欄位時，設定載入 SHALL 失敗並回報明確錯誤（沿用 KnownFields 嚴格解析）。

**Failure modes:**

- 無法解析的 DoH 請求（壞的 base64url、空 body、非 application/dns-message）回 HTTP 400。
- 不支援的方法回 405；非 /dns-query 路徑回 404。
- ACME 取證／續簽失敗：記錄錯誤、以 metric 或 log 顯露；既有憑證在到期前仍續用，不因單次續簽失敗立即中斷。

**Acceptance criteria:**

- internal/doh 單元測試涵蓋：GET/POST 解碼、錯誤輸入回 400/404/405、cache 標頭不超過最小 TTL、合成 writer 的 RemoteAddr 驅動正確 view、DoH 回應與等價 UDP/TCP 回應一致。
- ACME 流程以可注入的 ACME 目錄（如 pebble 或 mock）測試取證與熱輪替路徑，不打真實 LE。
- `make test`（race）與 `make lint` 通過；`make docs-build`（strict）通過。

**Scope boundaries:**

- In scope：internal/doh（HTTP/DoH 解碼、合成 writer、ACME 生命週期、port-80 responder、憑證熱輪替）、shadowdns.yaml `doh:` 設定與其 reload、metrics proto 標籤、文件。
- Out of scope：遞迴解析、app 層 443 ACL、TLS-ALPN-01、DNS-01、網域憑證、既有 UDP/TCP 查詢路徑的任何行為改動。

## Risks / Trade-offs

- [go-acme/lego 未必支援 IP identifier 或 LE shortlived profile] → 實作前先做 spike 驗證；若不支援，改評估 certmagic 或 fork patch x/crypto，並回報使用者再決定。
- [6 天短效憑證，續簽失敗會在效期內導致 443 憑證過期、DoH 中斷] → 於 ~1/3 效期提前續簽，續簽失敗以 log 與 metric 告警；保留既有憑證直到成功換新。
- [port 80 須對公網開放，擴大攻擊面] → port 80 僅服務 ACME challenge 路徑、其餘 404，設讀寫 timeout；不在 80 上提供任何 DNS 或 DoH。
- [DoH 為連線導向、成本高於 UDP（每連線數十 KB、TLS 握手吃 CPU）] → 本情境來源受限故風險低；仍設 idle/read timeout 與 request body 大小上限作基本衛生。
- [首個 TLS 使用者，無既有 cert 載入機制可複用] → 以 GetCertificate callback + atomic 指標自建，並在測試覆蓋輪替路徑。
- [使用者誤把權威 DoH 當遞迴 resolver] → proposal、spec 與文件明確標示「只回本機 zone、非遞迴」。
- [若採信 X-Forwarded-For 做 view 選擇，client 可偽造該標頭選擇任意 view、繞過 split-horizon 資訊邊界（443 無 app 層 ACL，僅靠防火牆）] → 本 change 不解析 XFF，view 來源 IP 一律取 TCP RemoteAddr；未來若引入反向代理才以 trusted-proxy 白名單 + 最右側不可信跳點解析。

## Migration Plan

- 純新增功能，無資料遷移。未設定 `doh:` 時行為與現狀完全相同。
- 部署：在防火牆放行公網對 port 80 的存取（ACME 用）、放行企業出網 IP 對 port 443 的存取；啟用 `doh:` 設定後重啟或 reload。
- 回滾：移除 `doh:` 設定並 reload／重啟即停用，無殘留狀態。

## Open Questions

- go-acme/lego 的最新穩定版本是否同時支援 IP identifier 與 LE shortlived profile？

  **已解答（task 1.1 spike，結論：採用 go-acme/lego v4.35.2）。** 以本地 pebble（ACME 測試伺服器，已內建 `shortlived` profile，validityPeriod 518400 秒）對 IP identifier `192.0.2.x`（spike 實測用 loopback）經 HTTP-01 成功簽出一張憑證：
  - **IP identifier（RFC 8738）**：簽出憑證的 SAN 為 IP（無 DNS 名）。lego `certificate.ObtainRequest.Domains` 接受 IP 字面值並自動建立 IP identifier。
  - **profile 選擇（draft-ietf-acme-profiles）**：`certificate.ObtainRequest.Profile = "shortlived"` 經 ACME order 帶出，簽出憑證效期為 6.0 天（≈518400 秒），符合 LE IP 憑證的 shortlived 效期。
  - **HTTP-01**：`client.Challenge.SetHTTP01Provider(http01.NewProviderServer(iface, port))` 可指定獨立 listener 的綁定埠（與 port 443 DoH 服務分離），驗證流程 `use http-01 solver → served key authentication → validated` 通過。

  三項均由 lego v4.35.2 原生支援，無需 fork x/crypto 或改評估 certmagic。實作以此版本為準。

已決議（原為待定，現定案）：

- 憑證到期時間與續簽結果指標：**需要**。spec「TLS certificate is renewed and hot-swapped without restart」已將續簽失敗的 metric 訂為 SHALL，故新增憑證續簽／到期 metric 是必做項，於 tasks 以獨立任務涵蓋（不只是 per-query proto 標籤）。
