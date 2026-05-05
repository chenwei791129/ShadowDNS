## 1. Load unified ShadowDNS configuration from a YAML file

- [x] 1.1 修改 `internal/shadowdnscfg/config.go` 的 `rawAliasGroup.UnmarshalYAML`：移除 `case yaml.SequenceNode` 分支，將 default 錯誤訊息更新為「aliases entry must be an object with 'members' (non-empty list of backup domains) and optional 'rewrite_rdata_labels' (bool)」，使 sequence node 也走進 type-mismatch 路徑（對應 spec scenario「Sequence form aliases value is rejected」）
- [x] 1.2 確認 `rawAliasGroup` 的 mapping 分支對 `members: []`（空列表）仍回傳「requires non-empty 'members' list」錯誤（既有邏輯，無需改動，僅驗證；對應 spec scenario「Aliases object form with empty members is rejected」）
- [x] 1.3 移除 `internal/shadowdnscfg/config.go:60-62` docstring 中關於「sequence equivalent to rewrite_rdata_labels=false」的說明，改為敘述只接受 mapping form

## 2. Validate aliases section

- [x] 2.1 確認 `internal/config/aliases.go` 的 `BuildAliasMap` 對 self-alias、cross-root duplicate backup、空白 domain 等三個拒絕條件保持不變（既有邏輯，無需改動，僅驗證對應 spec scenarios「Duplicate backup under different roots fails」「Self-alias entry fails」「Multiple backups under one root are all mapped to that root」仍通過）

## 3. 更新單元測試對應新 schema

- [x] 3.1 [P] 修改 `internal/shadowdnscfg/config_test.go`：把所有用 sequence form 的 fixture（如 `root.com: [backup.com]`）改為 mapping form
- [x] 3.2 [P] 在 `internal/shadowdnscfg/config_test.go` 新增測試 case：sequence form input 應被拒絕並回傳 type-mismatch 錯誤，錯誤訊息包含 `members` 字樣
- [x] 3.3 [P] 在 `internal/shadowdnscfg/config_test.go` 確認既有「mapping form missing members」「mapping form empty members」「mapping form unknown field」三個負面測試仍存在且通過

## 4. 更新測試與文件中的 fixture

- [x] 4.1 [P] 改寫 `testdata/integration/shadowdns.yaml`：把所有 sequence form 條目改為 mapping form（保留原 `rewrite_rdata_labels` 值，未指定者顯式寫 `rewrite_rdata_labels: false` 或省略）
- [x] 4.2 [P] 改寫 `packaging/shadowdns.yaml.example`：刪除「List form (legacy)」段落與註解，所有範例改成 mapping form；保留「object form 必填 members」「rewrite_rdata_labels 預設 false」的說明
- [x] 4.3 [P] 跑 `make test` 確認 integration 測試使用新 fixture 仍全部通過（與 1.x 改動一起驗）

## 5. 更新 dump-aliases.py 一律輸出 mapping form

- [x] 5.1 修改 `.local/tool/dump-aliases.py` 的 `build_aliases()`：移除 `if root in CDN_ROOTS and members` 分支，改為對所有非空 `members` 都輸出 `{"members": [...], "rewrite_rdata_labels": <bool>}`，其中 `rewrite_rdata_labels` 由 `root in CDN_ROOTS` 決定（True/False）
- [x] 5.2 修改 `.local/tool/dump-aliases.py` 的 `build_aliases()` 對應的 docstring 與回傳型別 annotation：從 `dict[str, list[str] | dict]` 改為 `dict[str, dict]`
- [x] 5.3 處理「root 有零個 backup」的 edge case：`members` 為空時不可走 mapping form（Go 拒絕空 members）。決策：對空 members 的 root 直接從輸出 dict 移除（YAML 中該 root 不出現），與「`aliases: {}` 為合法配置」一致；更新 docstring 反映此行為
- [x] 5.4 [P] 跑一次 `uv run .local/tool/dump-aliases.py` 對 stg API（待 credentials 修復後）或對合成輸入驗證，確認輸出全部為 mapping form 且無語法錯誤

## 6. 部署驗證

- [ ] 6.1 請使用者在 bench-ns2 上重跑 `dump-aliases.py` 產生新版 `/etc/shadowdns/shadowdns.yaml`，並用 `kill -HUP $(pidof shadowdns)` 觸發 reload
- [ ] 6.2 請使用者觀察 shadowdns 日誌，確認 reload 成功且無 YAML 解析錯誤
