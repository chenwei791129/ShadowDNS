## 1. Reopenable file sink

- [x] 1.1 在 `internal/logging/reopen.go` 實作 reopenable WriteSyncer，落實 design Decision: reopen sink 用 `sync.Mutex` 序列化，不用 atomic pointer swap：struct 含 `mu sync.Mutex` 與 `*os.File`、提供 `Write`、`Sync`、`Close`、`Reopen()`、`Path()` 方法（path 於建構時透過 `OpenReopenSink(path string)` 捕獲，不在 `Reopen` 重複傳入）；`Reopen` 開檔失敗時保留舊 fd 並回傳 error；開檔成功但舊 fd close 失敗則 swap 已完成、回傳 wrapped close error 供呼叫端記錄。
- [x] 1.2 [TDD] 撰寫 `internal/logging/reopen_test.go` 紅燈測試：併發 100 writer 同時呼叫 `Write` 與 `Reopen` 不丟訊息（驗證 Decision: reopen sink 用 `sync.Mutex` 序列化，不用 atomic pointer swap 的 mutex 正確性）；`Reopen` 對不可開啟路徑時舊 fd 仍可寫入且 error 回傳；Reopen 成功後新訊息寫入的檔案 inode 與舊檔不同。

## 2. Logger 整合 file sink

- [x] 2.1 落實 Requirement: Daemon SHALL support file-backed log output：修改 `internal/logging/logger.go` 的 `Options` 新增 `LogFile string` 欄位，依 design Decision: `--log-file` 空字串維持 stderr，非空才寫檔 實作：`New()` 在 `LogFile != ""` 時以 `os.OpenFile(LogFile, O_APPEND|O_CREATE, 0640)` 取得 fd 並包成 reopenable sink，落實 Decision: log 檔權限 0640，owner shadowdns:shadowdns 中的 mode 部分；空字串時走原 `zapcore.Lock(os.Stderr)`。`os.OpenFile` 失敗時回傳 error 不 fallback。
- [x] 2.2 [TDD] 擴充 `internal/logging/logger_test.go` 涵蓋 Requirement: Daemon SHALL support file-backed log output 的三個 scenario：`Options{LogFile: tmpfile}` 寫入內容落到該檔且 stderr 沒有（驗證 Decision: `--log-file` 空字串維持 stderr，非空才寫檔 的非空分支）；`Options{LogFile: ""}` 內容只寫到 stderr（驗證空字串分支）；`Options{LogFile: "/nonexistent/dir/foo.log"}` 回傳錯誤。

## 3. CLI flag

- [x] 3.1 [P] 在 `cmd/shadowdns/main.go` daemon 主指令（serve）註冊 `--log-file string`（default `""`）以對應 Requirement: Daemon SHALL support file-backed log output，啟動時將其值塞進 `logging.New(Options{LogFile: ...})`；確保 reload、prune-backup 等子指令未註冊此 flag。
- [x] 3.2 [P] 補 `cmd/shadowdns/main_test.go`：未帶 flag 時 logger sink 為 stderr；帶 flag 時 logger sink 為 reopenable file sink。

## 4. SIGUSR1 reopen handler

- [x] 4.1 落實 Requirement: Daemon SHALL reopen log file on SIGUSR1 與 design Decision: 以 SIGUSR1 觸發 in-process reopen，而非 logrotate `copytruncate`：在 `cmd/shadowdns/main.go` daemon 啟動流程當 `--log-file` 非空時 `signal.Notify` SIGUSR1，handler goroutine 呼叫 reopen sink 的 `Reopen(path)`；reopen error 透過既有 logger 印一筆 error log 但不 propagate；SIGHUP 路徑保留給既有 zone reload 不變動。
- [x] 4.2 [TDD] 在 `cmd/shadowdns/main_test.go` 加整合測試覆蓋 Requirement: Daemon SHALL reopen log file on SIGUSR1 的三個 scenario（同時驗證 Decision: 以 SIGUSR1 觸發 in-process reopen，而非 logrotate `copytruncate` 的訊號路徑）：rename log 檔後送 SIGUSR1，新訊息寫入新 inode（不同 inode）；rename 並刪父目錄後送 SIGUSR1，舊 fd 仍可寫且出現 reopen error log；送 SIGHUP 不影響 log fd。

## 5. systemd unit

- [x] 5.1 落實 Requirement: systemd unit SHALL pass --log-file flag by default：修改 `packaging/shadowdns.service` 的 `ExecStart=` 加 `--log-file /var/log/shadowdns/shadowdns.log`，落實 Decision: log 檔權限 0640，owner shadowdns:shadowdns 中的 owner 部分（保留現有 `User=shadowdns`、`Group=shadowdns`、`ReadWritePaths=/var/log/shadowdns` 設定）。

## 6. logrotate config

- [x] 6.1 [P] 落實 Requirement: deb package SHALL install a logrotate configuration：新增 `packaging/logrotate.shadowdns`，對 `/var/log/shadowdns/*.log` 設定 `daily` / `rotate 14` / `compress` / `delaycompress` / `missingok` / `notifempty` / `create 0640 shadowdns shadowdns` / `sharedscripts` / `postrotate` 區塊用 `systemctl show --property MainPID --value shadowdns.service` 解析 PID 後送 SIGUSR1（呼應 Decision: 以 SIGUSR1 觸發 in-process reopen，而非 logrotate `copytruncate`；不依賴 named.conf 設定的 pid-file 路徑，且只訊號 systemd 管理的那個 instance），unit inactive (MainPID=0) 或環境沒 systemd（systemctl 失敗）時 `|| true` + `[ "$pid" != "0" ]` 守衛確保 rotation exit 0。
- [x] 6.2 [P] 完成 Requirement: deb package SHALL install a logrotate configuration 的封裝部分：修改 `nfpm.yaml` 在 `contents` 加入 `packaging/logrotate.shadowdns` → `/etc/logrotate.d/shadowdns`，owner `root`、group `root`、mode `0644`。

## 7. 端對端驗證

- [x] 7.1 落實 Decision: 單檔 `/var/log/shadowdns/shadowdns.log`，不 split 成 access/error 的 e2e 行為，並覆蓋 design Goals 中「deb 安裝後預設行為即寫檔」與 Non-Goals 中「不引入 in-process rotation library」兩條：`make deb && make test-deb` 確認 package 內含 `/etc/logrotate.d/shadowdns`（`dpkg -L` 可見）、unit 內帶 `--log-file`、容器內服務啟動後寫入該單一檔，並驗證 binary 未連結 lumberjack 等 in-process rotation library（`go list -m all | grep -i lumberjack` 應為空，符合 design Non-Goals）。
- [x] 7.2 請使用者於 ns2 部署該 deb，跑 `sudo logrotate -fv /etc/logrotate.d/shadowdns` 並確認：rotated 檔案存在、現役檔由 daemon 重新建立（mode 0640、owner shadowdns:shadowdns，符合 Decision: log 檔權限 0640，owner shadowdns:shadowdns、新 inode）、daemon 程序 PID 不變、journal 不再湧入 daemon log（涵蓋 spec scenarios：Installed package contains the logrotate config、postrotate tolerates absent daemon、Default ExecStart writes to file、Operator can override via drop-in 由使用者人工驗證最後一條）。
