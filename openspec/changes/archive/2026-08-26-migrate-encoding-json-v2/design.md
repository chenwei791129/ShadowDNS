## Context

ShadowDNS 已要求 Go 1.27，但兩個 production HTTP 邊界仍直接使用 `encoding/json` v1 API：Ephemeral TXT API 同時 decode request 與 encode response，JSON DoH 則只 encode response。UDP、TCP 與 RFC 8484 wire-format DoH 使用 DNS wire format，不經 JSON。此次遷移會刻意收緊 Ephemeral PUT input；outbound JSON 則維持 decoded data與HTTP契約，允許非語義性的raw bytes差異。

## Goals / Non-Goals

**Goals:**

- 讓 production code 不再直接匯入 `encoding/json`，改用 Go 1.27 的 `encoding/json/v2`。
- Ephemeral PUT body採用v2原生嚴格語義，未知欄位仍被拒絕。
- 維持Ephemeral API與JSON DoH的outbound schema、decoded values及HTTP metadata。
- 先擷取v1固定案例，再對同一案例產生v2結果，交付簡短的local-only差異報告。
- 將新的Ephemeral PUT input規則同步寫入英文與正體中文手冊。

**Non-Goals:**

- 不修改UDP、TCP、wire-format DoH或authoritative DNS query logic。
- 不建立JSON benchmark、DoH load test或以UDP Perf-Guard推論JSON效能。
- 不加入v1 compatibility options，不建立共用codec wrapper，不升級dependency，也不做無關refactor。
- 不將一次性的raw baseline、candidate output或比較報告commit至repository。

## Decisions

### Ephemeral PUT採用原生v2嚴格解碼

PUT body透過`json.UnmarshalRead`解碼，並明確套用`json.RejectUnknownMembers(true)`。不套用`encoding/json.DefaultOptionsV1()`或`MatchCaseInsensitiveNames(true)`，因此欄位名稱大小寫不符、重複object member、invalid UTF-8、未知欄位及多個頂層JSON values都視為invalid JSON body並回HTTP 400。解碼失敗必須發生在store mutation前。

這延續現有`DisallowUnknownFields`的嚴格API意圖，同時移除v1會接受的ambiguous input。替代方案是保留v1 compatibility options，但那會抵銷這次遷移的輸入hardening目標。

### Outbound維持語義契約但不承諾byte equality

Ephemeral API與JSON DoH透過`json.MarshalWrite`輸出。HTTP status、Content-Type、DoH Cache-Control、JSON member名稱／型別／decoded values，以及DoH空`Question`／`Answer`的`[]`表示必須維持。現有`Encoder.Encode`產生的尾端newline也予以保留，避免無收益的framing變更。

不啟用完整v1 options，因此HTML-sensitive character escaping等raw representation可以改變；member ordering與非必要whitespace本來就不是JSON DoH contract。測試先decode再比較語義，只對明確保留的尾端newline做byte-level assertion。

### 以固定案例產生一次性前後比較報告

在修改production code之前，建立臨時且不commit的package-local capture helper，將固定案例與擷取邏輯集中在同一份helper中，透過既有HTTP handlers擷取v1結果至`.local/json-v2-comparison/`。該helper保留至遷移後，以完全相同的inputs與擷取邏輯產生candidate輸出，再於報告完成後移除。報告比較HTTP status與headers、decoded JSON、接受／拒絕結果及raw body差異。

Ephemeral inbound案例固定為：canonical lowercase fields、case-mismatched fields、unknown member、duplicate member、第二個頂層JSON value、invalid UTF-8、missing `value`與TTL clamp。Outbound案例固定為Ephemeral PUT success、DELETE success、validation error、authorization或ACL error，以及JSON DoH的A、多-answer、TXT含HTML-sensitive characters、NXDOMAIN、empty Answer與ECS response。

報告只記錄synthetic `example.com`／reserved-address資料，位置為`.local/json-v2-comparison/report.md`，不加入git。穩定契約由正式tests與spec保存。

### 驗證不加入JSON效能benchmark

修改`internal/**/*.go`仍觸發既有UDP Perf-Guard，目的僅是確認共享runtime path沒有退化。JSON migration本身以行為比較驗證，不建立microbenchmark或DoH load test；DoH效能不是此application的關注重點。

## Implementation Contract

### Behavior

- Ephemeral PUT只接受精確的lowercase `value`與`ttl` member名稱；合法single-value JSON維持現有store與response行為。
- Case mismatch、unknown member、duplicate member、invalid UTF-8或第二個頂層value均回HTTP 400及既有`status=error` response shape，store保持不變。
- Ephemeral success/error與JSON DoH response在decode後維持既有member名稱、型別與值。
- JSON DoH仍回`application/dns-json`、既有HTTP status與TTL-bounded Cache-Control；空collection仍為`[]`。
- 所有v2產生的JSON HTTP body維持單一尾端newline；其他raw-byte差異不是相容性契約。

### Acceptance Criteria

- 正式tests涵蓋五類嚴格拒絕案例，並在每次PUT拒絕後驗證store未變更。
- 正式tests以decoded value驗證Ephemeral與JSON DoH outbound契約，另驗證尾端newline與DoH空array。
- `.local/json-v2-comparison/report.md`列出案例總數、unchanged contracts、intended strictness changes、incidental serialization differences與unexpected differences；unexpected differences必須為0才能完成。
- `go fmt ./cmd/... ./internal/...`、`make lint`、`make test`、`make smoke`、`make docs-build`、Debian與container既有驗證均通過。
- Bounded simplify與code review各執行一次；之後執行既有Perf-Guard並按其threshold判定。

### Scope Boundaries

In scope為`internal/api`與`internal/doh`的direct JSON calls、其正式tests、雙語Ephemeral API手冊及local-only比較證據。Out of scope為DNS wire path、第三方JSON使用、performance benchmark、dependency變更與通用JSON abstraction。

## Risks / Trade-offs

- [既有client使用大小寫錯誤欄位或duplicate member後被拒絕] → 雙語文件明載精確schema與strict rejection，正式tests固定HTTP 400行為。
- [v2 raw serialization差異被誤判為API regression] → 比較報告將decoded contract與raw bytes分欄，只有語義或明確HTTP契約差異才阻擋完成。
- [capture helper意外進入commit] → helper只保留至同一份harness完成v1與v2擷取，報告完成後立即移除；pre-commit明確檢查不得存在且`.local/`不得stage。
- [UDP Perf-Guard被誤解為JSON效能結果] → tasks與報告明載其用途僅為共享runtime non-regression，且不產生JSON performance claim。
