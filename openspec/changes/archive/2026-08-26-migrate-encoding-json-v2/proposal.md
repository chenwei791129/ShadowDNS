## Summary

將 ShadowDNS 兩個 production JSON 邊界從 `encoding/json` 遷移至 Go 1.27 的 `encoding/json/v2`：Ephemeral TXT API 採用 v2 嚴格輸入語義，而 JSON DoH 維持既有 schema 與 HTTP 契約。

## Motivation

目前程式仍透過 v1 API 保留寬鬆的欄位名稱比對、重複 object member 與 invalid UTF-8 等 legacy semantics。專案已要求 Go 1.27，可直接採用 v2 較明確且較安全的輸入規則，同時藉由固定案例比較區分刻意的嚴格化與非語義性的 outbound serialization 差異。

## Proposed Solution

- 將 Ephemeral TXT API 的 request decoding 與 response encoding 改用 `encoding/json/v2`。
- PUT body 使用原生 v2 嚴格語義並繼續拒絕未知欄位；欄位名稱大小寫不符、重複 object member、invalid UTF-8 與多個頂層 JSON values 均回 HTTP 400，且不得修改 ephemeral store。
- 將 `application/dns-json` response encoding 改用 `encoding/json/v2`，維持 HTTP status、Content-Type、Cache-Control、Google Public DNS-compatible schema、欄位名稱／型別／值、空陣列表示與尾端 newline。
- 在 production code 遷移前擷取固定案例的 v1 baseline，遷移後以相同案例比較 decoded JSON 與 raw bytes，將一次性簡要報告寫入 `.local/`。
- 同步更新英文與正體中文 Ephemeral API 文件，說明嚴格的 PUT JSON input規則。

## Alternatives Considered

- 使用 `encoding/json.DefaultOptionsV1()` 模擬完整 v1 semantics：會保留本次希望移除的寬鬆輸入行為，因此不採用。
- 只遷移其中一個 package：會讓 binary 同時維護兩套 JSON semantics，因此不採用。
- 建立共用 JSON codec abstraction：目前兩個邊界的 options 與契約不同，額外 wrapper 只會轉送呼叫，因此不採用。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ephemeral-api`: PUT JSON body 採用 v2 嚴格 input semantics，並維持既有 response schema 與 HTTP 契約。
- `doh-endpoint`: `application/dns-json` 改由 v2 序列化，同時維持既有 decoded schema 與 HTTP 契約。

## Impact

- Affected specs: `ephemeral-api`, `doh-endpoint`
- Affected code:
  - Modified: `internal/api/server.go`, `internal/api/server_test.go`, `internal/doh/dnsjson.go`, `internal/doh/dnsjson_test.go`, `docs/ephemeral-api.md`, `docs/ephemeral-api.zh.md`
  - New: none
  - Removed: none
- Dependencies: no module dependency changes; uses the Go 1.27 standard library already required by `go.mod`
- Operational verification: existing UDP Perf-Guard remains the runtime non-regression gate because production Go files change, but it is not evidence of JSON performance; no JSON benchmark or DoH load test is added
