## Context

Production 的 BIND9 透過 named.conf `logging{}` 區塊啟用 query log，下游 parser 依賴其逐行格式。ShadowDNS 已解析 named.conf 的 `options{}` 與 `view{}` 區塊（`internal/config`），但 top-level dispatch 對 `logging` 區塊目前是靜默跳過。主 logger 走 zap（`internal/logging`），檔案 sink 由 `ReopenSink` 支援 SIGUSR1 reopen 配合 logrotate。

DNS hot path（`Server.ServeDNS`）目前對正常查詢不輸出任何 log；per-query log 一旦加入，formatter 與寫入路徑的效能即為 load-bearing 約束。

BIND9 query log 行格式（已 sanitize 的範例）：

```
07-Jun-2026 05:59:41.389 queries: info: client @0x7f3a2c001234 192.0.2.10#16361 (www.example.com): view view-eu: query: www.example.com IN A -E(0)DC (198.51.100.7)
```

依序為：時間戳（`print-time`）、category（`print-category`）、severity（`print-severity`）、client 物件 token、客戶端 IP#port、括號 qname、view 名稱、`query:` + qname/class/qtype、flags、收到查詢的本機位址。

## Goals / Non-Goals

**Goals:**

- 解析 named.conf `logging{}` 的 queries channel 設定，無須新增 CLI flag。
- 輸出與 BIND9 逐字相容的 query log 行，`print-time` / `print-category` / `print-severity` 誠實遵守。
- 發出點語意與 BIND 一致：view 匹配成功即記錄（含之後被 REFUSED 的查詢與 AXFR/IXFR）。
- 重用 SIGUSR1 + `ReopenSink` + logrotate 的 rotation 機制。
- Hot path 寫入零多餘 heap allocation（buffer pool + append-based formatting）。

**Non-Goals:**

- 不實作 BIND 內建的 `versions` / `size` 自旋轉（啟動時印 warning 提醒改用 logrotate）。
- 不實作 channel 的逐訊息 severity 過濾（query log 行恆為 info，severity 僅作 enable/disable 判斷）。
- 不支援 syslog / stderr channel、多 channel fan-out、`buffered` 等其他 channel 參數。
- 不處理 `queries` 以外的 category（`default`、`client`、`xfer-out` 等照舊忽略）。
- SIGHUP reload 不重新套用 `logging{}` 變更——query log 設定僅在啟動時生效，變更需重啟（與 zones reload 行為明確區隔）。
- 不輸出 `S`（TSIG）與 `V`（cookie 驗證通過）flags——ShadowDNS 無對應功能。
- 不修改 Prometheus metrics。

## Decisions

### Decision 1: logging{} 區塊解析進 internal/config，產出 QueryLogConfig

在 `internal/config` 新增 `logging.go`，提供 `ParseLogging`，由 `zones.go` 的 top-level dispatch 在遇到 `logging` 關鍵字時呼叫（取代現行的靜默跳過）。解析結果掛上 `Config` 結構為 `QueryLog *QueryLogConfig`（nil 表示停用）：

```go
type QueryLogConfig struct {
    FilePath        string // 解析後路徑（相對路徑已與 options directory join；directory 本身為相對時結果仍為相對，與 zone file 行為一致）
    PrintTime       string // "yes" | "no" | "local" | "iso8601" | "iso8601-utc"
    PrintCategory   bool
    PrintSeverity   bool
    RotationIgnored bool   // file 子句帶 versions/size → 啟動 warning
}
```

解析規則：

- 走既有 lexer（`internal/config` 的 token 流），與 `ParseOptions` 同風格：未知 channel 參數與未知 category 以 warning 略過，不報錯。
- `category queries { ... }` 列出多個 channel 時，取第一個 file channel 並以 warning 告知其餘被忽略。
- 停用情境（`Config.QueryLog == nil`）：`logging{}` 不存在、無 `category queries`、對應 channel 為 `null` 或其他內建 channel（`default_syslog`、`default_stderr`、`default_debug`）、使用者自訂的非 file channel（`syslog`/`stderr` 目的地）、channel `severity` 嚴於 info（`notice`、`warning`、`error`、`critical`）。停用一律不報錯；其中僅「使用者自訂非 file channel」與「severity 嚴於 info」兩種情境附 warning 說明原因，內建 channel（含 `default_syslog`）與其餘情境靜默停用——內建 channel 屬刻意指向非檔案目的地，不視為設定錯誤。
- `severity` 值 `info`、`debug`（含可選 level 數字）、`dynamic` 視為啟用。
- channel `file` 的相對路徑與 `options { directory }` join，與 zone file 路徑解析行為一致（`directory` 本身為相對路徑時結果仍為相對，依行程 cwd 解釋——與 zone file 既有行為相同，不額外保證絕對化）。

