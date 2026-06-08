## Context

ShadowDNS 是 authoritative-only DNS 伺服器，hot path 為 `internal/server/handler.go` 的 `ServeDNS`。現況：

- 回應從不攜帶 EDNS0 OPT record——`replyWithAnswer` 與 `replyRcode` 都不呼叫 `SetEdns0`，違反 RFC 6891（對帶 OPT 的查詢，回應應附 OPT）
- 查詢端的 OPT 已有兩處讀取：`udpMaxSize` 讀 UDPSize 決定截斷預算、`buildQueryEntry` 讀 EDNS 版本/DO/COOKIE 供 query log 旗標
- query log 已輸出 K 旗標（COOKIE 存在），V 旗標（cookie 有效）從不輸出，本變更維持不變
- 生產拓撲為四台 NS、各自獨立 public IP、無 anycast、無前置 LB——resolver 依 RFC 7873 按 server IP 各自快取 cookie，故各台獨立 secret 即正確，無同步需求
- 效能是 load-bearing 約束：使用者明定驗收標準為 dnspyre 前後壓測（獨立 client 主機跨網路對 test nameserver 施測）QPS 退化 < 2%、p99 不超過 baseline p99 + run-to-run 雜訊幅度（= baseline 多輪 p99 的 max − min）

## Goals / Non-Goals

**Goals:**

- 對帶 EDNS0 的查詢，所有回應路徑 echo OPT record（RFC 6891 合規）
- DNS Cookies Phase 1「只回應、不強制」：帶 COOKIE option 的查詢獲得完整 cookie 回應（RFC 9018 格式 server cookie）
- 格式錯誤的 COOKIE option 回 FORMERR（RFC 7873 §5.2.2，與強制與否無關）
- OPT 解析統一為每查詢單次，cookie 處理與 query log 共用解析結果
- 效能驗證：Go 微基準（三種路徑）＋ dnspyre 前後壓測（client → server 跨網路拓撲）達標

**Non-Goals:**

- BADCOOKIE（rcode 23）強制模式——留待未來 phase，本次不實作任何拒絕邏輯
- `cookie-secret` 設定項、secret 輪替、anycast 多機同步——無 anycast 拓撲，YAGNI
- Server cookie 的接收端驗證（Verify）——Phase 1 對每個帶 cookie 查詢一律重新計算新 server cookie，驗證結果無消費者（不強制、query log 不輸出 V），驗證邏輯連同有效期窗口整段省略
- query log 格式變更、新增 Prometheus 指標
- EDNS 其他 option（NSID、ECS、TCP Keepalive 等）的處理

## Decisions

### 決策一：OPT echo 集中於單一回應組裝點，回應 OPT 廣告 1232 bytes

在 handler 增加單一 helper（概念簽名 `attachOPT(m *dns.Msg, q *queryOpt)`）。現有程式碼共有**四個**回應組裝點，全部納入：

1. `replyWithAnswer` —— 成功回應
2. `replyRcode` —— 所有錯誤 rcode（NOTIMP、FORMERR、REFUSED、SERVFAIL），含 `handleTransfer` 內 ACL 拒絕等 pre-transfer REFUSED 回應（皆經 replyRcode，queryOpt 需穿線傳入 handleTransfer）
3. `negativeReply` —— NXDOMAIN/NODATA 負向回應（自行 SetReply + WriteMsg，不經前兩者）
4. ServeDNS 的 panic-recovery SERVFAIL —— deferred 函式內直接 WriteMsg；此為冷路徑且 panic 時 queryOpt 狀態不可信，允許在該處以 `req.IsEdns0()` 就地重判（單次解析原則的唯一例外，效能無關）

查詢無 OPT 則回應不附 OPT；有 OPT 則附上 version=0、UDPSize=1232 的 OPT，並掛載 cookie option（若適用）。1232 與 DNS Flag Day 2020 及 BIND 9.18 預設 `edns-udp-size` 一致，避免 IPv6 分片。替代方案「echo 客戶端的 UDPSize」被否決——OPT 的 Udp 欄位語意是「發送方自己的接收能力」，echo 對方的值是常見誤實作。AXFR/IXFR 資料串流回應（`internal/transfer` 產生的串流封包）維持現狀、不附 OPT——僅 pre-transfer 錯誤回應納入。

