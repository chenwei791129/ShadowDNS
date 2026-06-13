## 1. 元素模型與解析

- [x] 1.1 實作「統一的 address-match-list 元素模型」:在 `internal/config/match.go` 把 `match-clients` 從扁平 `[]MatchRule` 改為有序 `[]Element`,在既有 country/asn/ip/cidr/any 之上加入 `Negated`、具名參照、巢狀群組、內建 ACL 種類;`match-clients` 與 acl body 共用同一解析器。滿足 Requirement「Parse match-clients rule syntax」。驗證:`internal/config/match_test.go` 斷言 `! 192.0.2.0/24; { 198.51.100.0/24; }; any;`、`localhost`/`localnets`/`none`、具名 token 各解析成正確 Element;既有 country/asn/ip/cidr/any 行為等價。
- [x] 1.2 實作「頂層 acl 解析並儲存為具名 registry」:`internal/config/zones.go` 頂層 `acl "<name>" { ... };` 改為以 1.1 的解析器解析 body 並存入 `Config` 的具名 registry;重名以最後一筆為準並 WARN。滿足 Requirement「Parse and store named acl definitions」。驗證:`internal/config/zones_test.go` 斷言 acl 被儲存、重名 WARN。

## 2. 參照解析與內建 ACL

- [x] 2.1 實作「具名參照遞迴解析,未定義名稱 fail-closed」:在所有檔案載入後的 build 階段,把 acl body 與 view `match-clients` 內的具名參照解析為指向目標 Element 清單的指標;未定義名稱 → 丟棄該 element + WARN + 永不命中(fail-closed);參照環 → 斷開 + WARN。滿足 Requirement「Parse and store named acl definitions」。驗證:`internal/config/zones_test.go` 斷言已定義參照解析成功、`nosuchacl` 丟棄不 fatal、`a→b→a` 環被斷開。
- [x] 2.2 實作「內建 ACL any/none/localhost/localnets」:`any` 永遠命中、`none` 永不命中;`localhost`/`localnets` 於啟動/build 時以 Go `net` 介面列舉解析為具體位址/網段集合,reload 時重新列舉。滿足 Requirement「Parse match-clients rule syntax」。驗證:`internal/config/match_test.go` / `internal/view/matcher_test.go` 斷言四種內建 ACL 的命中行為(localhost 對本機位址命中)。

## 3. 評估語義

- [x] 3.1 實作「有序 first-match + 否定(reject)評估語義」:在 `internal/view/matcher.go` 以遞迴 `listAccepts(elements, srcIP, geoIP) bool` 取代內層「任一 rule 命中即選」;依宣告序,第一個命中的元素決定 accept(正向)/reject(否定);具名參照與巢狀群組以子清單遞迴 `listAccepts` 判斷命中;country/asn 對 geoIP、其餘對 srcIP。`Resolve` 對每個 view 呼叫一次,accept 才回該 view 名,否則 fall through。隨元素模型同步更新 `view.View.Rules` 的型別與 `internal/server/build.go` 的 config→view 規則交棒(`Rules: v.MatchClients`),並確保 `internal/config/zones.go` 的 `viewHasAny`(shadowed-view 警告用)仍能在新模型下偵測 `any`。滿足 Requirement「Resolve client IP to a view using first-match semantics」。驗證:`internal/view/matcher_test.go` 斷言 `! cidr; any;` 邊界、具名參照(含否定)遞迴、未定義參照不命中且非 catch-all;`make build` 編譯通過(build.go 交棒型別相容);既有無否定設定行為不變(回歸)。

## 4. 整合測試與驗證

- [x] 4.1 擴充「viewless 與 ACL-based 整合測試」:在 `test/integration/bind_compat_test.go` 加入定義 `acl` 並於 `match-clients` 具名引用、`!` 否定、巢狀群組、內建 ACL 的 view-based fixture(內容只用 RFC 2606 網域 / RFC 5737 IP),斷言指定來源 IP 落入/落出對應 view、否定語義正確、未定義參照的 view fail-closed 不服務。驗證:`make test` 中該整合測試通過。
- [x] 4.2 全套驗證:`make test`(race detector)與 `make lint` 全綠;確認既有 GeoIP/IP split-horizon view 選擇行為不變(回歸)。
- [x] 4.3 請使用者依 perf-guard 規則對 bench-ns2 執行效能回歸(本 change 動 `internal/config` 與 `internal/view`,後者在 query hot path),確認無 QPS/p99 回歸後再決定 commit。
