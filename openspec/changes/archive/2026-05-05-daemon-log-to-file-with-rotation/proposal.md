## Why

ShadowDNS daemon 目前透過 stderr 輸出 log，由 systemd 接管後寫入 systemd-journal。journald 的 retention 與 rotation 是**全機共享**設定（`SystemMaxUse=`、`MaxFileSec=`），無法針對單一 service 訂 rotation 策略；要達到 per-service rotation 必須啟用 `LogNamespace=` 跑獨立 journald instance，配置成本高且非 Debian 慣例。

ns2 上 shadowdns 啟動時噴大量 backup-override WARN（過去 10 分鐘 527K 行），在 stderr → journald → rsyslogd 串接路徑下放大成多 process 的 CPU 負擔（rsyslogd 136%、journal 91%、fluent-bit 82%）。將 daemon log 直接寫到 `/var/log/shadowdns/shadowdns.log` 並交由 `/etc/logrotate.d/shadowdns` 管理，可採 Debian 慣例做 per-service rotation，並砍掉 stderr → journald → rsyslog 那段 IPC 與文字 protocol 開銷。

## What Changes

- daemon 新增 `--log-file <path>` flag。空字串（預設）維持現行 stderr 行為（保留 dev 與 unit-test 路徑）；非空字串時以 `O_APPEND|O_CREATE` 打開該檔（mode 0640）作為 zap logger 的 sink。
- logger sink 包裝為 reopenable，內部以 `sync.Mutex` 序列化「寫入」與「重開」。
- daemon 註冊 SIGUSR1 handler：收到時 close 既有 file descriptor 再以同一路徑 `os.OpenFile` 打開，atomic 替換 sink 內的檔案 handle。配合 logrotate 「rename → postrotate signal」流程，不需要 copytruncate，亦不會丟訊息。
- SIGHUP **不變**：繼續為既有 `cmd/shadowdns/reload.go` 的 zone reload 訊號。SIGUSR1 與 SIGHUP 職責分離，避免 logrotate 觸發 zone reload 二次風暴。
- packaging：
  - `packaging/shadowdns.service` 的 `ExecStart` 預設加上 `--log-file /var/log/shadowdns/shadowdns.log`，使 deb 安裝完成後**預設即寫檔**。
  - 新增 `packaging/logrotate.shadowdns`：daily / rotate 14 / compress / delaycompress / missingok / notifempty / `create 0640 shadowdns shadowdns` / `postrotate` 送 SIGUSR1。
  - `nfpm.yaml` 將 logrotate config 安裝至 `/etc/logrotate.d/shadowdns`。
- `packaging/postinstall.sh` 不變（log dir 已建立並 chown）。

## Non-Goals

- **不**重構 backup-zone WARN 的 log cardinality（每筆 record 仍會印一行）。Source 端 aggregation 是另一個獨立問題，留給後續 change。本次 scope 只處理「sink 切換 + rotation」。
- **不**支援同時雙寫（同一份 log 同時送檔案與 stderr/journal）。`--log-file` 非空時，logger 只走檔案；空字串時只走 stderr。雙寫帶來 sink 競爭與 buffer 同步成本，本次不做。
- **不**改 log 編碼格式。`internal/logging/logger.go` 目前的 console encoder 與欄位順序維持不變；切 JSON 格式是另一個獨立決策。
- **不**為 prune-backup 等 CLI 子指令加 `--log-file`。子指令是 one-shot，輸出本來就由呼叫端 redirect，加 flag 沒有 ops 上的好處。
- **不**自帶 in-process rotation（如 `lumberjack`）。Rotation 全交給 `/etc/logrotate.d/`，避免 in-process 與外部 logrotate 同時管理同一檔案造成 race。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `logging`: 新增「daemon 可將 log 寫入指定檔案」與「收到 SIGUSR1 後 reopen log file」兩個 requirement。
- `deb-packaging`: 新增「deb 安裝 logrotate config」與「systemd unit 預設帶 `--log-file` flag」兩個 requirement。

## Impact

- Affected specs:
  - openspec/specs/logging/spec.md（新增 file sink 與 SIGUSR1 reopen requirement）
  - openspec/specs/deb-packaging/spec.md（新增 logrotate config 與 ExecStart 變更 requirement）
- Affected code:
  - New:
    - internal/logging/reopen.go（reopenable WriteSyncer 與單元測試）
    - internal/logging/reopen_test.go
    - packaging/logrotate.shadowdns
  - Modified:
    - internal/logging/logger.go（`Options` 新增 `LogFile`，`New` 視 `LogFile` 開檔或沿用 stderr）
    - internal/logging/logger_test.go（新增 `LogFile` 行為與 reopen 整合測試）
    - cmd/shadowdns/main.go（註冊 `--log-file` flag、SIGUSR1 handler）
    - cmd/shadowdns/main_test.go
    - packaging/shadowdns.service（`ExecStart` 加 `--log-file`）
    - nfpm.yaml（contents 新增 logrotate config 進 `/etc/logrotate.d/shadowdns`）