### 決策二：EDNS 版本 > 0 回 BADVERS

RFC 6891 §6.1.3 要求對不支援的 EDNS 版本回 BADVERS（extended rcode，需 OPT 攜帶）。這是「修復 RFC 6891 合規缺口」的一部分，與 BIND 行為一致，實作為 OPT 解析後的早期分支，**優先於 cookie 處理**——version > 0 時不解析、不計算、不附 COOKIE option。替代方案「忽略版本欄位」（現行為）會在加上 OPT echo 後變成主動回傳錯誤版本宣告，反而更不合規。

實作陷阱：extended rcode 的高 8 bits 編碼在 OPT TTL 欄位，miekg/dns 在 `Rcode > 15` 且訊息無 OPT 時 `Pack()` 回傳 `ErrExtendedRcode`，而本 codebase 慣例丟棄 `WriteMsg` 錯誤（`_ = w.WriteMsg(m)`）——BADVERS 回應若未先掛 OPT 會**靜默不送出任何封包**。BADVERS 必須經由 attachOPT 路徑組裝；BADVERS 回應依 RFC 6891 §7 最小格式包含 header、question section（SetReply 自然帶入）與 OPT。

### 決策三：server cookie 採 RFC 9018 格式、每次重新計算、不做驗證

格式：Version(1B, =1) + Reserved(3B, =0) + Timestamp(4B, Unix 秒) + Hash(8B)，Hash = SipHash-2-4(Client Cookie ‖ Version ‖ Reserved ‖ Timestamp ‖ Client-IP, secret)，IPv4 取 4 bytes、IPv6 取 16 bytes，secret 為 128-bit。Phase 1 不強制，驗證結果沒有任何消費者，因此對每個帶 cookie 的查詢一律計算全新 server cookie（成本與驗證相同：一次 SipHash），省去有效期窗口、半衰期重發等整塊邏輯。RFC 7873 §5.2.3 允許伺服器在任何回應中發送新 server cookie。替代方案「實作驗證＋有效期窗口」被否決為 Phase 1 範圍——無消費者的程式碼是死碼。

### 決策四：SipHash-2-4 採用 github.com/dchest/siphash 相依

選型 `github.com/dchest/siphash`：公有領域授權、零相依、提供 amd64/arm64 組語路徑、API 穩定多年。替代方案：(a) 手寫 SipHash——約 100 行可控，但需自行維護與驗證測試向量，無明顯效益；(b) `golang.org/x/crypto`——無 SipHash 實作。實作正確性以 RFC 9018 Appendix A 測試向量驗證（固定 secret、固定 timestamp、已知 client cookie 與 IP → 已知 server cookie）。

實作陷阱：`siphash.Hash(k0, k1 uint64, b []byte)` 的兩個 key word 以 **little-endian** 自 16-byte secret 拆出；若誤用 `binary.BigEndian.Uint64` 拆 key，hash 結果錯誤且無任何編譯或執行期錯誤，只會在測試向量比對時失敗。以 RFC 9018 向量測試為唯一正確性閘門，拆 key 一律 `binary.LittleEndian.Uint64`（或改用 `siphash.New(key []byte)` 由函式庫處理）。

### 決策五：OPT 解析統一為 queryOpt struct 單次解析

`ServeDNS` 入口附近一次性解析 OPT 為輕量 struct（present、version、udpSize、do、cookie option 指標），往下傳給回應組裝、`udpMaxSize` 與 `buildQueryEntry`。`buildQueryEntry` 改收解析結果而非重新迭代 `opt.Option`。

- **解析位置約束**：queryOpt 解析必須放在 ServeDNS **最前端、早於 opcode 與 question-count 檢查**（`req.IsEdns0()` 不觸碰 `req.Question`，前置安全），否則 NOTIMP、question-count FORMERR、CHAOS REFUSED 這些早退路徑拿不到 queryOpt，無法滿足「錯誤回應也帶 OPT」的 spec 要求
- **hex 長度陷阱**：miekg/dns 的 `EDNS0_COOKIE.Cookie` 為 hex 字串，`len(Cookie)` 是 raw byte 數的**兩倍**（8-byte client cookie = 16 hex chars）。COOKIE 長度邊界驗證（8 / 16–40）必須以 hex decode 後的 raw byte 長度判斷，不可直接比 hex 字串長度
- **多個 COOKIE option**：RFC 7873 §5.2 規定只處理**第一個**（最靠近 DNS header 者），其餘靜默忽略——不是 FORMERR。解析時取第一個 COOKIE option 即停
- client cookie 的 hex decode 與回應 cookie 的 hex encode 各發生一次，先以微基準量測、不預先優化

