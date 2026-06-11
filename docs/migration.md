# 從 BIND 遷移到 ShadowDNS

本文件為 DNS Ops 團隊提供將 BIND master 替換為 ShadowDNS 的操作指引，涵蓋環境前置條件、四階段切換步驟、Rollback 策略、監控檢核清單、常見問題，以及切換完成後的 Day 2 維運（監控告警與例行 SOP）。

---

## 適用前提

在開始任何切換動作之前，請逐項確認以下環境條件：

| 項目 | 說明 | 確認 |
|------|------|------|
| BIND master 穩定運行 | 現有 BIND master 無持續告警，近期無非預期重啟 | ☐ |
| Zone 資料完整備份 | 所有 zone file 與 `named.conf` 已備份至可回復位置 | ☐ |
| Slave IP 清單已知 | 列出所有 BIND slave 的 IP，用於 `allow-transfer` ACL 設定 | ☐ |
| MaxMind mmdb 可取得 | `GeoLite2-Country.mmdb` 與 `GeoLite2-ASN.mmdb` 可下載或已就位 | ☐ |
| mmdb 版本與 BIND 一致 | ShadowDNS 使用與 BIND `geoip-directory` 相同的 mmdb 檔，避免 GeoIP 判斷出現差異 | ☐ |
| `shadowdns.yaml` 產出機制確認 | 管理系統能自動產出 `shadowdns.yaml`（含 `aliases:` 區段），或已評估手動維護成本 | ☐ |
| 監控系統覆蓋兩端 | 監控系統可同時觀測 BIND 與 ShadowDNS 的 query QPS、錯誤率、記憶體 | ☐ |
| Rollback 程序已演練 | 團隊熟悉各 Phase 的回退流程（見下方 Rollback 策略） | ☐ |

---

## 四階段切換步驟

### Phase 0：開發與測試（本 change 範圍內）

**目標**：確認 ShadowDNS 在受控環境下能正確處理生產規模的設定檔與 zone 資料，且記憶體用量符合預期。

**執行步驟**：

1. 複製一份生產用 `named.conf`、`master.zones`、zone file 目錄到測試環境。
2. 準備 `shadowdns.yaml`（單一 YAML 檔，涵蓋 `aliases` 與可選的 `ephemeral_api` 區段；初始可手動整理，或讓管理系統在測試環境產出）。
3. 建置 ShadowDNS binary：

   ```bash
   go build -o shadowdns ./cmd/shadowdns
   ```

4. 以 `--dry-run` 執行啟動煙霧測試，確認設定解析無錯誤：

   ```bash
   ./shadowdns --dry-run \
       --named-conf /path/to/named.conf \
       --config     /path/to/shadowdns.yaml
   ```

5. 觀察啟動 log，確認：
   - 所有 view 與 zone 均成功載入
   - 無 `fatal` 或 `unsupported directive` 錯誤
   - GeoIP mmdb 載入成功

6. 以 `dig` 對 ShadowDNS 測試實例發送代表性查詢，驗證 root zone 與 backup zone 回應正確：

   ```bash
   # 查詢 root zone A record
   dig @<shadowdns-ip> example.com A

   # 查詢 backup zone（應得到與 root 相同的 IP，owner 為 example.net）
   dig @<shadowdns-ip> example.net A

   # 查詢 SOA（backup zone 的 serial 應與 root 一致）
   dig @<shadowdns-ip> example.net SOA
   ```

7. 量測記憶體用量（`ps` 或 `/proc/<pid>/status`），確認低於預期上限（約 50 MB）。
8. 執行單元測試與整合測試：

   ```bash
   go test ./...
   ```

**驗收條件**：

- 全部測試通過
- 記憶體用量符合預期
- 啟動 log 無 `fatal` 訊息
- 代表性 `dig` 查詢結果與 BIND 一致

**預估時間範圍**：中（包含多輪反覆測試與 bug 修正）

---

### Phase 1：並行驗證

**目標**：將 ShadowDNS 部署在非 production IP，與 BIND master 並行運行，比對兩者對相同 query 的回應一致性，連續觀察無異常。

**執行步驟**：

1. 在 BIND master 主機（或同網段備用主機）上，以不同 IP 或不同 port 啟動 ShadowDNS：

   ```bash
   ./shadowdns \
       --named-conf /etc/namedb/named.conf \
       --config     /etc/namedb/shadowdns.yaml \
       --listen     192.0.2.20:53
   ```

2. 確認 ShadowDNS 啟動成功，log 無錯誤。

3. 設計並執行平行比對查詢腳本，對每個 view 的代表性 domain（root zone、backup zone）同時查詢 BIND 與 ShadowDNS：

   ```bash
   # 比對兩端回應（RDATA 應一致；SOA serial 可能因時間差略有不同）
   diff \
     <(dig @<bind-ip>      example.com A +short) \
     <(dig @<shadowdns-ip> example.com A +short)

   diff \
     <(dig @<bind-ip>      example.net A +short) \
     <(dig @<shadowdns-ip> example.net A +short)
   ```

