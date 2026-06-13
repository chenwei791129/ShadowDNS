## Context

`bind-config-tolerant-parsing`(Change A)讓任何合法 BIND named.conf 載入不 fatal,但刻意不理解 `acl` 定義與較豐富的 `match-clients` 文法:頂層 `acl` 被略過,而 `match-clients` 中的具名參照、`!` 否定、巢狀 `{ }` 群組會被丟棄並 fail-closed(該 view 不服務)。

真實 BIND split-horizon 幾乎都用具名 ACL 表達 client 群(`acl "internal" { 10.0.0.0/8; }; view "internal" { match-clients { internal; }; }`)與否定(`match-clients { ! 192.0.2.0/24; any; }`)。Change A 下這些 view 不服務 — 設定能載入卻不運作。本 change 讓 view-matcher 真正理解具名 ACL、否定、巢狀群組與 BIND 內建 ACL,並以 BIND 的有序 address-match-list 語義評估。

現況(已查證):`internal/config/match.go` 的 `MatchRule` 介面有 `CountryRule`/`ASNRule`/`IPRule`/`CIDRRule`/`AnyRule`;`ParseMatchClients(body, path, startLine)` 逐 token 解析;`internal/view/matcher.go` 的 `Matcher.Resolve` 對每個 view 逐 rule 評估、任一 rule 命中即選該 view(等同 OR、無否定)。view-matcher spec 既有「Resolve client IP to a view using first-match semantics」需求,已是「第一個命中的 rule 選定該 view」,但無否定概念。

## Goals / Non-Goals

**Goals:**

- 頂層 `acl` 被解析儲存;`match-clients` 支援具名參照、`!` 否定、巢狀群組、內建 ACL。
- 以 BIND 有序 first-match + 否定語義評估 address-match-list,使 ACL-based split-horizon 真正運作。
- 未定義的具名參照維持 fail-closed(沿用 Change A 的安全底線)。

**Non-Goals:**

- 容忍式解析姿態、非-master zone 略過、fail-closed doctrine 本體(由 Change A 提供)。
- 強制 `allow-query` / `allow-transfer`(仍只 WARN 忽略)。
- address-match-list 內的 TSIG `key` 元素 — 解析後略過,不評估。
- migration guide / deb 範例(屬 `bind-migration-docs-examples`)。

## Decisions

### 頂層 acl 解析並儲存為具名 registry

頂層 `acl "<name>" { <address-match-list>; };` 由 `internal/config/zones.go` 解析(Change A 下走 skip,本 change 改為解析),其 body 用與 `match-clients` 相同的 address-match-list 解析器解析成元素清單,存進 `Config` 上的具名 registry(`map[string][]Element`,name 大小寫處理與 BIND 一致)。重複定義同名 acl → 以最後一筆為準並 WARN(對齊既有 duplicate 處理風格)。

替代方案:把 acl 內容在解析當下就 inline 展開進每個引用點 — 否決,巢狀參照與否定無法用扁平 concat 正確表達(見遞迴解析決策)。

### 統一的 address-match-list 元素模型

`match-clients` 與 `acl` body 共用一個 address-match-list 解析器,產出 `[]Element`。`Element` 在既有 rule 種類(country/asn/ip/cidr/any)之上增加:`Negated bool`(`!` 前綴)、具名參照(payload 為 acl 名稱,build 時解析為指向目標元素清單的指標)、巢狀群組(payload 為子 `[]Element`)、內建 ACL(`any`/`none`/`localhost`/`localnets`)。`match-clients` 改用此元素模型取代現有的扁平 `[]MatchRule`(既有 country/asn/ip/cidr/any 行為以正向、未否定元素表達,維持等價)。

替代方案:沿用 `[]MatchRule` 另加並行欄位表達否定/參照 — 否決,兩套結構並存易不同步。

### 有序 first-match + 否定(reject)評估語義

view 比對改為:對每個 view,以 `listAccepts(view.elements, client)` 判斷是否接受該 client;接受才選此 view,否則 fall through 下一個 view。`listAccepts` 依宣告序評估元素,第一個「命中」的元素決定結果:正向元素命中 → 接受(true);否定元素命中 → 拒絕(false,停止此清單);皆未命中 → 不接受(預設 deny,false)。這把 `Matcher.Resolve` 的內層「任一 rule 命中即選」改為「first-match 決定 accept/reject」,讓 `! 192.0.2.0/24; any;`(除該網段外皆接受)可正確表達。既有「無否定」設定行為等價(第一個正向命中即 accept,與舊 OR 在無否定時同結果)。

替代方案:維持 OR、否定另作特例 — 否決,無法表達 BIND 的「先否定後 any」常見慣用法。

### 具名參照遞迴解析,未定義名稱 fail-closed

