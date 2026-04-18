## Context

ShadowDNS 目前使用 Go 標準函式庫 `log/slog`，在 `cmd/shadowdns/main.go:146` 建構唯一的 production logger 實例（`slog.New(slog.NewTextHandler(os.Stderr, ...))`），並以 `*slog.Logger` 型別傳遞至 22 個 `.go` 檔的函式簽章。輸出皆為純文字、無 ANSI 色彩。

專案規劃將日誌實作層遷移至 `go.uber.org/zap`，以利未來整合 sampling、結構化輸出與多 sink（如同步寫入檔案）。此 change 將遷移工作與使用者回報的「terminal 日誌閱讀吃力」一併處理，避免 logger factory 被改動兩次。

**運行環境**：
- 本機開發：`./bin/shadowdns ...` 直接執行，stderr 指向 TTY
- Production：systemd unit（`packaging/shadowdns.service`）啟動，stderr 進入 journald（非 TTY）
- 未來預期：可能將日誌重導向至檔案（`2> /var/log/shadowdns.log`），非 TTY

## Goals / Non-Goals

**Goals:**

- 將所有 production 程式碼中的 `*slog.Logger` 完整替換為 `*zap.Logger`
- 在 TTY 環境下提供 level 欄位著色（`INFO`／`WARN`／`ERROR` 等）
- 支援三層著色決策：`-no-color` flag > `NO_COLOR` env var > `isatty(stderr)` 自動偵測
- 在非 TTY 環境（systemd、pipe、檔案重導向）自動輸出無色純文字
- 測試輔助 logger 同步遷移，確保 `go test` 全綠
- 保持 log 語意不變——現有的 `logger.Info(msg, "key", val)` 等呼叫行為與可讀訊息維持等價

**Non-Goals:**

- 著色時間、訊息、欄位（key=value）——僅 level
- 引入 `-color=always` 強制啟用旗標
- 引入第三方 pretty encoder
- 調整 log level 語意或新增 level
- 日誌檔案 sink 功能本身
- 變更 systemd service unit

## Decisions

### 採用 zap 的 CapitalColorLevelEncoder 作為著色機制

使用 zap 內建 `zapcore.CapitalColorLevelEncoder`，以 ANSI color code 包裝 level 字串。無色版本則使用 `zapcore.CapitalLevelEncoder`。Factory 根據運行時決策結果選擇其中之一。

**Alternatives considered**：
- 自製 ANSI-wrapping encoder：~100 行程式碼、需自行處理 group 巢狀、`WithAttrs`、字串 escape，長期維護成本高
- 第三方 `zap-prettyconsole`：小眾套件，維護風險高於 zap 本身
- 僅用 `NewDevelopmentEncoderConfig()`：雖然 default 啟用 color level encoder，但同時改動時間格式為人類可讀（`2026-04-15T10:30:00.000+0800`），與 Production 的 ISO8601 不一致，會造成日誌解析器意外變更

**Rationale**：僅 level 著色滿足使用者需求（operator 掃描日誌時快速辨識錯誤），zap 原生支援、零額外依賴、零維護成本。

### 著色決策的三層優先級

Factory 在 logger 建構時執行一次以下判定：

```
useColor :=
    opts.NoColor == false         // 第一層：CLI flag
    && os.Getenv("NO_COLOR") == ""  // 第二層：env var（非空即停用）
    && isatty.IsTerminal(os.Stderr.Fd())  // 第三層：TTY 偵測
```

優先級由高至低：flag > env var > isatty。任一層判為停用則結果為停用。

**Rationale**：
- `-no-color` flag 為使用者明示意圖，優先級最高
- `NO_COLOR` 為跨工具生態慣例（https://no-color.org），尊重使用者 shell 層設定
- isatty 為最後防線，避免非互動環境誤輸出 ANSI escape code 污染日誌解析

**Alternatives considered**：
- 僅提供 flag，不做 env var 與 isatty 偵測：違反 Unix 工具慣例，journalctl 會收到一團 ANSI 亂碼，使用者必須在 systemd unit 主動加 flag
- 提供 `-color=always|auto|never` 三態：對當前需求過度設計，`-no-color`（= `-color=never`）足以涵蓋所有現實使用情境

### isatty 使用 `github.com/mattn/go-isatty`

此套件目前已是 indirect dependency（透過 `mattn/go-colorable` 傳入），升為 direct。跨平台支援 Linux／macOS／Windows。

**Rationale**：`go-isatty` 是 Go 生態事實標準，API 穩定（`IsTerminal(fd uintptr) bool`）、零 CGO 依賴、維護活躍。

**Alternatives considered**：
- `golang.org/x/term.IsTerminal`：功能相同，但需新增 `golang.org/x/term` 作為 direct dep，此專案目前未使用

### Factory 抽離為 `internal/logging` package