替代方案：沿用「新增 CLI flag」與 `--log-file` 對稱——被否決，因為目標是讓既有 BIND9 named.conf 零改動直接餵給 ShadowDNS。

### Decision 2: internal/querylog 套件手寫 formatter，不走 zap

新套件 `internal/querylog` 提供 `Logger`，入口為單一 `Log(e Entry)`；`Entry` 為值型別 struct（client addr/port、qname 原始大小寫、qclass、qtype、view 名稱、RD/DO/CD/TCP、EDNS version（含是否存在）、COOKIE option 是否存在、本機位址）。

格式化策略（效能為 load-bearing）：

- `sync.Pool` 管理 `[]byte` buffer，整行 append-based 組裝，單次 `Write` 落檔，無中間 string 轉換。
- 時間戳用 `time.Time.AppendFormat` 寫入既有 buffer（layout `02-Jan-2006 15:04:05.000`，`print-time yes|local` 為本地時間；`iso8601` / `iso8601-utc` 用對應 layout；`no` 省略）。
- `@0x` token：BIND 印的是 client 物件指標，ShadowDNS 無對應物，以 atomic uint64 計數器的 hex 表示（`@0x` + 小寫 hex，無補零）佔位，維持 token 形狀讓下游 regex 不破。計數器掛在 `Logger` 上、token 值以參數傳入 formatter——測試可注入固定 token 與固定時間（`time.FixedZone`）達成 byte-exact 斷言，不依賴全域狀態。
- qname 去尾點後輸出（root zone 查詢時保留 `.`），大小寫保留 on-wire 原樣（handler 已有 `qnameOrig`）。`qnameOrig` 是 miekg/dns 解碼後的 presentation form，label 內特殊字元（空白、反斜線等）已依 RFC 1035 master-file 慣例 escape——與 BIND 的輸出一致，formatter 原樣寫出、不再轉換。
- qclass / qtype 用 miekg/dns 的字串表（未知型別落到 `TYPE<n>` / `CLASS<n>`，與 BIND 的 RFC 3597 表示一致）。
- flags 欄位輸出順序與 BIND 一致：`+`/`-`（RD）、`E(n)`（EDNS 帶版本）、`T`（TCP）、`D`（DO）、`C`（CD）、`K`（請求帶 COOKIE option；ShadowDNS 不驗證 cookie 故永不輸出 `V`）。`S` 永不輸出。
- 行尾本機位址取 `dns.ResponseWriter.LocalAddr()` 的 IP（不含 port）；wildcard bind（`0.0.0.0` / `::`）時誠實輸出該值，文件註明此限制（BIND 用 pktinfo 取得實際目的位址，ShadowDNS 不做）。

替代方案：客製 zap encoder——被否決：BIND 格式是位置型純文字，與 zap 的欄位模型不合，硬塞會同時犧牲效能與可讀性。

### Decision 3: 同步單次 Write，重用 ReopenSink，不做非同步批次

`querylog.Logger` 的 sink 直接用 `logging.OpenReopenSink` 開啟（`O_APPEND|O_CREATE`、mode 0640），每行一次 `Write`。`ReopenSink` 自帶 mutex 序列化 Write/Reopen，行內容在單次 Write 內送出，O_APPEND 保證行不交錯。

理由：非同步批次（channel + 背景 flusher）能省 syscall，但引入 crash 掉行、背壓策略、順序性等複雜度；v0.x 實驗階段先以最簡正確版本上線，留 dnspyre benchmark 驗證——若同步寫入造成可量測退化，再以後續 change 引入批次。開檔失敗時啟動直接失敗（與 `--log-file` 的 fail-loudly 行為一致）。

### Decision 4: 發出點在 view 解析成功之後，與 BIND 語意一致

