## 1. Config 解析

- [x] 1.1 在 `internal/config/options.go` 的 `OptionsBlock` struct 新增 `PidFile string` field，並在 `ParseOptions` 的 switch 中新增 `pid-file` case 呼叫 `lx.readScalarValue()` 存入 `block.PidFile`（涵蓋 PID file option parsed from named.conf）

## 2. PID File Lifecycle

- [x] [P] 2.1 在 `cmd/shadowdns/main.go` 的 `run()` 中，`srv.Start()` 前寫入 PID file（`os.Getpid()` + newline），defer 刪除。路徑為 `cfg.Options.PidFile`，空字串時不寫。目錄不存在時 log warning 並跳過（涵蓋 PID file written on startup、PID file removed on shutdown）
- [x] [P] 2.2 在 `cmd/shadowdns/main.go` 的 `main()` 中新增 `-reload` flag。當 `-reload` 為 true 時：驗證 `--named-conf` 已指定 → `config.LoadNamedConf()` → 驗證 `cfg.Options.PidFile` 非空 → 讀取 PID file → `strconv.Atoi` 解析 PID → `syscall.Kill(pid, syscall.SIGHUP)` → exit(0)。各步驟失敗時印 error 並 exit(1)（涵蓋 Reload flag sends SIGHUP to running instance）

## 3. 測試

- [x] [P] 3.1 `internal/config/options_test.go`：新增 `pid-file` option 解析測試，驗證有 `pid-file` 和沒有 `pid-file` 兩種情境（涵蓋 PID file option parsed from named.conf）
- [x] [P] 3.2 `cmd/shadowdns/main_test.go`：新增 PID file lifecycle 測試 — 啟動 `run()` 後驗證 PID file 存在且內容正確，cancel 後驗證 PID file 被刪除（涵蓋 PID file written on startup、PID file removed on shutdown）
- [x] [P] 3.3 `cmd/shadowdns/main_test.go`：新增 `-reload` 功能測試 — 啟動 server（寫 PID file）→ 呼叫 reload 邏輯 → 驗證 SIGHUP 已送達（zone 資料更新）。另測試 PID file 不存在、內容無效、`pid-file` 未設定等 error cases（涵蓋 Reload flag sends SIGHUP to running instance）
