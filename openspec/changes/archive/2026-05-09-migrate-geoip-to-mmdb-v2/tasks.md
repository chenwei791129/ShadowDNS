# Tasks

## 1. 升級 dependency 到 v2 direct require

- [x] 1.1 把 `github.com/oschwald/maxminddb-golang/v2` 從 indirect 升為 direct require。
  - 行為合約：`go.mod` 直接 require 區段含 `github.com/oschwald/maxminddb-golang/v2 v2.1.1`（無 `// indirect` 註解）；v1 entry 暫時保留（task 5 才移除，因為 task 2/3 還沒改完前 build 仍依賴 v1）。
  - 執行：`go get github.com/oschwald/maxminddb-golang/v2`
  - 驗證：`grep -n 'oschwald/maxminddb-golang/v2' go.mod` 顯示一行不含 `// indirect`；`go build ./...` 成功（v1 仍存在不衝突）。

## 2. 遷移 internal/view/geoip_country.go 到 v2 API

- [x] 2.1 [P] 修改 `(*CountryDB).Lookup` 內部實作，使用 v2 `Result.DecodePath(&iso, "country", "iso_code")`，並接受 `netip.Addr` 直接查詢（消除 `ip.AsSlice()`）。
  - 行為合約：相同 IP 輸入下，回傳 `(string, bool)` 與 v1 路徑 byte-equivalent — ISO code 命中時回 mmdb 字面值（含大小寫）+ true，未命中或空字串回 `("", false)`，nil 接收者回 `("", false)`，lookup error 一律 swallow 為 `("", false)`。
  - 修改範圍：`internal/view/geoip_country.go` 的 `import` 從 `"github.com/oschwald/maxminddb-golang"` 改為 `"github.com/oschwald/maxminddb-golang/v2"`；`Lookup` 函式 body；`Metadata()` 回傳型別從 `maxminddb.Metadata` 改為 v2 同名型別（欄位 `DatabaseType`、`BuildEpoch` 名稱與型別在 v2 不變）；`OpenCountryDB` 與 `Close` 使用 v2 `maxminddb.Open`。
  - 失敗模式：保留 v1 audit 行為 — `result.Err()` 與 `DecodePath` 的 error 一律視為 no-match，不 panic、不 propagate。
  - 驗證：`go test -race -count=1 ./internal/view/... -run 'TestCountryDB|TestOpenCountryDB'` 全綠（4 個既有測試：`TestCountryDB_Lookup`、`TestOpenCountryDB_MissingFile`、`TestCountryDB_Metadata_ReturnsMetadata`、`TestCountryDB_Metadata_NilReceiver`）。

## 3. 遷移 internal/view/geoip_asn.go 到 v2 API

- [x] 3.1 [P] 修改 `(*ASNDB).Lookup` 內部實作，使用 v2 `Result.DecodePath(&asn, "autonomous_system_number")`，並接受 `netip.Addr` 直接查詢。
  - 行為合約：相同 IP 輸入下，回傳 `(uint32, bool)` 與 v1 路徑 byte-equivalent — ASN 命中時回 `(uint32(<asn>), true)`，未命中、ASN 為 0 或 nil 接收者回 `(0, false)`，lookup error 一律 swallow 為 `(0, false)`。注意 v2 `DecodePath` 的 destination 為 `var asn uint`，最後 cast 為 `uint32`（與 v1 一致）。
  - 修改範圍：`internal/view/geoip_asn.go` 的 `import`、`Lookup` 函式 body、`Metadata()` 回傳型別、`OpenASNDB` 與 `Close` 全部換到 v2，與 task 2.1 對稱。
  - 驗證：`go test -race -count=1 ./internal/view/... -run 'TestASNDB|TestOpenASNDB'` 全綠（既有 ASN 測試套件）。

## 4. 新增 alloc/op benchmark

- [x] 4.1 [P] 在 `internal/view/geoip_country_test.go` 新增 `BenchmarkCountryDB_Lookup`，量化 alloc/op。
  - 行為合約：benchmark 對命中 IP（與既有 `TestCountryDB_Lookup` 同一個 fixture IP）跑 `b.N` 次 `Lookup`，呼叫 `b.ReportAllocs()`；輸出包含 `B/op` 與 `allocs/op` 欄位。
  - 驗證：`go test -bench=BenchmarkCountryDB_Lookup -benchmem -count=3 ./internal/view/...` 跑完無錯，記下 `allocs/op` 與 `ns/op`（應顯著低於 v1 路徑的 baseline，但不在 task 內 hard-assert 數值）。

