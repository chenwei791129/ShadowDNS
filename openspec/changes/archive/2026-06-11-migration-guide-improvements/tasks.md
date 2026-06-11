<!--
Each task description MUST state:
- the behavior or contract being delivered (what is observably true when the
  task is complete), and
- the verification target that proves completion (test, CLI invocation,
  analyzer check, manual assertion, or content review).

File paths are supporting context for locating the work, never the task
itself. "Edit file X" is not a valid task — it is missing both behavior and
verification.
-->

## 1. Reload 靜默失敗偵測

- [x] 1.1 在 `docs/migration.md` 新增「Day 2 維運：Reload 靜默失敗偵測」小節，說明 SIGHUP reload 失敗時程式保留舊 state 且 serial 不變的行為（對應 spec requirement: Day 2 reload failure detection）。內容須包含：(a) 以 `shadowdns_reload_total{result="failure"}` counter 與 `shadowdns_config_last_reload_success_timestamp_seconds` gauge 設告警的 PromQL 建議（主要偵測手段；兩個 metric 已隨 `reload-coverage-and-metrics` change 落地），(b) `dig @127.0.0.1 example.com SOA` serial probe 的完整指令範例（每次推送後的驗證步驟），(c) serial 比對邏輯（相符 = 成功、不符 = 告警 + rollback zone 檔），(d) log-based 輔助檢查說明（`/var/log/shadowdns/shadowdns.log` 中 ERROR 等級的 `reload failed` 訊息；log 為 console encoder 格式、非 logfmt，grep 範例須對應實際格式）。驗證方式：在 `docs/migration.md` 中確認上述四個要素均存在且使用 RFC 2606 範例網域，無內部真實網域。

## 2. GeoIP DB 過期監控

- [x] 2.1 在 `docs/migration.md` 新增「Day 2 維運：GeoIP DB 過期監控與月度輪換」小節，內容須包含：(a) 使用 `shadowdns_geoip_db_info{build_time}` metric 設過期告警的建議（閾值 >35 天），(b) mmdb 隨每次 SIGHUP reload 重新開啟（`reload-coverage-and-metrics` change 已落地）、GeoIP 更新不需 full restart 的說明，(c) 月度例行維護 SOP：下載新資料庫 tar.gz → 驗證壓縮檔 checksum（MaxMind 的 SHA256 對應 tar.gz、非裸 mmdb）→ 解壓 → 逐台 SIGHUP reload（每次一台，等該台 `shadowdns_geoip_db_info{build_time}` 更新後再繼續；未更新時以 reload metrics / 應用層 log 查原因）（對應 spec requirement: GeoIP DB expiry monitoring）。驗證方式：確認小節包含上述三個要素，alert 閾值為 35 天，逐台 reload 步驟完整。

## 3. Ephemeral 記錄揮發性警示

- [x] 3.1 在 `docs/migration.md` 新增「Day 2 維運：Ephemeral DNS-01 記錄揮發性」小節，說明 ephemeral 記錄為純記憶體儲存、**成功的 SIGHUP reload 與 restart 均會清除**（reload() 在成功時無條件呼叫 EphemeralStore.Clear()；引用 `internal/ephemeral/store.go` 的設計說明），並列出操作規則：**重啟或 SIGHUP reload 前**確認無進行中的 DNS-01 challenge（ephemeral API 僅有 PUT/DELETE 端點、無法列舉記錄，改以 `dig` 查詢 `_acme-challenge` TXT 記錄或 ACME client log 確認），以及將 shadowdns 重啟與 reload 排程與 ACME 憑證續期窗口錯開（對應 spec requirement: Ephemeral record restart awareness）。驗證方式：確認小節的操作前確認清單同時覆蓋 restart 與 SIGHUP reload，且包含排程錯開建議，並無任何 TBD / 空泛措辭。

## 4. Rolling Restart 操作指引

