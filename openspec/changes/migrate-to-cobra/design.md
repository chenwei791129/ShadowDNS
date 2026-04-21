## Context

ShadowDNS 目前的 CLI 解析集中在 [cmd/shadowdns/main.go](cmd/shadowdns/main.go) 的約 40 行 `flag.*Var(...)` 呼叫中,包含 12 個 flag 與一個客製化的 `flag.Usage` 函式。`-reload` 是唯一「非啟動伺服器」的操作模式,以 bool flag 的方式觸發一段獨立的 `sendSIGHUP()` 邏輯後 `os.Exit(0)`。

此設計在功能上可行,但缺點是:
1. **結構扁平**:`main()` 內部需要用 `if reloadFlag { ... }` 做分支,隨著未來可能新增 `check-config`、`zones list` 等操作會越來越難維護
2. **缺短旗標**:`-version` 無法提供 `-v` 別名 — stdlib `flag` 的最簡 workaround 是註冊同名變數兩次,`-h` 輸出會分兩行顯示
3. **非慣例的 dash 風格**:單 dash 的 `-named-conf` 讓熟悉 POSIX/GNU 工具的使用者需要額外記憶

此 change 發生在 ShadowDNS v0.x.x 實驗階段,只有 bench-ns2 一個部署,適合一次到位的 breaking CLI 重構。

## Goals / Non-Goals

**Goals:**

- 改用 `github.com/spf13/cobra` 作為 CLI framework,取代 stdlib `flag`
- 將 `-reload` 從 flag 提升為真正的 `reload` 子命令
- 所有 flag 改採雙 dash POSIX 風格(`--named-conf` 等)
- `--version` 同時綁定 `-v` short alias,help 顯示合併為 `-v, --version`
- 保留所有 flag 的現有預設值、型別、語意與優先順序規則(例如 `--listen` 對 named.conf `listen-on` 的 override 行為)
- Help 輸出改用 cobra 預設模板(仍顯示重要段落:flags、SIGHUP 重載說明)

**Non-Goals:**

- 不保留任何向下相容別名(不支援 `-named-conf`、`-reload`);一次乾淨切換
- 不新增 `reload` 以外的子命令(留給後續 change)
- 不產生 shell completion 或 man page(cobra 免費功能,但本 change 不啟用)
- 不變更 `named.conf` / `aliases.yaml` / zone 檔格式
- 不變更 PID 檔路徑解析、SIGHUP 處理、或 reload verify 邏輯本身 — 只改驅動入口

## Decisions

### Decision 1: 選用 cobra 而非 urfave/cli 或 alecthomas/kong

**選擇**:`github.com/spf13/cobra` + 其底層 `github.com/spf13/pflag`。

**理由**:
- cobra 是 Go 生態最廣泛採用的 CLI framework(Kubernetes、Docker、Helm、GitHub CLI 等),社群熟悉度高
- pflag 提供 GNU-style 長短旗標合併顯示(`-v, --version` 一行),正好滿足本 change 的驅動需求之一
- 內建自動 help、子命令樹、flag inheritance,減少自訂碼量
- 未來若需要 shell completion、man page 生成,cobra 內建支援,零額外依賴

**已考慮的替代方案**:
- `urfave/cli` — API 較輕,但短旗標合併顯示的模板客製較麻煩
- `alecthomas/kong` — declarative struct-based,優雅但生態較小,團隊熟悉度低
- 繼續用 stdlib `flag` + 自訂 Usage — 可達成短旗標顯示,但無子命令能力,未來要加 `check-config` 等仍需遷移

### Decision 2: Root command 預設跑伺服器,`reload` 為 subcommand

**選擇**:`rootCmd` 的 `RunE` 執行現行伺服器啟動流程;`reloadCmd` 作為 subcommand 執行 SIGHUP 發送。

```
shadowdns [flags]              # 啟動伺服器(預設行為)
shadowdns reload [flags]       # 送 SIGHUP 給跑著的伺服器
shadowdns --version / -v       # 印版本後退出
shadowdns --help / -h          # 印 help 後退出
```

**理由**:
- 保留「直接 `shadowdns` 啟動」的慣例,systemd `ExecStart` 不需加多餘的 `server` 關鍵字
- `reload` 作為 explicit subcommand 語意清晰,未來加 `check-config` 等也是平行擴充
- cobra 支援 root command 自己有 `RunE`,不需要強制加 `server` 層

**已考慮的替代方案**:
- `shadowdns server` + `shadowdns reload` 雙子命令(無預設行為)— 要求改動 systemd unit 為 `ExecStart=/usr/bin/shadowdns server ...`,語意更明確但徒增一層,未來若使用者忘記加 `server` 會拿到 help 而非啟動

### Decision 3: `reload` 子命令只繼承必要的 flag

**選擇**:`reload` 子命令只需要 `--named-conf`(讀取 PID 檔路徑)這個 flag,不繼承 `--listen`、`--metrics-addr`、`--dry-run` 等只在啟動伺服器時有意義的 flag。

**理由**:
- 減少使用者誤解:`shadowdns reload --listen :5353` 看起來像要換埠,其實完全無效
- Help 輸出更乾淨:`shadowdns reload --help` 只顯示相關 flag
- cobra 預設就是 subcommand 擁有自己的 flag set,不自動繼承 parent flags(除非明確用 `PersistentFlags()`),我們順勢使用此預設

### Decision 4: Flag 遷移為單次切換,不保留舊名稱別名

**選擇**:所有 flag 從單 dash 完全切換到雙 dash,**不**提供 `-named-conf` 作為 `--named-conf` 的 alias、也**不**保留 `-reload` flag。

