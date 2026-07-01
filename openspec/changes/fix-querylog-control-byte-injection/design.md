## Context

`serveJSON`（`internal/doh/dnsjson.go`）是唯一從「原始字串」設定 `Question[0].Name` 的查詢建構路徑。wire、TCP、wire-format DoH 都經 miekg/dns `UnpackDomainName`，其 `Question[0].Name` 已是 RFC 1035 presentation form（控制位元組與特殊字元皆已跳脫）。dns-json 以 `dns.Fqdn(name)` → `dns.IsDomainName`（接受控制位元組）→ `SetQuestion`，未做 wire unpack，使原始 `0x0A`／`0x0D` 存活進 `Question[0].Name`，直達 `querylog.appendLine` 逐字寫入 log，偽造整行 log（GitHub issue #9，security MEDIUM）。

`doh-endpoint` spec 既有需求已要求 dns-json name「match what wire-format DoH returns」；`query-logging` spec 亦已要求 qname 以 RFC 1035 慣例跳脫。落差就在 dns-json 未把 name 正規化為 presentation form。

## Goals / Non-Goals

**Goals**
- dns-json `name` 在進入 query pipeline 前，正規化為與 wire 路徑逐字相同的 presentation form。
- 關閉 query-log 換行注入向量，且不對 per-query logging hot path 增加成本（log sink 不改）。

**Non-Goals**
- 不改動 `querylog.appendLine`（見 Decision 3 rejected alternative）。
- 不改動 type 解析、回應 schema，或 wire / wire-format DoH 路徑。

## Decisions

**Decision 1：在 `serveJSON` 以 wire round-trip 正規化 name。**
在既有 `fqdn := dns.Fqdn(name)` 與 `dns.IsDomainName(fqdn)` 通過之後、`SetQuestion` 之前，對 `fqdn` 做 wire round-trip：以 `dns.PackDomainName(fqdn, wire[:], 0, nil, false)`（`wire` 為堆疊上的 `[256]byte`）編碼、再以 `dns.UnpackDomainName(wire[:off], 0)` 解碼，取得跳脫後的 canonical 名稱，以其 `SetQuestion`。這正是 wire 路徑對同一 on-wire 名稱所走的編解碼，結果逐字一致。

- 為何用 round-trip 而非自寫跳脫函式：跳脫規則（哪些位元組跳、`\DDD` vs `\<char>`）必須與 miekg `UnpackDomainName` 完全一致才能跨 transport 對齊；直接重用該編解碼，避免自寫版本與 miekg 漂移。
- Buffer 大小：wire 名稱上限 255 octets，`[256]byte` 堆疊陣列足夠且零 heap 配置；已對 miekg v1.1.72 實測，對任何通過 `IsDomainName`（≤255 octets）的名稱（含控制位元組、高位元組、特殊字元）此 buffer 皆成功、不回 error。

**Decision 2：round-trip 的 error 分支為防禦性（對合法輸入不可達）。**
已對 miekg v1.1.72 實測：任何通過 `dns.IsDomainName` 的名稱以 `[256]byte` buffer 呼叫 `PackDomainName` 都不會失敗（`PackDomainName` 僅在 buffer 過小時回 `ErrBuf`；過長名稱已被 `IsDomainName` 擋下）。因此 `Pack`／`Unpack` 的 error 分支對真實輸入不可達，但仍以 `if err != nil` 保留、回 HTTP 400（與既有 malformed-name 分支同一回應），確保「無法編碼為合法 wire 的名稱被拒而非 dispatch 或 surface 成 500」——這是防禦性錯誤處理，非可觸發的功能分支。

**Decision 3（rejected alternative）：不在 sink（`querylog.appendLine`）跳脫。**
已對 miekg v1.1.72 實測，wire 路徑的 presentation form 把可列印 master-file 特殊字元跳脫為 `\<char>`（space→`\ `、`(`→`\(`、`)`→`\)`、`;`→`\;`、`"`→`\"`），意即 wire 輸出**含字面**括號/分號/空白（前綴反斜線）；只有控制位元組（`< 0x20`）、`0x7f`、高位元組（`0x80`–`0xff`）在 wire 輸出中永為 `\DDD`、絕不字面出現。因此 sink 端若跳脫可列印分隔字元，會把 wire 輸入既有的 `\(` 二次跳脫為 `\\(`，破壞冪等與 BIND 一致性；sink 端只能安全處理控制位元組，無法涵蓋可列印分隔字元，會留下單行欄位混淆殘留。來源正規化則對**所有**位元組與 wire 逐字一致，故採之。

## Implementation Contract

- **函式**：`serveJSON`（`internal/doh/dnsjson.go`）。正規化插入點在 `dns.IsDomainName(fqdn)` 通過之後、`req.SetQuestion(...)` 之前。
- **可觀察行為（必須全部成立）**：
  1. dns-json `name` 含控制位元組時，最終 `req.Question[0].Name` 與 wire 路徑對相同 on-wire 名稱產出的字串逐字相同（原始 `0x0A` → `\010`），且不含任何原始控制位元組。
  2. 跨 transport 一致：同一 on-wire 名稱經 dns-json 與經 wire-format（`?dns=`）兩路徑，`Question[0].Name` 逐字相同。
  3. 純 letters/digits/hyphens 的 `name`：`req.Question[0].Name` 與正規化前一致（identity），on-wire 大小寫保留（`ExAmple.COM.`）。
  4. round-trip error（防禦性、對合法輸入不可達）時回 HTTP 400，不回 500、不 dispatch。
  5. 由此建構的查詢經同一 authoritative query path dispatch；view 選擇、ephemeral overlay、rate limiting、回應組裝行為不變。
- **In scope**：`serveJSON` 內的 name 正規化步驟。
- **Out of scope**：`querylog.appendLine`、type 解析、回應 schema、wire/wire-format DoH 路徑。

## Risks / Trade-offs

- [高位元組／特殊字元名稱的表示變更] → 對含高位元組（如 raw UTF-8）或 master-file 特殊字元的 dns-json name，其 `Question[0].Name`、JSON echo、zone/rate-limit fold key 由原始位元組變為 presentation form。這是**一致性修正**：使 dns-json 與 wire 路徑及 zone 檔（以 presentation form 存 owner）一致；此前這類 dns-json 名稱以原始位元組 fold，反而對不上 zone。已於 proposal 揭露；回歸測試以跨 transport 一致性斷言鎖定。
- [round-trip 對每個 dns-json GET 增加一次 pack+unpack] → 僅在 dns-json GET 路徑（HTTP，本就較重），非 wire UDP hot path；成本為單一短名稱編解碼、buffer 為堆疊配置，可忽略。
- [error 分支不可達可能被誤為死碼] → 以 Decision 2 明確標註為防禦性錯誤處理；不新增測試嘗試觸發（無可構造的合法輸入），於程式碼註解說明。

## Migration Plan

無資料遷移。純請求處理路徑的正規化插入，對正常名稱行為不變。部署後依 Perf-Guard 於 ns2 量測 baseline → 部署 → 重測。

## Open Questions

（無）
