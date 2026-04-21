# 從 BIND 遷移到 ShadowDNS

本文件為 DNS Ops 團隊提供將 BIND master 替換為 ShadowDNS 的操作指引，涵蓋環境前置條件、四階段切換步驟、Rollback 策略、監控檢核清單，以及常見問題。

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
| `aliases.yaml` 產出機制確認 | 管理系統能自動產出 `aliases.yaml`，或已評估手動維護成本 | ☐ |
| 監控系統覆蓋兩端 | 監控系統可同時觀測 BIND 與 ShadowDNS 的 query QPS、錯誤率、記憶體 | ☐ |
| Rollback 程序已演練 | 團隊熟悉各 Phase 的回退流程（見下方 Rollback 策略） | ☐ |

---

## 四階段切換步驟

### Phase 0：開發與測試（本 change 範圍內）

**目標**：確認 ShadowDNS 在受控環境下能正確處理生產規模的設定檔與 zone 資料，且記憶體用量符合預期。

**執行步驟**：

1. 複製一份生產用 `named.conf`、`master.zones`、zone file 目錄到測試環境。
2. 準備 `aliases.yaml`（初始可手動整理，或讓管理系統在測試環境產出）。
3. 建置 ShadowDNS binary：

   ```bash
   go build -o shadowdns ./cmd/shadowdns
   ```

4. 執行啟動煙霧測試（待 `--dry-run` flag 完成後使用），確認設定解析無錯誤：

   ```bash
   ./shadowdns \
       --named-conf /path/to/named.conf \
       --aliases    /path/to/aliases.yaml
   ```

5. 觀察啟動 log，確認：
   - 所有 view 與 zone 均成功載入
   - 無 `fatal` 或 `unsupported directive` 錯誤
   - GeoIP mmdb 載入成功

6. 以 `dig` 對 ShadowDNS 測試實例發送代表性查詢，驗證 root zone 與 backup zone 回應正確：

   ```bash
   # 查詢 root zone A record
   dig @<shadowdns-ip> example.com A

   # 查詢 backup zone（應得到與 root 相同的 IP，owner 為 backup.com）
   dig @<shadowdns-ip> backup-example.com A

   # 查詢 SOA（backup zone 的 serial 應與 root 一致）
   dig @<shadowdns-ip> backup-example.com SOA
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
       --aliases    /etc/namedb/aliases.yaml \
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
     <(dig @<bind-ip>      backup-example.com A +short) \
     <(dig @<shadowdns-ip> backup-example.com A +short)
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

**目標**：讓管理系統正式產出 `aliases.yaml`，逐台將 BIND slave 的 master 指向 ShadowDNS，驗證 AXFR 同步正確。

**執行步驟**：

1. 管理系統開始正式產出 `aliases.yaml` 並同步至 ShadowDNS 設定目錄。

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
   dig @<staging-slave-ip> backup-example.com AXFR
   ```

6. staging slave 驗證通過後，逐台對生產 slave 執行相同切換流程（每台切換後觀察至少 24 小時無異常再繼續）。

**驗收條件**：

- 所有 slave 均成功完成 AXFR，無 transfer failure
- Slave 上的查詢結果與 Phase 1 基準一致
- `aliases.yaml` 管理流程穩定，無遺漏 domain

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
| 啟動 log 錯誤 | ShadowDNS log（`level=ERROR`） | 正常運行時應無錯誤 log |

### GeoIP 指標

| 指標 | 觀察方式 | 預期行為 |
|------|----------|----------|
| 每 view 的 query 分布 | Query log 抽樣統計 | 與 BIND 基準的 view 分布接近（允許小幅差異） |
| 無 view 匹配（REFUSED）比例 | DNS server log | 不應異常升高；升高表示 GeoIP 資料或規則有問題 |

**GeoIP 抽樣方法**：從 ShadowDNS query log 中抽取 1000 筆，統計各 view 的 query 數量，與 BIND 相同時段的 view 分布比對。差異超過 5% 應進一步調查 mmdb 版本是否一致。

---

## 常見問題

