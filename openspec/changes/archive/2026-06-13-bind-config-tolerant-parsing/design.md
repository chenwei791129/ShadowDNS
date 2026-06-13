## Context

ShadowDNS 的目標是成為 BIND 的 drop-in 替代品:使用者以 `--named-conf` 指向現成的 `/etc/bind/named.conf` 即可遷移。但目前 named.conf 解析器(`internal/config/zones.go`)在頂層與 view scope 採「白名單 + 未知即 fatal」姿態,而 `internal/config/match.go` 對看不懂的 match-clients rule 也直接回 fatal error。真實 BIND 設定常見的 `acl` / `key` / `controls` / `statistics-channels` 頂層 block、view 內的 `allow-query` / `allow-recursion`、`named.conf.default-zones` 內的 `type hint` root zone、以及具名 acl 參照,通通會讓 ShadowDNS 在啟動時 fatal,使遷移無法進行。

值得注意的是,解析器其他部分早已是寬鬆姿態:`options{}` 未知選項警告後略過、`logging{}` 完整容忍 channel/category 語法、zone body 未知 directive 靜默略過。本 change 是把這個既有的寬鬆姿態擴展到頂層、view、zone-type 與 match-clients,使全面一致。

這是 BIND 相容路線圖三個 change 的第一個(本案 → `bind-named-acl-match-clients` → `bind-migration-docs-examples`)。

## Goals / Non-Goals

**Goals:**

- 任何語法正確的 BIND named.conf 都能載入而不 fatal;只有真正的語法錯誤(大括號不平衡、缺 `;`)才 fatal。
- 對被忽略的存取控制 directive 給出清楚的 WARN,讓 operator 知道 ShadowDNS 不強制它。
- 對無法評估的存取控制一律 fail-closed(往「不服務」倒),絕不 fail-open。

**Non-Goals:**

- 不解析 `acl` block 內容、不解析 match-clients 的具名參照(屬 `bind-named-acl-match-clients`)。
- 不採用 BIND 的 ordered first-match + `!` 否定 address-match-list 語義(屬 `bind-named-acl-match-clients`)。
- 不改 named.conf 載入路徑機制。
- 不寫 migration guide / deb 範例(屬 `bind-migration-docs-examples`)。
- 不實作 `allow-query` / `allow-transfer` 的 ACL 強制,只 WARN 表示忽略。

## Decisions

### 頂層與 view scope 改為 skip-unknown 解析姿態

把 `internal/config/zones.go` 頂層 dispatch 與 view body dispatch 的「未知 directive 即 fatal」改成「未知即略過」。這對齊 `options{}` / `logging{}` / zone-body 既有的寬鬆姿態,使整個解析器一致。略過時依「分層 log 策略」決定 log level。

替代方案:逐一列舉所有 BIND statement 進白名單 — 否決,BIND 文法有上百個 statement,列舉永遠追不上且維護成本高;「未知即略過」是唯一可持續的姿態。

### 頂層 skip helper 處理 `keyword [name|IP] { ... };` 與 `keyword value;` 兩種形狀

頂層 block(`acl "x" { }`、`key "x" { }`、`controls { }`、`server <IP> { }`、`masters <name> { }`)在 keyword 後、`{` 前可能有 name 或 IP token。新增一個 skip helper:消化 keyword 後的可選 token 直到遇到 `{`(則委派既有的 `skipBalancedBraceBlock`)或遇到 `;`(則為單值 directive)。重用 `internal/config/options.go` 既有的 `skipBalancedBraceBlock` / `skipOptionValue`,不重造輪子。

替代方案:沿用 `skipOptionValue`(它對 `{` 前的 name token 會誤判,把 block 內容當成 until-`;` 掃描而破壞同步)— 否決,會解析錯位。

### 非-master zone type 略過整個 zone 而非 fatal

zone `type` 不是 `master`(`hint` / `forward` / `stub` / `slave` / `secondary` / `redirect` / `mirror` 等)時,不再回 unsupported-type fatal error,而是完整消化該 zone block 後將其**丟棄**(不 append 進 view、不進 BuildState、不開檔),並依分層 log 記一條 INFO。zone body 內既有的「未知 directive 靜默略過」行為保留,所以 `forwarders { }` / `masters { }` 等 body 內容會被 `skipBalancedBraceBlock` 正確消化。