4. 擴展比對範圍至多種 record type（A、AAAA、CNAME、NS、MX、TXT、SOA）與多個 view（模擬不同來源 IP）。

5. 讓監控系統同時對兩端發送探測查詢，持續比對差異，連續觀察至少 7 天。

6. 若發現不一致，記錄具體 domain / query type / view，回報並修正 ShadowDNS。

**驗收條件**：

- 連續 7 天比對查詢無不一致
- 無 SERVFAIL 告警
- ShadowDNS process 無非預期重啟或 panic

**預估時間範圍**：中（含 7 天觀察窗口）

---

### Phase 2：Slave 切換

**目標**：讓管理系統正式產出 `shadowdns.yaml` 的 `aliases:` 區段，逐台將 BIND slave 的 master 指向 ShadowDNS，驗證 AXFR 同步正確。

**執行步驟**：

1. 管理系統開始正式產出 `shadowdns.yaml` 的 `aliases:` 區段 並同步至 ShadowDNS 設定目錄。

2. 選擇一台 staging BIND slave，將其 `masters { }` 設定由 BIND master IP 改為 ShadowDNS IP，然後重新載入：

   ```bash
   # 在 staging slave 上修改 named.conf 的 masters 區塊，例如：
   # masters { 192.0.2.20; };  ← 改為 ShadowDNS IP
   rndc reload
   ```

3. 確認 staging slave 成功完成 AXFR：

   ```bash
   # 觀察 slave 的 named log，應看到每個 zone 的 transfer successful
   journalctl -u named -f | grep "transfer of"
   ```

4. 對 staging slave 執行查詢比對，確認解析結果與 BIND master 一致。

5. 確認 backup zone 的 AXFR 內容中 owner name 與 RDATA 均已正確 rewrite（可用 `dig AXFR` 抽查）：

   ```bash
   dig @<staging-slave-ip> example.net AXFR
   ```

6. staging slave 驗證通過後，逐台對生產 slave 執行相同切換流程（每台切換後觀察至少 24 小時無異常再繼續）。

**驗收條件**：

- 所有 slave 均成功完成 AXFR，無 transfer failure
- Slave 上的查詢結果與 Phase 1 基準一致
- `shadowdns.yaml` 的 `aliases:` 區段 管理流程穩定，無遺漏 domain

**預估時間範圍**：中至長（依 slave 數量與逐台驗證節奏而定）

---

### Phase 3：BIND Master 退場

**目標**：在確認所有 slave 均穩定從 ShadowDNS 拉取後，將舊 BIND master 降為熱備援，最終下線。

**執行步驟**：

1. 確認所有生產 slave 均已切換至 ShadowDNS 且穩定運行超過 7 天。

2. 將 BIND master 設為「熱備援」狀態：保持程序運行，但不再接受管理系統的 zone 更新，僅作為緊急回切的備用。

3. 觀察 BIND master 是否仍有 slave 存取（若有，表示切換未完全）：

   ```bash
   # 在 BIND master 上觀察 AXFR 請求 log
   journalctl -u named | grep "AXFR"
   ```

4. 熱備援期間（建議 1–2 週），持續監控 ShadowDNS 的 QPS、錯誤率、記憶體。

5. 熱備援期結束、無異常後，下線 BIND master：

   ```bash
   systemctl stop named
   systemctl disable named
   ```

6. 更新文件，記錄 ShadowDNS 成為新的唯一 master。

**驗收條件**：

- 熱備援期間 ShadowDNS 無異常
- BIND master log 無 slave 存取（確認切換完全）
- 下線後 DNS 解析無影響

**預估時間範圍**：短至中（1–2 週熱備援觀察）

---

## Rollback 策略

每個 Phase 均有對應的回退方式，設計上以「新增 ShadowDNS instance，不移除 BIND」為原則，確保任一階段皆可安全回退。

### Phase 1 出問題

**回退方式**：停止 ShadowDNS process 即可。此時所有 slave 仍指向原 BIND master，無任何流量影響。

```bash
# 停止 ShadowDNS（依實際啟動方式調整）
kill <shadowdns-pid>
# 或
systemctl stop shadowdns
```

**影響範圍**：僅 ShadowDNS 並行實例停止，生產環境不受影響。

---

### Phase 2 出問題（已切換部分 slave）

**回退方式**：將已切換的 slave 的 `masters { }` 設定改回 BIND master IP，然後 reload：

```bash
# 在問題 slave 上修改 named.conf，將 masters 改回 BIND master IP
# masters { 192.0.2.1; };  ← 改回原 BIND master IP
rndc reload
```

BIND master 仍持有完整 zone 資料，slave 重新從 BIND 拉取 AXFR 後即可恢復。

**注意事項**：若 Phase 2 期間 zone 資料有更新，需確認 BIND master 的 zone 資料是最新的，或等 slave 完成 AXFR 後再驗證。

---

### Phase 3 出問題（熱備援期間）

**回退方式**：重啟舊 BIND master（前提是熱備援期尚未過期，BIND master 仍有完整且最新的 zone 資料）：

```bash
systemctl start named
```

然後將所有 slave 的 `masters` 改回 BIND master IP 並 reload。