### 決策六：secret 持有於 Server struct，SIGHUP 不輪替

啟動時以 `crypto/rand` 產生 16 bytes 存於 `Server` 欄位，不進 reload 快照（`s.state`）——SIGHUP 重載 zone/config 不應使既發 cookie 失效。程序重啟換 secret 為可接受行為（Phase 1 不強制，client 自然更新）。

本專案使用 Go 1.26：自 Go 1.24 起 `crypto/rand.Read` 保證不回傳錯誤（災難性失敗時由 runtime 直接中止程序），因此 secret 產生**無錯誤處理路徑**，`NewServer` 簽名不需改變（維持無 error 回傳），既有測試呼叫點零修改。

### 決策七：效能驗收採前後壓測＋微基準雙軌

- 微基準（`internal/server` 新增 benchmark）：無 EDNS、有 EDNS 無 cookie、有 cookie 三種 handler 路徑，加上 `internal/cookie` 的 Generate 單點基準；以 `-benchmem` 盯配置數
- 壓測拓撲：dnspyre 於獨立 client 主機上執行，跨網路對 test nameserver（ShadowDNS）施測；參數與報告格式沿用既定 dnspyre 壓測流程。baseline（部署前）與對照（以本地建置的 deb 套件部署後）以完全相同參數多輪實測，QPS 退化 < 2% 且 p99 ≤ baseline p99 + run-to-run 雜訊幅度（= baseline 多輪 p99 的 max − min）為過關
- 開放問題解法順序：先查 dnspyre 是否支援送 COOKIE option；若否，SipHash 路徑由微基準單獨把關，壓測僅覆蓋 OPT echo 路徑——此降級已被使用者接受

## Implementation Contract

**可觀察行為：**

1. `dig +noedns @server` → 回應不含 OPT record（行為不變）
2. `dig +edns=0 +nocookie @server` → 回應含 OPT（version 0、UDPSize 1232），無 COOKIE option
3. `dig +cookie @server`（dig 自動產生 8B client cookie）→ 回應 OPT 內含 COOKIE option：前 8B 為原 client cookie、後 16B 為 RFC 9018 server cookie；同一 client IP 重查所得 hash 段可用相同 secret 重算驗證
4. `dig +edns=1 @server` → 回 BADVERS，OPT 的 extended rcode 正確編碼，version 欄位為 0
5. COOKIE option 長度非 8 且非 16–40 bytes → FORMERR
6. 不帶 COOKIE 的查詢回答內容與現行完全一致；任何查詢都不會因 cookie 缺失/錯誤被拒答（無 BADCOOKIE）
7. query log 輸出格式與旗標語意不變（帶 cookie 查詢仍記 K，永不記 V）
8. OPT 內含兩個 COOKIE option 的查詢 → 只處理第一個，回應恰含一個 COOKIE option（RFC 7873 §5.2），不回 FORMERR
9. NXDOMAIN／NODATA 負向回應（經 negativeReply）與被 ACL 拒絕的 AXFR 查詢（REFUSED）對帶 EDNS 的查詢同樣附 OPT
10. 上述行為在 UDP 與 TCP 傳輸上一致（兩個 listener 共用同一 ServeDNS 路徑）；截斷預算僅適用 UDP 路徑（`truncateForUDP`），TCP 回應不截斷——維持現行行為

**介面／資料形狀：**

- 新套件 `internal/cookie`：核心 API 為以 client cookie bytes 與 client IP（netip.Addr）產生 24B server-side 完整 cookie bytes 的 Generate 函式，secret 由建構時注入；格式常數（版本=1、長度）由套件持有
- handler 內部 queryOpt struct 為 OPT 單次解析結果的載體，含 present/version/udpSize/do/cookie 欄位
- `go.mod` 新增 `github.com/dchest/siphash`

**失敗模式：**

