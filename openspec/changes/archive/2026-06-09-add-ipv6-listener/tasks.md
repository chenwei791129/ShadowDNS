## 1. IPv6 位址解析核心（internal/server/listenaddr.go）

- [x] 1.1 新增 `expandAnyIPv6() ([]string, error)`：經由可注入的 `ifaceAddrs` 列舉本機位址，僅納入 `To4()==nil && To16()!=nil` 的 IPv6 位址，過濾 link-local `fe80::/10`，保留 loopback `::1`。驗證：新增單元測試以 fixture（含 `2001:db8::1`、`fe80::1`、`::1`）驗證輸出為 `{2001:db8::1, ::1}`。（設計決策：expandAnyIPv6 的列舉與過濾規則）
- [x] 1.2 擴充 token 解析支援 IPv6 family：`any` 呼叫 `expandAnyIPv6`、`none` 維持空集合並記錄 `noneExplicit`、IPv6 literal（`ParseIP` 且 `To4()==nil`）原樣採用；非 IPv6 token（含 IPv4 literal、`!addr`、ACL 名稱）記 WARN 跳過且不致命。驗證：單元測試涵蓋 `listen-on-v6 { 10.0.0.1; 2001:db8::1; }` 時 IPv4 literal 被 WARN 跳過、僅回 `2001:db8::1`。
- [x] 1.3 修改 `ResolveListenAddresses` 簽章為 `(listenFlag string, listenOn []string, listenOnV6 []string, logger *zap.Logger)`：Precedence 1（明確 host，含 IPv6 bracket literal）仍只回 `{listenFlag}` 並忽略兩個 block；無 host 時回傳「v4 解析集合 ++ v6 解析集合」，v6 位址以 `net.JoinHostPort` 產生 bracket 形式，v4 在前 v6 在後。驗證：單元測試驗證 `--listen :53` + `listen-on { 10.0.0.1; }` + `listen-on-v6 { 2001:db8::1; }` 回傳 `{10.0.0.1:53, [2001:db8::1]:53}`，以及 `--listen [::1]:5353` 回傳 `{[::1]:5353}`。同步更新所有直接呼叫端使其編譯通過：`cmd/shadowdns/main.go`（兩處）、`internal/server/listenaddr_test.go`、`test/integration/listenon_test.go`。（實作 spec requirement「Derive listen address set from named.conf listen-on」；設計決策：ResolveListenAddresses 並聯 IPv4 與 IPv6 集合）
- [x] 1.4 調整空集合錯誤語意：僅當 v4∪v6 皆空才回傳啟動錯誤；單一 family 為空（`listen-on-v6 { none; }` 或缺省）而另一 family 非空時正常回傳。v6 缺省解析為空集合（不隱含 `any`）。驗證：單元測試驗證 `listen-on { 10.0.0.1; }` + `listen-on-v6 { none; }` 正常回傳 `{10.0.0.1:53}`，且 `listen-on { none; }` + 無 v6 仍回傳 fatal 錯誤。

## 2. 啟動與 reload 接線（cmd/shadowdns/main.go）

- [x] 2.1 啟動綁定路徑的 `ResolveListenAddresses` 呼叫改傳 `cfg.Options.ListenOnV6`，使解析出的 v6 位址經 `BindMany` 以 bracket 形式各自綁定 UDP+TCP listener。為讓 `--dry-run` 能在不綁定任何 listener 的前提下預覽實際 bind 集合，將該解析上移至 dry-run early-return 之前一次性完成（綁定路徑重用同一結果，不重複解析），並於 dry-run 摘要新增 `listen_addrs` 欄位；`scripts/smoke.sh` 注入 `listen-on-v6 { ::1; }`（不動共用整合 fixture）。驗證：`make smoke` 以含 `listen-on-v6 { ::1; }` 的 named.conf 執行 `--dry-run`，摘要 `listen_addrs` 列出 `[::1]:53`。（設計決策：分離 socket 逐位址綁定，而非 dual-stack wildcard；dry-run 預覽 bind 集合）
- [x] 2.2 reload drift 偵測路徑的 `ResolveListenAddresses` 呼叫改傳 `cfg.Options.ListenOnV6`，使 `AddrSetEqual` 比對涵蓋 v4∪v6；v6 介面位址變動時記 INFO「listen-address changes require restart」且不重新綁定。驗證：於 `cmd/shadowdns/listenon_test.go` 新增 reload drift v6 案例 `TestRun_ReloadLogsListenAddrChangeHint_IPv6`。注意：spec scenario 以 `2001:db8::1 → 2001:db8::2` 表述 drift，但這兩個 documentation-prefix 位址非本機介面位址、startup bind 必失敗，故為走完整 bind→reload→drift 路徑，啟動側改用恆可綁定的 loopback `::1`（斷言 startup 成功 bind `[::1]`），reload 後將 `listen-on-v6` 改為 `2001:db8::1`（drift 偵測只需解析、不需 bind）；斷言記錄 restart hint INFO，且無任何 `listener bound` 指向新 v6 位址（未重新綁定）。（設計決策：SIGHUP reload drift 偵測納入 IPv6）
- [x] 2.3 更新 `--listen` flag 的 help text，說明明確 host 可為 IPv6 bracket literal、`:port` 形式並聯 v4 與 v6。驗證：內容審查確認 help text 提及 IPv6；`go run ./cmd/shadowdns --help` 輸出包含 v6 說明。（設計決策：--listen 的 IPv6 並聯與單一位址逃生艙語意）

## 3. 文件

- [x] 3.1 [P] 更新 README：將 IPv6 listener 由 Planned 區段移至 Supported 區段、Feature comparison 表格 ShadowDNS 欄由 `Planned` 改為 `Yes`，並補充 `listen-on-v6` token（`any`/`none`/IPv6 literal）與分離 socket、link-local 過濾的支援說明。驗證：內容審查確認 README 不再將 IPv6 listener 列為 Planned，且 commit 前以 grep 確認無非 RFC 2606 內部網域洩漏。
- [x] 3.2 [P] 更新 `docs/migration.md` 的 listen 綁定章節：在 `--listen`/`listen-on` 優先序表格與說明中納入 `listen-on-v6`（v4∪v6 並聯、`--listen` host 可為 IPv6 bracket literal），並更新「不支援的 listen-on 語法」段落以反映 IPv6 literal 現於 `listen-on-v6` 受支援。驗證：內容審查確認 migration.md 與 README、spec requirement 描述一致，且 commit 前 grep 確認無非 RFC 2606 內部網域洩漏。

## 4. 驗證

- [x] 4.1 全測試套件通過：`make test`（race detector）與 `make lint` 皆無錯誤；新增/改寫的 `internal/server/listenaddr_test.go` 案例（v6 any 過濾、v6 literal、v4∪v6 並聯順序、`--listen [::1]` 逃生艙、v6-only-none 共存）全數通過。