具名參照與巢狀群組的「命中」定義為:該參照/群組的子清單對 client 回 `listAccepts == true`。否定再套用於整個參照/群組。解析在所有 acl 載入後的 build 階段進行:把參照解析為指向目標元素清單的指標(query hot path 不做 map 查找);偵測未定義名稱 → 丟棄該元素 + WARN + 該元素永不命中(fail-closed,沿用 Change A);偵測參照環(a→b→a)→ 斷開 + WARN。

替代方案:query 時才查 registry — 否決,hot path 多餘成本。

### 內建 ACL any/none/localhost/localnets

`any` → 永遠命中(正向接受);`none` → 永不命中(等同空,常與其他元素組合);`localhost` → server 本機所有位址(loopback + 各介面已指派位址);`localnets` → 各介面所屬網段(CIDR)。`localhost`/`localnets` 於啟動/build 時以 Go `net` 介面列舉解析為具體位址/網段集合,之後比對純線性,不做 syscall。

替代方案:`localhost`/`localnets` 標為不支援走 fail-closed — 否決,真實設定會用到;以介面列舉實作邊界清楚。

### viewless 與 ACL-based 整合測試

擴充 Change A 的 `test/integration/bind_compat_test.go`:加入定義 `acl` 並於 `match-clients` 具名引用、`!` 否定、巢狀群組、內建 ACL 的 view-based fixture,斷言指定來源 IP 落入/落出對應 view、否定語義正確、未定義參照的 view fail-closed 不服務。

## Implementation Contract

**Behavior**:含 `acl "x" { ... };` 定義並於 view `match-clients { x; }`(或 `! x;`、`{ a; b; }`、`localhost`/`localnets`/`none`)引用的 BIND 設定,view 選擇實際依 BIND 有序 address-match-list 語義運作。

**Interface / data shape**:
- `Config` 新增具名 acl registry(`map[string][]Element` 或等義)。
- `match-clients` 與 acl body 共用解析器產出 `[]Element`(含 `Negated`、參照、巢狀、內建種類)。
- `Matcher` 以 `listAccepts(elements, srcIP, geoIP) bool` 遞迴評估;`Resolve` 對每個 view 呼叫一次,接受才回該 view 名。

**Failure modes**:
- 未定義具名參照 → 該元素丟棄 + WARN + 永不命中(fail-closed);整個 match-clients 因此空/全 reject 的 view 不服務。
- 參照環 → 斷開 + WARN。
- `geoip asnum` 等已知形式寫錯仍 fatal(承襲 Change A)。
- 真正語法錯誤仍 fatal。

**Acceptance criteria**:
- `internal/config/match_test.go`:解析具名參照、`!` 否定、巢狀群組、內建 ACL 成正確 `[]Element`;未定義參照丟棄不 fatal。
- `internal/view/matcher_test.go`:`listAccepts` 的有序 first-match + 否定(`! cidr; any;` = 除該網段外皆接受)、具名參照遞迴、內建 ACL、未定義參照 fail-closed。
- `test/integration/bind_compat_test.go`:ACL-based view-based fixture 端到端正確選 view。
- `make test`(race)與 `make lint` 通過;既有 GeoIP/IP split-horizon 行為不變(回歸)。

**Scope boundaries**:
- 範圍內:`internal/config/match.go`(元素模型 + 解析)、`internal/config/zones.go`(acl 解析儲存 + build 階段參照解析)、`internal/view/matcher.go`(listAccepts 語義)、相關測試。
- 範圍外:Change A 的容忍姿態;`allow-query`/`allow-transfer` 強制;TSIG key 評估;文件/範例(Change C)。

## Risks / Trade-offs

- [first-match + 否定改寫 `Matcher.Resolve` 內層 → 可能改變既有無否定設定的選 view 結果] → `listAccepts` 在「全正向、無否定」時與舊 OR first-match 等價;以既有 `internal/view/matcher_test.go` 案例固定既有行為防回歸。
- [遞迴參照/巢狀評估在 query hot path → 效能退化] → 參照於 build 時解析為指標、`localhost`/`localnets` 於啟動展開為具體 CIDR;query 期間純線性走訪、無 map 查找與 syscall。實作後依 perf-guard 量測。
- [`localhost`/`localnets` 介面列舉在容器/多介面環境語義不一] → 於啟動快照當時介面位址,reload 時重新列舉;文件(Change C)說明其解析時機。
- [acl 參照環造成無窮遞迴] → build 階段偵測並斷開 + WARN,評估器對已訪節點設防護。

## Migration Plan

無資料遷移。部署到 bench-ns2,以一份含具名 acl + 否定的 BIND 風格設定確認 view 選擇正確,並確認既有 view-based 設定回歸無誤。實作完成後依 perf-guard 規則量測(動 `internal/config` 與 `internal/view`,後者在 query hot path,務必跑)。rollback:還原前一版 `.deb`。

## Open Questions

- `localhost`/`localnets` 是否要在本 change 全實作,或先支援 `any`/`none`+具名參照、把 `localhost`/`localnets` 暫走 fail-closed?(傾向全實作,因 drop-in 會遇到;以 Go `net` 介面列舉,邊界清楚。)
