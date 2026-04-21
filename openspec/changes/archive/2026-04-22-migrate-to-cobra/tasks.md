## 1. 引入 cobra 依賴 (Decision 1: 選用 cobra 而非 urfave/cli 或 alecthomas/kong)

- [x] 1.1 在專案根目錄執行 `go get github.com/spf13/cobra@latest github.com/spf13/pflag@latest`,確認 `go.mod` / `go.sum` 納入兩者
- [x] 1.2 執行 `go mod tidy` 清理間接依賴,檢視 `go.sum` diff 無意外傳遞依賴
- [x] 1.3 執行 `make build` 確認純依賴引入不會破壞現有程式碼(此時尚未改 main.go)

## 2. 重構 CLI 入口為 cobra (Decision 2: Root command 預設跑伺服器,`reload` 為 subcommand)

- [x] 2.1 在 [cmd/shadowdns/main.go](cmd/shadowdns/main.go) 建立 `rootCmd *cobra.Command`,將現行 `main()` 中伺服器啟動邏輯移入 `rootCmd.RunE`
- [x] 2.2 將 `flag.Usage` 原本的「All flags are parsed once at startup. SIGHUP re-reads...」說明搬到 `rootCmd.Long` 欄位(回應 design.md Open Questions)
- [x] 2.3 將 `main()` 簡化為只剩 signal handler 註冊 + `rootCmd.Execute()`,錯誤由 cobra 回傳後 `os.Exit(1)`
- [x] 2.4 執行 `go build ./...` 確認 rootCmd 可編譯(此時尚未處理 flag 與 reload subcommand)

## 3. Reload 子命令獨立檔案 (Decision 5: `reload` 子命令的實作碼放在獨立檔案)

- [x] 3.1 新增 [cmd/shadowdns/reload.go](cmd/shadowdns/reload.go),定義 `reloadCmd *cobra.Command`,`Use: "reload"`,`Short: "Send SIGHUP to a running shadowdns instance"`
- [x] 3.2 將 main.go 原本 `if reloadFlag { sendSIGHUP(...) }` 的 PID 讀取與 kill 邏輯抽成 `runReload(cmd *cobra.Command, args []string) error`
- [x] 3.3 在 `init()` 或 `main` 呼叫 `rootCmd.AddCommand(reloadCmd)` 註冊子命令
- [x] 3.4 確認 [cmd/shadowdns/main.go](cmd/shadowdns/main.go) 已移除舊 `reloadFlag` bool 變數與相關分支程式碼,符合 Reload flag sends SIGHUP to running instance requirement 的新定義

## 4. Flag 遷移為雙 dash POSIX 風格 (Decision 4: Flag 遷移為單次切換,不保留舊名稱別名)

- [x] 4.1 在 `rootCmd.Flags()` 重新註冊所有伺服器 flag(`--named-conf`、`--aliases`、`--listen`、`--metrics-addr`、`--pprof-enable`、`--dry-run`、`--no-notify`、`--reload-verify`、`--no-color`),預設值與型別與原 stdlib flag 完全相同
- [x] 4.2 為 `--version` 加上 short alias `-v`,使用 `rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "print version and exit")`;確認 `shadowdns --help` 輸出中顯示為 `-v, --version`
- [x] 4.3 以 `rootCmd.MarkFlagRequired("named-conf")` 或等效檢查,維持 `--named-conf` 為啟動伺服器時的必要 flag
- [x] 4.4 確認 `--listen` 的 override 語意未改變,符合 Derive listen address set from named.conf listen-on requirement(host 有值則覆蓋、無 host 則尊重 `listen-on`)
- [x] 4.5 確認 `--reload-verify` 接受 `hash`/`size`/`none` 的驗證仍在 startup 時執行,符合 Reload verify mode configuration requirement
- [x] 4.6 確認 `--no-notify` 的「flag > config」偵測改用 `rootCmd.Flags().Changed("no-notify")` 取代原 `flag.Visit` 機制,保留舊註解所述的優先順序不變

## 5. Reload 子命令 flag 範圍 (Decision 3: `reload` 子命令只繼承必要的 flag)

- [x] 5.1 在 `reloadCmd.Flags()` 只註冊 `--named-conf`(StringVar,必填),不註冊 `--listen`、`--metrics-addr`、`--dry-run`、`--no-notify`、`--reload-verify`
- [x] 5.2 不使用 `PersistentFlags()` 在 rootCmd 上宣告 `--named-conf`,避免 reload 意外繼承其他 root flag;改為在兩個 command 上各自獨立註冊以維持 flag 邊界清晰
- [x] 5.3 確認 `shadowdns reload --help` 輸出只顯示 `--named-conf` 與 `-h, --help`,不顯示伺服器 flag
- [x] 5.4 確認當 operator 執行 `shadowdns reload` 缺少 `--named-conf` 時,cobra 回傳 `MarkFlagRequired` 的錯誤訊息並以非零碼退出,符合 Reload flag sends SIGHUP to running instance requirement 的 "Reload requires named-conf flag" scenario

## 6. 測試更新 (Decision 6: 測試策略 — 保留現有 e2e 測試的精神,更新 flag 語法)

