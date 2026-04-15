## Why

ShadowDNS 目標是替換 BIND，但目前 `zone-parser` 無法載入一個合法的 BIND zone 檔：當檔案內出現 `$INCLUDE "path"`（雙引號包裹的路徑）時，miekg/dns 的 scanner 會把 `"` 當作 `zQuote` token，而 `$INCLUDE` 的 state 只接受 `zString`，於是 parser 直接回傳致命錯誤：

```
dns: expecting $INCLUDE value, not this...: "\"" at line: 2240:10
```

雙引號 `$INCLUDE` 是 BIND 9 中合法且常用的寫法（特別是在由工具自動產生、路徑可能含特殊字元時）。ShadowDNS 若要宣稱「BIND-compatible」，就必須在載入 zone 檔時容許這個語法，否則真實 BIND 佈署無法無縫切換到 ShadowDNS。

## What Changes

- `zone-parser` 在把 zone 檔餵給 miekg/dns 之前，先做一層 **BIND 相容性前處理**：
  - 將 `$INCLUDE` / `$include` directive 行上的「雙引號包裹路徑」去引號後再傳下去
  - directive 比對限定在行首（忽略前導空白），避免誤傷 TXT 等 RR 資料中的 quoted string
  - 保留原行號，使後續 parser 回報的錯誤行號仍與原檔一致
- 在 `testdata/integration/` 補一份使用 `$include "..."` 語法的 zone fixture，讓整合測試能覆蓋此路徑
- `internal/zone/parser_test.go` 增加單元測試，涵蓋：
  - 單獨一行的 `$include "path"` 能成功載入被 include 的記錄
  - 大小寫變體 `$INCLUDE "path"` 同樣成功
  - `$include path`（未加雙引號，原本就支援的語法）仍然成功
  - TXT 記錄中的 quoted string（如 `@ IN TXT "v=spf1 -all"`）不受前處理影響
- README 的「BIND 相容性」段落新增一條，明記支援 `$INCLUDE` 加雙引號路徑

## Non-Goals

- 不擴充其他 BIND 專屬 directive（`$GENERATE`、`$DATE` 等），本 change 只處理 `$INCLUDE` 雙引號語法
- 不 fork 或 patch upstream 的 miekg/dns；維持為外部相依
- 不改變 zone 檔的絕對 / 相對路徑解析語意；只影響 token 層的引號處理
- 不處理 BIND named.conf 的載入；本 change 範圍只在 zone 檔（`$INCLUDE` directive）

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `zone-parser`: 放寬「Parse RFC 1035 master zone files」requirement 的 `$INCLUDE` 接受語法；允許檔名為雙引號包裹的字串

## Impact

- Affected specs: `zone-parser`
- Affected code:
  - `internal/zone/parser.go` — 新增前處理 reader，包裝原本傳給 `dns.NewZoneParser` 的 `io.Reader`
  - `internal/zone/parser_test.go` — 新增對應單元測試
  - `testdata/integration/` — 新增一份使用雙引號 `$include` 的 zone fixture 與對應 include 片段
  - `README.md` — 「BIND 相容性」段落新增條目
- 不影響：`dns-server`、`zone-transfer`、`alias-resolver`、`view-matcher`、`config-loader`、`sighup-reload`
- 無行為退化風險：原本能解析的 zone 檔前處理後為相同內容（正則不匹配即原樣傳遞）
