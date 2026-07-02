## Context

RRL 的帳戶 key 由 `ImputedName`（`internal/ratelimit/key.go`）依 category 推導。`CategoryResponses`（positive answer）目前取精確 query name；`CategoryNxdomains`/`CategoryNodata` 取 authority SOA owner（zone origin）以聚合隨機子網域洪泛。RRL writer（`internal/ratelimit/writer.go`）於單一 `WriteMsg` 匯流點，只從 response message + RemoteAddr 推導帳戶——刻意與 handler 解耦（見該檔 doc comment）。

問題：wildcard 合成的 positive answer，其 owner 已由 `rewriteWildcardOwner` 改寫成 query name，與精確匹配真實紀錄無法從 message 區分。因此攻擊者對 `*.example.com` 洪泛隨機 label 時，每個都成為唯一滿額度帳戶，`responses-per-second` 永不觸發（GitHub issue #11，security MEDIUM）。BIND 把 wildcard 合成紀錄的帳戶歸到 wildcard owner 以折疊此洪泛。

約束：RRL writer 無 zone 存取、只看 message，無法自行判斷 wildcard 合成；訊號必須由 handler（唯一知道匹配到 wildcard 的地方）plumb 過去。`ImputedName` 為純函式、被多處測試鎖定，應盡量不改其簽章語意。

## Goals / Non-Goals

**Goals**
- wildcard 合成的 positive answer 以最接近 wildcard owner（`*.zone`）為 RRL 帳戶 key，折疊 wildcard 洪泛為單一帳戶。
- 非 wildcard positive answer、NXDOMAIN/NODATA/errors 的既有帳戶邏輯完全不變。

**Non-Goals**
- 不改 `all-per-second` 語意/預設；不改 wildcard 匹配或對外回應內容；不改 metrics writer。

## Decisions

**Decision 1：以 typed RRL writer 參照 + wildcard owner 兩個 `queryOpt` 欄位 plumb 訊號。**
`queryOpt` 增加：
- `rrl *ratelimit.ResponseWriter`：本次查詢的 RRL writer 參照（無 limiter 時 nil）。因 `queryOpt` 已在 `internal/server`、且該套件已 import `internal/ratelimit`，此耦合可接受。
- `rrlWildcardOwner string`：wildcard 合成點設為 `dnsutil.LookupKey(wildcardOwner)`；非 wildcard 為空。

**賦值時序（重要）**：RRL writer 於 `ServeDNS` 早期（`w = ratelimit.NewResponseWriter(w, limiter)` 處）安裝，但 `qo` 要到稍後 `qo := parseQueryOpt(req)` 才建立。因此在安裝處把 typed writer 捕獲進一個區域變數（例如 `var rlWriter *ratelimit.ResponseWriter`；無 limiter 時維持 nil），並在 `parseQueryOpt` 建立 `qo` **之後**才 `qo.rrl = rlWriter`。不可在 writer 安裝行嘗試寫 `qo.rrl`（該處 `qo` 尚不存在）。

為何用 typed 參照而非 optional-interface 型別斷言：handler 交給回應函式的 `w` 是最外層 metrics writer，`metrics.ResponseWriter` 匿名嵌入的是 `dns.ResponseWriter` 介面，不會 promote `ratelimit.ResponseWriter` 的自訂 override 方法；型別斷言最外層 writer 會失敗。直接持有 typed `*ratelimit.ResponseWriter` 參照可繞過整條 wrapper chain 設定 override，且不需改 metrics writer。

**Decision 2：override 在 RRL writer 的 `WriteMsg` 套用，`ImputedName` 維持純函式不變。**
`ratelimit.ResponseWriter` 增加 per-query 欄位 `responsesNameOverride string` 與方法 `SetResponsesAccountName(name string)`。`WriteMsg` 計算 `name := ImputedName(m, category)` 後，若 `category == CategoryResponses && w.responsesNameOverride != ""`，以 override 取代 `name`。RRL writer 於每筆查詢新建（見 `ServeDNS` 安裝點），override 為 per-query 狀態，設一次用一次，writer 於查詢結束即丟棄，無需重置、無跨查詢污染。

**Decision 3：列舉並涵蓋 handler 中「每一個」wildcard 合成 positive-answer 發射路徑。**
wildcard 合成答案不只一處。必須在所有下列路徑設 `qo.rrlWildcardOwner`（`collapseRootCNAME` 收 `qo` by value，`answeredCollapsed` 是捕獲外層 `qo` by reference 的閉包；兩者都只要在「呼叫前」於外層設好 owner 即可被帶入/看見）：

- **root zone 直接 wildcard**（`handleRootQuery`）：`rootZone.LookupWildcard(qname, qtype)` 命中時，於分支入口設 `qo.rrlWildcardOwner = dnsutil.LookupKey(wRRs[0].Header().Name)`（即 `*.parent`，在 `rewriteWildcardOwner` 改寫**之前**）。這同時涵蓋隨後的直接回應與經 `collapseRootCNAME` 的回應（後者接 qo by value，複本已帶 owner）。
- **root zone wildcard-CNAME chain**：`rootZone.LookupWildcard(qname, dns.TypeCNAME)` 命中的分支，設 `qo.rrlWildcardOwner = dnsutil.LookupKey(wCNAMEs[0].Header().Name)`。同樣涵蓋其 `collapseRootCNAME` 與 `FollowCNAME` 直接回應兩條出口。
- **alias 非 collapse wildcard**（`ResolveWildcard`）與 **alias collapse wildcard**（`ResolveWildcardCollapse`）：僅此兩個 alias 函式為 wildcard 合成（`ResolveCNAMEFallback*`／`ResolveExact*`／ephemeral TXT 為真實紀錄，維持以 query name 為 key，不設 owner）。擴充這兩函式的回傳值以額外提供 wildcard owner，handler 僅在「答案來自 wildcard resolver」的分支設 `qo.rrlWildcardOwner`；collapse 路徑於呼叫捕獲 `qo` 的 `answeredCollapsed` 閉包**之前**設好。

