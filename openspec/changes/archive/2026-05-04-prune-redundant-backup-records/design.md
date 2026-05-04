## Context

ShadowDNS 目前對 backup zone 的 runtime 行為（`internal/zone/classify.go`、`internal/alias/override.go`）已經穩定：只保留 TXT/MX/SRV、其它類型 drop、overlay 語意為「backup 對某 `(owner, type)` 有任何記錄就整組取代 root」。但 zone file 是 operator 人手維護的，實務上從 root 複製、改造後常常留著大量 runtime 無效的記錄。目前沒有工具能協助清理，proposal 要新增離線 CLI `shadowdns prune-backup`。

本 change 限定為 **CLI-only 的離線操作**：讀配置 → 比對 → dry-run 或寫檔；不跨服務、不改 runtime、不開 port。複雜度集中在「如何把 parse 後的 RR 決定精準地映射回原檔行號，讓刪除不破壞 relative name 與 directive」。

## Goals / Non-Goals

**Goals**
- 提供可離線、可審視（dry-run）的 backup zone 清理工具。
- 遵守既有 runtime 語意：只要 runtime 不會 serve 的、或 runtime 答案與 root rewrite 完全等價的，就是候選刪除。
- 保留 operator 手寫格式：`$TTL` / `$ORIGIN` / `$INCLUDE` / `$GENERATE` directive、relative owner name、行尾註解。
- 支援遞迴處理 `$include` 檔。

**Non-Goals**
- 不改 runtime：`OverridableTypes` 集合、`filterBackupRecords` 行為、`alias.Resolve` 流程都不動。
- 不嘗試保留空行或獨立註解行（operator 已接受）。
- 不處理 CNAME 成為 overridable 類型的可能性（可另開 change）。
- 不實作跨行 RR 範圍偵測以外的複雜 round-trip（例如不嘗試還原 `$GENERATE` 展開後的逐筆記錄）。
- 不提供 HTTP / API 觸發入口，也不整合到 `shadowdns reload`。

## Decisions

### Decision: 採用 line-based 刪除而非 parse-serialize

Parse → `miekg/dns.RR.String()` → 寫回檔，是最短路徑，但會把 relative owner 展開成 FQDN、丟失行尾註解、把多行 RR 壓成單行。Operator 體驗很差，會降低採用意願。

改採 line-based：先用 parser 得到「要刪哪些 RR」，再對原檔做以行為單位的刪除。被保留的 RR 行原封不動寫回；relative owner、`$TTL` 相對值、行尾註解、$INCLUDE 指令都保留。空行與獨立註解行則在 prune 的同一 pass 中一併剝除——proposal 已在 Non-Goals 中說明這是 operator 同意的取捨，等同把 prune 也當成一次 normalization，讓輸出與 operator 可持續維護的形態一致。

**Alternatives**:
- Pure parse-serialize：被 reject（FQDN 展開不可接受）。
- Mixed：保留原行且一路保留空行/註解。被 reject，因為不執行任何 normalization 會讓檔案越來越雜亂，operator 已明確表示傾向刪除。

### Decision: RR → 行號映射透過自寫 line lexer，而非 miekg/dns 內部行號

`miekg/dns.ZoneParser` 在 Err() 等 API 上會透露行號，但沒有穩定公開的 per-RR line 欄位。強行反查其內部實作會耦合未來升版風險。

改由本 package 自寫一個輕量 line lexer：
- 逐行讀 raw bytes，追蹤 `line_start` / `line_end`（1-based）。
- 偵測 directive 行：以 `$` 開頭（允許前導 whitespace）。
- 偵測獨立註解行：strip 後以 `;` 開頭或為空。
- 偵測 RR 起始：非 directive、非獨立註解；若包含未關閉 `(` 進入 multi-line 模式，吃到對應 `)` 行為止。
- 把每個 RR 的 raw text 單獨餵給 `dns.NewRR`（attach 當前 `$ORIGIN` / `$TTL` context）得到可比對的 RR 值；比對後決定丟棄或保留原 line range。

這條路只依賴 miekg/dns 的單行 `NewRR`，不碰 ZoneParser 內部狀態，穩定且易於單元測試。

**Alternative**: 直接 fork/inline miekg/dns 的 ZoneParser 加行號欄位。被 reject，維護成本高。

### Decision: RRSet 相等比對以 "sorted rdata set" 為準

同一 `(owner, type)` 下，RRSet 順序無意義（DNS 協議層級亦如此）。比對方法：
1. 取 backup RRSet 與 root RRSet 的 canonical rdata 字串（`rr.String()` 去掉 header 的 ttl、用 lowercase owner）。
2. 各自 sort 成 `[]string`。
3. `reflect.DeepEqual` 或 `slices.Equal`。
TTL 差異**不視為**差異（TTL 是 operator 可調參數，不涉及語意）。class 目前全站只有 IN。

**Alternative**: 用 `dns.IsDuplicate` 逐筆比對。被 reject，因為它對 header TTL 的敏感度與我們想要的「語意相等」不完全一致，加上我們本來就要 set-level 比較，自寫更透明。

### Decision: SOA 與 zone apex 的 NS 永遠豁免

