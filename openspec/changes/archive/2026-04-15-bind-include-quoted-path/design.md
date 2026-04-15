## Context

ShadowDNS 使用 [miekg/dns](https://github.com/miekg/dns) v1.1.72 的 `ZoneParser`（`internal/zone/parser.go:39-43`）解析 RFC 1035 master zone 檔，並透過 `SetIncludeAllowed(true)` 啟用 `$INCLUDE` directive。miekg 的 scanner 在 `$INCLUDE` 後只接受 `zString` token（未加引號字串）；當 zone 檔寫成 `$include "path"` 時，`"` 會被 tokenize 成 `zQuote`，解析器回傳：

```
dns: expecting $INCLUDE value, not this...: "\"" at line: L:10
```

此語法在 BIND 9 zone 檔中合法且常見（路徑含空白或特殊字元時更必要）。由許多 BIND 配置自動化工具生成的 zone 檔都採用雙引號寫法，因此 ShadowDNS 要宣稱相容 BIND 便無法把問題推給維運方。

**相關檔案**：
- `internal/zone/parser.go` — 唯一呼叫 `dns.NewZoneParser` 的位置
- miekg/dns `scan.go:406-408`（`zExpectDirInclude` state）— 上游拒絕引號的原因
- `testdata/integration/master/*.fwd` — 目前的 fixture 未覆蓋 `$INCLUDE` 任一形式

## Goals / Non-Goals

**Goals:**

- 讓 `$INCLUDE "path"` 與 `$INCLUDE path` 兩種語法都能被 `zone-parser` 成功載入
- 不改變 miekg/dns 的行為，也不 fork 或 patch 上游；維持為純相依
- 錯誤行號回報仍與原始 zone 檔一致（不因前處理而偏移）
- TXT 等 RR 中的合法 quoted string 不受影響（例：`@ IN TXT "v=spf1 -all"`）

**Non-Goals:**

- 不支援 BIND 專屬 directive `$GENERATE`、`$DATE`、`$ORIGIN` 以外的延伸
- 不支援 named.conf 載入（僅處理 zone 檔）
- 不改變相對路徑 / origin 繼承語意（去引號後的 path 直接餵給 miekg，由 miekg 維持既有行為）
- 不展開 include（仍由 miekg 負責）

## Decisions

### Decision 1: Pre-processing wrapper instead of fork

在 `internal/zone/parser.go` 中用一層自訂 `io.Reader`（或在 `os.Open` 後以 `bufio.Scanner` 讀完產生替代 buffer）先對輸入 zone 檔做前處理：逐行檢查，若該行（剔除前導空白後）以 `$INCLUDE` 或 `$include` 開頭，就把接在 directive 之後的第一段雙引號包裹片段改成去引號版本；其他行原樣輸出。產出的新 reader 再交給 `dns.NewZoneParser`。

**為何不 fork miekg/dns**：
- upstream 已穩定、被廣泛使用；維護 fork 長期成本高、升級 rebase 痛
- 解法只需在 ShadowDNS 這層處理一種語法糖，不值得分叉整個函式庫
- 日後若 upstream 願意接受支援雙引號的 PR，可以無痛拿掉 wrapper

**為何不自行展開 include**：
- 需要自己管相對路徑、origin 繼承、`$ORIGIN` directive 互動，等於重寫一段 zone parser
- miekg 已處理這些細節，重做只增加出錯面積

**alternative**：直接 regex replace 整個檔案也可行，但逐行 + 行首比對更安全；TXT 記錄裡的 `"..."` 絕不可能出現在行首（zone 檔首欄是 owner name 或 directive），所以行首錨定就足以隔離。

### Decision 2: Line-anchored token-level matching

前處理時，行的識別規則：
1. 去除前導空白後，檢查是否為 `$INCLUDE` 或 `$include`（case-insensitive，BIND 明確不分大小寫）後跟至少一個空白字元
2. 取 directive 後的剩餘字串，跳過其餘空白，若下一個 non-space 字元為 `"`，則要求找到下一個配對的 `"`，把這兩個雙引號刪除；之後內容原樣保留（origin 參數、行尾註解等）
3. 若沒有配對的結尾 `"`，保持原樣不動（讓 miekg 照樣回報錯誤，不要自己吞錯誤）
4. 其他一律不動

**為何不處理註解內的 `$INCLUDE`**：註解以 `;` 開頭，第一個 non-space 字元會是 `;` 不是 `$`，自然不會被匹配。

**為何不用 full regex**：BIND 的 `$INCLUDE` 語法實際有三段（directive、path、optional origin），用狀態機逐段掃描比 regex 更容易精準處理「只去掉 path 的引號，保留 origin」。

### Decision 3: Preserve line numbers

前處理只做「把 `"` 和 `"` 這兩個字元替換為空白字元」而非「整個刪除」。這樣：
- 行數與原檔 1:1 對應
- 同一行後方 column 也幾乎對齊（只差兩個字元）
- miekg 後續回報的 `line:col` 對 operator debugging 幾乎一致

如果「替換為空白」會產生語法問題，退一步的做法是完全刪除雙引號並在 logger 裡把原檔案路徑與行號關聯起來；但實測上 miekg scanner 對多餘空白是容許的（空白即 `zBlank` 分隔），所以用空白替換更簡單。

### Decision 4: Fixture domain naming for testdata

新增的 zone fixture 統一使用 [RFC 2606 / RFC 6761](https://www.rfc-editor.org/rfc/rfc6761) 保留測試域名（`example.com`、`example.org`、`test`）與 `192.0.2.0/24` 測試 IP 範圍。include 子片段的檔名採用與既有 `testdata/integration/master/*.fwd` 一致的風格（如 `example.com_view-th.fwd`），額外新增：
- `testdata/integration/master/example.com_include.fwd`（主 zone，含 `$include "…"` 語法）
- `testdata/integration/master/cnames/example.com_cname`（被 include 的 CNAME 片段）

**為何不重用既有 fixture**：既有 fixture 對應其他測試場景的固定 record 集合，若塞入 `$INCLUDE` 行可能影響其他 test。新增獨立 fixture 隔離 blast radius。

## Risks / Trade-offs

- **[風險] 前處理誤傷非 `$INCLUDE` 的 quoted data**
  → **緩解**：行首錨定（只有行開頭是 `$INCLUDE`/`$include` directive 才進入處理邏輯）；TXT / HINFO 等 RR 的 owner name 不可能是 `$INCLUDE`。單元測試中包含「TXT 記錄的 quoted string 不受影響」情境。

- **[風險] 未配對引號被悄悄吞掉**
  → **緩解**：Decision 2 明訂未配對時保持原樣，讓 miekg 的錯誤訊息照常出現；不試圖修復壞語法。

- **[風險] 大檔案前處理導致記憶體 / 啟動延遲增加**
  → **緩解**：以 `bufio.Scanner` 逐行掃描並輸出到一個包裹的 `io.Reader`（例如 `io.Pipe` 搭配 goroutine，或一次讀入 `bytes.Buffer` — 後者簡單且 zone 檔通常 < 100MB）。在 benchmark 可加 baseline 比較；若顯著退化再換 streaming 策略。

- **[trade-off] ShadowDNS 成為 BIND 語法糖的負責方**
  → 本 change 引入首個「BIND-isms」前處理點；若未來還要處理其他 BIND 擴充（如 `$GENERATE`），應把前處理抽成獨立 sub-package（例如 `internal/zone/bindcompat`）。此次 change 不必提前抽離，但建議在實作時讓前處理函式在 `parser.go` 中具名化、易於日後搬遷。

## Migration Plan

- 向下相容：原本能成功解析的 zone 檔內容不受影響，只是多走一次行掃描
- 無設定旗標：前處理永遠啟用；沒必要為這種相容性暴露 knob
- 回退：若發現對某個 edge case 造成 regression，可在 `parser.go` 把 wrapper 拿掉一行回到原 reader