**前提條件**：BIND master 必須在熱備援期間（1–2 週）保持 zone 資料一致。一旦管理系統僅更新 ShadowDNS 而不同步至 BIND，熱備援的 zone 資料將逐漸過期，屆時回退可能需要重新同步 zone 資料。

**建議**：在熱備援期間，管理系統同時更新兩端設定（BIND master + ShadowDNS），直到確認無問題後才停止更新 BIND。

---

## 監控檢核清單

切換前後應持續觀察以下指標，建議在 Phase 1 並行驗證期間即建立基準值。

### DNS Query 指標

| 指標 | 觀察方式 | 預期行為 |
|------|----------|----------|
| Query QPS（每 view） | 監控系統 / query log 統計 | 切換後 QPS 分布與 BIND 基準一致 |
| NOERROR 比例 | DNS server log / 監控 | 應維持在切換前水位 |
| NXDOMAIN 比例 | DNS server log / 監控 | 切換後不應異常升高 |
| SERVFAIL 比例 | DNS server log / 監控 | 應為 0 或極低；任何 SERVFAIL 需立即調查 |
| REFUSED 比例 | DNS server log / 監控 | 僅應在合理場景出現（out-of-zone query、CHAOS query） |

### Zone Transfer 指標

| 指標 | 觀察方式 | 預期行為 |
|------|----------|----------|
| AXFR 失敗率（每 slave） | Slave BIND log（`transfer of ... failed`） | 切換後應為 0 |
| AXFR 完成時間 | Slave log / 監控 | 與 Phase 1 基準相近；若顯著升高需查 ShadowDNS 效能 |
| NOTIFY 送出 | ShadowDNS log | Zone 更新後應見 NOTIFY sent 記錄 |

### ShadowDNS Process 指標

| 指標 | 觀察方式 | 預期行為 |
|------|----------|----------|
| Process 記憶體用量 | `ps`、`/proc/<pid>/status`、或監控 | 應低於 BIND master 的約 20%（目標 ~50 MB） |
| Process 是否存活 | 監控 / systemd | 無非預期重啟 |
| 啟動 log 錯誤 | ShadowDNS log（ERROR 等級；實際為 console encoder 的 tab 分隔格式，非 logfmt） | 正常運行時應無錯誤 log |

### GeoIP 指標

| 指標 | 觀察方式 | 預期行為 |
|------|----------|----------|
| 每 view 的 query 分布 | Query log 抽樣統計 | 與 BIND 基準的 view 分布接近（允許小幅差異） |
| 無 view 匹配（REFUSED）比例 | DNS server log | 不應異常升高；升高表示 GeoIP 資料或規則有問題 |

**GeoIP 抽樣方法**：從 ShadowDNS query log 中抽取 1000 筆，統計各 view 的 query 數量，與 BIND 相同時段的 view 分布比對。差異超過 5% 應進一步調查 mmdb 版本是否一致。

Prometheus metrics 告警規則（reload 失敗、latency、GeoIP 新鮮度）見下方「Day 2 維運」章節。

---

## 常見問題

**Q：切換後某個 backup domain 解到錯誤的 IP**

檢查 `shadowdns.yaml` 的 `aliases:` 區段 中該 backup domain 的對應是否正確（即指向正確的 root domain）。確認對應的 root domain 在 ShadowDNS 中已正確載入：

```bash
dig @<shadowdns-ip> <root-domain> A
dig @<shadowdns-ip> <backup-domain> A
```

若兩者 RDATA 不一致，表示 root zone 資料與 `shadowdns.yaml` 的 `aliases:` 區段 的對應有誤，或 root zone 資料本身有問題。

---

**Q：Slave 持續發起 AXFR，無法完成**

可能原因：

1. **SOA serial 未正確同步**：若 ShadowDNS reload（重啟）後 backup zone 的 SOA serial 沒有隨 root zone 更新，slave 可能陷入反覆 AXFR 迴圈。確認 ShadowDNS 啟動後 backup zone 的 SOA serial 與對應 root zone 一致：

   ```bash
   dig @<shadowdns-ip> <root-domain>   SOA +short
   dig @<shadowdns-ip> <backup-domain> SOA +short
   ```

2. **allow-transfer ACL 設定錯誤**：若 slave IP 不在 `allow-transfer` 清單中，AXFR 會收到 REFUSED。檢查 `named.conf` 的 `allow-transfer` 設定。

3. **TCP 連線問題**：AXFR 走 TCP，確認 ShadowDNS 主機的防火牆允許 slave IP 的 TCP/53 連線。

---

**Q：GeoIP 判斷結果與 BIND 不一致**

1. 確認 ShadowDNS 使用的 mmdb 檔與 BIND 的 `geoip-directory` 是相同版本的檔案：

   ```bash
   ls -la /usr/local/share/GeoIP/GeoLite2-Country.mmdb
   ls -la /usr/local/share/GeoIP/GeoLite2-ASN.mmdb
   ```

2. 確認 `geoip asnum` 規則的 AS number 格式正確（應為 `"AS<數字> <描述>"`，ShadowDNS 只取數字部分）。若格式不符，ShadowDNS 會在啟動時 fatal。

