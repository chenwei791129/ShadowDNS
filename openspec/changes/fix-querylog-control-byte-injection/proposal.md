## Problem

當 DoH listener 與 BIND9 相容 query logging 同時啟用時，未經認證的遠端用戶端可透過 `application/dns-json` 的 `name` 參數，把原始控制位元組（尤其換行 `0x0A`、歸位 `0x0D`）注入 query log 檔案，偽造出格式完整、可亂真的整行 log（假 client IP、view、timestamp）——log 偽造 / 否認 / SIEM 汙染。

wire DNS 路徑（含 wire-format DoH）安全，是因為 miekg/dns 的 `UnpackDomainName` 在 wire unpack 時已把控制位元組跳脫為 `\DDD` presentation 形式，其 `Question[0].Name` 為 presentation form。dns-json 路徑則從不做 wire unpack：`serveJSON`（`internal/doh/dnsjson.go`）以 `q.Get("name")` 取得 `name`，只經 `dns.Fqdn`（僅補點、不跳脫）與 `dns.IsDomainName`（結構驗證，接受 `0x0A`/`0x0D`/控制位元組），即 `SetQuestion`。原始換行因此存活進 `Question[0].Name`，被下游 `querylog.appendLine` 逐字寫入 log 行。

已對 miekg/dns v1.1.72 實測確認：`dns.IsDomainName("evil\nfake")` 回傳 ok=true 且 `Question[0].Name` 內含 `0x0A`；同一名稱經 wire pack/unpack round-trip 則被跳脫為 `\010`。

## Root Cause

同一份 `Question[0].Name` 在不同 transport 有兩種表示法：wire / wire-format DoH 為**已跳脫的 presentation form**，dns-json 為**原始位元組**。`doh-endpoint` spec 既有需求已規定 dns-json 的 name 須正規化為 FQDN 且「match what wire-format DoH returns for the same name」，但未涵蓋控制/特殊位元組的正規化，使 dns-json 產出的 `Question[0].Name` 與 wire 路徑不一致，未跳脫的控制位元組直達下游 log sink。

## Proposed Solution

在**來源**修補：於 `serveJSON`，在既有 `dns.Fqdn` + `dns.IsDomainName` 驗證之後、`SetQuestion` 之前，對名稱做一次 wire round-trip——以 `dns.PackDomainName`（buffer 為堆疊上的 `[256]byte`，wire 名稱上限 255 octets）編碼、再以 `dns.UnpackDomainName` 解碼，取得與 wire 路徑逐字相同的 presentation form 名稱，並以其 `SetQuestion`。

此法使所有 transport 交給下游的 `Question[0].Name` 都是相同的 canonical presentation form：控制位元組於進入 query pipeline 前即被跳脫，關閉注入向量；且與 wire-format DoH 對同一名稱的行為完全一致，同時保持 log sink（hot path）零改動。

**為何在來源而非 sink 端跳脫**：已對 miekg/dns v1.1.72 實測，wire 路徑的 presentation form 把可列印 master-file 特殊字元跳脫為 `\<char>`（space→`\ `、`(`→`\(`、`;`→`\;`），意即 wire 輸出**確實含字面括號/分號/空白**（前綴反斜線）。因此在 log sink 端跳脫這些字元會對 wire 輸入二次跳脫（`\(`→`\\(`），破壞冪等；只有控制位元組與高位元組在 wire 輸出中永為 `\DDD`、絕不字面出現。sink 端因此無法在不破壞 wire 冪等的前提下涵蓋可列印分隔字元，唯有來源正規化能對所有位元組與 wire 路徑一致。

## Non-Goals

- 不改動 `querylog.appendLine` 或任何 log 欄位格式；log sink 維持逐字 append（下游收到的已是 canonical presentation form）。
- 不改動 dns-json 的 `type` 解析、回應 schema，或 wire / wire-format DoH 既有行為（其名稱本已為 presentation form）。
- 不改變正常名稱（letters/digits/hyphens）的行為：round-trip 對其為 identity，on-wire 大小寫保留。

## Success Criteria

- 對 dns-json `name` 含控制位元組（如 `0x0A`、`0x0D`、`0x00`、`0x09`、`0x7f`）的請求，最終 `Question[0].Name` 與 wire 路徑對相同 on-wire 名稱產出的字串逐字相同（控制位元組跳脫為 `\010`/`\013`/`\000`/`\009`/`\127`）；由此產生的 query log 行不再含任何原始控制位元組，換行注入無法再偽造額外 log 行。
- 跨 transport 一致：同一 on-wire 名稱經 dns-json 與經 wire-format（`?dns=`）兩路徑，其 `Question[0].Name` 逐字相同。
- 對純 letters/digits/hyphens 的 dns-json name，`Question[0].Name` 與行為與修補前逐字一致；`ExAmple.COM` 的 on-wire 大小寫保留（`ExAmple.COM.`）不回歸。
- 既有 `internal/doh/dnsjson_test.go` 全數通過。
- Perf-Guard（依檔案分類，`internal/**` 屬 must-run；惟本變更僅動 dns-json GET 路徑、不觸及 wire UDP hot path，標準 dnspyre wire benchmark 預期無位移）：ns2 baseline → 部署 → 重測，QPS 未下降 > 5% 且 p99 未上升 > 15%。

## Impact

- Affected specs: doh-endpoint（MODIFY「application/dns-json queries are parsed from name and type parameters」需求：新增 name 須正規化為與 wire 一致的 presentation form、控制位元組跳脫的規定與 scenario）
- Affected code:
  - Modified: internal/doh/dnsjson.go
  - New: (none)
  - Removed: (none)
- Affected tests:
  - Modified: internal/doh/dnsjson_test.go（新增控制位元組正規化與跨 transport 一致性的回歸測試）
- Behavior-change disclosure（非 goal，但為必要揭露）: 對含**高位元組**（`0x80`–`0xff`，如 raw UTF-8）或 master-file 特殊字元的 dns-json name，其 `Question[0].Name`、JSON Question echo、以及 zone/rate-limit fold key 會由「原始位元組」變為「presentation form」。這使 dns-json 與 wire 路徑及 zone 檔（本就以 presentation form 存 owner）一致——是一致性修正而非回歸（此前這類 dns-json 名稱以原始位元組 fold，反而無法對上以 presentation form 存的 zone）。