**wildcard owner 的正確來源（避免 review 指出的陷阱）**：alias 的 `synthesizeWildcardRRs`（`internal/alias/override.go`）內，區域變數 `wildcardOwner := RewriteName(qname, ...)` 是**per-label 改寫後**的名稱，**不可**用作帳戶 key（用它等於又 per-label 分散）。真正的 wildcard 節點是傳入的 `wRRs[0].Header().Name`（即 `*.<rootZone.Origin>`，改寫**之前**）。因此 `synthesizeWildcardRRs` 須擷取並向上回傳這個 `*.<origin>` 節點名，供 `ResolveWildcard`／`ResolveWildcardCollapse` 一路回傳到 handler。

- owner 一律經 `dnsutil.LookupKey` 折疊（與 `ImputedName` 對其他 category 的折疊一致，確保 0x20 大小寫變體共用同帳戶）。root 與 alias 皆以 `*.<origin>` 形式為 key，對同一 wildcard 一致聚合。

**Decision 4：override 於 `replyWithAnswer` 寫出前套用（單一套用點）。**
`replyWithAnswer` 是所有 positive-answer 的單一寫出點（root 直接、collapse、alias、ephemeral 皆經此），且已收 `qo`。在其 `w.WriteMsg(m)` 之前：`if qo.rrl != nil && qo.rrlWildcardOwner != "" { qo.rrl.SetResponsesAccountName(qo.rrlWildcardOwner) }`。非 wildcard 查詢 `rrlWildcardOwner` 為空、不觸發，行為不變。因所有出口都收斂到 `replyWithAnswer`，只要各 wildcard 分支正確設好 `qo.rrlWildcardOwner`，此單一套用點即涵蓋全部路徑。

## Implementation Contract

- **帳戶 key 行為（可觀察）**：
  1. wildcard 合成的 positive answer → RRL 帳戶名為最接近 wildcard owner（`*.zone`，LookupKey 折疊）；同一 wildcard 下不同 label 共用單一 `{block, responses, *.zone}` 帳戶。
  2. 非 wildcard positive answer（精確匹配真實紀錄）→ 帳戶名仍為精確 query name（既有行為）。
  3. NXDOMAIN/NODATA → zone origin；errors → 空名稱（既有行為，完全不變）。
  4. 無 limiter（`RateLimiter` 未設）時，plumbing 為 no-op，回應路徑行為不變。
- **不變**：`ImputedName` 簽章與其對各 category 的既有回傳；對外回應的 owner 仍為 query name；wildcard 匹配結果。
- **In scope**：`queryOpt` 兩欄位；`ServeDNS` 於 `parseQueryOpt` 後設定 `qo.rrl`；**全部** wildcard 合成發射路徑設定 `qo.rrlWildcardOwner`（root 直接、root wildcard-CNAME chain、含各自的 `collapseRootCNAME` 出口；alias `ResolveWildcard` 與 `ResolveWildcardCollapse`，後者經 `answeredCollapsed` 閉包）；`replyWithAnswer` 套用 override；`ratelimit.ResponseWriter` override 欄位/方法/`WriteMsg` 分支；`internal/alias`（`synthesizeWildcardRRs`/`ResolveWildcard`/`ResolveWildcardCollapse` 回傳 `*.<origin>` wildcard 節點名）及其所有既有呼叫點的簽章更新。
- **Out of scope**：metrics writer；`ImputedName` 內部；`all-per-second`；非 UDP 路徑（TCP 不限流）；CNAME-fallback／exact／ephemeral-TXT 等真實紀錄路徑（維持以 query name 為 key）。

## Risks / Trade-offs

- [plumbing 觸及多個合成點，漏設一處會使該路徑的 wildcard 洪泛仍不聚合] → 回歸測試需分別覆蓋 root-zone wildcard 與 alias wildcard 兩類合成路徑的聚合；並斷言非 wildcard 精確匹配不受影響。
- [`queryOpt` 持 `*ratelimit.ResponseWriter` 造成 server→ratelimit 型別耦合] → server 套件已 import ratelimit（RRL writer 於此安裝），耦合已存在；以 nil-safe 使用避免 limiter 未啟用時的相依。
- [override 為 per-query writer 狀態] → writer 於每筆查詢新建、查詢結束丟棄，override 設一次用一次，無並發或跨查詢污染風險；以 writer_test 直接斷言「設 override 後 CategoryResponses 用 override、其他 category 不受影響」。
- [alias 解析簽章擴充影響既有呼叫點] → 以額外回傳值或小結構承載 wildcard owner，更新所有呼叫點；既有 alias 測試作為回歸守門。

## Migration Plan

無資料遷移。純 in-memory RRL 帳戶 key 推導調整，對外回應不變。部署後依 Perf-Guard 於 ns2 量測 baseline → 部署 → 重測，確認 hot path 無 QPS/p99 回歸。

## Open Questions

（無）