3. 如果 mmdb 版本相同但結果仍有差異，可能是 BIND 的 GeoIP module 版本與 MaxMind 新版 mmdb schema 有差異。以 ShadowDNS 的 query log 抽樣，找出被判斷到不同 view 的 IP，用 `mmdblookup` 工具驗證：

   ```bash
   mmdblookup --file /usr/local/share/GeoIP/GeoLite2-Country.mmdb \
              --ip <client-ip> country iso_code
   ```

---

**Q：ShadowDNS 啟動時出現 `unsupported directive` 錯誤**

ShadowDNS 在啟動時會拒絕部分 BIND 指令（如 `type slave`、`type forward`、`dnssec-enable`、`allow-update`）。錯誤訊息會指出具體檔案路徑與行號。

處理方式：從 `named.conf` 或 `master.zones` 中移除該指令，或將對應 zone 從 ShadowDNS 的設定範圍中排除。

---

**Q：記憶體用量高於預期**

預期記憶體用量約為 BIND master 的 20%（~50 MB 對應 25,200 zone 的情境）。若實際用量偏高：

1. 確認 `shadowdns.yaml` 的 `aliases:` 區段 有完整列出所有 backup domain，沒有 backup domain 被誤當成 root 全量載入。
2. 用 `ps aux` 或 `cat /proc/<pid>/status | grep VmRSS` 觀察 RSS（常駐記憶體），避免與 VSZ（虛擬記憶體）混淆。

---

## 監聽位址行為（Listen address behavior）

ShadowDNS 讀取 `named.conf` 的 `listen-on`（IPv4）與 `listen-on-v6`（IPv6）指令，決定要綁在哪些 IP 上。行為與 BIND9 相容：**每個位址各開一個 socket**，單一位址 bind 失敗（例如 systemd-resolved 已佔住 `127.0.0.53:53`）時只會 log WARN 並繼續，不會讓整個 server 起不來。

### 位址來源優先順序

| 情境 | `--listen` | `listen-on` | `listen-on-v6` | 實際綁定 |
|------|------------|-------------|----------------|----------|
| 預設 | `:53` | 未指定 | 未指定 | 所有 IPv4 介面位址（隱含 `any`），無 IPv6 |
| 預設 + 指定 listen-on | `:53` | `{ 10.0.0.1; 10.0.0.2; }` | 未指定 | `10.0.0.1:53`、`10.0.0.2:53` |
| Port hint + 雙族並聯 | `:53` | `{ 10.0.0.1; }` | `{ 2001:db8::1; }` | `10.0.0.1:53`、`[2001:db8::1]:53`（v4 在前） |
| IPv6-only | `:53` | 未指定 | `{ ::1; }` | `[::1]:53` |
| Override（IPv4） | `127.0.0.1:5353` | 任意 | 任意 | `127.0.0.1:5353`（忽略兩個 block） |
| Override（IPv6 bracket） | `[::1]:5353` | 任意 | 任意 | `[::1]:5353`（忽略兩個 block） |
| Port hint | `:5353` | `{ 10.0.0.1; }` | 未指定 | `10.0.0.1:5353`（port 從 `--listen` 繼承） |

**關鍵規則**：`--listen` **有 host component 才是 override**（例如 `127.0.0.1:5353` 或 IPv6 bracket literal `[::1]:5353`）；`:PORT` 形式只提供 port，位址分別從 `listen-on`（IPv4）與 `listen-on-v6`（IPv6）取得，v4 在前、v6 以 bracket 形式 `[addr]:port` 排列。`listen-on-v6` 缺省為空集合（opt-in），**不像 `listen-on` 缺省會展開所有 IPv4 介面**；若不設定 `listen-on-v6`，就不會啟動任何 IPv6 listener。當 v4 與 v6 解析結果合併後皆為空時才 fatal；單一 family 空、另一族非空時正常啟動。

### 不支援的 listen-on / listen-on-v6 語法

下列 BIND 語法在 `listen-on` 與 `listen-on-v6` 中均**不支援**，會被 log WARN 並跳過，不會影響解析：

- Exclusion syntax：`listen-on { !10.0.0.1; any; };`（`!addr` 排除）
- ACL 參照：`listen-on { trusted-net; };`
- Port override：`listen-on port 5353 { ... };`（請改用 `--listen :5353`）
- `interface` keyword

**IPv6 literal 位址現在於 `listen-on-v6` 受支援**（如 `listen-on-v6 { 2001:db8::1; ::1; };`）。若在 `listen-on` 中放 IPv6 literal，或在 `listen-on-v6` 中放 IPv4 literal，該條目會被 log WARN 並跳過（位址 family 不符）。

### 與 systemd-resolved 的互動

在 Ubuntu 24.04 / Debian 12 等預設啟用 systemd-resolved 的發行版上，`127.0.0.53:53` 和 `127.0.0.54:53` 已被 stub listener 佔住。ShadowDNS 在展開 `listen-on { any; };` 時會嘗試綁那些位址、收到 `EADDRINUSE`，並 log 一筆帶 hint 的 WARN：

