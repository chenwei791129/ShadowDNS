## 1. 設定載入：collapse_cname_chain 欄位與查表

- [x] 1.1 在 `internal/config/aliases_test.go` 新增失敗測試，依 spec「Validate aliases section」驗證 `BuildAliasMap` 輸出 collapse 查表：key 為 root origin 的 lookup-fold FQDN（混大小寫 root key `Root.COM` 正規化為 `root.com.`）、值為群組的 `collapse_cname_chain`、未宣告時查無 entry。驗證：新測試先紅。
- [x] 1.2 依 design「D7: 設定載入 — `collapse_cname_chain` 欄位與既有驗證鏈整合」實作 `config.AliasGroup` 新欄位 `CollapseCNAMEChain` 與 `BuildAliasMap` 第三個回傳值——具名型別 `config.CollapseFlags`（`map[string]bool`，root-fold key，與 `AliasFlags` 並列宣告、missing-key-即-false 語意文件掛在型別上）。驗證：1.1 測試轉綠，`go test ./internal/config/` 全綠。
- [x] 1.3 在 `internal/shadowdnscfg/config_test.go` 新增失敗測試，依 spec「Load unified ShadowDNS configuration from a YAML file」驗證：`collapse_cname_chain: true` 可解析、省略時預設 false、未知欄位錯誤訊息列出 `members, rewrite_rdata_labels, collapse_cname_chain` 三個合法欄位。驗證：新測試先紅。
- [x] 1.4 實作 `rawAliasGroup` 的 `collapse_cname_chain` yaml 欄位、`UnmarshalYAML` allowed-keys 與錯誤訊息更新、`shadowdnscfg.Config` 新增 `CollapseFlags` 欄位並於 `Load` 填入。驗證：1.3 測試轉綠，`go test ./internal/shadowdnscfg/` 全綠。

## 2. zone 層收合追蹤

- [x] 2.1 [P] 在 `internal/zone/collapse_test.go` 新增失敗測試，涵蓋 spec「Collapse an in-zone CNAME chain to its terminal records」與「Synthesize a single CNAME when the chain leaves the zone」的追蹤語意：三種 outcome（Records / Tail / NoData）、MinTTL 取被消耗鏈含最終記錄的最小值（300/60/600 → 60）、出境 target 保留 zone 檔原始 case、wildcard 中間跳點可續追、qtype=CNAME 時 CNAME 一律視為跳點（outcome 僅 Tail / NoData）、深度預算邊界依 design「D3: 深度上限耗盡視同出境（`CollapseTail`）」（恰 8 條 CNAME 收尾於 A → Records；9 條 → Tail 且 target 為第 8 條的 target；迴圈同樣於預算耗盡時回 Tail，含 cutoff target 等於 qname 的自指形態）、鏈尾被 wildcard 覆蓋但 wildcard 無該 qtype 也無 CNAME → NoData（RFC 4592 wildcard-NODATA 鏈尾）、鏈尾 dangling（zone 內不存在）→ NoData、回傳 RRs 為 zone 存放切片且未被就地修改、任何輸入不 panic；另加 `FollowCNAME` ↔ `CollapseCNAME` 終點一致性 parity 測試（**僅鏈長 ≤ 7** 且於 zone 內收尾的鏈，flag-on terminal == flag-off 鏈尾記錄；鏈長 ≥ 8 因 `FollowCNAME` 迭代預算不解析 terminal，排除於 parity 之外，見 D3）。驗證：新測試先紅。
- [x] 2.2 依 design「D2: zone 層新增獨立收合追蹤函式，`FollowCNAME` 熱路徑零修改」於 `internal/zone/collapse.go` 實作 `func (z *Zone) CollapseCNAME(initial []dns.RR, qtype uint16) CollapseResult`（`*Zone` method；逐跳規則與 `FollowCNAME` 相同：exact qtype → exact CNAME → wildcard qtype → wildcard CNAME，上限 `MaxCNAMEDepth`；qtype=CNAME 時跳過 qtype 查詢步驟），`FollowCNAME` 本體零修改。驗證：2.1 測試轉綠，`go test -race ./internal/zone/` 全綠。

## 3. ServerState 接線

- [x] 3.1 依 design「D1: 收合查表以 root origin 為 key，handler 以 `match.RootZone` 查表」將 collapse 查表接入 runtime state：`server.ServerState` 新增 `CollapseFlags` 欄位、`BuildState` 簽章增加對應參數並寫入 state、production 呼叫點 `cmd/shadowdns/main.go`（啟動與 SIGHUP reload 兩處）同步傳遞，並機械同步所有直接呼叫 `BuildState` 的測試檔簽章（`internal/server/server_test.go`、`internal/server/build_test.go`、`cmd/shadowdns/main_test.go`、`test/integration/helpers_test.go`、`test/integration/reload_diff_test.go`、`test/integration/axfr_test.go`、`test/integration/listenon_test.go`、`test/integration/case_preservation_test.go`；除呼叫形狀外斷言零修改）。行為契約：reload 後新查表隨 state snapshot 原子生效。驗證：`internal/server/build_test.go` 新增 state 欄位斷言、`go build ./...` 與既有測試全綠。

## 4. root 查詢路徑收合