- [x] 6.1 [P] 更新 [cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) 中所有 `exec.Command(binPath, "-version")` 改為 `"--version"`,其他 flag 同理
- [x] 6.2 [P] 新增測試:`TestShortVersionFlag` 驗證 `./shadowdns -v` 輸出與 `--version` 完全相同
- [x] 6.3 [P] 新增測試:`TestReloadSubcommand` 驗證 `./shadowdns reload --named-conf <path>` 在有 PID 檔情境下成功送出 SIGHUP、在缺 PID 檔時非零退出,涵蓋 PID file option parsed from named.conf requirement 所述的 "no pid-file configured" scenario
- [x] 6.4 [P] 新增測試:`TestHelpShowsCombinedVersionFlag` 執行 `./shadowdns --help`,使用 regex 斷言輸出同一行中包含 `-v` 與 `--version`
- [x] 6.5 執行 `make test` 確認所有單元測試通過

## 7. Packaging 更新

- [x] 7.1 [P] 更新 [packaging/shadowdns.service](packaging/shadowdns.service) 的 `ExecStart` 從 `/usr/bin/shadowdns -named-conf ... -aliases ...` 改為 `/usr/bin/shadowdns --named-conf ... --aliases ...`
- [x] 7.2 [P] 更新 [packaging/named.conf.example](packaging/named.conf.example) 註解,把提及 `shadowdns -reload` 的句子改為 `shadowdns reload`
- [x] 7.3 [P] grep 整個 `packaging/` 目錄確認沒有其他 `-named-conf` / `-reload` / `-listen` 遺漏
- [x] 7.4 執行 `make deb` 產生新 `.deb`,用 `dpkg-deb --contents` 檢視 systemd unit 內容與預期相符
- [x] 7.5 執行 `make test-deb`(容器整合測試)確認新 .deb 在 lxc/podman 內能啟動並通過 DNS 查詢測試

## 8. Documentation 與 tooling 更新

- [x] 8.1 [P] grep `README.md` 與 `docs/`(若有)找出 `-named-conf` / `-reload` / `-listen` 等舊語法範例,全部更新為 `--` 版本與 `reload` subcommand
- [x] 8.2 [P] 更新 [.claude/skills/release-shadowdns](.claude/skills/release-shadowdns) 中描述 CLI flag diff 邏輯的部分 — skill 的流程會比對新舊 binary `--help` 輸出以決定是否需要更新 systemd override.conf,需確保 parse 邏輯辨識得雙 dash
- [x] 8.3 [P] 在 `CHANGELOG.md`(若有)或等效變更記錄加入一則描述 CLI 介面變更的條目,敘述用一般性文字,**不**使用 `BREAKING CHANGE:` 或 `!` 標記

## 9. Spec 覆蓋與 trace 更新

- [x] 9.1 確認 `openspec/changes/migrate-to-cobra/specs/dns-server/spec.md` 的 Derive listen address set from named.conf listen-on requirement 與實際 [internal/server](internal/server) 程式碼使用 `--listen` 一致
- [x] 9.2 確認 `openspec/changes/migrate-to-cobra/specs/sighup-reload/spec.md` 的 Reload verify mode configuration requirement 與 [internal/server/fingerprint.go](internal/server/fingerprint.go) 的 flag 讀取一致
- [x] 9.3 確認 `openspec/changes/migrate-to-cobra/specs/pid-file/spec.md` 的兩個 requirement(PID file option parsed from named.conf、Reload flag sends SIGHUP to running instance)與新 [cmd/shadowdns/reload.go](cmd/shadowdns/reload.go) 實作一致

## 10. 本機端到端驗證 (Decision 7: Commit 與發版政策)

- [x] 10.1 執行 `make build` 並跑 `./bin/shadowdns --help`,目視確認輸出中 `-v, --version` 顯示於同一行,且 `Long` 文字仍包含「All flags are parsed once at startup. SIGHUP re-reads...」段落(回應 Open Questions)
- [x] 10.2 執行 `./bin/shadowdns reload --help`,目視確認只顯示 `--named-conf` 與 `-h, --help`
- [x] 10.3 執行 `./bin/shadowdns -v` 與 `./bin/shadowdns --version`,目視確認輸出完全相同
- [x] 10.4 執行 `make lint`,修正 golangci-lint 警告
- [x] 10.5 執行 `make smoke`(dry-run)與 `make test-deb`,確認端到端流程無 regression

## 11. 交接給使用者手動驗證 (Decision 7: Commit 與發版政策)

- [x] 11.1 **停下**,向使用者回報「實作完成,請手動驗證」,列出建議驗證步驟:(a) 本機 `./bin/shadowdns --help` 視覺確認 (b) 在 bench-ns2 dry-run 安裝新 .deb (c) 確認 release-shadowdns skill 在下次部署時能正確處理新 flag syntax
- [x] 11.2 **不**自動 `git add` / `git commit` — 等使用者明確說「驗證通過可以 commit」才進行
- [x] 11.3 使用者核可後,拆成多個 conventional commits:`refactor(cli): migrate CLI to cobra framework`、`refactor(cli): convert reload flag to subcommand`、`chore(packaging): update systemd unit and examples for cobra CLI`、`chore(skills): update release-shadowdns to handle cobra --flag syntax`
- [x] 11.4 每個 commit 訊息**不**使用 `feat!:` / `fix!:` 驚嘆號,也**不**在 body / footer 加 `BREAKING CHANGE:` —— 遵循 feedback_no_breaking_marker_until_v1 規則,保留版號於 v0.x.x
- [x] 11.5 確認 `.claude/CLAUDE.md` 是否需同步更新(例如若 README 有 CLI 範例被此 change 改動,CLAUDE.md 中 Project Build Commands 一節仍保持正確)
