## 1. 遷移前基準與測試契約

- [x] 1.1 依「以固定案例產生一次性前後比較報告」在修改 production JSON calls 前，以臨時 package-local helper 執行既有 handlers，將 Ephemeral inbound、Ephemeral outbound 與 JSON DoH 固定案例的 v1 HTTP status、relevant headers、decoded JSON及raw body寫至 `.local/json-v2-comparison/baseline/`；確認案例清單完整且只含synthetic/reserved資料，保留同一helper至candidate擷取與報告完成後再移除，並以 `git status --short`驗證helper不在working tree。
- [x] 1.2 [P] 在 `internal/api/server_test.go` 先新增「PUT endpoint adds or refreshes an ephemeral TXT value」契約測試：case mismatch、unknown member、duplicate member、invalid UTF-8與第二個頂層JSON value皆須HTTP 400、decode為既有`status=error` shape且store未變；另固定合法body、missing value、TTL clamp與response單一尾端newline，先以現行v1 implementation執行並記錄哪些strict案例預期在遷移前失敗。
- [x] 1.3 [P] 在 `internal/doh/dnsjson_test.go` 先新增「application/dns-json responses follow the Google Public DNS schema」契約測試：A、多-answer、TXT含`<>&`、NXDOMAIN、empty Answer與ECS response須維持HTTP metadata與decoded values，`Question`／`Answer`空section須為`[]`且body須有單一尾端newline；以現行v1 implementation執行，確認既有outbound contract可作candidate比較基準。

## 2. v2 Production遷移

- [x] 2.1 實作「Ephemeral PUT採用原生v2嚴格解碼」：在 `internal/api/server.go` 以 `encoding/json/v2.UnmarshalRead`與`RejectUnknownMembers(true)`取代v1 decoder，不套用v1 compatibility或case-insensitive options；執行`go test ./internal/api`驗證五類invalid input均在store mutation前回HTTP 400，合法PUT／DELETE行為不變。
- [x] 2.2 [P] 實作「Outbound維持語義契約但不承諾byte equality」的JSON DoH部分：在 `internal/doh/dnsjson.go` 以`encoding/json/v2.MarshalWrite`輸出並明確補上單一尾端newline，維持Content-Type、Cache-Control、schema、decoded values與空array；執行`go test ./internal/doh`驗證所有DoH JSON契約測試通過。
- [x] 2.3 完成「Outbound維持語義契約但不承諾byte equality」的Ephemeral部分：以`encoding/json/v2.MarshalWrite`取代`internal/api/server.go`的v1 response encoder並保留單一尾端newline；以`rg -n '"encoding/json"|json\\.NewEncoder|json\\.NewDecoder' internal/api internal/doh`確認production direct v1 calls為零，再執行`go test ./internal/api ./internal/doh`。

## 3. 行為差異報告與文件

- [x] 3.1 使用與baseline相同的固定案例產生`.local/json-v2-comparison/candidate/`，依「Behavior」逐案比較HTTP status、relevant headers與decoded JSON，並另列raw body差異；確認case mismatch、duplicate member、invalid UTF-8與第二個頂層value只出現預期的accepted→HTTP 400變化，而合法input、outbound schema及HTTP契約無差異。
- [x] 3.2 依「Acceptance Criteria」產生`.local/json-v2-comparison/report.md`，列出總案例數、unchanged contracts、intended strictness changes、incidental serialization differences與unexpected differences，且只有unexpected differences為0才可完成；人工檢查報告不含真實domain、host、IP、payload或credential，並確認`.local/`未被stage。
- [x] 3.3 [P] 依「Scope Boundaries」同步更新`docs/ephemeral-api.md`與`docs/ephemeral-api.zh.md`，明載PUT member名稱大小寫敏感，unknown／duplicate member、invalid UTF-8及多個頂層JSON values回HTTP 400且不修改store；執行`make docs-build`並人工比對兩種語言的規則與案例一致。

## 4. 完整驗證與交還

- [x] 4.1 執行`go fmt ./cmd/... ./internal/...`、`make lint`、`make test`、`make smoke`與`make docs-build`，確認source、strict input、outbound contract及雙語文件全部通過，且沒有新增dependency或JSON benchmark。
- [x] 4.2 [P] 執行既有Debian與container驗證：`make deb`、`make test-deb`、`make container-image`、`make verify-container`與`make test-container`，確認v2遷移不改變package或runtime contract；記錄實際使用的container runtime與任何未執行項目。
- [x] 4.3 依「驗證不加入JSON效能benchmark」執行一次bounded `simplify`與一次`auto-code-review xhigh --fix`，只處理本change範圍；之後重跑受修正影響的tests，且不得因review產生finding而自動開始額外review輪次。
- [x] 4.4 Review chain完成後依project Perf-Guard對Go source change執行back-to-back baseline／candidate UDP CNAME與A驗證，套用QPS down >5%或p99 up >15%的regression thresholds；報告明載結果只證明共享runtime non-regression，不宣稱JSON或DoH效能改善，PASS後將完整驗證與local comparison報告交還使用者且不自動stage或commit。