- [x] 4.2 [P] 在 `internal/view/geoip_asn_test.go` 新增 `BenchmarkASNDB_Lookup`，與 4.1 對稱。
  - 行為合約：與 4.1 相同，但測 `ASNDB.Lookup`，使用 ASN fixture IP。
  - 驗證：`go test -bench=BenchmarkASNDB_Lookup -benchmem -count=3 ./internal/view/...` 跑完無錯。

## 5. 移除 v1 dependency 並執行 go mod tidy

- [x] 5.1 從 `go.mod` 移除 `github.com/oschwald/maxminddb-golang v1.13.1` direct require entry。
  - 行為合約：`go.mod` direct require 區段不再含 v1；若有 transitive dependency 需要 v1，`go mod tidy` 會自動把 v1 標為 indirect。
  - 執行：手動編輯 `go.mod` 刪除 line 10（`github.com/oschwald/maxminddb-golang v1.13.1`），然後 `go mod tidy`。
  - 驗證：(1) `grep -E '"github.com/oschwald/maxminddb-golang"' internal/ cmd/ -r` 無輸出（first-party code 無 v1 import path）；(2) `go list -deps ./... | grep 'oschwald/maxminddb-golang$'` 回傳空（無任何 transitive 需要 v1，或顯示存在但僅 indirect）；(3) `go build ./...` 成功。

## 6. 完整測試與靜態檢查

- [x] 6.1 跑 `make test`、`make lint`、`make smoke` 並全綠。
  - 行為合約：所有單元測試（含 race detector + count=1）通過；`golangci-lint run` 無 finding；`shadowdns --dry-run` 載入既有測試 config 無 error。
  - 驗證：三個 make target 各自 exit code 0；輸出無 FAIL/ERROR 字樣。

## 7. 本地 build deb 並部署到 bench-ns2

- [x] 7.1 用 release-shadowdns skill local-change mode build 並部署。
  - 行為合約：`shadowdns_0.0.0~migrate-geoip-to-mmdb-v2_amd64.deb` 安裝在 bench-ns2，`dpkg -l shadowdns` 顯示 `0.0.0~migrate-geoip-to-mmdb-v2`；`systemctl is-active shadowdns` 回 `active`；`/var/log/shadowdns/shadowdns.log` 在 30s 觀察窗內無 error/fatal/panic 關鍵字。
  - 執行：依 `release-shadowdns` skill 流程（`make deb` with `VERSION=0.0.0-migrate-geoip-to-mmdb-v2` → `scp` → `dpkg -i` → `systemctl restart` → `restart_and_watch.sh`）。
  - 驗證：skill 的 `restart_and_watch.sh` exit code 0；ssh 上 `dpkg -l shadowdns` 顯示新版本字串。

## 8. dnspyre 壓測 + pprof 比對

- [x] 8.1 從 bench-ns1 跑 dnspyre 與 30s pprof，產出比較報告。
  - 行為合約：產生比較報告 `.local/dnspyre/report/compare-baseline-recheck-vs-migrate-geoip-to-mmdb-v2.md`，含 (1) QPS 對照表（baseline-recheck 30,833 vs 本次）、(2) pprof top 10 對照、(3) `view.(*CountryDB).Lookup` 的 callees 對照（特別是 `runtime.newobject` 在 Lookup 路徑下的 cum%）、(4) 達標判定（QPS +5~8%、CountryDB.Lookup cum < 7%、`runtime.newobject` cum < 6%）。
  - 執行：`ssh bench-ns1 "dnspyre -s 198.18.0.8 -t A -d 3m -c 100 --no-distribution @/tmp/cname-domains.txt > /tmp/dnspyre-cname-<TS>.txt 2>&1"` 配 `ssh bench-ns2 "curl -s -o /tmp/cpu-after-<TS>.pprof http://127.0.0.1:9153/debug/pprof/profile?seconds=30"`；報告依 `local-dnspyre-benchmark` skill 模板。
  - 驗證：報告檔案存在；`go tool pprof -base <baseline-recheck> <new>` 顯示 `view.(*CountryDB).Lookup` cum 下降；報告明確標示是否達標。

- [x] 8.2 根據 8.1 結果更新 plan §1.6 的優先級表中 #1 行的「實測收益」欄位（若 plan 已合併或未標記，加一個 `2026-MM-DD migrate-geoip-to-mmdb-v2 實測` 註腳記錄結果）。
  - 行為合約：`.local/plans/2026-05-06-qps-regression-vs-bind9-fix.md` §1.6 優先級表 #1 行附上實測 QPS Δ% 與 pprof 數據，使下次重排優先級時有對照基準。
  - 驗證：grep 該檔案能找到 `migrate-geoip-to-mmdb-v2` 字串與本次實測結果。
