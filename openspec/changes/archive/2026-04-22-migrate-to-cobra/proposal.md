## Why

目前 ShadowDNS 使用 Go 標準程式庫 `flag` 來解析命令列,這帶來三個侷限:(1) 沒有 short flag 支援,`-version` 無法提供常見的 `-v` 別名;(2) 沒有子命令語意,`-reload` 這種一次性操作只能以 flag 的形式存在,與「啟動伺服器」的預設行為混在同一個 `main` 函式內;(3) 單一 dash 的 `-named-conf`、`-listen` 等寫法偏離 POSIX/GNU 慣例,讓使用者需要額外學習。

趁 ShadowDNS 仍在 v0.x.x 實驗階段、唯一部署是 bench-ns2 的視窗,把 CLI 遷移到 `github.com/spf13/cobra` 一次到位,可以同時解決以上三點,並為未來可能新增的子命令(例如 `shadowdns check-config`、`shadowdns zones list`)預留成長空間。對 DNS query hot path 無任何影響 — cobra 的成本全部集中在啟動階段(+~5ms、+~1MB binary)。

## What Changes

- 引入 `github.com/spf13/cobra` 與 `github.com/spf13/pflag` 依賴,重構 `cmd/shadowdns/main.go` 為 cobra root command
- `-reload` flag 改為 `reload` 子命令 — 語意不變,仍是找 PID 檔並送 SIGHUP
- 所有 flag 從單 dash 遷移到雙 dash(POSIX 風格):`--named-conf`、`--aliases`、`--listen`、`--metrics-addr`、`--pprof-enable`、`--dry-run`、`--no-notify`、`--reload-verify`、`--no-color`、`--version`
- 新增 `-v` 作為 `--version` 的 short flag,`-h` 輸出合併顯示為 `-v, --version`
- `cmd/shadowdns/main.go` 的自訂 `flag.Usage` 換成 cobra 內建的 help 模板(cobra 自動產生 `--help` 輸出與 subcommand 樹)
- 更新 `packaging/shadowdns.service` 的 `ExecStart` 使用新 flag 語法
- 更新 `packaging/named.conf.example` 註解中提及 `shadowdns -reload` 的地方改為 `shadowdns reload`
- 更新 `.claude/skills/release-shadowdns` 中的指令範例(diff CLI flags 的邏輯需跟著改)
- **CLI 介面變更**:這是一次性的 breaking CLI 變更,但在 v0.x.x 階段屬於可接受的迭代成本;commit 訊息**不**使用 `feat!:` 或 `BREAKING CHANGE:` footer,避免 release 自動化工具推升至 v1.0.0(見 `feedback_no_breaking_marker_until_v1`)

## Non-Goals (optional)

- **不**新增除了 `reload` 以外的子命令 — 未來需要的子命令(`check-config`、`zones list` 等)由後續 change 處理
- **不**改變任何 flag 的預設值、型別或語意(僅改名稱前綴)
- **不**保留舊的單 dash 或 `-reload` flag 作為向下相容別名 — 一次乾淨切換
- **不**改動 `named.conf`、`aliases.yaml`、zone 檔等設定檔格式
- **不**提供 shell completion 產生器(這是 cobra 免費送的功能,但非本 change 目標;如需可另開 change)
- **不**撤銷 bench-ns2 的舊部署;release-shadowdns skill 會在下次部署時自動套用新 override.conf

## Capabilities

### New Capabilities

(無)

### Modified Capabilities

- `dns-server`: `-listen` 相關的 requirement/scenario 文字需更新為 `--listen`
- `sighup-reload`: `-reload-verify` 需更新為 `--reload-verify`
- `pid-file`: 提及 `-reload` flag 的 requirement 需改寫為 `reload` 子命令,所有 scenario 中的 `shadowdns -reload -named-conf ...` 需更新為 `shadowdns reload --named-conf ...`

## Impact

- **Affected specs**: `dns-server`、`sighup-reload`、`pid-file`(delta 形式修改既有 requirements);`deb-packaging` 的 systemd unit 檔案雖會改,但 spec-level requirements 不受影響
- **Affected code**:
  - `cmd/shadowdns/main.go`(CLI 結構重寫)
  - `cmd/shadowdns/main_test.go`(測試語法更新:`-version` → `--version` / `-v`,新增 `reload` 子命令測試)
  - `go.mod` / `go.sum`(新增 `spf13/cobra`、`spf13/pflag`)
- **Affected packaging**:
  - `packaging/shadowdns.service`(`ExecStart` 改雙 dash)
  - `packaging/named.conf.example`(註解中的 `shadowdns -reload` → `shadowdns reload`)
- **Affected tooling**:
  - `.claude/skills/release-shadowdns`(新舊 CLI flag diff 邏輯需改)
  - `README.md` / 任何提及 CLI 範例的 docs
- **Affected deployments**:
  - bench-ns2 的 systemd override.conf 會在下次 release 時由 release-shadowdns skill 自動更新
- **Dependencies added**: `github.com/spf13/cobra`、`github.com/spf13/pflag`