**理由**:
- ShadowDNS 在 v0.x.x 階段,bench-ns2 是唯一部署,沒有外部使用者契約需要維持
- 保留別名會讓程式碼與 help 輸出變複雜(每個 flag 要註冊兩次)
- 乾淨切換配合 release-shadowdns skill 一併更新 bench-ns2 的 systemd override.conf,不會留下技術債

**已考慮的替代方案**:
- 用 pflag 的 `NormalizeFunc` 把單 dash flag 名稱轉成雙 dash — 可行但等同保留舊語法,違背「乾淨切換」的意圖

### Decision 5: `reload` 子命令的實作碼放在獨立檔案

**選擇**:新增 `cmd/shadowdns/reload.go` 放 `reloadCmd` 的定義與 `runReload()` 函式,從 `main.go` 中抽離出現行的 sendSIGHUP 邏輯。`main.go` 僅保留 root command 與啟動流程。

**理由**:
- 每個 subcommand 一個檔案是 cobra 社群慣例(參考 kubectl、helm)
- 讓 `main.go` 不再需要 `if reloadFlag { ... }` 分支,減少耦合
- 未來新增子命令時有清楚的檔案位置慣例

### Decision 6: 測試策略 — 保留現有 e2e 測試的精神,更新 flag 語法

**選擇**:[cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) 中的 `-version` / `-named-conf` 等語法全部更新為 `--version` / `--named-conf`,並加入以下新案例:
- `-v` short alias 產生與 `--version` 相同輸出
- `shadowdns reload --named-conf <path>` 等同舊 `shadowdns -reload -named-conf <path>` 行為
- `shadowdns --help` 輸出包含 `-v, --version` 合併顯示

**理由**:
- 保持 e2e 測試仍然以 built binary 的實際 CLI 行為為驗證對象,而非 cobra 內部結構
- 新測試覆蓋本 change 的三個驅動 feature(短旗標、subcommand、help 顯示)

### Decision 7: Commit 與發版政策

**選擇**:
- 實作分多個 conventional commit,例如 `refactor(cli): migrate to cobra framework`、`refactor(cli): convert reload flag to subcommand`、`chore(packaging): update systemd unit for new CLI flags`
- **不**使用 `feat!:` 或 `BREAKING CHANGE:` footer(即使改動事實上是 breaking CLI 介面)
- 實作完成後**停下**等使用者手動驗證,**不**自動 commit

**理由**:
- 遵循 `feedback_no_breaking_marker_until_v1`:v0.x.x 階段避免 release 自動化推升至 v1.0.0
- 遵循 `feedback_manual_verify_before_commit`:牽涉 CLI、systemd、部署的重構須使用者在 bench-ns2 實跑驗證
- 拆多個 commit 讓 review 與 rollback 更細粒度(`git revert` 能獨立撤銷某一步)

## Risks / Trade-offs

- **[風險] Binary size 增加 ~1MB** → **Mitigation**:相對於目前 `bin/shadowdns` 的 ~20MB+(miekg/dns + zap + prometheus),+1MB 約 5%,可接受;若敏感可在 CI 加 binary size check 做長期追蹤(本 change 不做)
- **[風險] 啟動時間增加 ~5ms**(cobra command tree init)→ **Mitigation**:DNS server 是長駐程序,啟動一次性成本無實務影響;query hot path 完全不受影響
- **[風險] 遺漏更新 packaging/docs 中的 CLI 範例** → **Mitigation**:tasks.md 明確列出要 grep 的目標檔案(`packaging/`、`README.md`、`.claude/skills/release-shadowdns`、`openspec/specs/*/spec.md`),每個都有獨立 task;`make smoke` 與 `make test-deb` 作為最後把關
- **[風險] bench-ns2 部署時新舊 systemd unit 不一致導致 dpkg post-install 失敗** → **Mitigation**:release-shadowdns skill 在 dpkg -i 後有「reconcile override.conf」步驟,會讀取新安裝的 unit 檔決定是否需要更新 override;本 change 需同步更新該 skill 使其認得新 flag 語法
- **[風險] 測試 regex / assertion 寫死了舊 flag 輸出格式** → **Mitigation**:`main_test.go` 既有測試用的是 `exec.Command(binPath, "-version")` 這類精確字面量,會在 `make test` 直接失敗,不會靜默通過;tasks.md 明確列出測試更新 task

## Migration Plan

1. **開發階段**(本機):依 tasks.md 順序完成程式碼、測試、packaging、specs、docs 更新;本機跑 `make build test lint smoke deb test-deb`
2. **使用者驗證**:實作完成後停下,請使用者手動驗證以下情境(不自動 commit):
   - `./bin/shadowdns --help` 輸出包含 `-v, --version` 合併一行
   - `./bin/shadowdns -v` 與 `./bin/shadowdns --version` 產生相同輸出
   - `./bin/shadowdns reload --help` 顯示 reload 子命令專屬 help
   - `make test-deb` 通過
3. **Commit**:使用者核可後,依 Decision 7 拆成多個無 breaking marker 的 conventional commits
4. **部署到 bench-ns2**:呼叫 release-shadowdns skill;skill 會自動 diff 新舊 CLI flag,偵測到 systemd override.conf 需更新時套用新語法

**Rollback**:若 bench-ns2 發現問題,`dpkg -i` 回裝前一個 `.deb` 版本即可;git revert 多個 commit 回到舊 stdlib flag 實作。

## Open Questions

- 是否要在 cobra root command 保留原本 `flag.Usage` 中那段「All flags are parsed once at startup. SIGHUP re-reads...」說明?
  - **暫定**:是,放進 root command 的 `Long` 欄位,`shadowdns --help` 仍會印出來。實作時確認 cobra 模板中 `Long` 顯示位置合理即可。
