# named.conf 相容性

ShadowDNS 直接讀取既有的 BIND `named.conf`，不需要轉換格式。本頁說明支援的指令範圍、view 比對語意、RRL 與 query logging 設定，以及會被拒絕的指令。

## 支援的 options 指令

`options` 區塊支援：`directory`、`geoip-directory`、`listen-on`、`listen-on-v6`、`allow-transfer`、`recursion`、`minimal-responses`、`version`、`hostname`、`transfer-format`、`notify`。

`geoip-directory` **只在使用 geo 規則時必填**：只要任一 view 的 `match-clients` 含 `geoip country` / `geoip asnum` 規則就必須設定，違反時啟動失敗，錯誤訊息會指出第一個違規的 view。有設定時（即使沒有 geo 規則）mmdb 檔案照常載入與驗證；省略 `geoip-directory` 與設為空字串（`geoip-directory "";`）等價，皆視為未設定。詳見 [GeoIP 資料庫](geoip.md)。

### listen-on（IPv4）

- 支援 `listen-on { any; };` 與明確的 IPv4 位址清單，採逐位址綁定。
- 個別位址綁定失敗（例如某個 `127.0.0.x` alias 被 `systemd-resolved` 佔用）只記 WARN 後跳過；只要至少一個 listener 綁定成功，伺服器即可啟動。
- `--listen` flag 與 `listen-on` 的優先序規則詳見[從 BIND 遷移](../migration.md)。

### listen-on-v6（IPv6）

- 與 IPv4 相同的逐位址綁定模型。
- 支援的 token：`any`（列舉本機 IPv6 介面位址，排除需要 zone index 的 link-local `fe80::/10`，但包含 loopback `::1`）、`none`、明確的 IPv6 位址字面值（如 `2001:db8::1`）。
- IPv6 為 **opt-in**：沒有 `listen-on-v6` 區塊就不開 IPv6 listener，純 IPv4 部署不受影響。
- 不支援的 token（IPv4 字面值、排除語法 `!addr`、ACL 名稱、`port N`）記 WARN 後跳過，不會導致啟動失敗。

## View 與 match-clients

```text
view "<name>" {
    match-clients { ... };
    ...
};
```

- 採 **first-match** 語意（與 BIND 相同）：依宣告順序由左至右評估，第一條命中的規則決定 view。
- 沒有任何 view 命中時回應 **REFUSED**。
- 支援的規則類型：

| 規則類型 | 範例 |
|----------|------|
| GeoIP country | `geoip country TW` |
| GeoIP ASN | `geoip asnum "AS64500 Example ISP"` |
| 單一 IPv4 位址 | `192.0.2.10` |
| IPv4 CIDR | `198.51.100.0/24` |
| 任意來源 | `any` |

!!! warning "`any` view 必須宣告在最後"
    `match-clients` 含 `any;` 的 view 會命中**所有** client。若它排在更精確的 view（如 GeoIP view）之前，後者永遠不會被評估。ShadowDNS 啟動時會對「非最後一個 view 使用 `any`」記 WARN，但不會阻止啟動。

!!! warning "ASN 描述字串格式"
    `geoip asnum` 的字串必須符合 `"AS<數字> <描述>"` 格式（解析規則為 `^AS(\d+)\s`），描述文字會被忽略。不以 `AS` + 數字 + 空白開頭的字串（例如缺少 `AS` 前綴的 `"64500"`）會導致啟動失敗。

## Response Rate Limiting（RRL）

RRL 透過 BIND 相容的 `rate-limit { ... }` 區塊設定，**只支援放在全域 `options` 內** —— 放在 `view` 區塊內會被警告並忽略（v1 不支援 per-view rate limiting）。

RRL 只套用於 **UDP 回應**；TCP 回應永不限速。

支援的子選項（預設值與 BIND 一致）：

| 子選項 | 說明 |
|--------|------|
| `responses-per-second` | 每個 client prefix 的最大回應速率 |
| `referrals-per-second` | 僅為 BIND 相容性而解析；永不觸發（ShadowDNS 為純權威伺服器，不發 referral） |
| `nodata-per-second` | NODATA 回應速率上限 |
| `nxdomains-per-second` | NXDOMAIN 回應速率上限 |
| `errors-per-second` | 錯誤回應（SERVFAIL、REFUSED 等）速率上限 |
| `all-per-second` | 跨所有回應類別的全域上限 |
| `window` | 追蹤視窗（秒） |
| `slip` | 被限速的回應中，以 truncated 回覆取代直接丟棄的比例 |
| `ipv4-prefix-length` | client 分組用的 IPv4 prefix 長度 |
| `ipv6-prefix-length` | client 分組用的 IPv6 prefix 長度 |
| `exempt-clients` | 豁免限速的 client ACL |
| `log-only` | 只記錄不實際丟棄 |
| `max-table-size` | 追蹤的 client prefix 數量上限 |
| `min-table-size` | table 最小配置大小 |

`qps-scale` **不支援**，會被警告並忽略。

## Query logging（BIND 格式）

ShadowDNS 解析標準的 `logging{}` 區塊（`channel` 的 `file`/`severity`/`print-*` 加上 `category queries`），對每筆完成 view 比對的查詢，以 BIND queries category 的**完全相同格式**寫入一行 —— 既有的下游 log 解析器不需任何修改。

- 輪替交由 logrotate + SIGUSR1 處理；BIND 內建的 `versions`/`size` 參數會被警告並忽略。
- SIGUSR1 會連同 `--log-file` 一起重開 query log 檔。
- SIGHUP reload 會重新套用 `logging{}` 變更：路徑與 `print-*` 選項的修改不需重啟即可生效。

## 不支援／會被拒絕的指令

| 指令 | 行為 |
|------|------|
| `type slave`、`type forward` zone | 啟動時 fatal error |
| `allow-update`、`dnssec-enable` | 啟動時拒絕 |
| `rate-limit` 於 view 內 | 警告並忽略 |
| `qps-scale` | 警告並忽略 |

Recursion 永遠關閉（`recursion no` 恆為有效），ShadowDNS 是純權威伺服器。

## 範例

```text
options {
    directory           "/etc/namedb";
    geoip-directory     "/usr/local/share/GeoIP/";
    listen-on           { any; };
    listen-on-v6        { none; };
    recursion           no;
    minimal-responses   yes;
    version             none;
    hostname            none;
    allow-transfer      { 192.0.2.10; 192.0.2.11; };
};

include "master.zones";
```

Zone file 採 RFC 1035 master file 格式（`$TTL`、`$ORIGIN`、`@`、跨行 `(...)`、`;` 註解），並支援 `$INCLUDE` / `$include` 指令（裸路徑與 BIND 式雙引號路徑皆可，指令名稱不分大小寫）。限制：路徑本身**不可包含空白**（miekg/dns scanner 的底層限制，加引號也無法繞過），且引號形式只在最上層 zone file 有效 —— 經 `$INCLUDE` 拉進來的片段由底層 parser 直接讀取，內部必須使用裸路徑形式。
