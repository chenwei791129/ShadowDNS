## Why

ShadowDNS 目前僅以來源 IP（resolver 的 IP）做 GeoIP view 選擇。當查詢經由公共 resolver（主要是 Google Public DNS，約佔全網 ECS 流量九成）轉送時，來源 IP 反映的是 resolver 所在地而非終端使用者所在地，導致 geo 選擇失準。RFC 7871 EDNS Client Subnet（ECS）讓 resolver 把使用者的子網帶進查詢；同類 GeoDNS 軟體（gdnsd、PowerDNS、Knot DNS）均已支援，實作 ECS 可在 Google DNS 流量上比遷移來源 BIND（不支援 ECS）更精準。詳見 docs/ecs-implementation-survey.md 的產業調查。

## What Changes

- 新增 ECS 解析：在現有單次 OPT 迭代（queryOpt 設計）中擷取並驗證 `EDNS0_SUBNET` option，不增加 hot path 的第二次迭代
- view 選擇引入「geo 查詢位址」概念：ECS 位址僅覆寫 geo 類規則（country/ASN）的查詢輸入；IP/CIDR ACL 規則一律使用真實來源 IP，防止以偽造 ECS 冒充其他 view（view-spoofing）
- 回應寫回 ECS option（SCOPE PREFIX-LENGTH 採 echo 來源 prefix length 的保守合規策略），組裝點集中於 attachOPT，仿 DNS COOKIE 的 respCookie 模式
- RFC 7871 邊界行為：source prefix length 為 0 的 opt-out 查詢（含 `dig +subnet=0` 的 FAMILY 0 形式）不以 ECS 選 view 且回應 scope 0；handler 可達的格式錯誤（非零 query SCOPE、prefix 外非零位）回 FORMERR，wire 層違規由 DNS library 解包時拒絕（既有行為，與開關無關）
- 新增 CLI flag `--ecs-enable`（預設 `false`）：預設關閉時完全忽略查詢中的 ECS 且回應不帶 ECS option，行為與 BIND 一致，確保自 BIND 遷移的相容性

## Capabilities

### New Capabilities

- `edns-client-subnet`: RFC 7871 ECS 的解析、驗證、geo 覆寫語意、SCOPE 寫回與 opt-in 開關行為

### Modified Capabilities

- `view-matcher`: view 解析需求由「接受單一 client IP」擴充為「ACL 規則（IP/CIDR）以來源 IP 評估、geo 規則（country/ASN）以 geo 查詢位址評估」；未提供 geo 查詢位址時兩者相同，行為與現行完全一致

## Impact

- Affected specs: 新增 `edns-client-subnet`；修改 `view-matcher`
- Affected code:
  - New: internal/dnsutil/ecs.go、internal/dnsutil/ecs_test.go、internal/server/handler_ecs_test.go
  - Modified: internal/server/handler.go（queryOpt 解析、attachOPT 寫回、view 解析呼叫點）、internal/view/matcher.go（雙位址 Resolve）、internal/view/matcher_test.go（既有單參數呼叫點遷移＋雙位址案例）、cmd/shadowdns/main.go（--ecs-enable flag 與啟動 log）、cmd/shadowdns/main_test.go（flag 斷言）、internal/server/server.go（Server 開關欄位）、README.md 與 docs/index.md 與 docs/index.zh.md（功能比較表 ECS 列 Planned → 已支援）、docs/reference/cli.md 與 docs/reference/cli.zh.md（flag 參考表新增 --ecs-enable 列）
  - Removed:（無）