```
level=WARN msg="listener bind failed; skipping address"
  addr=127.0.0.53:53
  err="bind UDP 127.0.0.53:53: ... address already in use"
  hint="likely systemd-resolved stub on loopback; set DNSStubListener=no
         in /etc/systemd/resolved.conf if this address is expected"
```

**這是預期行為，不是錯誤**。對外介面（`10.x.x.x`、`192.168.x.x` 等）仍會成功綁上，對外 DNS 服務正常。若你需要 ShadowDNS 真的監聽 `127.0.0.53`，關閉 systemd-resolved stub：

```bash
sudo sed -i 's/^#DNSStubListener=.*/DNSStubListener=no/' /etc/systemd/resolved.conf
sudo systemctl restart systemd-resolved
```

### SIGHUP reload 不會重綁 listener

若 reload 後 `listen-on` 或 `listen-on-v6` 改變，ShadowDNS **不會**重新開 socket。這是刻意的設計，避免 reload 造成短暫的 port 接手空窗。reload drift 偵測涵蓋 v4 與 v6 的合集；偵測到任一 family 的位址集合有變動時會 log：

```
level=INFO msg="reload: listen-address set differs from bound set; restart to apply
                (cause: listen-on/listen-on-v6 change and/or interface change since startup)"
  current_bound=[10.0.0.1:53, 127.0.0.1:53]
  new_resolved=[10.0.0.2:53]
```

執行 `systemctl restart shadowdns` 才會套用新的監聽位址。

### BREAKING 行為差異（與 v0.3.0 之前比較）

- 預設綁定從「單一 `0.0.0.0:53` wildcard socket」改為「per-address bind」。視覺上的差別：啟動 log 從 1 筆變成 N 筆 `listener bound`。
- 新增網卡 / IP alias 不會自動被 pick up；BIND 的 `interface-interval` 動態掃描本版不支援，請用 `systemctl restart shadowdns` 讓新位址進入監聽集合。
- `--listen` 語意從「綁定目標」改為「override hint + port hint」。若你之前寫 `--listen :53` 是期望 `0.0.0.0` wildcard 行為，現在會被當成「port hint，位址從 listen-on 取（或 any 展開）」——行為在大多數情況一致，但顯式 log 會不同。

---

## Day 2 維運

本章涵蓋切換完成（ShadowDNS 成為唯一 master）後的穩態維運：以 Prometheus metrics 為主的告警設定與例行 SOP。前述「監控檢核清單」的指標基準在 Day 2 仍然適用。

### Reload 靜默失敗偵測

SIGHUP reload 失敗時（例如 zone 檔語法錯誤、config parse error），ShadowDNS **不會 crash，也不會中斷服務**——程式保留前一份設定繼續應答，對外可見的症狀只有 SOA serial 停在舊值。若沒有主動偵測，過期資料可能持續服務數小時而無人察覺。

**主要偵測手段：reload metrics 告警**

