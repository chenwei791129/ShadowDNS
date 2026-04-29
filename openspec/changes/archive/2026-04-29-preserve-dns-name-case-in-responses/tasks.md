## 1. dnsutil case-aware helpers（落實 Decision: `Canonicalize` 重新定義為 case-preserving）

- [x] 1.1 在 `internal/dnsutil/dnsutil.go` 新增 `LookupKey(name string) string` — 回傳 `strings.ToLower(strings.TrimSuffix(name, ".")) + "."`，作為比對 / map key 的 lowercase fold helper（落實 Decision: 採 single-source 原 case 儲存 + on-demand lowercase fold）
- [x] 1.2 修改 `internal/dnsutil/dnsutil.go` 的 `Canonicalize`：移除 `strings.ToLower`，只保留 trailing dot normalization；更新 doc comment 說明語義變更為 case-preserving
- [x] 1.3 [P] 新增 / 更新 `internal/dnsutil/dnsutil_test.go` 覆蓋 `Canonicalize` case-preserving、`LookupKey` lowercase fold、空字串、無 trailing dot、有 trailing dot、混合 case 等情境
- [x] 1.4 在整個 codebase 跑 `Canonicalize` callsite audit：每個呼叫點判斷該維持 `Canonicalize`（output / storage / 結構儲存）還是改為 `LookupKey`（map key / 比對）— 詳見 design Migration Plan Step 3 清單；產出一份 audit table 寫進 PR description

## 2. Zone storage 保留原 case（落實 Requirement: Preserve zone-file case in stored RRs while indexing on lowercase）

- [x] 2.1 確認 `internal/zone/zone.go` `AddRR` 用 `strings.ToLower` 僅作為 index key，不 mutate RR 物件本身，落實 Preserve zone-file case in stored RRs while indexing on lowercase 的 invariant；加 doc comment 明文敘述；新增 unit test 斷言儲存的 `RR.Header().Name` byte-for-byte 等於輸入 case
- [x] 2.2 確認 `internal/zone/parser.go` 第 56 行 `strings.ToLower(rr.Header().Name)` 僅用於 `IsInZone` check 不寫回 RR；加 doc comment 明確化此 invariant
- [x] 2.3 [P] 新增 `internal/zone/parser_test.go` 覆蓋 `Parse mixed-case zone file preserves owner case`、`Parse mixed-case CNAME target preserves RDATA case`、`Lookup with lowercase qname returns mixed-case stored RR`

## 3. Alias config 保留原 case（落實 Requirement: Match alias map keys case-insensitively while preserving config case）

- [x] 3.1 在 `internal/config/aliases.go` 落實 Match alias map keys case-insensitively while preserving config case：`AliasGroup.Members` 與 root 名稱保留 yaml 原 case；alias map 的 key 改用 `dnsutil.LookupKey(name)` 而非 `Canonicalize`；確保 `Detect` 路徑用 lowercase fold key 做 lookup
- [x] 3.2 在 `internal/shadowdnscfg/config.go`：`AliasFlags` 的 root 索引同樣用 `LookupKey` fold；保留原 yaml 寫法在 struct field 中
- [x] 3.3 [P] 更新 `internal/config/aliases_test.go` 與 `internal/shadowdnscfg/config_test.go`：增加 mixed-case backup 名稱（例如 yaml 寫 `Example.com`）的 parse / lookup 測試 — 斷言 lookup 用 lowercase 命中、Members slice 保留 capital G、`AliasFlags` 結構保留原 case

## 4. Alias rewrite case-preserving output（落實 Requirement: Preserve DNS name case across alias rewrite，及 Decision: `RewriteName` / `RewriteNameAnywhere` 改為 case-preserving output）