- `ServeDNS`：view 匹配成功（`Matcher.Resolve` 回傳非空）後、zone 匹配之前發出——之後被 REFUSED（qname 不在任何 zone）的查詢仍會被記錄，與 BIND「記錄收到的查詢，與最終 rcode 無關」一致。
- no-view REFUSED、CHAOS class REFUSED、malformed（FORMERR）、NOTIMP：不進 query log（BIND 中這些在 query 處理開始前就被擋下，記在 client category debug 等級），維持走現有 main logger。
- `handleTransfer`：AXFR/IXFR 在其內部 view 解析成功後發出（BIND 也將 transfer 查詢記入 queries category；IXFR 無論 UDP 或 TCP 都走此分支，UDP 時 flags 無 `T`）。注意現行程式碼順序是 allow-transfer ACL 檢查先於 view 解析——ACL REFUSED 的請求在 view 解析前即返回，**不進 query log**；不為了記錄而調動 ACL 與 view 解析的既有安全順序。
- `Server` 結構新增 `QueryLog *querylog.Logger` 欄位（nil = 停用，hot path 以單一 nil 判斷短路），與 `Metrics`、`EphemeralStore` 的可選依賴模式一致。

### Decision 5: SIGUSR1 同時 reopen 兩個 sink，rotation warning 與 dry-run 摘要

- `cmd/shadowdns/main.go` 的 SIGUSR1 handler 從「單一 `LogReopener`」擴充為依序 reopen main log 與 queries log 的 sink；任一失敗保留舊 fd 並記 error（沿用既有 reopen 失敗語意），兩者互不影響。queries log 啟用而 `--log-file` 未設定時，仍需安裝 SIGUSR1 handler。
- 解析到 `RotationIgnored == true` 時，啟動（含 `--dry-run`）經 main logger 印一則 warning：ShadowDNS 不實作 BIND 內建 rotation，請以 logrotate + SIGUSR1 接手。
- `--dry-run` 摘要納入 query log 狀態：啟用時印解析後檔案路徑與 print-* 生效值；停用時印原因，原因字串涵蓋全部五種停用情境：無 `logging{}` 區塊、無 `category queries`、null/內建 channel、非 file channel、severity 嚴於 info。
- SIGHUP reload：reload 路徑重跑 `config.LoadNamedConf` 會解析到新的 `logging{}` 內容，但 reload **不重新套用** query log 設定——既有 sink 的 fd 原封不動、in-flight 寫入不受影響、新解析出的 `QueryLogConfig` 直接丟棄（無需修改 reload 程式碼，忽略即是現狀）。`logging{}` 出現語法錯誤時該次 reload 失敗並保留舊狀態，與 `options{}` 解析錯誤的既有 reload 語意一致。同理，其他 `LoadNamedConf` 呼叫者（如 prune-backup 子命令）也會繼承「語法錯誤即失敗」的新嚴格性。

## Implementation Contract

**行為**：

- named.conf 含 `logging { channel X { file "<path>" ...; severity debug; print-time yes; print-category yes; print-severity yes; }; category queries { X; }; }` 時，每筆 view 匹配成功的 DNS 查詢在 `<path>` 追加一行，格式為：
  `<dd-Mmm-yyyy HH:MM:SS.mmm> queries: info: client @0x<hex> <client-ip>#<port> (<qname-no-trailing-dot>): view <view-name>: query: <qname-no-trailing-dot> <class> <qtype> <flags> (<local-ip>)`
- `print-time no` 省略時間戳段；`print-category no` 省略 `queries: `；`print-severity no` 省略 `info: `。各段間以單一空白接續，省略後不留殘餘空白。
- flags 段：第一字元恆為 `+`（RD=1）或 `-`（RD=0）；隨後依序視情況接 `E(<version>)`、`T`、`D`、`C`、`K`，無分隔字元。
- 停用情境下不建立任何檔案、不輸出任何 query log，DNS 行為完全不變。

**介面 / 資料形狀**：

- `internal/config`：`Config` 新增 `QueryLog *QueryLogConfig` 欄位；`QueryLogConfig` 形狀見 Decision 1。
- `internal/querylog`：`New(path string, cfg Config) (*Logger, *logging.ReopenSink, error)` 風格的建構式 + `(*Logger).Log(Entry)`；`Entry` 各欄位見 Decision 2。確切簽名由實作微調，但「單一入口、值型別 Entry、nil Logger 安全短路」為契約。
- `internal/server`：`Server.QueryLog` 為可選依賴，nil 時 hot path 行為與現行完全相同。

**失敗模式**：

