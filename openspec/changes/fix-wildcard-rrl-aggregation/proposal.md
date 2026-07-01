## Problem

ShadowDNS 的 BIND 相容 Response Rate Limiting（RRL）對 positive answer（`CategoryResponses`）以**精確 query name** 作為帳戶 key（`ImputedName` 回傳 `dnsutil.LookupKey(m.Question[0].Name)`，無 wildcard 意識）。對一個服務 wildcard 的 zone（如 `*.example.com`），偽造受害者來源 IP 的攻擊者可洪泛大量隨機 label（`r1.example.com`、`r2.example.com`…）。每個都被 wildcard 合成為 NOERROR positive answer，其 owner 被改寫成該唯一 query name；在 `WriteMsg` 分類為 `CategoryResponses` 時產生一個唯一、滿額度的帳戶 key `{block, responses, name}`，因此 `responses-per-second` 限制**永不觸發**，無論對受害者的總回應速率多高——反射/放大攻擊的主要 RRL 防線因而被繞過。

對比：NXDOMAIN/NODATA 路徑正確地聚合到 zone-origin SOA owner（`ImputedName` 對 `CategoryNxdomains`/`CategoryNodata` 取 authority SOA owner），使隨機子網域洪泛收斂為單一帳戶。BIND 對 wildcard 合成紀錄同樣把帳戶歸到 wildcard owner，正是為了把此類洪泛折疊為單一帳戶。ShadowDNS 唯一能擋此攻擊的是獨立的 `all-per-second` 聚合閘，但它預設為 0（停用）且不繼承自 `responses-per-second`。

## Root Cause

`ImputedName`（`internal/ratelimit/key.go`）對 `CategoryResponses` 一律以精確 query name 為帳戶 key，不知道該 positive answer 是否由 wildcard 合成。RRL writer（`internal/ratelimit/writer.go`）刻意只從 response message 推導帳戶名（與 handler 解耦），而「此答案是否 wildcard 合成、其 wildcard owner 為何」無法單從 message 推得——合成答案的 owner 已被改寫為 query name，與精確匹配的真實紀錄無異。

## Proposed Solution

讓 wildcard 合成的 positive answer 以**最接近的 wildcard owner**（`*.zone`）作為 RRL 帳戶 key，鏡像 NXDOMAIN 對 zone-origin 的聚合、與 BIND 一致。因 RRL writer 只看 message，需由 handler 在合成 wildcard 答案時，把 wildcard owner 訊號 plumb 給 RRL writer：

- 在 `queryOpt` 增加兩個欄位：一個承載本次查詢的 typed RRL writer 參照（於 `ServeDNS` 安裝 writer 時設定，無 limiter 時為 nil），一個承載 wildcard owner 的 `LookupKey` 折疊字串（於 wildcard 合成點設定，非 wildcard 時為空）。
- 在每個 wildcard 合成 positive-answer 的回應點，設定 wildcard owner：root zone 路徑取 `zone.LookupWildcard` 回傳 RR 在 `rewriteWildcardOwner` 改寫前的 `Hdr.Name`（即 `*.parent`）；alias 路徑讓 `alias.ResolveWildcard`／`ResolveWildcardCollapse` 額外回傳匹配的 wildcard owner。
- 在 `replyWithAnswer` 寫出前，若本次帶有 wildcard owner 且 RRL writer 非 nil，將 owner 設為 RRL writer 對 `CategoryResponses` 的帳戶名 override。
- `ratelimit.ResponseWriter` 增加一個 per-query override 欄位與設定方法；其 `WriteMsg` 在 `category == CategoryResponses` 且 override 非空時，以 override 取代 `ImputedName` 的結果。`ImputedName` 本身維持純函式、不變。

如此，`*.example.com` 下的隨機 label 洪泛全部折疊到單一帳戶 `{block, responses, *.example.com.}`，`responses-per-second` 得以生效。

## Non-Goals

- 不改動 `ImputedName` 的既有 NXDOMAIN/NODATA/errors 聚合邏輯，也不改其對非 wildcard positive answer（精確匹配真實紀錄）仍以精確 query name 為 key 的行為。
- 不改動 `all-per-second` 的語意或預設值。
- 不改動 metrics writer（override 以 typed RRL writer 參照直接設定，不需經 metrics 轉發）。
- 不改動 wildcard 匹配（RFC 4592）本身或回應內容（owner 仍改寫為 query name 對外呈現；本變更只影響 RRL 帳戶 key）。

## Success Criteria

- 對服務 `*.example.com` 的 zone、`responses-per-second = R`，同一 client block 洪泛 N 個不同隨機 label（皆 wildcard 合成 NOERROR），全部聚合到單一帳戶 `{block, responses, *.example.com.}`；超過 R 的回應被判 over-limit（drop/slip），而非各自滿額度通過。
- 非 wildcard 的 positive answer（精確匹配真實紀錄）仍以精確 query name 為 key，兩個不同真實名稱仍分屬不同帳戶（既有行為不回歸）。
- NXDOMAIN/NODATA 對 zone-origin 的聚合、errors 對空名稱的聚合維持不變。
- 既有 `internal/ratelimit/*_test.go`、`internal/server/handler_ratelimit_test.go` 全數通過。
- Perf-Guard（hot-path 變更必跑，`internal/ratelimit` + `internal/server` 均在每筆 UDP 回應路徑）：ns2 baseline → 部署 → 重測，QPS 未下降 > 5% 且 p99 未上升 > 15%。

## Impact

- Affected specs: response-rate-limiting（MODIFY「Account key construction with name imputation」需求：positive answer 於 wildcard 合成時以最接近 wildcard owner 為帳戶名，並新增對應 scenario）
- Affected code:
  - Modified: internal/ratelimit/writer.go（override 欄位/方法，並於 WriteMsg 套用；`ImputedName`（key.go）不變）、internal/server/handler.go（queryOpt 欄位、qo.rrl 賦值、所有 wildcard 合成點設 owner、replyWithAnswer 套用）、internal/alias/override.go（synthesizeWildcardRRs 擷取並回傳 `*.<origin>` 節點，ResolveWildcard/ResolveWildcardCollapse 簽章帶出 owner，更新所有呼叫點）
  - New: (none)
  - Removed: (none)
- Affected tests:
  - Modified: internal/ratelimit/writer_test.go、internal/server/handler_ratelimit_test.go（root 直接/CNAME-chain、alias/alias-collapse 四條 wildcard 路徑的聚合回歸，及非 wildcard 不回歸）、internal/alias 既有測試（新簽章 + owner 為 `*.<origin>`）
- Docs: 檢視 MkDocs RRL 相關頁是否需補「wildcard 合成答案依 wildcard owner 聚合（與 BIND 一致）」一句；若更新則雙語系同步並 `make docs-build`（見 tasks 5.1）
