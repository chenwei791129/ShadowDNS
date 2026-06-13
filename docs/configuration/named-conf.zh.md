# named.conf 相容性

ShadowDNS 直接讀取既有的 BIND `named.conf`，不需要轉換格式。本頁說明支援的指令範圍、view 比對語意、RRL 與 query logging 設定、分層容忍契約，以及會被拒絕的指令。

## 分層容忍契約

ShadowDNS 是 BIND `named.conf` 的 **drop-in** 讀取器：只要設定語法合法就能載入，並以四個層級之一回應每個構造，讓行為可預測、不必靠試誤摸索。其指導原則是 **fail-closed** —— ShadowDNS 無法評估的構造，永遠不會被允許**放寬**存取。

| 層級 | ShadowDNS 的處理 | 代表性構造 |
|------|------------------|------------|
| **Silent**（DEBUG／不記錄） | 消化並忽略該構造，不產生操作者可見的雜訊 | 頂層或 view 範圍內未識別的非存取控制、非控制平面指令（`masters`、`dnssec-enable`……）；`zone` 區塊內的存取控制指令（跳過，仍不強制）；經由內建 channel（`default_syslog`、`null`……）停用 query logging |
| **INFO** | 跳過並記錄一筆 INFO —— 純資訊性，無需動作 | recursion 家族指令（`recursion`、`forwarders`、`dnssec-validation`）；`type` 非 `master` 的 zone（被丟棄，`file` 永不開啟） |
| **WARN** | 跳過／丟棄並記錄 WARN —— 建議檢視 | **top level 或 view 範圍**的存取控制指令（`allow-query`、`allow-recursion`、`allow-transfer`、`allow-update`、`allow-notify`、`blackhole`），附「does not enforce」訊息；ShadowDNS 未實作的控制平面／安全指令（`controls`、`key`、`server`、`statistics-channels`、`trusted-keys`、`managed-keys`、`trust-anchors`），附「has no effect」訊息；未知的 `match-clients` token（丟棄，fail-closed）；未定義或成環的 `acl` 參照（fail-closed）；以保留內建名（`any`／`none`／`localhost`／`localnets`）定義的 `acl`（忽略 —— 參照解析為內建）；與結構性關鍵字（`view`、`zone`、`options`、…）僅差一個編輯距離的指令（視為打字錯誤，跳過並附建議更正）；重複的 `acl` / `options` / 頂層 zone 名稱（最後一筆為準）；不支援的 `listen-on` token；`qps-scale` 與 view 範圍的 `rate-limit`；非最後一個的 `any` view |
| **fail-closed（fatal）** | 中止啟動並指出違規的檔案與行號 | 真正的語法錯誤（括號不對稱、缺終止 `;`、未封閉區塊）；格式錯誤的 `geoip asnum`（無前綴 `AS<數字>`）；非 2 字母 ISO 3166-1 的 `geoip country` 碼；view 使用 `geoip` 規則但未設 `geoip-directory`；view 區塊與頂層 zone 混用 |

### Fail-closed doctrine

fail-closed doctrine 專門規範 WARN 層：當 `match-clients` 清單（或 `acl` body）的某個存取控制元素無法評估 —— 未知 token、參照到未定義的具名 ACL、或參照成環 —— **正向**元素會被丟棄並視為**永不命中**，絕不視為**匹配所有人**。**否定**的無法評估元素（例如 `!undefined-acl`）則改以一個「拒絕整個清單」的標記取代：若直接丟棄，後續的 `any` 就會匹配所有人，因此改讓它失去的排除語意使清單收斂（整個清單拒絕）而非放寬。同樣的取代也適用於**否定參照所指的 acl 解析後變成空清單**的情況 —— 無論該 acl 是宣告為空，或因其成員自身被 fail-closed 丟棄而變空 —— 因為一個被悄悄丟棄的排除同樣會放寬清單。所以完全由無法評估的元素組成的 view 不服務任何 client，而非匹配所有人。這保證了 `match-clients` 清單中的一個打字錯誤，永遠不會悄悄把受限 view 暴露給所有 client。

