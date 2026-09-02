## 1. 設定層：initial_delay 的解析與驗證

- [x] 1.1 先寫失敗測試：在 `internal/shadowdnscfg/doh_test.go` 依 design 的「設定介面」契約與 spec 的 initial_delay parsing and validation 範例表，覆蓋八種輸入（欄位缺漏、空字串、`0s`、`30s`、`2m`、`-1s`、`30`、`soon`），斷言載入成功時的生效 duration、載入失敗時錯誤訊息點名 `initial_delay` 且無效值案例包含被拒絕的原始值。驗證：`go test ./internal/shadowdnscfg/ -run TestLoad` 出現預期的失敗（欄位尚未存在）。
- [x] 1.2 依 design 的「設定欄位以 YAML 字串承載並解析為 Go duration」，讓 `doh.acme.initial_delay` 成為可載入的選填欄位：原始 YAML 結構新增字串欄位、正規化後的 DoH ACME 設定結構新增 duration 欄位，建構函式以 `time.ParseDuration` 轉換，空字串或缺漏得到零值，無法解析與負值各自回傳點名該欄位的錯誤，且不做上限檢查。同時確認 DoH server listens on a configured address 的其餘行為未回歸：既有必填欄位缺漏仍失敗並點名該欄位、嚴格解碼仍拒絕未知鍵（例如 `doh.acme.email`），未設定 `initial_delay` 的既有測試資料不需修改斷言即通過。驗證：`go test ./internal/shadowdnscfg/` 與 `go test ./cmd/shadowdns/ -run DoH` 全綠，1.1 的八個案例皆通過。

## 2. 憑證迴圈：可取消的首次發憑證延遲

- [x] 2.1 先寫失敗測試：在 `internal/doh/acme_test.go` 依 design 的「可取消等待抽成獨立 helper 以便確定性測試」，為 `certManager` 的等待方法覆蓋三情境——零延遲立即回傳續行、正值延遲的經過時間不小於設定值（單邊斷言，使用極短 duration）、等待期間取消 context 回傳中止。驗證：`go test ./internal/doh/ -run TestCertManager` 出現預期的失敗（方法尚未存在）。
- [x] 2.2 依 design 的「等待放在憑證迴圈開頭，而非 DoH server 的執行核心」與「延遲的計時起點為憑證迴圈進入時」，在 `certManager` 新增初始延遲欄位並實作可取消等待方法，於背景憑證迴圈進入 for 迴圈之前呼叫；等待以可停止的計時器搭配 context 完成，避免遺留未觸發的執行期計時器。欄位形狀比照既有的 `retryInterval`（同為 `time.Duration` 結構欄位），但所有權不同——`retryInterval` 是建構函式獨佔的內部常數，初始延遲是外部設定，因此以建構函式參數傳入（`Server.Run` 帶入 `s.cfg.ACME.InitialDelay`，測試於建構時傳入），不採建構後指派。驗證：2.1 三個情境全綠。
- [x] 2.3 實作 Initial ACME certificate issuance can be delayed at startup 的觀測面：生效延遲為正時，在等待開始輸出一筆 Info 日誌，訊息點明正在延後首次 ACME 發憑證並帶入設定的 duration，不含任何 challenge token 或金鑰材料；生效延遲為零時不輸出該筆日誌。驗證：新增使用 `zaptest` 觀測 logger 的測試，斷言正值案例出現該筆日誌且欄位含 duration、零值案例不出現。
- [x] 2.4 依 design 的「取消路徑不產生任何憑證失敗訊號」，證明等待期間取消不會嘗試取得憑證：以會記錄呼叫次數的 stub 取得函式與 fake metrics 驅動背景憑證迴圈，於延遲期間取消 context 後，斷言取得函式零次呼叫、失敗續期計數為零，且迴圈已返回。驗證：`go test -race ./internal/doh/` 全綠。
- [x] 2.5 證明首次延遲不污染重試與續期時序。注意可測性邊界：迴圈內的 `wait` 計算是內嵌的，`renewRetryInterval`（10m）與 `minRenewInterval`（1m）都是套件常數，design 的「範圍邊界」把重試/續期時序常數列為範圍外，所以不得為了斷言真實常數值而抽出或改動它們——否則測試必須真的等上數分鐘。改以下列兩項覆蓋：（a）失敗重試路徑：以極短的初始延遲與直接指派的極短 `retryInterval` 驅動 `cm.run`，讓 obtain 連續失敗兩次並記錄呼叫時間，斷言第一次呼叫距啟動不小於初始延遲、第二次距第一次約為 `retryInterval` 且明顯小於初始延遲——證明重試走的是迴圈內既有路徑、沒有再套用初始延遲；（b）續期路徑：以既有的 `TestRenewDelay` 守住純函式 `renewDelay` 未被改動，並在程式碼審閱中確認初始等待呼叫位於 `for` 迴圈之外、迴圈內的 `wait` 計算逐字未變（結構上不可能把初始延遲加進續期排程）。驗證：`go test ./internal/doh/ -run TestRenewDelay` 及新增的重試時序測試全綠。
- [x] 2.6 完成設定到執行期的接線並逐項對齊 design 的「執行期行為」契約：DoH server 建立 `certManager` 時帶入設定的初始延遲，使 `doh.acme.initial_delay` 為正值時第一次取得確實被延後；兩個 listener 的啟動與綁定流程逐字不變，listener 啟動與等待併行進行、互不阻擋；等待期間尚無憑證時的 TLS handshake 行為維持現況（以錯誤結束）。驗證：`go test -race ./internal/doh/ ./cmd/shadowdns/` 全綠。注意 `make smoke` 不覆蓋這裡：`scripts/smoke.sh` 內嵌產生的 `shadowdns.yaml` 只有 `aliases:` 區段、沒有 `doh:`，而 `scripts/` 不在本變更的 Impact 範圍內。改為手動以 `bin/shadowdns-$(go env GOOS)-$(go env GOARCH) --named-conf <fixture> --config <含 doh.acme.initial_delay 的暫時設定檔> --dry-run` 確認載入成功；另跑一次 `make smoke` 確認未設定該欄位的既有路徑無回歸。