**Q：切換後某個 backup domain 解到錯誤的 IP**

檢查 `aliases.yaml` 中該 backup domain 的對應是否正確（即指向正確的 root domain）。確認對應的 root domain 在 ShadowDNS 中已正確載入：

```bash
dig @<shadowdns-ip> <root-domain> A
dig @<shadowdns-ip> <backup-domain> A
```

若兩者 RDATA 不一致，表示 root zone 資料與 `aliases.yaml` 的對應有誤，或 root zone 資料本身有問題。

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

1. 確認 `aliases.yaml` 有完整列出所有 backup domain，沒有 backup domain 被誤當成 root 全量載入。
2. 用 `ps aux` 或 `cat /proc/<pid>/status | grep VmRSS` 觀察 RSS（常駐記憶體），避免與 VSZ（虛擬記憶體）混淆。

---

## 監聽位址行為（Listen address behavior）

ShadowDNS 讀取 `named.conf` 的 `listen-on` 指令，決定要綁在哪些 IP 上。行為與 BIND9 相容：**每個位址各開一個 socket**，單一位址 bind 失敗（例如 systemd-resolved 已佔住 `127.0.0.53:53`）時只會 log WARN 並繼續，不會讓整個 server 起不來。

### 位址來源優先順序

| 情境 | `--listen` | `listen-on` | 實際綁定 |
|------|------------|-------------|----------|
| 預設 | `:53` | 未指定 | 所有 IPv4 介面位址（隱含 `any`） |
| 預設 + 指定 listen-on | `:53` | `{ 10.0.0.1; 10.0.0.2; }` | `10.0.0.1:53`、`10.0.0.2:53` |
| Override | `127.0.0.1:5353` | 任意 | `127.0.0.1:5353`（忽略 listen-on） |
| Port hint | `:5353` | `{ 10.0.0.1; }` | `10.0.0.1:5353`（port 從 `--listen` 繼承） |

**關鍵規則**：`--listen` **有 host component 才是 override**（例如 `127.0.0.1:5353`）；`:PORT` 形式只提供 port，位址仍從 `listen-on` 取得。這讓 `--listen :0`（測試用 ephemeral port）+ `listen-on { 127.0.0.1; }` 能正確配合。

### 不支援的 listen-on 語法

下列 BIND 語法目前會被 log WARN 並跳過，不會影響解析：

- Exclusion syntax：`listen-on { !10.0.0.1; any; };`（`!addr` 排除）
- ACL 參照：`listen-on { trusted-net; };`
- Port override：`listen-on port 5353 { ... };`（請改用 `--listen :5353`）
- `interface` keyword

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

若 reload 後 `listen-on` 改變，ShadowDNS **不會**重新開 socket。這是刻意的設計，避免 reload 造成短暫的 port 接手空窗。偵測到變動時會 log：

```
level=INFO msg="reload: listen-address changes require restart to take effect"
  current_bound=[10.0.0.1:53, 127.0.0.1:53]
  new_resolved=[10.0.0.2:53]
```

執行 `systemctl restart shadowdns` 才會套用新的監聽位址。

### BREAKING 行為差異（與 v0.3.0 之前比較）

- 預設綁定從「單一 `0.0.0.0:53` wildcard socket」改為「per-address bind」。視覺上的差別：啟動 log 從 1 筆變成 N 筆 `listener bound`。
- 新增網卡 / IP alias 不會自動被 pick up；BIND 的 `interface-interval` 動態掃描本版不支援，請用 `systemctl restart shadowdns` 讓新位址進入監聽集合。
- `--listen` 語意從「綁定目標」改為「override hint + port hint」。若你之前寫 `--listen :53` 是期望 `0.0.0.0` wildcard 行為，現在會被當成「port hint，位址從 listen-on 取（或 any 展開）」——行為在大多數情況一致，但顯式 log 會不同。

---

## 緊急聯絡

DNS Ops on-call 聯絡方式請洽團隊內部 wiki。

發現生產問題時，優先執行對應 Phase 的 Rollback 策略，確保服務恢復後再進行根因分析。
