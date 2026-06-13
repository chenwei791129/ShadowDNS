## 1. 解析姿態翻轉

- [x] 1.1 在 `internal/config/zones.go` 實作「頂層與 view scope 改為 skip-unknown 解析姿態」:頂層 dispatch 與 view body dispatch 對未知 directive 改為消化其 value 或 balanced block 後略過,不再回 fatal;只有語法錯誤(大括號不平衡、缺 `;`)仍 fatal。滿足 Requirement「Tolerate unrecognized directives at top level and view scope」。驗證:`internal/config/zones_test.go` 新增頂層 `acl`/`controls`/`key` 被略過、view 內 `allow-query` 被略過、unbalanced brace 仍 fatal 的案例,`make test` 通過。
- [x] 1.2 實作「頂層 skip helper 處理 `keyword [name|IP] { ... };` 與 `keyword value;` 兩種形狀」:新增 helper 消化 keyword 後的可選 name/IP token,遇 `{` 委派既有 `skipBalancedBraceBlock`、遇單值則消化到 `;`,重用 `internal/config/options.go` 既有 skipper。驗證:`internal/config/zones_test.go` 對 `acl "x" {…}`、`key "x" {…}`、`controls {…}`、`server <IP> {…}`、`masters <name> {…}` 各形狀斷言 token 同步正確、後續 view 仍正常解析。

## 2. zone type 與 match-clients 容忍

- [x] 2.1 在 `internal/config/zones.go` 的 `parseZone` 實作「非-master zone type 略過整個 zone 而非 fatal」:`type` 非 `master` 時完整消化 block 後丟棄該 zone(不 append、不開檔),不再回 unsupported-type 錯誤;zone body 既有靜默略過保留。滿足 Requirement「Parse view and zone declarations from master.zones」。驗證:`internal/config/zones_test.go` 斷言頂層 `type hint`、view 內 `type forward` 被略過且不 fatal、`type master` 仍保留,`make test` 通過。
- [x] 2.2 在 `internal/config/match.go` 實作「match-clients 無法評估的 rule 採 fail-closed 而非 fatal」:`parseOneRule` 對不符任何已知形式的 token(具名 acl 參照、`!` 否定、巢狀群組)改為標記丟棄而非回 `unknown rule` error;`ParseMatchClients` 不 append 該 rule、改為把被丟棄的 token 與行號回傳給呼叫端,並由 `parseView`(握有 `viewName`/`logger`)記 WARN;`resolveTopLevelZones` 的 `any;` 呼叫端忽略該回傳。`geoip asnum` 等已知形式寫錯仍維持 fatal。滿足 Requirement「Parse match-clients rule syntax」。驗證:`internal/config/match_test.go` 斷言 `internal-net;` 被丟棄不 fatal、`geoip asnum "Chinanet";` 仍 fatal;`internal/config/zones_test.go` 斷言 parseView 對含被丟棄 rule 的 view 記 WARN 並指名 view。
- [x] 2.3 驗證 `internal/view/matcher.go` 對縮減/空 `Rules` 的 view「Fail closed when a match-clients rule cannot be evaluated」:`Matcher.Resolve` 既有行為對空 `Rules` 即永不命中、fall through,**不需改 matcher code**;確認被丟棄的 rule 絕不退化成 `any`、全數被丟棄的 view 不服務其 zone。滿足 Requirement「Fail closed when a match-clients rule cannot be evaluated」。驗證:`internal/view/matcher_test.go` 斷言只含被丟棄 rule(空 `Rules`)的 view 不被選中、且不被當 catch-all。

## 3. 分層 log 與清理

- [x] 3.1 實作「分層 log 策略」:curate 存取控制 directive 名單(`allow-query`/`allow-recursion`/`allow-transfer`/`allow-update`/`allow-notify`/`blackhole`)→ 略過時 WARN;recursion 族(`recursion`/`forwarders`/`dnssec-validation`)與被略過的非-master zone type → INFO;其餘 → DEBUG/靜默。驗證:`internal/config/zones_test.go` 以 observed zap logs 斷言 `allow-query` 出 WARN、recursion 族出 INFO。
- [x] 3.2 實作「移除失效的 rejectedTopLevel 黑名單」:刪除 `internal/config/zones.go` 的 `rejectedTopLevel` 變數與其分支(姿態翻轉後成死碼)。驗證:`make lint` 無未使用變數告警、`make test` 通過、原本被黑名單的 keyword 現走 skip 路徑。

## 4. 整合測試 fixture 與驗證

- [x] 4.1 [P] 新增 viewless + default-zones 整合測試 fixture:在 `testdata/integration/bindcompat/` 建立 Debian 風格 viewless 設定(`named.conf` include `named.conf.options`/`named.conf.local`/`named.conf.default-zones`,default-zones 含 `zone "." { type hint; }` 與 localhost/127/0/255 的 `type master` zone 及對應 `db.local`/`db.127`/`db.0`/`db.255`),並附 `README.md` 說明用途。內容只用 RFC 2606 網域與 RFC 5737 IP。驗證:`make build` 後以 `--dry-run` 指向此 fixture 載入成功不 fatal。
- [x] 4.2 新增 `test/integration/bind_compat_test.go` 並在 `test/integration/helpers_test.go` 增補載入 bindcompat fixture 的 helper:斷言載入不 fatal、`type hint` zone 被略過、localhost 等 `type master` zone 正常 serve、頂層 `acl`/`key`/`controls` 不 fatal、match-clients 含具名參照時該 view fail-closed 不服務。驗證:`make test` 中該整合測試通過。

## 5. 收尾驗證

- [x] 5.1 全套驗證:`make test`(race detector)與 `make lint` 全綠;確認既有 view-based 整合測試(GeoIP split-horizon)行為不變(回歸)。
- [x] 5.2 請使用者依 perf-guard 規則對 bench-ns2 執行效能回歸(本 change 動 `internal/config`,屬 load-time;規則要求跑),確認無 QPS/p99 回歸後再決定 commit。
