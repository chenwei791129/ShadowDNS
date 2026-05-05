## Context

ShadowDNS daemon 透過 `internal/logging/logger.go` 構造的 zap logger 寫入 `os.Stderr`（`logger.go:39`）。在 systemd unit 下 stderr 會被 systemd 接管寫入 journal。journald 的 retention 是以 `/etc/systemd/journald.conf` 全機共享，per-service 設策略需要 `LogNamespace=` 跑獨立 journald instance，配置成本與運維成本都高。

ns2 上實際運行顯示，daemon 啟動期間發出大量 backup-override WARN（startup 一波 ≈527K 行 / 10 分鐘），在 stderr → journald → rsyslog → fluent-bit 的多層 pipeline 中，三個 process 同時被打到 80%+ CPU。雖然 cardinality 才是根因，但「daemon log 沒有 per-service rotation 控制」是長期 ops 痛點，與此次 cardinality 議題正交，值得獨立解決。

`packaging/shadowdns.service` 已經將 `/var/log/shadowdns` 列入 `ReadWritePaths`，`packaging/postinstall.sh` 也建立並 chown 該目錄為 `shadowdns:shadowdns` mode 0750；packaging 早已為「直接寫檔」情境鋪好路，code 端尚未跟進。

`cmd/shadowdns/reload.go:71` 已將 SIGHUP 用於 zone reload。任何「log file reopen」訊號必須與 SIGHUP 分離，否則 logrotate 觸發後會順便 reload zones、再噴一輪 startup-style WARN。

## Goals / Non-Goals

### Goals

- Daemon 可選擇將 zap logger 輸出導向指定檔案，預設指向 `/var/log/shadowdns/shadowdns.log`。
- 提供一個 in-process 機制讓檔案 handle 在外部工具 rename 後可重新打開新 inode，避免訊息遺失。
- 透過 `/etc/logrotate.d/shadowdns` 以 Debian 慣例做 daily rotation，rotation 與 in-process reopen 解耦。
- deb 安裝後預設行為即寫檔，不依賴 operator 修改 override.conf。
- 不破壞既有測試與 CLI 子指令（reload、prune-backup）的輸出行為。

### Non-Goals

- 不解決 backup-zone WARN 的 cardinality 問題（log 量本身）。
- 不支援同時雙寫檔案與 stderr。
- 不更動 log 編碼格式（保持目前的 zap console encoder）。
- 不在 prune-backup 等 one-shot 子指令上加 `--log-file`。
- 不引入 in-process rotation library（如 `lumberjack`），rotation 全交給 logrotate。

## Decisions

### Decision: 以 SIGUSR1 觸發 in-process reopen，而非 logrotate `copytruncate`

**選擇**：daemon 註冊 SIGUSR1 handler，handler 內部呼叫 reopen sink 的 `Reopen()` 方法 close 舊 fd 並 `os.OpenFile` 同路徑取得新 fd，logrotate 在 `postrotate` 階段送 SIGUSR1。

**理由**：
- `copytruncate` 不需要 signal，但會有 copy 與 truncate 之間的 race window，期間寫入會被截掉。雖然平常 log 量小，但本服務啟動瞬間可能 880 行/秒，window 內的 byte 數不可忽略。
- SIGUSR1 是 nginx / fluentd / syslog-ng 等服務 reopen log 的慣例；對 sysadmin 認知成本低。
- 既然已經要為 file sink 改 logger code，多寫一個 reopen sink + signal handler 邊際成本很小（≈30 行 + signal.Notify 一行）。
- 與 SIGHUP 職責分離：SIGHUP=reload zones（既有），SIGUSR1=reopen log file。logrotate 不會誤觸 zone reload。

**替代方案**：
- `copytruncate` — 上述 race window 缺點。
- in-process rotation（lumberjack）— 跟 `/etc/logrotate.d/` 規範衝突，使用者明確要走 logrotate.d 慣例。

### Decision: 單檔 `/var/log/shadowdns/shadowdns.log`，不 split 成 access/error

**選擇**：所有 zap logger 輸出導向單一檔案。

**理由**：
- 目前 logger 只有一條 stream，沒有 query log / access log 概念；強行 split 必須先在 code 端引入「access vs error 分流」抽象，本次 scope 不打算做。
- 未來若導入 per-query log（memory 中已記載這個計畫），那是另一個 capability 的事，到時再決定要不要 split。
- 單檔降低 logrotate config 與 SIGUSR1 reopen 實作複雜度（只要管一個 fd）。

**替代方案**：
- access.log + error.log 分流 — YAGNI，現在沒有 access log。

### Decision: `--log-file` 空字串維持 stderr，非空才寫檔

**選擇**：CLI flag 預設值 `""`，行為與目前完全一致；非空時切到檔案 sink，**不**雙寫。

**理由**：
- 維持向後相容：unit-test、本機 dev、`go run ./cmd/shadowdns` 不需任何改動就能繼續用 stderr 看 log。
- 雙寫帶來 sink 競爭、緩衝同步、以及「兩份 log 內容微妙不一致」的混淆，本次不引入。
- deb 預設透過 `shadowdns.service` 的 `ExecStart` 帶入 `--log-file /var/log/shadowdns/shadowdns.log`，使「裝完 deb 即寫檔」成為預設行為。Operator 仍可透過 override.conf 將 flag 拿掉回到 stderr/journal。