## 3. 守恆檢查：不改動的面向

- [x] 3.1 依 design 的「不新增指標且不設上限值，reload 路徑不改動」確認三件事：SIGHUP 的 DoH 設定漂移比較未被修改，且因採整個結構值比較而自動涵蓋新欄位（改動 `initial_delay` 後 reload 會輸出既有的「需重啟才生效」提示）；未新增任何 Prometheus 指標；未加入上限值檢查。驗證：`go test ./cmd/shadowdns/ -run Reload` 全綠，並以 `git diff --stat` 確認 `internal/metrics/` 與漂移比較函式所在檔案未被改動或僅有無關改動。

## 4. 手冊與範例設定（中英雙語同步）

- [x] 4.1 [P] 設定參考文件呈現新欄位：在 `docs/configuration/shadowdns-yaml.md` 與 `docs/configuration/shadowdns-yaml.zh.md` 的 doh 欄位表新增 `acme.initial_delay` 列（Required 欄為否）、於範例設定加入該欄位，並改寫「所有欄位皆為必填」的敘述以反映這是 `doh.acme` 底下第一個選填欄位；同時說明零值預設、負值與無法解析會導致載入失敗、以及不設上限。驗證：內容審閱確認兩語版本欄位表與敘述一致，且中文版連結指向基準 `.md` 路徑。
- [x] 4.2 [P] DoH 指南說明使用時機：在 `docs/guides/doh.md` 與 `docs/guides/doh.zh.md` 加入一段說明 `initial_delay` 是啟動傳播窗口——用於路由資料平面尚未收斂的部署，只延後首次發憑證、不影響重試與續期、期間 DoH handshake 會失敗、屬 restart-only 設定。驗證：內容審閱確認兩語版本語義一致且未出現真實網域或非 RFC 5737 的 IP。
- [x] 4.3 [P] 套件範例設定同步：在 `packaging/shadowdns.yaml.example` 的 doh 註解區補上選填欄位說明，並在被註解的範例區塊加入 `initial_delay`。注意該註解區現有的清單標題是 `# Required fields:`（約在檔案第 98 行），不可把選填欄位塞進該清單——另起一段 `# Optional fields:` 承載 `acme.initial_delay`。驗證：內容審閱確認與設定參考文件的欄位語意一致，且 required / optional 兩段分別列出的欄位與程式碼一致。
- [x] 4.4 手冊建置在嚴格模式下通過：驗證 `make docs-build` 成功（警告即失敗），確認導覽與連結無破損。

## 5. 收尾驗證

- [x] 5.1 全套件驗證通過：`make test`（含 race detector）與 `make lint` 皆無失敗。
- [x] 5.2 對照 design 的「驗收條件」與「範圍邊界」逐項核對，確認實作未觸及 Non-Goals 所列項目（listener bind-ready 訊號、上限值、新指標、SIGHUP 套用語意、共享 challenge 儲存），且 Goals 所列四項皆有對應的通過測試或文件證據。驗證：以 `git diff --name-only` 比對實際改動檔案清單與 proposal 的 Impact 一致。
- [x] 5.3 (User) 在 post-implementation review chain 結束後，依 CLAUDE.local.md 執行 Perf-Guard ns1→ns2 benchmark：本變更改動 `internal/doh/acme.go`、`internal/doh/server.go`、`internal/shadowdnscfg/config.go`，皆落在「Must run」清單（`internal/**/*.go`，排除 `*_test.go`）。預期效能中性——新增的等待只在啟動時執行一次、不在 DNS query hot path 上——但仍須依規則實測確認無回歸，才交付使用者做 commit 決定。
