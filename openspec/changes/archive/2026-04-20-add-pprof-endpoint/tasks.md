## 1. Flag 與 opts 擴充

- [x] 1.1 於 `cmd/shadowdns/main.go` 的 CLI opts struct（約 L155）新增 `PProfEnable bool` 欄位，緊接於 `MetricsAddr` 之後
- [x] 1.2 於 flag 註冊區塊（約 L229，緊接 `-metrics-addr` 之後）加入 `flag.BoolVar(&opts.PProfEnable, "pprof-enable", false, "Expose Go pprof profiling endpoints on the metrics HTTP server under /debug/pprof/ (default disabled)")`

## 2. 啟動期衝突驗證

- [x] 2.1 新增啟動期參數驗證：當 `opts.PProfEnable == true` 且 `opts.MetricsAddr == ""` 時，呼叫 `logger.Sugar().Fatalw(...)` 輸出明確錯誤訊息並以非零 exit code 離開，涵蓋規格中「Conflicting flags produce fatal startup error」場景

## 3. pprof handler 掛載（主實作）

- [x] 3.1 在 metrics mux 建立區塊（約 L403）實作條件式掛載：當 `opts.PProfEnable && opts.MetricsAddr != ""` 時，以手動逐條註冊方式將 `net/http/pprof` 的 `pprof.Index`、`pprof.Cmdline`、`pprof.Profile`、`pprof.Symbol`、`pprof.Trace` 掛到 mux 的 `/debug/pprof/` 前綴，並用 `pprof.Handler(name)` 掛載 `heap`、`goroutine`、`allocs`、`threadcreate`、`block`、`mutex` 共六條 named profile routes
- [x] 3.2 確認整支 binary 未使用 `_ "net/http/pprof"` blank import（grep `net/http/pprof` 應僅出現在 main.go 的一般 import 中），對應規格「DefaultServeMux is not polluted」場景
- [x] 3.3 確認未呼叫 `runtime.SetBlockProfileRate` 或 `runtime.SetMutexProfileFraction`，對應規格「Block and mutex profiles return empty by default」場景

## 4. Integration tests

- [x] 4.1 於 `test/integration/` 新增 `pprof_test.go`，涵蓋 Requirement「Expose pprof profiling endpoints (opt-in)」的全部場景
- [x] 4.2 撰寫 test case「pprof disabled by default」：啟動不帶 `-pprof-enable` 的實例，GET `/debug/pprof/` 期望 HTTP 404、GET `/metrics` 期望 HTTP 200
- [x] 4.3 撰寫 test case「pprof enabled via flag」：啟動帶 `-pprof-enable` 的實例，GET `/debug/pprof/` 期望 HTTP 200 且回應內含 pprof index HTML、GET `/debug/pprof/heap` 期望回傳非空 body 且 Content-Type 屬於 pprof binary、GET `/debug/pprof/goroutine?debug=1` 期望回傳文字格式 goroutine dump
- [x] 4.4 撰寫 test case「Conflicting flags produce fatal startup error」：以 `-pprof-enable` 搭配 `-metrics-addr ""` 啟動子行程，期望非零 exit code 且 stderr 含明確衝突訊息
- [x] 4.5 撰寫 test case「DefaultServeMux is not polluted」：啟動帶 `-pprof-enable` 的實例後，於測試行程內（不同 port）起一個臨時 `http.Server` 使用 `http.DefaultServeMux`，GET `/debug/pprof/` 期望 HTTP 404
- [x] 4.6 撰寫 test case「Block and mutex profiles return empty by default」：啟動帶 `-pprof-enable` 的實例，GET `/debug/pprof/block?debug=1` 與 `/debug/pprof/mutex?debug=1` 期望 HTTP 200 且回應樣本計數為 0

## 5. 文件與追蹤

- [x] [P] 5.1 於 `README.md` 的 CLI flag / Operations 說明中補充 `-pprof-enable` 行為、預設值與「需搭配 metrics server 啟用」的限制，並加上安全警告（僅在受信任網路或 localhost bind 情境啟用）
- [x] [P] 5.2 於 `openspec/specs/prometheus-metrics/spec.md`（archive 時同步後的主 spec）更新 `@trace` 區塊，將新增的 `cmd/shadowdns/main.go` pprof 掛載程式碼與新測試檔案 `test/integration/pprof_test.go` 加入 code/tests 清單
- [x] [P] 5.3 於 `CHANGELOG.md` 下一個未發佈版本新增條目：「Add opt-in `-pprof-enable` flag that exposes Go pprof endpoints on the metrics HTTP server under `/debug/pprof/` (disabled by default).」

## 6. 驗證

- [x] [P] 6.1 執行 `make lint` 確認無 lint 錯誤
- [x] [P] 6.2 執行 `make test` 確認全部單元測試通過
- [x] [P] 6.3 執行 `make smoke` 確認 dry-run 啟動成功
- [x] 6.4 手動驗證：`./bin/shadowdns -named-conf <fixture> -pprof-enable` 啟動後 `curl http://127.0.0.1:9153/debug/pprof/heap -o /tmp/heap.pb.gz` 取得非空 profile，再以 `go tool pprof /tmp/heap.pb.gz` 開啟確認檔案有效
