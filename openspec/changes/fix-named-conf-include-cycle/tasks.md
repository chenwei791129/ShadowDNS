## 1. 回歸測試（TDD，先寫）

- [x] 1.1 新增 `internal/config/include_cycle_test.go`：建立 (a) 自我 include 的 fixture 與 (b) A↔B 互相 include 的 fixture，分別驅動 `LoadNamedConf`，斷言回傳的 error 訊息含 "cycle" 與 offending path，且測試 process 不發生 stack overflow / crash；另加一個合法（acyclic）但同一檔案從兩個分支被 include 的 fixture，斷言不被誤判為 cycle。此覆蓋需求 "Detect and reject named.conf include cycles"。驗證：`go test ./internal/config/ -run IncludeCycle` 在修補前因 stack overflow 而失敗。

## 2. 修補 include 迴圈偵測

- [x] 2.1 在 `internal/config/zones.go`：將一組「目前位於 active include chain 的已訪問絕對路徑集合」thread 進 `loadFile`（修改 `loadFile` 簽章或以輔助結構攜帶；`LoadNamedConf` 初始化該集合）。在 `case "include"` 跟進前，以 `filepath.Abs` 解析 includePath，若已在 active chain 中則回傳 `fmt.Errorf("...cycle detected at %s...", includePath)` 並停止遞迴；正常跟進後於遞迴返回時將該路徑移出 active chain，確保同一檔案從不同分支合法 include 不被誤判。觀察結果：cyclic include 回 error、不 crash；acyclic 行為不變。此覆蓋需求 "Detect and reject named.conf include cycles"。驗證：步驟 1.1 測試全綠。

## 3. 驗證與回歸

- [x] 3.1 確認既有 named.conf 載入、include 順序、路徑解析、reload 行為對合法設定不變。驗證：`go test -race ./internal/config/ ./internal/server/ ./test/integration/ -run 'Config|Load|Include|Reload'` 全數通過。
- [x] 3.2 整體品質閘通過。驗證：`make lint` 與 `make test` 皆 exit 0。