替代方案:列舉「容忍的 recursion/次級 type 族」白名單 — 否決,與「未知即略過」原則不一致,且未來 BIND 新增 type 又要追;只要「非 master 即略過」最簡單且涵蓋未來。

### match-clients 無法評估的 rule 採 fail-closed 而非 fatal

`internal/config/match.go` 的 `parseOneRule` 遇到看不懂的 token(具名 acl 參照、`!` 否定、巢狀清單)時,不再回 `unknown rule` fatal error,而是把該 token 視為**被丟棄的 rule**:不 append 進 rules slice。

**logging 的歸屬**:`ParseMatchClients(body, path, startLine)` 本身既無 logger 也無 view 名,無法自行記「指名 view」的 WARN。因此 `ParseMatchClients` 改為把被丟棄的 token(連同行號)回傳給呼叫端;`parseView`(`internal/config/zones.go`,握有 `viewName` 與 `logger`)據此記 WARN。`resolveTopLevelZones` 內合成 `any;` 的呼叫端不會產生 drop,忽略該回傳即可。

被丟棄的 rule 使該 view 的 `Rules` slice 縮減;在 view 比對時等同「永不命中」。若某 view 的 match-clients 全數被丟棄,其 `Rules` 為空,`Matcher.Resolve` 的內層迴圈跑零次、永不回傳該 view,即不服務其 zone(fail-closed),而非退化成 `any`(fail-open)。

這是不可退讓的安全底線:看不懂的存取控制往安全側倒。`Matcher.Resolve`(`internal/view/matcher.go`)對空/縮減 `Rules` 的 view 既有行為就是「不命中、fall through」,**matcher 不需改 code**;fail-closed 結果由既有「no matching view → caller 回 REFUSED」語義支撐。

替代方案:(a) 維持 fatal — 否決,破壞 drop-in;(b) 把無法評估的 rule 當 `any` — 否決,fail-open 會把 zone 對全世界放送,是安全退化。

### 分層 log 策略

略過 directive 時依類別決定 log level:

- **WARN**:存取控制 directive 被忽略 — curate 名單 `allow-query` / `allow-recursion` / `allow-transfer` / `allow-update` / `allow-notify` / `blackhole`。訊息明示「ShadowDNS 不強制此 ACL」。match-clients rule 被 fail-closed 丟棄時亦 WARN。
- **INFO**:recursion 族(`recursion` / `forwarders` / `dnssec-validation`)與被略過的非-master zone type。這些是 drop-in 預期會遇到的,用 INFO 避免 blessed 設定每次啟動/reload 噴 WARN。
- **靜默 / DEBUG**:其餘未知頂層或 view directive。

名單以小集合常數維護,落在被忽略 directive 集合外者歸 INFO/DEBUG。

### 移除失效的 rejectedTopLevel 黑名單

`internal/config/zones.go` 既有的 `rejectedTopLevel` 黑名單(`dnssec-enable` / `allow-update` / `controls` / `key` / `acl` / `server`)在現行碼中與「未知 keyword」走相同 fatal 分支,無實際區別效果。解析姿態翻轉後,這些 keyword 都改走 skip 路徑,黑名單變成死碼,予以移除。

### 新增 viewless + default-zones 整合測試 fixture

在 `testdata/integration/` 下新增獨立的 BIND-compat fixture 目錄(不擠進現有 view-based 共用 fixture),內含一份 Debian 風格的 viewless 設定:`named.conf` include `named.conf.options` / `named.conf.local` / `named.conf.default-zones`,其中 default-zones 含 `zone "." { type hint; }` 與 localhost/127/0/255 的 `type master` zone(各帶 db 檔)。整合測試斷言:設定載入成功不 fatal、`type hint` zone 被略過、localhost 等 `type master` zone 正常 serve、頂層若含 `acl`/`key`/`controls` 不 fatal、match-clients 含具名參照時該 view fail-closed 不服務。`test/integration/helpers_test.go` 增補載入此 fixture 的 helper。

