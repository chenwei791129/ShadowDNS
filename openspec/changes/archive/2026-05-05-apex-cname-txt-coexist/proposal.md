## Why

ShadowDNS 目前對「同一個 owner 同時存在 CNAME 與其他類型記錄」的處理策略，在 `openspec/specs/dns-server/spec.md` §1250 寫的是「CNAME 對所有非 CNAME query 一律優先」，但實作（`internal/server/handler.go` 的 exact-match-first → CNAME-fallback 路徑）對 static zone 同 owner 的 TXT 查詢會直接回該 TXT，而非 CNAME synthesis。這個分歧造成兩個問題：(1) 規格與實作的真值點不一致，未來變更難以判斷哪邊是 bug；(2) 使用者明確偏好 Cloudflare 的並存行為（已附 `example.org` 截圖：apex 同時放 TXT 與 CNAME，TXT 查詢回 TXT、CNAME 查詢回 CNAME），而 BIND9 會在 zone load 時把這種組合視為錯誤。本變更把 CF 並存行為固化為規格、補上 regression 測試，避免未來無意中倒回 BIND 風格。

## What Changes

- 修改 `openspec/specs/dns-server/spec.md` §1250 的措辭：把「靜態 zone 同 owner 也存在的非 CNAME 記錄」納入 exact-match 優先的例外，與現有 §1252 的 ephemeral TXT overlay 例外並列。CNAME synthesis 僅在該 owner 的目標 qtype **沒有** exact match 時觸發。
- 在 `dns-server` spec 增加一個新 scenario，描述 zone apex 同時存在 CNAME 與 TXT 時，TXT 查詢回 TXT、CNAME 查詢回 CNAME 的行為。
- 在 `testdata/integration/master/example.com_view-other.fwd` 與 `testdata/integration/master/example.com_view-th.fwd` 兩個 view fixture 的 zone apex `@` 各加一筆 CNAME 記錄（target 指向 zone 內既有名字，例如 `www.example.com.`）；apex 既有的 SOA/NS/A/AAAA/MX/TXT 全保留。雙 view 同步是為了維持既有 fixture 的對稱性（既有所有 owner 都同時存在於兩個 view），integration test 從 loopback 發 query 會命中 view-other。
- 在 `test/integration/query_test.go` 新增 `TestQuery_Apex_CNAME_TXT_Coexist`：對 apex 發 CNAME 查詢回 CNAME、發 TXT 查詢回原本的 SPF TXT、發 A 查詢回原本的 apex A（鎖定 CF 並存行為，且確認非 TXT 的其他 type 不會被新加入的 apex CNAME 干擾）。

## Non-Goals (optional)

- **不**在 zone load 時對 CNAME + 其他類型並存報錯或警告。Cloudflare 允許、使用者明確偏好允許，故 `internal/zone/parser.go` 與 `internal/zone/zone.go` 的 `AddRR` 不變。
- **不**實作 Cloudflare 的 CNAME flattening（apex A 查詢沿著 CNAME 解析到 target 的 A）。本變更維持「apex 有自己的 A 記錄就回那個 A」的現況，apex CNAME 只對 explicit CNAME query 有意義。
- **不**改動 ephemeral TXT overlay 邏輯（§1252）。該段例外與 static zone 的例外並列但獨立。
- **不**修改 `internal/server/handler.go` 的查詢路徑。實作行為已是正確的 CF 並存行為，本變更僅對齊規格與補測試。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `dns-server`: §1250 的 CNAME-vs-coexisting-records 規則新增 static zone exact-match 例外，並補上對應 scenario。

## Impact

- Affected specs: `dns-server`
- Affected code:
  - Modified:
    - openspec/specs/dns-server/spec.md
    - testdata/integration/master/example.com_view-other.fwd
    - testdata/integration/master/example.com_view-th.fwd
    - test/integration/query_test.go
  - New: (none)
  - Removed: (none)