ShadowDNS **唯一會強制**的存取控制指令是 **options 範圍的 `allow-transfer`**，它就是 AXFR ACL（scope 區分見[從 BIND 遷移](../migration.md#存取控制模型差異)）。view 或 zone 範圍的 `allow-*` 指令落在上述 WARN 層，不會被強制。

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

- 採 **first-match** 語意（與 BIND 相同）：address-match-list 依宣告順序評估，**第一個命中**的元素決定結果 —— 正向元素選中該 view，否定元素（`!`）命中則**拒絕**該 view（評估落到下一個 view）。若沒有任何元素命中，則不選中該 view。
- 沒有任何 view 命中時回應 **REFUSED**。
- 支援的元素形式：

| 元素 | 範例 | 比對對象 |
|----------|------|------|
| GeoIP country | `geoip country TW` | geo 查詢位址的國別 |
| GeoIP ASN | `geoip asnum "AS64500 Example ISP"` | geo 查詢位址的 AS 編號 |
| 單一 IPv4 位址 | `192.0.2.10` | 來源 IP |
| IPv4 CIDR | `198.51.100.0/24` | 來源 IP |
| 具名 acl 參照 | `internal` | 被參照的 `acl` 所比對的內容 |
| 巢狀群組 | `{ 192.0.2.0/24; 198.51.100.0/24; }` | 群組自身的有序清單 |
| 否定 | `! 192.0.2.0/24` | 反轉：命中的 client 會被**拒絕** |
| `any` | `any` | 所有 client（catch-all） |
| `none` | `none` | 不命中任何 client |
| `localhost` | `localhost` | 伺服器自身的位址 |
| `localnets` | `localnets` | 伺服器各介面所屬的網段 |

GeoIP country/ASN 元素比對 geo 查詢位址（有 [ECS](../guides/ecs.md) 衍生位址時用它，否則用來源 IP）；其餘所有元素一律比對傳輸層來源 IP，因此偽造的 ECS 位址永遠無法滿足 IP/CIDR/具名 acl 規則。

!!! warning "`any` view 必須宣告在最後"
    `match-clients` 含 `any;` 的 view 會命中**所有** client。若它排在更精確的 view（如 GeoIP view）之前，後者永遠不會被評估。ShadowDNS 啟動時會對「非最後一個 view 使用 `any`」記 WARN，但不會阻止啟動。

!!! warning "ASN 描述字串格式"
    `geoip asnum` 的字串必須符合 `"AS<數字> <描述>"` 格式（解析規則為 `^AS(\d+)\s`），描述文字會被忽略。不以 `AS` + 數字 + 空白開頭的字串（例如缺少 `AS` 前綴的 `"64500"`）會導致啟動失敗。

!!! warning "國別碼格式"
    `geoip country` 碼必須是 2 字母的 ISO 3166-1 alpha-2 碼（例如 `TW`、`US`），比對時不分大小寫。非恰好兩個字母的碼（例如 `usa`、數字、CIDR）會導致啟動失敗，而非退化成永遠不命中的規則。

### 具名 ACL

用頂層 `acl` 區塊定義可重用的 client 群組，再於任一 view 的 `match-clients`（或另一個 `acl`）以名稱參照：

```text
acl "internal" {
    10.0.0.0/8;
    192.0.2.0/24;
};

view "internal" {
    match-clients { internal; };
    // ...
};

view "external" {
    match-clients { ! internal; any; };   // 除 internal 以外的所有人
    // ...
};
```

- `acl` body 使用與 `match-clients` **相同的元素文法** —— 包含 `geoip` 規則、`!` 否定、巢狀群組、內建 ACL，以及對其他具名 ACL 的參照。
- 參照會解析為被參照 acl 的清單並遞迴評估；前綴 `!` 會否定整個參照。
- **未定義的參照採 fail-closed**：正向參照到沒有 `acl` 定義的名稱時會被丟棄並記 WARN 且永不命中 —— 該 view 不服務任何 client，而非匹配所有人。**否定**的未定義參照（`!name`）則以「拒絕整個清單」取代而非丟棄，使它所表達的排除語意不會悄悄放寬存取。否定參照所指的 acl 解析後為**空**清單時（宣告為空，或因成員自身被丟棄而變空），也以相同方式、基於相同理由取代。
- 參照**環**（`a` → `b` → `a`）會被斷開並記 WARN。
- **重名**的 `acl` 以**最後一筆**定義為準並記 WARN。

!!! note "`localhost` / `localnets` 於載入時解析"
    `localhost`（伺服器自身位址）與 `localnets`（直接連接的網段）兩個內建 ACL，會在載入設定時從主機網路介面列舉展開，並於每次 reload 重新列舉。

## 無 view 形態（隱含 `_default` view）

ShadowDNS 不要求設定任何 `view` 區塊。你可以在所有 `view` 區塊之外，於 `named.conf` 或其任一 `include` 檔中直接宣告頂層 zone。

在 Debian/Ubuntu 上，設定慣例會拆成 `named.conf` / `named.conf.options` / `named.conf.local` 的 include 結構。最上層的 `named.conf` 只負責拉進另外兩個檔：

```text
// named.conf
include "named.conf.options";
include "named.conf.local";
```

```text
// named.conf.options
options {
    directory   "/etc/bind";
    listen-on   { any; };
    recursion   no;
};
```

```text
// named.conf.local
zone "example.com" {
    type master;
    file "db.example.com";
};

zone "example.net" {
    type master;
    file "db.example.net";
};
```

此處的 `directory "/etc/bind"` 為 Debian 慣用（權威 zone 檔放設定同層）。

頂層 zone 的 zone-body 規則與 view 內 zone **完全相同**：僅支援 `type master`，且相對 `file` 路徑沿用解析期的語意。

!!! warning "請將 `options` 置於頂層 zone 宣告之前"
    請將 `options` 區塊放在頂層 zone 宣告之前。否則相對 `file` 路徑會以該 zone 宣告所在檔案的目錄為基底解析，而非 `options.directory`。

### `_default` view 如何合成

當整份設定（含所有 `include`）**沒有任何 `view` 區塊**、但有**至少一個頂層 zone** 時，ShadowDNS 會合成一個名為 `_default` 的 view：

- 其 `match-clients` 等同 `{ any; }` —— 匹配所有來源 IP。
- 依宣告順序包含所有頂層 zone。

這對齊 BIND 在無 view 時的行為。

### 不需要 GeoIP

合成的 `_default` view 只含 `any` 規則、**不含 geo 規則**，因此無 view 形態完全不需要設定 `geoip-directory`，也不需要任何 mmdb 檔。這正是 [GeoIP 資料庫](geoip.md#未載入-geoip-時)所述的條件式需求行為。

### 混用 view 與頂層 zone 是啟動錯誤

當設定中同時存在**任何 `view` 區塊**與**任何頂層 zone**（不論宣告順序、不論分散在哪些檔案），ShadowDNS 啟動失敗並回致命錯誤。訊息會指出第一個頂層 zone（其名稱、來源檔案路徑與行號）。這對齊 BIND「一旦使用 view，所有 zone 都必須在 view 內」的規則。

### 頂層 zone 重名

重複的頂層 zone 名稱**不會 fatal** —— 全部條目都會保留。合成時，ShadowDNS 對每個重複名稱輸出一條 Warn，列出該名稱所有宣告的位置，並說明服務時以**最後一筆宣告為準**。

!!! warning "從無 view BIND 遷移的兩個表面差異"
    - **Query log：**每一行會多出 `view _default:` 子句，而無 view 設定下的 BIND 查詢日誌不含 view 子句。下游 log 解析器需留意這多出的欄位。
    - **Prometheus metrics：**view label 會出現 `_default` 值。

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

## 指令處理摘要

ShadowDNS 遇到的每個 BIND 指令都會落到[分層容忍契約](#分層容忍契約)的四個層級之一。下表摘錄操作者最常詢問的情況：

| 指令 | 層級 | 行為 |
|------|------|------|
| `type slave`、`type forward` zone | INFO | zone 被丟棄（不服務），其 `file` 永不開啟；載入繼續 |
| `allow-update`、`allow-notify`、`blackhole` | WARN | 跳過並記錄為不強制 |
| **view 範圍**的 `allow-query` / `allow-recursion` / `allow-transfer` | WARN | 跳過並記錄為不強制；同樣指令在 `zone` 區塊內則靜默跳過 —— 兩種範圍皆不強制（見[存取控制模型](../migration.md#存取控制模型差異)） |
| `controls`、`key`、`server`、`statistics-channels`、`trusted-keys` | WARN | 跳過並記錄為無效（ShadowDNS 未實作這些控制平面／安全功能） |
| `dnssec-enable` | Silent | 跳過，不產生操作者可見的雜訊 |
| `recursion`、`forwarders`、`dnssec-validation` | INFO | 跳過；ShadowDNS 為純權威 |
| `rate-limit` 於 view 內 | WARN | 跳過；RRL 只支援 `options` 範圍 |
| `qps-scale` | WARN | 跳過；不實作 load-adaptive scaling |
| 括號不對稱／缺 `;`／未封閉區塊 | fail-closed | 中止啟動並指出檔案與行號 |
| `geoip asnum` 無前綴 `AS<數字>` | fail-closed | 中止啟動 |
| 非 2 字母 ISO 3166-1 的 `geoip country` 碼 | fail-closed | 中止啟動 |
| view 區塊與頂層 zone 混用 | fail-closed | 中止啟動並指出第一個頂層 zone |

Recursion 永遠關閉（`recursion no` 恆為有效），ShadowDNS 是純權威伺服器。**options 範圍的 `allow-transfer` 是 ShadowDNS 唯一會強制的存取控制指令** —— 它就是 AXFR ACL（見[從 BIND 遷移](../migration.md#存取控制模型差異)）。

## 範例

在 Debian/Ubuntu 上，設定會拆成 include 結構。最上層的 `named.conf` 只負責串接各部分：

```text
// named.conf
include "named.conf.options";
include "named.conf.local";
```

`named.conf.options` 放全域的 `options`（與 `logging`）區塊：

```text
// named.conf.options
options {
    directory           "/etc/bind";
    geoip-directory     "/usr/local/share/GeoIP/";
    listen-on           { any; };
    listen-on-v6        { none; };
    recursion           no;
    minimal-responses   yes;
    version             none;
    hostname            none;
    allow-transfer      { 192.0.2.10; 192.0.2.11; };
};
```

`named.conf.local` 放 `view` 區塊與其 zone。跨 view 同名的 split-horizon zone 採 `db.<zone>-<view>` 連字號命名慣例，讓每個 view 的副本各有獨立檔案：

```text
// named.conf.local
view "th" {
    match-clients { geoip country TH; };
    zone "example.com" {
        type master;
        file "db.example.com-th";
    };
};

view "other" {
    match-clients { any; };
    zone "example.com" {
        type master;
        file "db.example.com-other";
    };
};
```

此處的 `directory "/etc/bind"` 為 Debian 慣用（權威 zone 檔放設定同層）。只存在於單一 view 的 zone 不需 view 後綴 —— 使用純 `db.<zone>`，例如 `db.include-test.example`。

Zone file 採 RFC 1035 master file 格式（`$TTL`、`$ORIGIN`、`@`、跨行 `(...)`、`;` 註解），並支援 `$INCLUDE` / `$include` 指令（裸路徑與 BIND 式雙引號路徑皆可，指令名稱不分大小寫）。限制：路徑本身**不可包含空白**（miekg/dns scanner 的底層限制，加引號也無法繞過），且引號形式只在最上層 zone file 有效 —— 經 `$INCLUDE` 拉進來的片段由底層 parser 直接讀取，內部必須使用裸路徑形式。