## Implementation Contract

**Behavior**:以 `--named-conf` 指向任何語法正確的 BIND named.conf(含 `acl`/`key`/`controls`/`server`/`statistics-channels` 頂層 block、view 內 `allow-query` 等、`type hint`/`forward`/`slave` zone、match-clients 具名 acl 參照)時,ShadowDNS 啟動不 fatal。`--dry-run` 行為一致。

**解析姿態**:
- 頂層與 view scope 的未知 directive → 略過(消化其 value 或 balanced block),不 fatal。
- zone `type != master` → 該 zone 被丟棄,不 serve、不開檔。
- match-clients 無法評估的 rule → 丟棄,等同永不命中。

**Failure modes**:
- 真正的語法錯誤(大括號不平衡、缺 `;`、unterminated block)→ 維持 fatal,訊息含 file:line。
- 存取控制被忽略 → 不 fatal,記 WARN。
- match-clients 全數無法評估的 view → fail-closed(該 view 不匹配任何 client、不服務其 zone),記 WARN;**絕不** fail-open 成 `any`。

**Logging contract**:
- WARN:被忽略的存取控制 directive(`allow-query`/`allow-recursion`/`allow-transfer`/`allow-update`/`allow-notify`/`blackhole`)、被 fail-closed 丟棄的 match-clients rule。
- INFO:被略過的 recursion 族 directive、被略過的非-master zone type。
- DEBUG/靜默:其餘未知 directive。

**Acceptance criteria**:
- 新增的 `test/integration/bind_compat_test.go` 對 viewless+default-zones fixture 斷言上述載入成功、type hint 略過、localhost master zone serve、頂層 acl/key/controls 不 fatal、match-clients 具名參照 fail-closed。
- `internal/config/zones_test.go` / `internal/config/match_test.go` 覆蓋:頂層未知 block 略過、view 未知 directive 略過、非-master type 略過、match-clients 無法評估 rule 丟棄+fail-closed、各層 log level。
- `make test` 通過(race detector);`make lint` 通過。

**Scope boundaries**:
- 範圍內:`internal/config/zones.go`、`internal/config/match.go` 的解析姿態與 log;整合測試 fixture 與測試。
- 範圍外:acl 內容解析、具名參照解析、first-match/否定語義(`bind-named-acl-match-clients`);migration guide、deb 範例(`bind-migration-docs-examples`);ACL 強制。

## Risks / Trade-offs

- [把存取控制 directive 靜默忽略 → operator 誤以為 ACL 仍生效,造成安全退化] → 對所有被忽略的存取控制 directive 強制 WARN;對無法評估的 match-clients 採 fail-closed 而非 fail-open;在 `bind-migration-docs-examples` 補文件說明 ShadowDNS 的存取控制模型。
- [skip-unknown 過於寬鬆 → 真正打錯字的 directive 被默默略過,設定錯誤不易察覺] → 接受此 trade-off(BIND 自身對未知 statement 也是 fatal,但 ShadowDNS 的 drop-in 目標優先;打錯字的存取控制至少會 WARN)。INFO 層保留可觀測性。
- [頂層 skip helper 對 `keyword name { } ` 形狀解析錯位 → 破壞後續 token 同步] → 以 `internal/config/zones_test.go` 對 `acl`/`key`/`controls`/`server`/`masters` 各形狀逐一測試,確認消化邊界正確。
- [fail-closed 讓既有 GeoIP/IP-based 設定(本來能評估)意外不服務] → 只有「無法評估」的 rule 才 fail-closed;`any`/geoip/IP/CIDR 等既有可評估 rule 行為完全不變,以測試固定既有行為。

## Migration Plan

無資料遷移。部署:建置 `.deb` 部署到 bench-ns2,確認既有 view-based 設定行為不變(回歸),並以一份 BIND 風格設定確認載入不 fatal。rollback:還原前一版 `.deb`。實作完成後依 perf-guard 規則跑效能回歸(動 `internal/config`,屬 load-time,但規則要求跑)。

## Open Questions

(none — 開放點已於 Decisions 敲定:存取控制名單、fail-closed 語義、skip helper 形狀處理。)