- [x] 4.1 修改 `internal/alias/rewrite.go` 的 `RewriteName`，落實 Preserve DNS name case across alias rewrite：簽章不變但語義改為 case-preserving — `n` 保留原 case，`root` 必須是 lowercase（呼叫端用 `LookupKey`），`backup` 必須是 alias config 寫入的原 case；match 用 `strings.ToLower(n)` 跟 `root` 比；output 用 n 原 case 的 prefix slice + backup 原 case suffix
- [x] 4.2 修改 `internal/alias/rewrite.go` 的 `RewriteNameAnywhere`：同樣改為 case-preserving — match index 用 lowercase fold 計算位置，output 用原 n 的 byte slice 拼接 backup 原 case；保持 allocation-free 設計（pre-`Grow` 的 `strings.Builder`，不額外配置 slice）
- [x] 4.3 修改 `internal/alias/override.go` 的 `Resolve` / `ResolveExact` / `ResolveExactNoCNAME` / `ResolveCNAMEFallback` / `ResolveWildcard` / `finalizeBackupRRs`：呼叫 `RewriteRR` 時傳入 lowercase root（用 `LookupKey`）與原 case backup（從 alias config 結構讀）
- [x] 4.4 [P] 更新 `internal/alias/rewrite_test.go` 與 `internal/alias/rewrite_anywhere_test.go`：每個現有 case 補充對應 mixed-case query / mixed-case backup config / mixed-case zone RDATA 的 expected output；新增邊界 case：query 全大寫、backup 全大寫、zone 全大寫、空 prefix
- [x] 4.5 [P] 更新 `internal/alias/override_test.go`：既有斷言改為 case-preserving expected output；新增 alias yaml 寫 `Example.com` 的整合 case

## 5. Handler 用原 case 組 response（落實 Requirement: Preserve query case in the response Question section、Preserve owner-name case in answer authority and additional sections，及 Decision: Handler 用 `req.Question[0].Name` 替代 lowercased qname 組裝 response）

- [x] 5.1 修改 `internal/server/handler.go` 落實 Preserve query case in the response Question section 與 Preserve owner-name case in answer, authority, and additional sections：`qname := strings.ToLower(q.Name)` 改為 `qname := dnsutil.LookupKey(q.Name)` 並另外保留 `qnameOrig := q.Name`；handler 內所有 zone matching / alias detect 路徑用 `qname`，所有觸及 response 組裝的呼叫改用 `qnameOrig`
- [x] 5.2 修改 `rewriteWildcardOwner(records, qname)` 簽章為 `rewriteWildcardOwner(records, qnameOrig)`，斷言 wildcard 命中時 owner = query 原 case
- [x] 5.3 確認 `replyWithAnswer` / `s.negativeReply` 內部 `m.SetReply(req)` 不 mutate `req.Question[0].Name`（miekg/dns 預設行為，加 unit test fence）
- [x] 5.4 修改 `internal/server/build.go`：alias resolve 入口傳入 query name 用原 case，內部用 `LookupKey` fold
- [x] 5.5 [P] 新增 `internal/server/handler_test.go`（或既有檔擴充）：針對 mixed-case query 對 root zone / alias zone / wildcard zone 的 Question section echo + Answer section owner case 行為斷言；落實 Decision: 配置端不做 case validation；接受任何 case 並原樣儲存— 對不同 yaml case 配置（全 lowercase / 全 uppercase / mixed-case backup）驗證 lookup 一致命中且 output 用各自原 case

## 6. Integration tests（端到端覆蓋）

- [x] 6.1 新增 `test/integration/case_preservation_test.go`：以 mixed-case yaml（root `originzone.com` + backup `Example.com`）跑：(a) query `www.example.com.` 回應 owner / target case；(b) query `WwW.ExAmPlE.cOm.` 回應 Question section echo + Answer section owner / RDATA case；(c) query `WWW.EXAMPLE.COM.` 全大寫 echo 回 Question section
- [x] 6.2 [P] 新增 root-zone case-preservation 整合 case（非 alias 路徑）：query `Service.Root.Com.` 回應 owner case 來自 zone-file
- [x] 6.3 [P] 新增 wildcard case-preservation 整合 case：zone 有 `*.root.com. A 1.2.3.4`，query `WWW.Root.Com. A` 回應 owner = `WWW.Root.Com.`

## 7. 驗證與收尾

- [x] 7.1 `make test` 全綠（含所有 unit + integration）；`make lint` 無新警告
- [x] 7.2 [P] 更新 `CHANGELOG.md`：記錄 case-preservation 行為變更、ops 端 yaml backup 名稱 case 應與 BIND zone-file case 一致的提示（v0.x.x 階段不標記 breaking）
- [ ] 7.3 [P] 部署到 bench-ns2 跑 dnspyre 一致性檢查，確認 `www.example.com` 從 INCONSISTENT 移除；用 `dig +qr WWW.EXAMPLE.COM` 驗 Question section 大寫 echo 正確