- COOKIE option 格式錯誤（hex decode 後 raw 長度非 8 且非 16–40）→ FORMERR，回應含 OPT echo 但**不含 COOKIE option**
- EDNS version > 0 → BADVERS（優先於 cookie 處理；回應不含 COOKIE option，含 question section 與 OPT）
- 多個 COOKIE option → 非錯誤：處理第一個、忽略其餘（RFC 7873 §5.2）
- cookie 計算本身無失敗路徑（純函式、無 I/O）；Go 1.24+ 的 `crypto/rand.Read` 不回傳錯誤，secret 產生無錯誤處理分支

**驗收標準：**

- `make test`（race detector）全綠；`internal/cookie` 測試含 RFC 9018 Appendix A 測試向量
- handler 整合測試覆蓋上述可觀察行為 1–7 與 10（dig 行為以 miekg/dns 構造等價查詢驗證；TCP 一致性以 TCP client 對同一查詢斷言）
- `go test -bench` 三路徑微基準數據記錄於 change 內（前後對照）
- dnspyre 前後壓測報告達標（client → server 跨網路拓撲）：QPS 退化 < 2%、p99 ≤ baseline p99 + 雜訊幅度（= baseline 多輪 p99 的 max − min）
- `make lint` 通過

**範圍邊界：**

- In scope：`internal/cookie`（新）、`internal/server/handler.go`（含 replyWithAnswer、replyRcode、negativeReply、panic-recovery 四個回應組裝點與 handleTransfer 的 queryOpt 穿線）、`internal/server/server.go`、`go.mod`/`go.sum`、`README.md`、相關測試與基準
- Out of scope：BADCOOKIE 強制、設定檔項目、query log 與 metrics 變更、AXFR/IXFR **資料串流**回應的 OPT 處理（`internal/transfer` 串流封包維持現狀；pre-transfer 錯誤 rcode 回應**在** scope 內，經 replyRcode 自動涵蓋）、ephemeral API、其他 EDNS option

## Risks / Trade-offs

- [每回應多一個 OPT RR 的組裝與打包成本，可能拖慢所有 EDNS 查詢] → 微基準先行，dnspyre 前後壓測（client → server 跨網路拓撲）把關；OPT struct 可在 helper 內就地構造避免額外間接層
- [miekg/dns EDNS0_COOKIE hex 字串造成每 cookie 查詢 2 次小配置] → 先量測再優化；若微基準顯示配置成為瓶頸，可預先配置 hex buffer，但不預先做
- [dnspyre 可能無法送 COOKIE option，壓測覆蓋不到 SipHash 路徑] → 已與使用者確認降級方案：微基準單獨把關 cookie 路徑
- [重啟換 secret 使既發 cookie 失效] → Phase 1 不強制，client 收到新 cookie 即更新，無服務影響
- [BADVERS／FORMERR 新分支改變少數異常查詢的回應] → 與 BIND 行為一致，屬合規修正；整合測試明確覆蓋
- [truncateForUDP 在 Answer 清空後若仍超預算，OPT 是否保留] → OPT 計入打包尺寸且不可被丟棄（RFC 6891），截斷僅丟 Answer RR；新增測試確認帶 OPT 的截斷回應仍含 OPT 且 ≤ 預算

## Migration Plan

1. 部署前：於獨立 client 主機上執行 dnspyre，對 test nameserver 現裝版本實測 baseline（多輪、記錄 p99 的 max − min 作為雜訊幅度）
2. 以本地建置的 deb 套件部署至 test nameserver，驗證啟動日誌無誤
3. 於 client 主機以完全相同參數複測 dnspyre，比對 QPS/p50/p99；未達標則回滾並回到優化迭代
4. 回滾：重新安裝最新 GitHub release 的 deb（cookie 為純新增行為，無資料遷移）

## Open Questions

- ~~dnspyre 是否支援發送 COOKIE option~~ **已解決（apply 階段查證，dnspyre 3.11.1）**：支援，且有兩種方式——(1) 專屬旗標 `--cookie`（RFC 7873，自動為每個請求加上 8-byte client cookie，並在收到 server cookie 後一併回送）；(2) 通用旗標 `--ednsopt=10:<hex>`（手動指定 option code 10 與 payload）。已於本機 loopback 壓測以 `--ednsopt=10:<hex>` 實測驗證 SipHash 路徑可被壓測覆蓋（見 benchmarks.md）。4.3/4.4 的跨網路壓測可同時覆蓋 OPT echo 與 cookie 路徑，無需降級
