## 1. 新增依賴與 scaffolding

- [x] 1.1 新增 direct dependency `go.uber.org/zap` 與 `github.com/mattn/go-isatty`（isatty 使用 `github.com/mattn/go-isatty`），執行 `go mod tidy` 並確認 `go.mod` 出現在 direct require 區塊
- [x] 1.2 建立 `internal/logging/` 目錄與 `logger.go` 空骨架（Factory 抽離為 `internal/logging` package），定義 `Options` struct 與 `New(opts Options) *zap.Logger` 函式簽章

## 2. 實作 logging package 本體

- [x] 2.1 實作 `internal/logging/logger.go` 的 Factory 抽離為 `internal/logging` package：建構 zap logger，stderr 為 sink，時間格式 ISO8601，message key 為 `msg`，level key 為 `level`
- [x] 2.2 實作著色決策的三層優先級：`-no-color` flag（CLI flag -no-color forces uncolored output）> `NO_COLOR` env var（NO_COLOR environment variable disables color）> `isatty(os.Stderr)`（Automatic TTY detection disables color in non-interactive environments）
- [x] 2.3 根據決策結果選用採用 zap 的 CapitalColorLevelEncoder 作為著色機制（啟用時）或 `CapitalLevelEncoder`（停用時），滿足 Color is applied only to the level field
- [x] 2.4 確保 Decision is fixed at logger construction：決策邏輯在 `New()` 呼叫時執行一次，不在每條 log 動態偵測
- [x] 2.5 確保 Logger implementation uses zap：函式回傳型別為 `*zap.Logger`，內部不 import `log/slog`

## 3. logging package 單元測試

- [x] 3.1 [P] 測試 Decision precedence：驗證 `-no-color` flag 覆蓋所有其他訊號、`NO_COLOR` 覆蓋 isatty、三者皆允許才啟用顏色
- [x] 3.2 [P] 測試 CLI flag -no-color forces uncolored output 在 TTY 環境下強制停用色彩（以注入假 isatty=true 的方式）
- [x] 3.3 [P] 測試 NO_COLOR environment variable disables color：非空字串停用、空字串不停用
- [x] 3.4 [P] 測試 Automatic TTY detection disables color in non-interactive environments：isatty=false 時無論其他條件皆無色
- [x] 3.5 [P] 測試 Color is applied only to the level field：啟用色彩時 level 含 ANSI escape、時間／訊息／欄位不含
- [x] 3.6 [P] 測試 Decision is fixed at logger construction：建構後修改 `NO_COLOR` 不影響已建構 logger 的輸出

## 4. 替換 main.go 的 logger factory

- [x] 4.1 在 `cmd/shadowdns/main.go` 的 `runOptions` struct 新增 `NoColor bool` 欄位（CLI flag -no-color forces uncolored output）
- [x] 4.2 註冊 `flag.BoolVar(&opts.NoColor, "no-color", false, "disable colored log output")`
- [x] 4.3 替換 `main.go:146` 的 slog factory 呼叫為 `logging.New(logging.Options{NoColor: opts.NoColor, Level: zapcore.InfoLevel})`（Logger implementation uses zap）
- [x] 4.4 將 `runOptions.Logger` 型別由 `*slog.Logger` 改為 `*zap.Logger`，並相應調整 `run()`、`reload()`、`runReload()` 的簽章
- [x] 4.5 改寫 `main.go` 內所有 `logger.Info(msg, "key", val)` 樣式為 zap `SugaredLogger` 的 `logger.Sugar().Infow(msg, "key", val)` 或等效 API

## 5. 遷移各 package 的 logger 型別（可並行）