新增 `internal/logging/logger.go`，對外暴露：

```go
package logging

// Options 捕捉建構 logger 所需的所有輸入。
type Options struct {
    NoColor bool  // 對應 -no-color flag
    Level   zapcore.Level  // 預設 InfoLevel
}

// New 依據 Options 與運行環境（env var、isatty）建構 zap logger。
// Factory 被呼叫時立即決定著色策略，不在 log 時動態偵測。
func New(opts Options) *zap.Logger
```

**Rationale**：
- 與 `internal/metrics`、`internal/config` 等既有結構一致，單一職責
- 測試友善——可注入假 env、假 fd，驗證三層決策邏輯
- `cmd/shadowdns/main.go` 的 `main()` 只需一行呼叫

**Alternatives considered**：
- 將 factory 留在 `cmd/shadowdns/main.go`：測試困難（需整合測試才能覆蓋）、與既有 package 分層風格不一致

### 測試輔助 logger 統一使用 `zap.NewNop()` 或 observer

測試目前用 `slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))` 靜音日誌。遷移後：

- **不檢查日誌內容的測試**（多數）：改用 `zap.NewNop()`——零開銷、不需 buffer
- **檢查日誌內容的測試**（少數）：改用 `go.uber.org/zap/zaptest/observer`：
  ```go
  core, recorded := observer.New(zapcore.DebugLevel)
  logger := zap.New(core)
  // ... run code under test
  assert.Equal(t, "expected msg", recorded.All()[0].Message)
  ```

**Rationale**：`observer.ObservedLogs` 提供結構化存取（`.All()`、`.FilterMessage()`、`.FilterLevelExact()`），比 slog 時代的 `bytes.Buffer + 字串比對` 更精準、更穩定。

### 保留 slog 於測試以外的範圍為零

production 程式碼中所有 `log/slog` import 皆須移除。若有任何第三方函式庫要求 `*slog.Logger`（目前沒有），以後可再評估是否引入 `samber/slog-zap` adapter；此 change 不處理此假設。

## Risks / Trade-offs

- **Call site 改寫量大（22 檔）** → 使用 AST-based 自動轉換（`gopls rename` + 正規表示式批次處理）加手動 review；分多個 commit（依 package 邊界切割：config、zone、view、transfer、server、cmd、test）方便 review 與 revert
- **zap 與 slog 的 key=value API 細節差異**（zap 使用 `zap.String("k", v)` 強型別、slog 接受 `any` 可變參數） → 採用 `SugaredLogger` (`logger.Infow(msg, "k", v)`) 維持近似 slog 的 ergonomic，避免全面重寫為強型別欄位
- **journalctl 輸出若仍帶 ANSI** → isatty 偵測保證 systemd 下為 false；但若有人日後在 service unit 指定 `-no-color=false`+強制 TTY，journal 會髒。以 Non-Goal 明示此邊界案例不處理
- **色彩在 Windows 終端機支援** → `go-isatty` 有跨平台支援，但 Windows cmd.exe 需 VT 模式；ShadowDNS 是 Linux daemon，Windows 非主要目標，僅需確認開發環境（macOS、Linux）正常
- **測試斷言若因 ANSI code 污染失敗** → 測試 logger 一律使用 `NewNop()` 或 `observer`，不經 production factory，不會有 ANSI 注入
- **效能** → ShadowDNS 的日誌屬低頻事件（啟動、reload、NOTIFY 失敗），zap 的零 allocation 優勢實質無感；但不劣化效能是必然

## Migration Plan

依 package 邊界分階段提交，每階段可獨立通過 `make test`：

1. **新增 `internal/logging` package 與測試**（不動舊程式碼）
2. **`cmd/shadowdns/main.go` 切換至新 factory**（flag 定義、logger 建構點；其他檔案仍收 `*zap.Logger`）
3. **依序改寫各 package**：`config` → `zone` → `view` → `alias` → `transfer` → `server`
4. **改寫測試**：同步替換測試 logger，驗證所有 `go test ./...` 通過
5. **移除 `log/slog` import**：以 `grep -r "log/slog"` 最終確認 production 零殘留
6. **go.mod tidy**：`go mod tidy` 清理，確認 zap 與 go-isatty 列為 direct
7. **README／CLI help**：更新 `-no-color` flag 文件
8. **驗證腳本**：`make build && ./bin/shadowdns --help` 檢查 flag 顯示；手動跑 `./bin/shadowdns -named-conf ...` 確認有色；`./bin/shadowdns 2>/tmp/log` 確認檔案無 ANSI；`NO_COLOR=1 ./bin/shadowdns ...` 確認無色

**Rollback**：每個階段為獨立 commit，revert 範圍明確；若遷移中途發現 blocker（例如第三方 lib 強制要求 slog），可於第 3 階段前中止並回退。