ShadowDNS 經 `--metrics-addr`（預設 `:9153`）揭露 `shadowdns_reload_total{result="success"|"failure"}` counter 與 `shadowdns_config_last_reload_success_timestamp_seconds` gauge——語意詳見 [README 的 shadowdns.yaml schema 一節](../README.md#shadowdnsyaml-schema)；兩個 `result` label 組合在啟動時即預先初始化，告警表達式不需處理 metric 缺席。建議告警規則：

```promql
# reload 失敗即告警
increase(shadowdns_reload_total{result="failure"}[15m]) > 0

# staleness 告警：若 zone 推送頻率固定（例如至少每日一次），
# 可偵測「推送了卻一直沒有成功 reload」的情況
# （注意：process 重啟也會重設此 gauge，故僅靠它無法涵蓋所有情境）
time() - shadowdns_config_last_reload_success_timestamp_seconds > 86400
```

**每次推送後的驗證步驟：serial probe**

zone 變更推送並送出 SIGHUP 後，比對磁碟上的 serial 與線上應答的 serial：

```bash
# 1. 讀取磁碟上 zone 檔宣告的 serial。多行 SOA 寫法中 serial 是括號後的
#    第一個數值（慣例上標註「; serial」）；注意若 SOA 行帶有顯式 TTL，
#    行內第一個數字是 TTL 而非 serial，別取錯欄位
grep -m1 -A1 'SOA' /etc/namedb/master/example.com.zone

# 2. 取得線上應答的 serial（dig SOA +short 輸出的第 3 個欄位）
dig @127.0.0.1 example.com SOA +short | awk '{print $3}'
```

比對邏輯：

- **兩者相符** → reload 成功，推送完成。
- **線上 serial 比磁碟舊** → reload 靜默失敗，正在服務過期設定。立即告警，並將 zone 檔回滾到上一個已知可用版本（讓下次 reload 至少恢復一致狀態），再調查新 zone 檔的錯誤原因。

推送後驗證的另一半——回應內容比對——見本章「持續性答案一致性回歸驗證」。

**輔助手段：log 檢查**

未部署 Prometheus 的環境，可監看應用層 log 中 ERROR 等級的 `reload failed` 訊息：

```bash
# 以 tail 限定範圍，避免每次檢查都重複掃出輪替窗口內的歷史事件
sudo tail -n 5000 /var/log/shadowdns/shadowdns.log | grep -E 'ERROR\s+reload failed'
```

注意：ShadowDNS 的 log 為 console encoder 格式（tab 分隔的 `時間  等級  訊息  欄位`），**不是** logfmt 的 `level=ERROR msg="..."` 形式；設定 log-based alert 時 pattern 要對應實際格式（本文件較早章節的 log 摘錄為示意寫法）。

### Latency 監控

ShadowDNS 以 `shadowdns_dns_request_duration_seconds` histogram（label：`view`）記錄每筆查詢的處理時間，bucket 邊界涵蓋 **0.1 ms 到 100 ms**，對授權 DNS 的 sub-millisecond 常態與十毫秒級異常都有足夠解析度。

以 `histogram_quantile` 推導分位數延遲：

```promql
# 全域 p99（聚合所有 view）；改用 sum by (le, view) 可看各 view，
# 把 0.99 換成 0.5 / 0.95 即得 p50 / p95
histogram_quantile(0.99,
  sum by (le) (rate(shadowdns_dns_request_duration_seconds_bucket[5m])))
```

**告警建議**：對上述全域 p99 查詢式加上 `> 0.01`（10 ms，恰為 bucket 邊界之一，分位數估計在此處最準確）即為告警條件。實際閾值依環境 SLA 調整；建議同時看 p50（常態水位漂移）與 p99（尾端惡化），兩者背離往往指向 GC、磁碟 I/O（query log）或單一 view 的熱點問題。

### GeoIP DB 過期監控與月度輪換

MaxMind 每月更新 GeoLite2 資料庫。mmdb 過期會讓 GeoIP view 判斷逐漸偏離現實（IP 區段易主、ASN 重新分配），症狀是特定來源的查詢被導到錯誤的 view——這類偏差不會觸發任何錯誤告警，只能靠主動監控 DB 新鮮度。

**過期監控（告警閾值：35 天）**

ShadowDNS 將載入中的 mmdb 建置時間揭露為 `shadowdns_geoip_db_info` gauge（值恆為 1，`build_time` label 為 RFC3339 字串）：

```
shadowdns_geoip_db_info{build_time="2026-05-13T00:00:00Z",database="country"} 1
shadowdns_geoip_db_info{build_time="2026-05-13T00:00:00Z",database="asn"} 1
```

由於 `build_time` 是字串 label，純 PromQL 無法直接換算年齡；建議用排程腳本抓取 `/metrics` 計算，超過 **35 天**即告警（MaxMind 月更週期約 30 天，35 天表示已錯過一輪更新）：

```bash
# cron 檢查：任一 database 的 build_time 超過 35 天即輸出 STALE（無輸出 = 通過）
curl -s http://127.0.0.1:9153/metrics \
  | awk -F'"' '/^shadowdns_geoip_db_info/{print $2}' \
  | while read -r ts; do
      [ "$(date -d "$ts" +%s)" -lt "$(date -d '35 days ago' +%s)" ] && echo "STALE: $ts"
    done
```

若監控棧支援從 label 取數值（例如 VictoriaMetrics 的 MetricsQL），也可直接以查詢式告警。

**月度例行維護 SOP**

mmdb 檔在每次 SIGHUP reload 都會重新開啟（見 [README 的 GeoIP databases 一節](../README.md#geoip-databases)），GeoIP 更新**不需要重啟 process**——把新檔案放到原路徑後逐台 reload 即可：

1. 下載新版 Country 與 ASN 資料庫的 tar.gz 套件，**驗證 checksum** 後再解壓（MaxMind 提供的 SHA256 檔對應 tar.gz 壓縮檔，不是解出來的 `.mmdb`），確認無誤後把 `.mmdb` 放到生產路徑：

   ```bash
   sha256sum -c GeoLite2-Country_<date>.tar.gz.sha256
   sha256sum -c GeoLite2-ASN_<date>.tar.gz.sha256
   tar -xzf GeoLite2-Country_<date>.tar.gz
   tar -xzf GeoLite2-ASN_<date>.tar.gz
   ```

2. 逐台（每次一台）觸發 reload（systemd unit 已定義 `ExecReload` 送 SIGHUP）：

   ```bash
   sudo systemctl reload shadowdns
   ```

3. 確認該台的 `shadowdns_geoip_db_info{build_time}` 已反映新的建置日期；若未更新，用 reload metrics 與應用層 log（見「Reload 靜默失敗偵測」）找出 reload 失敗原因：

   ```bash
   curl -s http://<instance-ip>:9153/metrics | grep '^shadowdns_geoip_db_info'
   ```

4. 該台驗證通過後，再對下一台執行步驟 2–3，直到所有實例完成。

### Ephemeral DNS-01 記錄揮發性

透過 ephemeral API（`PUT /v1/txt/{fqdn}`）寫入的 ACME DNS-01 challenge TXT 記錄是**純記憶體儲存**：**process 重啟**（restart、升級、主機重開機）與**成功的 SIGHUP reload** 都會清空所有 ephemeral 記錄。後者是刻意設計（見 `internal/ephemeral/store.go`：「ephemeral state does not survive a config reload」），確保 reload 後的服務狀態完全由設定檔推導；完整的生命週期行為見 [docs/ephemeral-api.md](ephemeral-api.md)。

若清除發生在 ACME challenge 寫入 TXT 之後、CA 驗證查詢之前，該次 challenge 會驗證失敗，憑證續期中斷。

**操作前確認清單**

執行重啟**或**送出 SIGHUP 之前：

1. 確認目前沒有進行中的 DNS-01 challenge。ephemeral API 只有 PUT / DELETE 端點、無法列舉記錄，請改用以下任一方式確認：

   ```bash
   # 直接查詢 challenge TXT 記錄是否存在（NXDOMAIN / 空回應 = 無進行中的 challenge）
   dig @127.0.0.1 _acme-challenge.example.com TXT +short
   ```

   或檢查 ACME client（certbot / lego 等）的 log 與排程狀態，確認沒有正在執行的續期流程。

2. 若有進行中的 challenge，等它完成（成功或失敗皆可）後再執行重啟 / reload。

**排程建議**：將 shadowdns 的重啟 / reload（含 zone 推送觸發的 SIGHUP）排在 ACME 憑證續期窗口之外——ACME client 的續期排程通常可固定時段（例如 certbot 的 systemd timer），制度上即可消除互相干擾的機會。固定窗口建立後，例行 zone 推送的 SIGHUP 不需逐網域 dig 確認；上述確認清單主要用於計畫外的重啟 / reload。

### 重啟成本與 Rolling Restart SOP

**哪些變更走 SIGHUP、哪些需要重啟**

現行版本中，絕大多數設定變更都由 SIGHUP reload 套用，需要 full restart 的只剩三類：

| 變更類型 | 套用方式 |
|----------|----------|
| Zone 資料（zone 檔、`master.zones`） | SIGHUP reload |
| GeoIP mmdb 更新 | SIGHUP reload |
| RRL（rate-limit）設定 | SIGHUP reload |
| Query log 路徑 / 選項（`logging{}`） | SIGHUP reload |
| `shadowdns.yaml` 的 `aliases:` 區段 | SIGHUP reload |
| **`shadowdns.yaml` 的 `ephemeral_api:` 區段** | **full restart**（API server 只在啟動時依當下設定建立一次；reload 不會重讀 listen / allow / token，也不會啟動或停止 API） |
| **任何 CLI flag**（如 `--log-file`、`--listen`、`--metrics-addr`） | **full restart**（flags 為 process-lifetime sticky，systemd unit 修改後需 `daemon-reload` + restart） |
| **`listen-on` / `listen-on-v6` 位址變更** | **full restart**（reload 會偵測 drift 並 log 提示，但刻意不重綁 socket，見「SIGHUP reload 不會重綁 listener」一節） |

**重啟的效能成本**

重啟後的 ShadowDNS 處於冷啟動狀態：在 dnspyre benchmark 中觀察到**重啟後的首輪壓測 QPS 約低 30%**，隨後回穩。此為 benchmark 觀察值（Go runtime 暖機、OS page cache 等因素），**不是服務容量保證**，但容量規劃與重啟排程仍應假設重啟後短時間內尖峰處理能力下降。

**Rolling Restart SOP**

前提：**生產部署至少 2 台實例**。單實例部署沒有 rolling 的空間，任何重啟都是服務中斷。

1. 把需要重啟的設定變更**批次化**：累積到維護窗口一次套用，而不是每改一個 flag 就重啟一輪。
2. 完成「Ephemeral DNS-01 記錄揮發性」一節的操作前確認清單後，從第一台開始，移出流量（若前端有 LB / anycast 撤告）後重啟：

   ```bash
   sudo systemctl restart shadowdns
   ```

3. 確認該台健康（本檢查序列也供升級 / 回滾 SOP 引用）：

   ```bash
   # process 存活
   systemctl is-active shadowdns

   # 無 ERROR log（無輸出 = 通過；log 檔跨重啟累積，注意排除重啟時間點之前的歷史行）
   sudo tail -n 200 /var/log/shadowdns/shadowdns.log | grep ERROR

   # 應答正常
   dig @127.0.0.1 example.com SOA +short
   ```

4. 等該台 QPS 回到重啟前基準（觀察監控的 QPS 曲線，或比對 `rate(shadowdns_dns_requests_total[1m])` 與重啟前水位）後，再對下一台執行步驟 2–3。
5. 全部完成後，以監控確認整體 QPS、錯誤率回到變更前基準。

### 升級與回滾 SOP

**v0.x.x 是實驗階段：每次升版都假設有 breaking CLI / config 變更**（flag 改名、設定 schema 調整、預設值改變），因此 `--dry-run` 驗證是**強制步驟**。

**標準升級流程（逐台執行）**

1. 下載新版 `.deb` 套件，並**保留現行版本的 `.deb`** 供回滾（先記下目前版本）：

   ```bash
   shadowdns --version   # 記錄回滾基準
   ```

2. 先把新版 binary 解包到暫存目錄（不安裝、不影響運行中的服務），以新 binary 對**現行服務實際使用的設定路徑**執行 `--dry-run` 驗證（路徑以 `systemctl cat shadowdns` 中 `ExecStart` 的 flag 為準；套件預設為 `/etc/shadowdns/`）：

   ```bash
   dpkg-deb -x shadowdns_<new-version>_amd64.deb /tmp/shadowdns-new
   /tmp/shadowdns-new/usr/bin/shadowdns --dry-run \
       --named-conf /etc/shadowdns/named.conf \
       --config     /etc/shadowdns/shadowdns.yaml
   ```

   `--dry-run` 語意見 [docs/benchmark.md](benchmark.md)。任何 parse error、不支援的 flag、schema 不相容都在這一步暴露。**dry-run 失敗就停止升級**——因為尚未安裝任何東西，不需要回滾，直接調查相容性問題。

3. dry-run 通過後安裝新套件，依 Rolling Restart SOP 逐台套用（步驟 2–4：Ephemeral 確認清單、重啟、健康檢查、等 QPS 回穩）：

   ```bash
   sudo dpkg -i shadowdns_<new-version>_amd64.deb
   sudo systemctl restart shadowdns
   ```

4. **回滾**（任一台啟動失敗或行為異常時）：

   ```bash
   sudo dpkg -i shadowdns_<previous-version>_amd64.deb
   sudo systemctl restart shadowdns
   ```

   重啟後跑一次 Rolling Restart SOP 步驟 3 的健康檢查，驗證無誤後再回頭調查新版的失敗原因。已升級成功的其他實例可暫時維持新版（確認新舊版可並存服務），或一併回滾以維持版本一致——依故障性質判斷。

### 持續性答案一致性回歸驗證

Phase 1 步驟 3 的 answer-diff 比對不應在切換完成後束之高閣——它是常態維運工具，**每次 zone 變更推送後都應執行**，比對兩台實例（熱備援期間為 BIND vs ShadowDNS；之後為新舊版 ShadowDNS、或任兩台應該一致的實例）對相同查詢的回應差異：

```bash
# 推送 zone 變更並確認兩台都完成 reload 後，比對受影響網域的回應
diff \
  <(dig @<instance-a-ip> example.com A +short | sort) \
  <(dig @<instance-b-ip> example.com A +short | sort)
```

比對範圍建議覆蓋本次變更的 zone 及其 backup / alias 網域，record type 比照 Phase 1 步驟 4（A、AAAA、CNAME、NS、MX、TXT、SOA）；比對 SOA 時可先以 `awk '{$3=""; print}'` 拿掉 serial 欄位，避免 reload 時間差造成的假差異。

**特別注意 alias / CNAME flattening**：ShadowDNS 的 backup 網域改寫邏輯（owner name 與 RDATA rewrite）是與 BIND 行為差異最大的部分，邊界 case（深層 CNAME 鏈、跨 zone 指向、萬用字元記錄與 alias 的交互）可能產生與預期不同的回應。任何 answer-diff 差異都**不應**直接視為雜訊放行——先確認是已知的可接受差異（如前述 SOA serial 時間差），否則一律主動調查。

### Query Log 磁碟管理

授權 DNS server 的 query log **每一筆查詢一行**——生產 QPS 數千的環境，單日 log 可達數 GB。未受控的 query log 是磁碟爆滿、進而影響服務的常見肇因。

**查核 logrotate 設定**

`.deb` 套件安裝的 logrotate 設定位於 `/etc/logrotate.d/shadowdns`（內容以 repo 的 `packaging/logrotate.shadowdns` 為準；預設每日輪替、保留 14 份、壓縮，輪替後以 SIGUSR1 通知 ShadowDNS 重開 log 檔）。以乾跑模式驗證：

```bash
sudo logrotate -d /etc/logrotate.d/shadowdns
```

輸出會列出匹配的 log 檔與將執行的動作但不實際輪替；確認 `/var/log/shadowdns/*.log` 有被匹配、輪替策略如預期。

**依實際查詢量調整輪替頻率**

預設「每日 × 14 份」是通用起點，不是容量承諾。上線後以實際量校準：觀察單日 log 大小（`du -sh /var/log/shadowdns/`），推算 14 天保留量是否在磁碟預算內；量大時改 `hourly` 或縮短保留份數，量小時可拉長保留期換取更長的查詢回溯窗口。

**Query log 與應用層 log 是兩條流**

- **Query log**：每筆 DNS 查詢的記錄，路徑由 `logging{}` 設定決定，受上述 logrotate 管理。
- **應用層 log**：啟動、reload、錯誤等程式事件，寫往 `/var/log/shadowdns/shadowdns.log`（讀取需 sudo）。排查服務異常（reload 失敗、bind 失敗、panic）看這裡，不要在 query log 裡撈。

---

## 緊急聯絡

DNS Ops on-call 聯絡方式請洽團隊內部 wiki。

發現生產問題時，優先執行對應 Phase 的 Rollback 策略，確保服務恢復後再進行根因分析。