- [x] 4.1 在 `internal/server/handler_test.go` 新增失敗測試，依 design「D6: root 路徑的四個收合接入點」涵蓋：(a) 多跳鏈收合為單條最終記錄（owner=on-wire qname、TTL=60）；(b) 出境鏈收合為單條合成 CNAME 且中間名稱不出現於回應；(c) spec「Respond NODATA when the chain ends in-zone without the requested type」— AAAA 查 A-only 鏈尾回 NOERROR+SOA 且存在 `*.example.com AAAA` wildcard 時仍不被諮詢；(d) spec「Direct CNAME-type queries follow the unified collapse rule」— 直查 CNAME 出境回合成 CNAME、境內走到底回 NODATA；(e) 中間名稱直查同樣收合；(f) wildcard CNAME 起點收合。驗證：新測試先紅。
- [x] 4.2 依 design「D5: 回應組裝 — owner、case、TTL 與 backup 改寫的組合」與「D4: 收合的 NODATA 必須短路，不得 fall through 到 wildcard 合成」實作 `handleRootQuery` 的四個收合接入點，全部路由到單一收合 helper：flag 讀取（`match.RootZone` 查 `CollapseFlags`）只發生在接入點內部（flag off 部署與未命中 CNAME 的查詢零新增 map 讀取）；wildcard 起點直接餵原始 wildcard CNAME 切片（不先 `rewriteWildcardOwner` 預複製）；Records → 每筆恰一次 `dns.Copy`（owner=qnameOrig 與 TTL 覆寫皆寫在副本）、Tail → 合成 `dns.CNAME`、NoData → 直接 `negativeReply`；ephemeral TXT overlay 順序不變。驗證：4.1 測試轉綠，`go test -race ./internal/server/` 全綠。

## 5. backup 查詢路徑收合

- [x] 5.1 在 `internal/alias/override_test.go` 新增失敗測試：collapse 專用 backup 解析入口（exact / CNAME fallback / wildcard 三階段，既有 `Resolve*` 簽章不變）的行為——backup namespace owner 保留 on-wire case 且每筆恰一次 `dns.Copy`（TTL/owner 寫在副本，zone 存放切片不可變）、最終記錄 RDATA 經 RDATA-only 改寫 primitive 套 in-bailiwick / `rewrite_rdata_labels` 規則（行為與 `RewriteRR` 的 RDATA 分支一致）、合成 CNAME 的出境 target 在 `rewrite_rdata_labels: true` 時套 label-anywhere 改寫（templated CNAME 場景）且 false 時原樣輸出、NODATA 以 `nodata=true` 表達並滿足不變式 `nodata=true ⇒ len(rrs)==0`。驗證：新測試先紅。
- [x] 5.2 實作 `internal/alias/override.go` 的 collapse 專用解析入口與 `internal/alias/rewrite.go` 的 RDATA-only 改寫 primitive（自 `RewriteRR` 抽出共用 type-switch，`RewriteRR` 改為呼叫同一 primitive、外部行為不變），及 `handleBackupQuery` 在 flag on 時各階段改走 collapse 入口、對 `nodata=true` 的短路（依 design「D4: 收合的 NODATA 必須短路，不得 fall through 到 wildcard 合成」直接 `negativeReply`）。驗證：5.1 測試轉綠，`go test -race ./internal/alias/ ./internal/server/` 全綠。

## 6. 整合測試與範例設定

- [x] 6.1 新增 `test/integration/cname_collapse_test.go`，端到端重現 design Implementation Contract 表的六種查詢形態（root A、backup A、AAAA NODATA、直查 CNAME、出境合成 CNAME、中間名稱直查）。測試自建獨立的 named.conf / zone / `shadowdns.yaml` fixture（比照 `test/integration/case_preservation_test.go` 的自備 fixture 模式），不修改共用 `testdata/integration/` fixture——避免牽動既有測試的回應斷言。並驗證 spec「Zone transfers are never collapsed」（flag on 時 AXFR 仍含原始 `www.example.com. 300 CNAME lb.example.com.`）與 spec「Collapse is a per-alias-group opt-in that defaults to off」（未宣告 flag 的 group 仍回完整 CNAME 鏈；byte-level 不變的機械證明由 7.1 既有測試零修改承擔）。驗證：`go test ./test/integration/` 全綠。
- [x] 6.2 [P] 更新 `packaging/shadowdns.yaml.example`：alias group 註解加入 `collapse_cname_chain`（預設 false、行為摘要、與 `rewrite_rdata_labels` 並列）。驗證：內容審閱——欄位名與 spec 一致、範例僅使用 RFC 2606 保留網域。

## 7. 全量驗證

- [x] 7.1 全量回歸：`make test`（race detector）與 `make lint` 全綠；確認既有測試檔除呼叫簽章調整外斷言零修改（flag off 行為不變的機械證明，呼應 spec「Collapse is a per-alias-group opt-in that defaults to off」）。驗證：兩個 make 目標 exit 0。
- [x] 7.2 依 CLAUDE.local.md Perf-Guard 流程執行效能基準對照（baseline → 部署 → post-change，CNAME 與 A 兩清單，QPS -5% / p99 +15% 門檻），產出 `perfguard-add-cname-chain-collapsing-<TS>.md` 報告。驗證：報告判定 PASS（REGRESSION 則停下交使用者裁決）。
- [x] 7.3 請使用者在 ns2 以 dig 實測收合行為（flag on 的 root/backup/出境/NODATA 四種形態）並確認回應符合預期後，決定是否 commit。
