## 1. 手冊內容

- [x] 1.1 實作「相容契約與遷移步驟擴充既有兩頁,不新增頁面」之相容契約部分:在 `docs/configuration/named-conf.md` 與 `docs/configuration/named-conf.zh.md`(「named.conf Compatibility」頁)加入分層容忍對照表(silent/INFO/WARN/fail-closed)、fail-closed doctrine、以及 B 交付的 `acl`/`match-clients` 元素與內建 ACL 說明,雙語一致。驗證:`make docs-build` 於 `--strict` 下通過;雙語檔成對更新。
- [x] 1.2 實作「相容契約與遷移步驟擴充既有兩頁,不新增頁面」之遷移步驟部分:在 `docs/migration.md` 與 `docs/migration.zh.md`(「Migrating from BIND」頁)加入 drop-in 章節 — `--named-conf /etc/bind/named.conf` 指法、被容忍/忽略的構造、存取控制模型差異 — 以 `match-clients` 選 view、honors **options-scope** `allow-transfer`(既有 AXFR ACL),但不強制 **view/zone-scope** 的 `allow-query`/`allow-recursion`/`allow-transfer`(記 WARN);敘述須與既有頁面依賴 options-scope `allow-transfer` 的 Prerequisites/troubleshooting 內容一致、不矛盾。雙語一致。驗證:`make docs-build --strict` 通過;雙語檔成對更新;與既有 migration.md 的 allow-transfer 敘述無矛盾。
- [x] 1.3 更新 features/比較表:在 `README.md`(英文)features 清單與 `docs/index.md`/`docs/index.zh.md` 比較表標註 BIND drop-in 相容。驗證:`make docs-build --strict` 通過;`README.md` 與 `docs/index.*` 描述一致。

## 2. deb viewless 範例

- [x] 2.1 實作「viewless deb 範例為自足單檔,不附 db 檔」:新增 `packaging/named.conf.viewless.example` — 自足 viewless 骨架(`options` block + 一至兩個頂層 `type master` zone,無 view),含註解指向遷移指南說明 default-zones 相容;內容只用 RFC 2606 網域 / RFC 5737 IP。滿足 Requirement「Viewless BIND-style example configuration file」。驗證:`make build` 後以 `--dry-run --named-conf` 指向此範例(zone file 路徑可解析)載入不 fatal。
- [x] 2.2 實作「與 debian-named-conf-layout 在 nfpm.yaml 採 append-only 協調」:在 `nfpm.yaml` 對 `named.conf.viewless.example` append 一條 install entry(dst `/etc/shadowdns/named.conf.viewless.example`),不改既有 `named.conf.example` entry。滿足 Requirement「Viewless BIND-style example configuration file」。驗證:`make deb` 產出的套件含該檔(或檢視 nfpm contents);既有 example entry 未被更動。

## 3. 驗證

- [x] 3.1 全套文件驗證:`make docs-build` 於 `--strict` 下無斷鏈、無 nav 不符;對本 change 改動的所有檔跑 sanitize grep gate(只允許 RFC 2606 網域 / RFC 5737 IP)並通過。本 change 為純 docs/packaging,依規則 perf-guard 跳過。