- [x] 4.1 在 `docs/migration.md` 新增「Day 2 維運：重啟成本與 Rolling Restart SOP」小節，說明現行版本（`reload-coverage-and-metrics` 落地後）zone 資料、GeoIP mmdb、RRL 設定、query log 設定、`shadowdns.yaml` 的 `aliases:` 區段均由 SIGHUP reload 套用；僅 CLI flag 變更（如 `--log-file`、`--listen`、`--metrics-addr`，flags 皆為 process-lifetime sticky）、listen-on / listen-on-v6 位址變更、與 `shadowdns.yaml` 的 `ephemeral_api:` 區段（API server 僅於啟動時建立、reload 不重讀）需要 full restart。冷啟動效能影響須說明「重啟後首次 dnspyre benchmark 觀察到 QPS 約低 30%」並標明此為 benchmark 觀察值、非服務容量保證。SOP 須包含：(a) 生產部署至少 2 台實例的要求，(b) rolling restart 步驟（一台一台重啟、等 QPS 回穩再繼續），(c) 把需重啟的設定變更批次化排入維護窗口的建議（對應 spec requirement: Rolling restart operations）。驗證方式：確認 SOP 步驟可操作（有具體指令或檢查點），冷啟動觀察值說明與出處標註存在。

## 5. 持續性答案一致性回歸

- [x] 5.1 在 `docs/migration.md` 新增「Day 2 維運：持續性答案一致性回歸驗證」小節，說明 answer-diff 不應只在切換當下執行，而應常態化於每次 zone 變更推送後進行，比對兩台實例（BIND vs ShadowDNS，或新舊版）的回應差異，特別說明 alias/CNAME flattening 改寫邏輯可能產生邊界 case 差異，需主動調查（對應 spec requirement: Continuous answer consistency regression）。內容須包含使用 RFC 2606 範例網域的 `diff <(dig ...) <(dig ...)` 範例指令。驗證方式：確認小節有具體的 `dig` + `diff` 範例指令，且網域全為 RFC 2606 範例網域。

## 6. Query Log 磁碟管理

- [x] 6.1 在 `docs/migration.md` 新增「Day 2 維運：Query Log 磁碟管理」小節，說明 authoritative server query log 量極大，並包含：(a) 查核 `packaging/logrotate.shadowdns` logrotate 設定的步驟（`logrotate -d /etc/logrotate.d/shadowdns` 乾跑驗證），(b) 依實際查詢量調整輪替頻率的建議，(c) 應用層錯誤看 `/var/log/shadowdns/shadowdns.log`（需 sudo）的說明（對應 spec requirement: Query log disk management）。驗證方式：確認三個要素均存在，logrotate 乾跑指令正確，log 路徑說明清楚。

## 7. 升級 / 回滾 SOP

- [x] 7.1 在 `docs/migration.md` 新增「Day 2 維運：升級與回滾 SOP」小節，列出標準升級流程：(1) 下載新 `.deb` 套件，(2) 執行 `shadowdns --dry-run --named-conf <path> --config <path>` 驗證新版 config 解析，(3) rolling restart 套用新版，(4) 失敗時回滾（`dpkg -i <previous.deb>` + `systemctl restart shadowdns` + 驗證）。須說明 v0.x.x 實驗階段每次升版可能有 breaking CLI/config 變更，`--dry-run` 為強制步驟（對應 spec requirement: Upgrade and rollback SOP）。驗證方式：確認四步驟均完整，`--dry-run` 指令語法正確，回滾步驟可操作。

## 8. Latency 監控指引

- [x] 8.1 在 `docs/migration.md` 新增「Day 2 維運：Latency 監控」小節，說明使用 `shadowdns_dns_request_duration_seconds` histogram 監控 p50/p95/p99 延遲，提供 PromQL 範例（histogram_quantile），建議 p99 告警閾值（以授權 DNS 為例：>10ms），並說明 bucket 邊界範圍為 0.1ms–100ms（已由 `dns-latency-histogram-buckets` change 細化）（對應 spec requirement: Latency monitoring guidance）。驗證方式：確認 PromQL 範例語法正確，p99 告警閾值有說明，bucket 邊界說明正確。

## 9. 內容一致性與網域消毒

- [x] 9.1 對 `docs/migration.md` 新增的所有 Day 2 維運段落進行內部網域消毒審查：執行 `grep -n` 掃描新增段落，確認所有網域 / zone / 主機名稱範例均為 RFC 2606 範例網域（example.com / example.net / example.org）或明確的佔位符（如 `<zone>`、`<instance-ip>`），無任何非 RFC 2606 TLD 殘留。驗證方式：`grep` 掃描結果顯示無非 RFC 2606 TLD，確認文件可安全 commit。