- queries log 檔案無法開啟 → 啟動失敗並回報路徑與錯誤（不靜默降級）。
- SIGUSR1 reopen 失敗 → 保留舊 fd、經 main logger 記一則 error，後續寫入不中斷。
- `logging{}` 區塊有語法錯誤（不成對括號等）→ 啟動失敗並回報行號（與 `options{}` 解析錯誤同行為）；可解析但語意不支援（syslog channel、severity 嚴於 info）→ 停用 + warning，不失敗。

**驗收標準**：

- `internal/config` 單元測試：production 形狀的 `logging{}`（含 `versions`/`size`）解析出正確 `QueryLogConfig` 與 `RotationIgnored`；各停用情境回傳 nil；語法錯誤回報行號。
- `internal/querylog` 單元測試：以固定 `Entry`、固定時間（`time.FixedZone` 固定時區）與注入的固定 token 驗證整行輸出逐字節等於 BIND 格式預期字串（含 print-* 三旗標的 2^3 組合中至少全開、全關、僅時間關三組）；benchmark 以 in-memory sink（不落實體檔案）量測 `Log` 全路徑，`go test -bench -benchmem` 確認穩態 allocs/op = 0（含時間戳與 hex token 寫入，皆不豁免）。
- `internal/server` handler 測試:view 匹配成功的查詢（含 zone 外 REFUSED、AXFR）各產生一行；no-view / CHAOS / FORMERR / NOTIMP / allow-transfer ACL REFUSED 不產生。
- `make test`、`make lint`、`make smoke` 通過。`--dry-run` 的 query log 摘要（啟用與停用兩分支）由 cmd/shadowdns 單元測試覆蓋；smoke 的 fixture 不含 `logging{}` 區塊，走停用分支即可，不需修改。

**範圍邊界**：in scope = 上述全部；out of scope = Non-Goals 列出的項目（自旋轉、syslog、多 channel、SIGHUP 重設定、metrics）。

## Risks / Trade-offs

- [同步 Write 在高 QPS 下增加每查詢一次 syscall 的成本] → formatter 零配置把成本壓到 syscall 本身；以 dnspyre benchmark（bench-ns1/ns2 對照）驗證，若有可量測退化再以後續 change 引入批次寫入。
- [wildcard bind 時行尾本機位址輸出 `0.0.0.0`，與 BIND 的實際目的位址不同] → 文件明確記載此限制；production 部署綁定具體位址時無此問題。
- [下游 parser 可能依賴 `@0x` token 的指標語意（同 client 重複）] → token 形狀（`@0x` + hex）保持一致，僅值的語意不同；若下游確有依賴再行調整。
- [`logging{}` 從靜默跳過改為解析，理論上既有 named.conf 可能因區塊內語法問題從「能啟動」變「啟動失敗」，且同時影響 SIGHUP reload 與 prune-backup 等所有 `LoadNamedConf` 呼叫者] → v0.x 實驗階段可接受；錯誤訊息附行號，且僅影響真正有語法錯誤的設定檔；reload 失敗保留舊狀態。
- [SIGHUP 不重讀 logging{}，ops 改了設定可能誤以為已生效] → dry-run 摘要與文件明示「query log 設定僅啟動時生效」。
- [deb 部署的 systemd unit 帶 `ProtectSystem=strict` 且 `ReadWritePaths` 僅含 `/var/log/shadowdns`，named.conf 指向其他目錄（如 BIND 慣用路徑）時開檔會被 sandbox 擋下而啟動失敗] → 屬「fail loudly」的預期行為；Migration Plan 與文件註明：log 路徑放 `/var/log/shadowdns/` 下，或以 systemd override 擴充 `ReadWritePaths`。

## Migration Plan

1. 部署新版 binary（deb 包），named.conf 無 `logging{}` 時行為完全不變。
2. 在 bench-ns2 的 named.conf 加入與 production 相同形狀的 `logging{}`（log 路徑置於 `/var/log/shadowdns/` 下，或先以 systemd override 擴充 `ReadWritePaths`），重啟並確認 queries log 產出與 startup warning。
3. 補上 logrotate 規則（postrotate 發 SIGUSR1），驗證 rotate 後續寫不中斷。
4. Rollback：移除 `logging{}` 區塊或回退 binary 皆可，無持久狀態。

## Open Questions

（無——同步 vs 非同步寫入已在 Decision 3 拍板為同步，後續以 benchmark 為憑再議。）