SOA 是 zone file 合法性的必要條件（BIND 等工具讀檔會報錯），NS 在 apex 屬於 zone 宣告自己的權威伺服器，兩者都不屬於「從 root 複製的冗餘」。儘管 runtime 的 `filterBackupRecords` 會把它們 drop，zone file 本身必須留著，否則 operator 下次 parse 這個檔（例如用 BIND、named-checkzone）會失敗。

豁免範圍限定：
- 任何 SOA（全 zone 只會有一個，位於 apex）。
- `owner == backup zone origin` 且 `type == NS` 的 RRSet。
- 非 apex 的 NS（sub-delegation）屬於非 overridable，runtime 不 serve，按一般規則刪。

### Decision: `$include` 檔遞迴處理，directive 本身保留

被 include 的檔在 merge 後構成 backup zone 的完整 RR 集合；要不要刪某筆記錄，需要看它在 merged set 中的歸屬與 root 的對應關係。實作上：
1. Parse 階段遞迴展開 `$include`，得到「merged RR list」與「每筆 RR 的來源 (file, line range)」。
2. 比對階段在 merged 層級做判定。
3. 刪除階段對每個實體檔各自處理（main + 每個 include target）。
4. Main 檔中的 `$include "path"` 這一整行**永遠保留**（不管被 include 的檔最終是否變空）。若 include target 被清空剩下不到一筆 RR，檔案會只剩 directive 與 SOA/NS 豁免；允許但會在 INFO log 提示。

`$include` 的 path 解析遵循 named.conf 的 `directory` 選項：相對路徑以 `directory` 為 base，與 `internal/zone/parser.go` 的 `rewriteBindIncludes` 現行行為保持一致。

### Decision: 預設 dry-run，`--apply` 才寫檔；寫前自動 `.bak`

破壞性操作預設零副作用。`--apply` 觸發寫入流程：
1. 對每個實體檔：rename 原檔為 `<path>.bak`（覆寫既有 `.bak`——連續跑兩次 apply，第二次 `.bak` 是第一次 apply 的結果；若要保留原始 pristine 備份，operator 自行 git-commit 或先手動備份）。
2. 寫入新內容到原檔路徑（write tmp → fsync → rename，確保 crash-safe）。
3. 任一檔寫入失敗 → 停止整個 run，回報錯誤；先寫成功的檔已成既成事實，依 `.bak` 恢復。
**Alternative**: 全部先寫 tmp、最後一起 rename（兩階段 commit）。被 reject，實作複雜度偏高，且跨多檔原子性本來就無法真正保證；`.bak` + 失敗即停足以作為運維工具的安全邊界。

### Decision: 多 view 處理策略：各 view 獨立

ShadowDNS 透過 view 把同一 zone origin 對應到不同檔案（如 `backup.example_view-th.fwd` vs `backup.example_view-other.fwd`）。prune 以 `(view, zone origin)` 為處理單位，每個 view 內部計算該 view 下的 root 對應關係。因此同一 origin 在不同 view 下可能刪得不一樣多——這是正確的，因為它們本來就是獨立 zone instance。

### Decision: Exit code 語意

- `0`：dry-run 完成（即使發現冗餘）或 apply 成功。
- 非 0：config/named.conf 載入失敗、zone file parse 失敗、apply 寫檔失敗。
- **找到冗餘本身不是錯誤**：prune 是建議性工具，operator 可以選擇不 apply。

## Risks / Trade-offs

- [Line lexer 對邊界 case 解析不精準] → 先針對 testdata/integration 下現存 backup zone file 建立 golden test，涵蓋 SOA 多行、行尾註解、`$include`、`$TTL`、relative owner；任何新 case 先加 golden。
- [`$GENERATE` 未展開比對] → 宣告 Non-Goal；prune 永不刪 `$GENERATE` directive 或其「未展開時看不到的 record」；INFO log 警示 operator 該檔含 `$GENERATE`、人工檢視。
- [operator 已有 `.bak` 被 `apply` 蓋掉] → 在 apply 開始前，若任一目標檔已存在 `.bak`，INFO 提示但仍覆蓋；若要嚴格保護，operator 自行 `cp -a *.fwd /backup/...`。
- [crash between two file writes] → 失敗停止 + `.bak` 恢復；文件中明示此邊界。
- [誤判 RRSet 相等導致刪除 overlay 意圖] → RRSet 級別比對 + 任何差異全保留；測試涵蓋「只差一筆」「只差 rdata」「完全相等」三組情境。
- [RR 解析依賴當前 `$TTL` / `$ORIGIN` context] → line lexer 在 scan 過程中維護 active `$TTL` 與 `$ORIGIN`，feed 給 `dns.NewRR(line, currentOrigin, currentTTL)`；single-line 形式的 RR 若缺省 TTL，以 active `$TTL` 補上（miekg/dns `NewRR` 行為）。

## Migration Plan

- 首發版本純新增 CLI sub-command，不動任何既有檔、不改 config schema。
- 不需 feature flag、不需 config migration。
- 發布後 operator 可先在 test host（bench-ns2）跑 dry-run 審視輸出，再決定是否 apply。
- 回滾：若 sub-command 輸出有誤，operator 不要 apply；若 apply 後發現錯誤，用 `.bak` 還原即可。

## Open Questions

- 是否需要額外支援 `--json` 輸出 dry-run 結果供外部工具串接？本版不納入，保留給未來。
- 是否需要 `--zone <origin>` 只處理單一 zone？本版固定全掃；若 zone 數量多 operator 有 partial-apply 需求再加。