**替代方案**：
- 透過 `shadowdns.yaml` config 設定 — config 適合 daemon 的內部行為，但 log 輸出位置與「啟動腳本怎麼跑」高度相關（ExecStart、override.conf 都在那裡），放 CLI flag 更直觀。也避免 config 載入失敗那一刻沒地方 log 的雞生蛋問題。

### Decision: reopen sink 用 `sync.Mutex` 序列化，不用 atomic pointer swap

**選擇**：reopen sink 結構 `{ mu sync.Mutex; f *os.File }`，`Write` 與 `Reopen` 都在 lock 下操作。

**理由**：
- zap core 的寫入頻率相對 logrotate 觸發頻率高出多個量級，但 `sync.Mutex` uncontended 的 cost 在納秒級，對 daemon 寫 log 不成瓶頸。
- atomic pointer swap 可避開 lock 但要小心舊 fd 的 close 時機（已有 in-flight 寫入怎麼辦）；Mutex 寫法直接、容易證明正確性。
- 既然 zap 的內建 lock sink (`zapcore.Lock`) 也是 mutex 模型，行為一致。

**替代方案**：
- `atomic.Pointer[os.File]` — 寫得對，但要處理 close 與最後一筆寫入的 happens-before；本場景不值得。

### Decision: log 檔權限 0640，owner shadowdns:shadowdns

**選擇**：daemon 第一次寫檔時若檔案不存在，以 `O_CREATE` 建出，mode 透過 `os.OpenFile(path, O_APPEND|O_CREATE, 0640)` 套上；rotation 後新檔由 logrotate `create 0640 shadowdns shadowdns` 指令保證權限。

**理由**：
- 0640 讓 group 成員（如 ops 加入 `shadowdns` group）能讀但不能寫，降低權限風險。
- daemon 以 `shadowdns` user 跑（systemd unit 已設），直接擁有檔案。
- log dir 已是 0750 owned by shadowdns:shadowdns，daemon 有權建立檔案。

## Risks / Trade-offs

- **Risk**: SIGUSR1 handler 若 panic 會打掛整個 daemon → Mitigation：handler 內任何 error（reopen 失敗）只記到既有 sink 並 return，不 propagate；同時保留舊 fd（reopen 失敗時不要 close），確保下游寫入仍能落地。
- **Risk**: logrotate 與 daemon 啟動 race（rotate 在 daemon 啟動時觸發，daemon 仍在 zone 載入階段尚未 `signal.Notify(SIGUSR1)`）→ Mitigation：`postrotate` 用 `systemctl show --property MainPID --value shadowdns.service` 取得 PID（unit inactive 時得 `0`，systemd 不可用時 systemctl 失敗），再以 `[ "$pid" != "0" ]` 守衛 + `|| true` 包住 `kill -USR1`，rotation 一律 exit 0；若 daemon 在 startup 中收到 SIGUSR1 也會被 Go runtime 因無 handler 而靜默丟棄（不會 terminate process）。Cron 跑 daily，與 daemon 啟動的 5 分鐘窗口幾乎不重疊。原本以 `pid-file` 路徑判斷的設計已捨棄，因 named.conf 的 `pid-file` 路徑由 operator 設定，packaging 無法預測；亦比 `pkill -x shadowdns` 精準，只訊號 systemd 管理的 instance、不會誤觸 operator 自行啟動的同名 binary。
- **Risk**: 既有 ns2 升級後 systemd unit 變更但 override.conf 沒同步 → Mitigation：`shadowdns.service` 維持向後相容（`ExecStart=` 重設後再加 flag），override.conf 若也設了 `ExecStart=` 則由 operator 決定要不要更新；release-shadowdns skill 的 step 2/3 會 diff CLI flag 並提示。
- **Risk**: `/var/log/shadowdns` 磁碟用量超預期 → Mitigation：rotate 14 + delaycompress + compress；若 startup 期單檔過大，後續可改 hourly 或加 size 上限。
- **Trade-off**: 砍掉 stderr 路徑就少掉「在 `journalctl -u shadowdns` 直接看 log」的便利 → Mitigation：操作慣例改用 `tail -F /var/log/shadowdns/shadowdns.log`，比 journalctl 更快（不過 journald 索引）。並在 README / spec 內記錄此變更。

## Migration Plan

1. 升級 deb 後 daemon 重啟即生效（ExecStart 帶新 flag）。systemd 自動 daemon-reload 由 postinstall 執行。
2. ns2 既有 override.conf 完整重寫了 ExecStart，需同步加上 `--log-file` flag 才會寫檔；release-shadowdns skill 已涵蓋此檢查流程（step 2/3）。
3. logrotate config 安裝後由 cron daily 自動執行；不需手動觸發。
4. Rollback：移除 `--log-file` flag（或刪除 override.conf 該行），daemon 重啟回到 stderr/journal。logrotate config 留著無害（`missingok` 保證檔案不存在不報錯）。

## Open Questions

- 暫無。所有設計決策已收斂。
