# CNAME 鏈收合

ShadowDNS 可以在回應中收合 zone 內的 CNAME 鏈：不再把鏈上每一條中間 CNAME 紀錄送出，而是由伺服器內部消耗整條鏈、只回答最終結果。中間跳點的名稱——內部 load balancer、pool 成員、路由層——永遠不會出現在線路上，zone 內部的命名因此得以保密。

收合是**以 alias group 為單位的 opt-in**（`shadowdns.yaml` 的 `collapse_cname_chain`，預設 `false`）。flag 關閉時，鏈的輸出與 BIND 以及先前版本的 ShadowDNS byte-level 一致，包含紀錄順序與 TTL。

!!! note "這不是 apex CNAME Flattening"
    本功能收合的是「合法持有 CNAME 的名稱」*在回應中*的鏈，並不允許 CNAME 與 SOA/NS 共存於 zone apex（即部分託管 DNS 服務提供的 ANAME / ALIAS / "CNAME flattening"）。apex flattening 是另一個仍在規劃中的獨立功能——見[功能比較表](../index.md#與-bind-的功能比較)。

---

## 啟用方式

在 `shadowdns.yaml` 的 alias group 上設定 flag：

```yaml
aliases:
  example.com:
    members:
      - example.net
    collapse_cname_chain: true
```

- flag 宣告在 **root** domain 上，群組內所有備援成員無條件繼承——對 `example.net` 的查詢與對 `example.com` 的查詢收合行為完全一致。
- 省略或 `false` 即關閉。未知欄位在載入時會被拒絕（strict decoding），打錯字會讓啟動失敗、或讓 SIGHUP 保留前一份設定，而不是默默停用功能。
- flag 參與既有的 [SIGHUP 熱重載](../configuration/shadowdns-yaml.md#sighup-熱重載)：新增或移除都隨設定快照原子性生效。

!!! warning "降版前先回滾設定"
    舊版 ShadowDNS 二進位會拒絕未知的 YAML 欄位。要降版到引入本功能之前的版本，請先從 `shadowdns.yaml` 移除 `collapse_cname_chain`、SIGHUP，再降版。

---

## 統一收合規則

當收合對命中的 zone 啟用、且查詢的解析走進一條 CNAME 鏈時，伺服器會消耗所有停留在同一 zone 內的跳點，並依鏈尾狀態回答：

| 鏈尾形態 | 回應 |
|---|---|
| zone 內存在查詢型別的紀錄 | **只回最終紀錄。** owner = 查詢名稱（保留 on-wire 大小寫）、TTL = 被消耗鏈（含最終紀錄）的最小值。回應中不含任何 CNAME。 |
| 鏈離開 zone（或深度預算耗盡） | **恰好一條合成 CNAME**：owner = 查詢名稱、target = 第一個未解析的名稱、TTL = 鏈最小值。其他 zone 內名稱一概不出現。 |
| zone 內名稱但沒有查詢型別（含 dangling target） | **NODATA**——NOERROR 且 authority section 帶 zone SOA。被消耗的鏈不會輸出，也不會退回對查詢名稱做 wildcard 合成。 |

TTL 實例——zone 內有以下紀錄時：

```
www.example.com.     300  IN CNAME  lb.example.com.
lb.example.com.       60  IN CNAME  pool-a.example.com.
pool-a.example.com.  600  IN A      192.0.2.10
```

查詢 `www.example.com. A` 的回應恰好是：

```
www.example.com.  60  IN  A  192.0.2.10
```

（TTL = min(300, 60, 600)；`lb` 與 `pool-a` 完全不出現在回應中）。同一查詢在 flag 關閉時回完整的三條紀錄。

鏈的範圍是單一 zone：target 指向任何其他 zone——即使是 ShadowDNS 自己服務的 zone——都視為出境，回合成 CNAME。

---

## 直查 CNAME 與中間名稱

- `qtype=CNAME` 的查詢永遠不會洩漏儲存的 target。在統一規則下 CNAME 紀錄一律是跳點，所以結果只可能是合成的鏈尾 CNAME（鏈出境）或 NODATA（鏈於 zone 內走到底）。
- 中間跳點名稱仍可被直接查詢——存在性不被隱藏——但其回應同樣套用收合規則。查詢上例的 `lb.example.com. A` 會回 `lb.example.com. 60 IN A 192.0.2.10`。
- 從 wildcard 合成 CNAME 起始（或途經）的鏈，收合行為相同。

---

## 備援 zone 查詢

對備援成員的查詢，收合後的答案帶備援 namespace 的 owner（on-wire 查詢名稱），RDATA 名稱欄位仍套用該群組既有的改寫規則：

- 最終紀錄套用 in-bailiwick / `rewrite_rdata_labels` 的 RDATA 改寫，與未收合的答案完全一致。
- 合成鏈尾 CNAME 的 target 比照現行儲存型 CNAME target 的處理：群組設定 `rewrite_rdata_labels: true` 時套 label-anywhere 改寫（templated CDN 式 target），否則套 in-bailiwick 後綴規則。

因此對上例的鏈查詢 `www.example.net. A`，答案是 `www.example.net. 60 IN A 192.0.2.10`。

---

## 邊界情況

- **深度預算**：一次追鏈最多消耗 8 條 CNAME 紀錄（與未收合的追鏈共用同一上限）。更長的鏈——或 zone 資料中的 CNAME 迴圈——會合成一條指向第一個未解析名稱的 CNAME，讓 client 得以續查。迴圈情況下可能產生自指紀錄（owner = target）；這是 zone 設定錯誤下的已知產物，由 client 端 resolver 自身的追鏈上限終止。
- `ANY` 等 **meta-qtype** 套用同一規則，於鏈尾以 NODATA 收場。
- **Zone transfer 永不收合**：無論 flag 與否，AXFR 都照常傳輸 zone 的原始紀錄，由既有的 `allow-transfer` ACL 把關。

---

## 用 dig 驗證

```bash
# 收合後的最終答案：單條 A、無 CNAME、TTL 為鏈最小值
dig @192.0.2.53 www.example.com A

# 備援成員繼承 root 的 flag
dig @192.0.2.53 www.example.net A

# 鏈尾沒有 AAAA：NODATA（NOERROR + authority 帶 SOA）
dig @192.0.2.53 www.example.com AAAA

# 直查 CNAME（鏈在 zone 內走到底）：NODATA，不洩漏儲存的 target
dig @192.0.2.53 www.example.com CNAME
```

---

## 維運備註

- 取鏈最小 TTL 與主要託管 DNS 服務商的收斂行為一致，也是保守的選擇：任何下游快取答案的時間都不會超過鏈上任一環節原本允許的時間。
- 收合以查詢時每跳一次的記憶體查表換取更小的回應；flag 關閉的路徑完全不變，並經基準測試確認無效能回歸。
- `aliases` 欄位的完整參考見 [shadowdns.yaml](../configuration/shadowdns-yaml.md)，收合後備援答案流經的改寫管線見 [Zone Aliasing 原理](zone-aliasing.md)。
