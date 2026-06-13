## Context

`bind-config-tolerant-parsing`(A)與 `bind-named-acl-match-clients`(B)交付了 BIND drop-in 的執行期能力,但尚無文件。專案已有兩頁天然歸屬的手冊:`docs/configuration/named-conf.md`(「named.conf Compatibility」)與 `docs/migration.md`(「Migrating from BIND」,Operations 之下,已含四階段切換步驟)。本 change 為純文件 + 一個 packaging 範例,不動執行期。

協調點:parked 的 `debian-named-conf-layout` 擁有 `packaging/named.conf.example` 的 Debian 三檔分割重塑,且會改 `nfpm.yaml`。本 change 只新增獨立的 `named.conf.viewless.example` 並對 `nfpm.yaml` append 一條 entry,避免與其搶改同一檔內容。

## Goals / Non-Goals

**Goals:**

- 把 A/B 的分層容忍 + fail-closed 契約與 BIND drop-in 遷移步驟寫成使用者讀得到的手冊。
- 提供可複製的 viewless 範例。

**Non-Goals:**

- 任何執行期/解析行為變更(A/B 已交付)。
- 重塑既有 `named.conf.example`(屬 `debian-named-conf-layout`)。
- 新增手冊頁面或 nav(擴充既有兩頁即可)。

## Decisions

### 相容契約與遷移步驟擴充既有兩頁,不新增頁面

分層容忍對照表(silent/INFO/WARN/fail-closed)、fail-closed doctrine、B 交付的 `acl`/`match-clients` 元素與內建 ACL,寫進既有「named.conf Compatibility」頁(`docs/configuration/named-conf.md` + `.zh.md`)。`--named-conf /etc/bind/named.conf` 指法、被忽略構造、存取控制模型差異寫進既有「Migrating from BIND」頁(`docs/migration.md` + `.zh.md`)。存取控制敘述須區分 scope:ShadowDNS 以 `match-clients` 選 view、honors **options-scope** `allow-transfer`(既有 AXFR ACL,見 zone-transfer spec「Enforce allow-transfer ACL」),但不強制 **view/zone-scope** 的 `allow-query`/`allow-recursion`/`allow-transfer`;此區分須與既有頁面依賴 options-scope `allow-transfer` 的內容一致,不可籠統說「不強制 allow-transfer」而矛盾。雙語檔同步更新;`.zh.md` 連到 base `.md` 路徑並用目標頁中文標題錨點。

替代方案:新增獨立 bind-compatibility 頁 — 否決,與既有「named.conf Compatibility」頁職責重疊、徒增 nav 維護。

### viewless deb 範例為自足單檔,不附 db 檔

`packaging/named.conf.viewless.example` 為自足單檔:`options` block + 一至兩個頂層 `type master` zone,並以註解指向遷移指南說明 BIND default-zones 相容(default-zones 載入行為已由 A 的整合 fixture 測試,範例不需附帶可運作的 db 檔)。內容只用 RFC 2606 網域 / RFC 5737 IP。

替代方案:附完整 default-zones + db.local 等 — 否決,範例不需可運作的本機 zone,徒增檔案且偏離「示範 viewless 佈局」的目的。

### 與 debian-named-conf-layout 在 nfpm.yaml 採 append-only 協調

本 change 對 `nfpm.yaml` 只新增 viewless 範例的 install entry,不改既有 `named.conf.example` entry。`deb-packaging` spec 以 ADDED scenario 描述新範例安裝,不改既有 example scenario。誰先 apply 由 apply 順序建議決定;append-only 使合併衝突最小。

## Implementation Contract

**Behavior**:`make docs-build`(strict)成功產出含擴充內容的手冊;`.deb` 安裝後 `/etc/shadowdns/named.conf.viewless.example` 存在且為合法 viewless 骨架。

**內容契約**:
- named.conf Compatibility 頁:分層容忍對照表 + fail-closed doctrine + `acl`/`match-clients` 元素與內建 ACL,雙語一致。
- Migrating from BIND 頁:drop-in 指法 + 被忽略構造 + 存取控制模型差異,雙語一致。
- README features / `docs/index.md` 比較表:標註 BIND drop-in 相容,雙語一致(README 為英文)。
- `packaging/named.conf.viewless.example`:自足 viewless 骨架,RFC 2606/5737。

**Acceptance criteria**:
- `make docs-build` 在 `--strict` 下通過(無斷鏈、無 nav 不符)。
- 雙語檔成對更新(不單改一語)。
- `deb-packaging` spec 新增 scenario 描述 viewless 範例安裝;`nfpm.yaml` 有對應 entry。
- sanitize grep gate 對改動檔通過。

**Scope boundaries**:
- 範圍內:上述 docs 檔、`packaging/named.conf.viewless.example`、`nfpm.yaml` append、`deb-packaging` spec ADDED scenario。
- 範圍外:執行期變更;`named.conf.example` 重塑;新手冊頁/nav。

## Risks / Trade-offs

- [與 `debian-named-conf-layout` 同改 `nfpm.yaml` 造成合併衝突] → 本 change 對 `nfpm.yaml` 採 append-only、不碰既有 entry;apply 順序建議明列先後。
- [雙語文件不同步] → 將「成對更新」列為 acceptance,審查時逐項對照。
- [手冊內容與 A/B 實際行為漂移] → 契約表直接對應 A/B 的 spec 用語(分層 log、fail-closed),引用同一組詞彙。

## Open Questions

(none)
