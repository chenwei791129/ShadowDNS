## Why

目前 ShadowDNS 使用標準函式庫 `log/slog`，在互動式終端中輸出缺乏視覺層級，operator 閱讀啟動、reload、NOTIFY 等事件日誌較為吃力。同時規劃中欲將日誌套件遷移至 `go.uber.org/zap`，以取得更豐富的 encoder 生態與後續可能的 sampling／結構化整合空間。此變更將兩件事合併完成——遷移到 zap 並在終端機輸出中加入 level 著色——避免 factory 層配置被改動兩次。

## What Changes

- **BREAKING**（內部 API）：所有 production 程式碼中的 `*slog.Logger` 參數改為 `*zap.Logger`。影響 22 個 `.go` 檔的 function signature 與 call site。
- 移除 `cmd/shadowdns/main.go:146` 的 `slog.New(slog.NewTextHandler(...))`，改用新的 zap logger factory。
- 新增命令列旗標 `-no-color`（default `false`），強制停用彩色輸出。
- 新增 `NO_COLOR` 環境變數支援（依 https://no-color.org 標準：非空字串即視為停用）。
- 新增 `isatty(os.Stderr)` 自動偵測：非 TTY（systemd journald、pipe、檔案重導向）時自動停用顏色。
- 使用 zap 內建 `zapcore.CapitalColorLevelEncoder`——**僅 level 字串（`INFO`／`WARN`／`ERROR` 等）著色**，其他欄位保持原色。
- 測試檔（~18 個 `_test.go`）中自建的 `slog.New(slog.NewTextHandler(&buf, nil))` 改為 zap 測試 logger（寫入 `bytes.Buffer` 的 observer core 或 no-op logger）。
- `go.mod` 新增 direct dependency：`go.uber.org/zap`、`github.com/mattn/go-isatty`（後者目前為 indirect）。
- 移除 `log/slog` 的 import（僅在少數測試輔助／zap adapter 場景保留——目標是 production 程式碼全數淨零）。

## Non-Goals (optional)

- **不**著色時間、訊息、key=value 欄位——僅 level（保留 zap 原生能力，避免自製 custom encoder 的維護負擔）。
- **不**引入 `-color=always|auto|never` 三態旗標，`-no-color` 就足夠；若未來需要「管道仍要顏色」的情境（如 `shadowdns | less -R`）再另提 change。
- **不**引入第三方 pretty console encoder（如 `zap-prettyconsole`）——維持 zap 原生 encoder 的維護邊界。
- **不**同步處理「日誌寫入檔案」的功能——僅保證 isatty 偵測為之後此功能鋪路；檔案 sink 本身屬於獨立 change。
- **不**調整日誌 level 語意或增加新 level（沿用 `Info`／`Warn`／`Error`）。
- **不**變更 systemd service unit (`packaging/shadowdns.service`)——非 TTY 環境下 isatty 自動回傳 false，journald 不會收到 ANSI escape code。

## Alternatives Considered (optional)

- **slog + `lmittmann/tint`**：改動最小（僅 1 個檔案），顏色層次更豐富（時間／訊息／欄位全著色）。但使用者已規劃遷移至 zap，採此方案後未來會整包丟棄，做白工。
- **手寫 ANSI-wrapping zap encoder**：可達成全欄位著色，但需實作 encoder 邊界情境（Group 巢狀、`WithAttrs` 繼承、字串 escape），~100 行 encoder 程式碼長期維護成本高。非必要複雜度。
- **先純遷移 zap 再另提 change 加顏色**：logger factory 會被改兩次，review 成本翻倍，拆分無實質好處。

## Capabilities

### New Capabilities

- `logging`：定義 logger factory 的建構規則——使用 zap 為底層實作、著色決策的三層優先級（`-no-color` flag > `NO_COLOR` env > `isatty`）、著色範圍（僅 level）、以及非 TTY 環境下的行為。此 capability 在現有程式碼中屬於隱性約定，此 change 將其形式化為 spec。

### Modified Capabilities

（none）

現有 capability 皆以實作細節方式使用 logger，spec 層級並未約束 logger 型別或格式，故 requirement 無變動。

## Impact

- **Affected specs**：
  - 新增 `openspec/specs/logging/spec.md`
- **Affected code**（22 個 production `.go` 檔 + ~18 個測試檔）：
  - `cmd/shadowdns/main.go`（logger factory、新增 flag）
  - `cmd/shadowdns/main_test.go`
  - `internal/server/server.go`、`build.go`、`handler.go`、`listener.go`、`server_test.go`
  - `internal/config/options.go`、`options_test.go`、`zones.go`、`zones_test.go`、`aliases.go`、`aliases_test.go`
  - `internal/zone/parser.go`、`parser_test.go`、`classify.go`、`classify_test.go`
  - `internal/view/loader.go`、`loader_test.go`
  - `internal/transfer/axfr.go`、`notify.go`、`notify_test.go`
  - `test/integration/helpers_test.go`、`axfr_test.go`
  - `scripts/gen-container-testdata.go`
- **Affected dependencies**（`go.mod`）：
  - 新增 direct：`go.uber.org/zap`、`github.com/mattn/go-isatty`
  - 保留間接：`log/slog`（stdlib，不可移除）
- **Affected documentation**：
  - `README.md`（若有 CLI flag 列表，需新增 `-no-color`）
  - `packaging/named.conf.example`（不受影響）
- **Affected systems**：
  - systemd deployment：無需改動 service unit，isatty=false 自動生效
  - CI：需確認 `make test` 在非 TTY 環境執行不會因 ANSI escape code 污染測試斷言