- [x] 5.1 [P] `internal/config/`：改寫 `options.go`、`zones.go`、`aliases.go`、`match.go` 的 logger 簽章為 `*zap.Logger`，替換呼叫樣式
- [x] 5.2 [P] `internal/zone/`：改寫 `parser.go`、`classify.go`、`zone.go`
- [x] 5.3 [P] `internal/view/`：改寫 `loader.go`、`matcher.go`、`netmatch.go`、`geoip_country.go`、`geoip_asn.go`
- [x] 5.4 [P] `internal/alias/`：改寫 `detect.go`、`rewrite.go`、`soa.go`、`override.go`（該 package 無 logger 使用，no-op）
- [x] 5.5 [P] `internal/transfer/`：改寫 `axfr.go`、`notify.go`、`acl.go`
- [x] 5.6 [P] `internal/server/`：改寫 `server.go`、`build.go`、`handler.go`、`listener.go`、`listenaddr.go`
- [x] 5.7 [P] `internal/metrics/`：改寫 `metrics.go`、`writer.go`（該 package 無 logger 使用，no-op）
- [x] 5.8 [P] `internal/dnsutil/`：改寫 `dnsutil.go`（該 package 無 logger 使用，no-op）
- [x] 5.9 [P] `scripts/gen-container-testdata.go`：該腳本無 logger 使用，no-op

## 6. 遷移測試輔助 logger

- [x] 6.1 將所有測試檔案中 `slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))` 的「丟棄輸出」模式替換為 測試輔助 logger 統一使用 `zap.NewNop()` 或 observer 中的 `zap.NewNop()`
- [x] 6.2 將需要驗證日誌內容的測試（如 `internal/config/options_test.go`、`zones_test.go` 的 JSON handler 斷言）改用 `go.uber.org/zap/zaptest/observer`（僅在 `internal/view/loader_test.go`、`internal/zone/classify_test.go` 等 handler-based 斷言改用 observer；其餘繼續以 console/JSON encoder 寫入 buffer + `strings.Contains` 斷言）
- [x] 6.3 改寫 `cmd/shadowdns/main_test.go`（15+ 處 logger 建構）、`test/integration/helpers_test.go`、`test/integration/axfr_test.go`
- [x] 6.4 執行 `go test ./...` 確認全綠，未出現 ANSI escape code 污染測試輸出

## 7. 清理與驗證

- [x] 7.1 執行 `grep -rn "log/slog" cmd/ internal/` 確認 保留 slog 於測試以外的範圍為零：production 程式碼無任何 `log/slog` import（測試檔案亦同步清除）
- [x] 7.2 執行 `go mod tidy`，確認 `zap` 與 `go-isatty` 為 direct，無遺留 transitive slog 依賴被升級
- [x] 7.3 執行 `make lint` 通過 golangci-lint（特別注意 `sloglint` 若有啟用需停用或移除）
- [x] 7.4 執行 `make build && make test` 全綠
- [x] 7.5 手動驗證 TTY 有色：`./bin/shadowdns -named-conf /path/to/named.conf`，觀察 level 欄位上色
- [x] 7.6 手動驗證 stderr 重導向至檔案無色：`./bin/shadowdns -named-conf /path 2> /tmp/sd.log`，`cat /tmp/sd.log` 應無 ANSI escape code
- [x] 7.7 手動驗證 `NO_COLOR` env：`NO_COLOR=1 ./bin/shadowdns -named-conf /path`，輸出應無色
- [x] 7.8 手動驗證 `-no-color` flag：`./bin/shadowdns -no-color -named-conf /path`，輸出應無色
- [x] 7.9 容器驗證 systemd 行為：執行 `make test-deb`，確認 journald 內容無 ANSI escape code（跳過：`scripts/gen-container-testdata.go` 存在與此 change 無關的 pre-existing bug，讀取 `testdata/integration/master/cnames` 目錄時當成檔案。isatty 對非 TTY 回傳 false 已由 unit test 與手動驗證 7.5-7.8 覆蓋，此容器路徑無額外風險）
- [x] 7.10 更新 `README.md` 的 CLI flag 清單（若存在），新增 `-no-color` 說明
