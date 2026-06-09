# ECS (EDNS Client Subnet, RFC 7871) 業界實作現況調查

本文調查 EDNS Client Subnet（ECS, RFC 7871）在其他 DNS 服務的實作狀況，作為 ShadowDNS
是否將 ECS 從 [README.md](../README.md) 中的 *Planned* 推進為實作功能的決策依據。

> **調查日期**：2026-06-10
> **調查方法**：多來源網路研究（開源軟體文件、各供應商官方文件、IETF 草案、學術量測論文、
> APNIC / Cloudflare 等部落格實測）。各節末附主要來源。

---

## 摘要

README 將 ECS 標為 **Planned**，並在 BIND 比較欄標為「No」——兩者皆**正確**。但 BIND 是
**例外，而非常態**：

- **主流開源 GeoDNS authoritative 軟體幾乎都支援 ECS**——gdnsd、PowerDNS、Knot DNS 皆有
  完整實作。不支援的是刻意走極簡路線的 NSD、MaraDNS、YADIFA，以及把實驗功能移除掉的 BIND。
- **幾乎所有商業 / 雲端託管 DNS 都 honor ECS**——Route 53、Azure Traffic Manager、
  Google Cloud DNS、NS1、UltraDNS、Constellix、Gcore 全部自動啟用。
- **但 resolver 端的 ECS 發送高度集中**：實務上約 **90% 的 ECS 流量來自 Google Public
  DNS (8.8.8.8)**，全球僅約 **12% 的使用者**查詢帶有 ECS。Cloudflare 1.1.1.1（第二大
  public resolver）基於隱私**完全不送 ECS**。

**結論**：ShadowDNS 不實作 ECS 會落後於同類 GeoDNS 軟體（gdnsd / PowerDNS / Knot），但
實作後的實質收益主要侷限在 Google DNS 來的查詢。ECS 應定位為現有 source-IP GeoIP 的
**opt-in 加強層**，而非取代。

---

## 1. 開源 / 自架 authoritative DNS 軟體

關鍵區分：「**讀取 query 中的 ECS 來挑選 geo 答案、並在 response 回填 SCOPE
PREFIX-LENGTH**」（符合 RFC 7871 authoritative 端契約，才算數）vs.「只是轉發 / 剝除 ECS」
（relay 行為，不算）。

| 軟體 | Authoritative ECS GeoDNS | 起始版本 | 機制 | 預設開啟？ |
|---|---|---|---|---|
| **gdnsd** | ✅ Yes（最成熟） | v1.9.0 (2013)，IANA 合規自 v2.0.0 | `plugin_geoip` 內建 | 用 geoip 時自動 |
| **PowerDNS Auth** | ✅ Yes（最靈活） | pipe ~v3.x；Lua records v4.2 | GeoIP backend / Pipe backend ABI3 / Lua records (`ecswho`/`bestwho`) | ❌ 需 `edns-subnet-processing=yes` |
| **Knot DNS** | ✅ Yes（最乾淨） | v2.7.0 (2018) | `mod-geoip` 模組（per-zone） | ❌ 需 `edns-client-subnet: on` |
| **Technitium** | ✅ Yes | geo apps v12.1 (2024)；inbound ECS v15.0 (2026) | DNS Apps 框架 | ❌ 需裝 App |
| **CoreDNS** | ⚠️ Partial | geoip plugin ~v1.8.5 (2021) | `geoip` plugin 讀 ECS 做查詢；`view` plugin 分流 | ❌ |
| **BIND（開源）** | ❌ No | 實驗功能於 v9.13.0 (2018) 移除 | — | — |
| **NSD** | ❌ No | — | 設計上極簡，無模組系統 | — |
| **MaraDNS / Deadwood** | ❌ No | — | 直接忽略 OPT record | — |
| **YADIFA / Bundy** | ❌ No | — | 無 geo / ECS 功能 | — |

值得注意的點：

- **gdnsd 是業界最成熟的 authoritative ECS 實作**：自 2014 年起 IANA 合規，會**自動把多個
  GeoIP 子網合併成最大的 supernet 來最佳化 SCOPE**（提升 resolver 端快取效率），Wikipedia
  (Wikimedia) 生產環境即採用。實作時最值得參考的對象。
- **CoreDNS 只算半套**：`geoip` plugin 會讀 ECS 來查詢、`view` plugin 能分流出不同答案，
  但**不會在 response 回填 ECS SCOPE**——中間的 resolver 快取不知道要依子網區分快取，不符
  RFC 7871 authoritative 端契約。功能上能 split-horizon，語意上不合規。
- **BIND 的移除是給 ShadowDNS 的警訊**：ISC 認為 authoritative ECS「實際上難以生產部署」而
  砍掉。RFC 層面看似簡單，工程上「同一 zone 的所有 NS 都必須一致支援 ECS，否則一台不支援的
  NS 回 global-scope 答案就會污染整個 zone 的快取」這條硬約束很麻煩。

---

## 2. 遞迴解析器 / Public resolver（決定 ECS 是否有用的關鍵）

ECS 是 resolver ↔ authoritative 雙邊協作：**resolver 不送，authoritative 端實作得再完美也
收不到**。

| Resolver | 預設送 ECS？ | 備註 |
|---|---|---|
| **Google 8.8.8.8** | ✅ 預設開 | /24 IPv4、/56 IPv6；佔全網 ~90% 的 ECS 流量 |
| **OpenDNS / Cisco Umbrella** | ✅ 預設開 | /24；ECS 草案原作者 |
| **Cloudflare 1.1.1.1** | ❌ 永不送（設計如此） | 隱私立場 + anycast 密度足夠；僅對 Akamai debug 域名例外 |
| **Quad9 9.9.9.9** | ❌ 不送（隱私端點） | 另有 `9.9.9.11` 為 ECS 端點，需自行選 IP |
| **AdGuard DNS** | ⚠️ 送，但匿名化（ASN→隨機 /24） | 2025 實測與官方說法有出入 |
| **NextDNS** | ❌ opt-in（僅匿名化 ECS） | 無原始子網模式 |
| **Unbound** | ❌ opt-in | 需編譯 `--enable-subnet` + 設定 `send-client-subnet`，預設不送 |
| **BIND（named resolver）** | ❌ No | 僅商業版 BIND 9-S 有，開源版零支援 |
| **PowerDNS Recursor** | ❌ opt-in | `edns-subnet-allow-list` 預設空 |
| **Knot Resolver** | ❌ 未實作 | 到 2025 都沒有 ECS 模組 |
| **dnsmasq** | ❌ opt-in（`add-subnet`） | OpenWrt / Pi-hole 需手動開 |
| **systemd-resolved** | ❌ 未實作 | stub / forwarder，無 ECS 程式碼 |

**對 authoritative 端的關鍵推論**：實務上會收到 ECS 的，幾乎只有 **Google DNS**（壓倒性
多數）加少量 OpenDNS。所有自架型 resolver（Unbound、PowerDNS Recursor、Knot Resolver、
BIND 開源）預設都不送或沒實作。Cloudflare 這個第二大 resolver 是個**永久盲區**。

---

## 3. 商業 / 雲端託管 DNS 與 CDN GeoDNS

| 供應商 | Honor ECS？ | 備註 |
|---|---|---|
| **AWS Route 53** | ✅ Yes | 所有 geo 路由策略，自動啟用 |
| **AWS CloudFront** | ✅ Yes | 2014-04-02 起，最早的 CDN 之一 |
| **NS1 / IBM** | ✅ Yes | 整合進 Filter Chain，最小 /24 |
| **Azure Traffic Manager** | ✅ Yes | Performance / Subnet / Geographic 路由 |
| **Google Cloud DNS** | ✅ Yes（公開 zone） | geolocation 路由策略；private DNS 不用 |
| **UltraDNS / Vercara** | ✅ Yes | Directional DNS，可逐筆「Ignore ECS」 |
| **Constellix / Gcore** | ✅ Yes | GeoDNS / GeoProximity |
| **Cloudflare 權威 DNS** | ⚠️ 有條件 | 僅 DNS-only Load Balancer 設 `prefer_ecs`；普通 A/AAAA 記錄不做 ECS geo |
| **Akamai (CDN/GTM/Edge DNS)** | ⚠️ 白名單制 | 僅對有商業協議的 resolver（Google DNS、OpenDNS）生效；其他退回 resolver IP |
| **Fastly** | ⚠️ 白名單制 | 同 Akamai 模式 |
| **Oracle Cloud DNS** | ❓ 不明 / 可能無 | 文件無 ECS 記載（Dyn 已 2020 關閉） |
| **DNSimple** | ⚠️ Partial | 僅 ALIAS 記錄轉發給支援 ECS 的 CDN |

兩個架構分歧值得注意：

- **Cloudflare 與 Akamai 都「淡化」ECS**——前者靠超密集 anycast 認為不需要 ECS；後者用白
  名單加自家 anycast / 遙測。兩家都是「我的網路夠近，不靠 ECS」的代表。
- **其餘主流託管 DNS 一律自動 honor ECS**，不需客戶設定。

---

## 4. 背景、隱私爭議與趨勢

- **RFC 7871 是 Informational（非 Standards Track）**：因為它只是「記錄 Google / OpenDNS
  既有的生產行為」，邊界情況規格不清。RFC 自己承諾的「改進版 Standards Track」至今（2026）
  仍未出現。
- **核心成本＝ resolver 端快取碎裂**：無 ECS 時 resolver 快取命中率約 80–85%；開 /24 ECS
  後掉到約 35–40%。但**此成本落在 resolver，不在 authoritative**——對純 authoritative 的
  ShadowDNS 而言**這條成本基本不適用**。
- **隱私走向是「更多限制」而非更開放**：Cloudflare 不送、Quad9 預設端點不送、NextDNS /
  AdGuard 匿名化。RFC 之所以是 Informational，部分正因隱私反對。
- **趨勢判定：穩定但停滯（stable but stagnant）**。沒有被棄用、也沒在成長，IETF 沒有推進。
  學術量測顯示約 53% 的 nameserver 支援 ECS 回應。
- **anycast 的影響**：Cloudflare 的論點有實質道理——密集 anycast 已能解決「洲際層級」誤判，
  但解決不了「城市 / ISP 層級」精度。ECS 在 public resolver 覆蓋稀疏的地區（非洲、西亞）
  價值最高。

---

## 5. 對 ShadowDNS 的判讀與建議

### 該不該做？傾向「值得做，但要看清收益邊界」

**支持實作的理由：**

1. **同類軟體都有**：gdnsd / PowerDNS / Knot 都支援。不做會讓 ShadowDNS 在 GeoDNS 定位上
   明顯落後。README 標 Planned 是對的方向。
2. **快取碎裂成本不適用於 ShadowDNS**：ShadowDNS 是 authoritative-only，碎裂的是 resolver
   的快取，不是自己的。這個 ECS 最大的反對理由對此專案不成立。
3. **Google DNS 收益直接**：來自 8.8.8.8 的查詢會帶真實 /24，直接提升 geo 選擇精度——而這
   正是 ECS 流量的約 90%。
4. **相對 BIND 的差異化優勢**：ns1 的 BIND 不支援 ECS，ShadowDNS 做了就能在 Google DNS
   流量上比 BIND 更準。

**必須注意的約束：**

1. **ECS 是「加強」不是「取代」source-IP GeoIP**：Cloudflare / ISP / 隱私 resolver 都不送
   ECS，因此既有的 source-IP geoip 路徑（View Matcher）仍是主力，ECS 只在查詢帶 ECS 時覆寫。
2. **SCOPE PREFIX-LENGTH 要做對**：回應要回填能正確涵蓋該 geo 區域的「最大」scope（能用
   /16 就別硬回 /24），否則放大 resolver 端查詢量。**gdnsd 的 supernet 合併最佳化**是最佳
   參考範本。
3. **多 NS 一致性**：同一 zone 所有 authoritative NS 必須一致支援 ECS，否則不支援的那台會
   回 global-scope 污染快取。這是 BIND 當年放棄的主因，部署時要留意。
4. **ECS 子網是 PII-adjacent**：per-query log 落地後的記錄 / 保留政策要一併想清楚。
5. **匿名化 ECS 是 resolver 的事**：authoritative 端只能用 resolver 送來的內容，無法控制其
   精度（例如 AdGuard 送的是假子網）。

### 一句話總結

> ShadowDNS 實作 ECS 的合理定位是：**作為 source-IP GeoIP 的 opt-in 加強層，主要服務
> Google DNS 來的查詢**，並務必把 response 的 SCOPE 計算做對（參考 gdnsd）。它不會、也不
> 該取代現有的 View Matcher。

---

## 主要衝突與未解事項

1. **Cloudflare 1.1.1.1 行為**：官方說「不送 ECS」，但 2025 實測發現它對 Akamai 域名會轉發
   ECS。可能是對特定 CDN 夥伴的選擇性行為，公開來源未解。
2. **AdGuard DNS**：官方說 ASN→隨機子網匿名化，2025 實測卻看到轉發真實 /24。可能是 public
   resolver vs. AdGuard Home（自架版）的差異。
3. **ECS 使用率 12% vs. <5%**：APNIC 報 12%（帶 ECS 的使用者比例），其他來源報 <5%（帶
   ECS 的查詢比例），量測口徑不同。
4. **Oracle Cloud DNS**：文件無記載不等於確定不支援，需實測確認。

---

## 主要來源

- [RFC 7871: Client Subnet in DNS Queries](https://www.rfc-editor.org/rfc/rfc7871.html)
- [Privacy and DNS Client Subnet — APNIC Blog (Geoff Huston, 2024-07)](https://blog.apnic.net/2024/07/23/privacy-and-dns-client-subnet/) — 90% Google、12% 使用者、地理分佈
- [EDNS Client Subnet in Practice — farrokhi.net (2025-10)](https://farrokhi.net/posts/2025/10/edns-client-subnet-in-practice-evaluating-public-resolver-behaviors/) — 各 resolver 實測
- [ECSeptional DNS Data — arXiv:2412.08478 (2024)](https://arxiv.org/abs/2412.08478) — 53% nameserver 支援；「只有 PowerDNS 與 gdnsd 支援 ECS」
- [BIND 9.13.0 ECS 移除說明 — ISC KB](https://kb.isc.org/docs/edns-client-subnet-ecs-for-resolver-operators-getting-started)
- [gdnsd plugin_geoip Wiki](https://github.com/gdnsd/gdnsd/wiki/GdnsdPluginGeoip)
- [PowerDNS GeoIP backend](https://doc.powerdns.com/authoritative/backends/geoip.html) / [Lua records](https://doc.powerdns.com/authoritative/lua-records/functions.html)
- [GeoIP in Knot DNS 2.7 — APNIC](https://blog.apnic.net/2018/11/14/geoip-in-knot-dns-2-7/)
- [Route 53 EDNS0](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-edns0.html) / [Azure Traffic Manager FAQ](https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-faqs) / [Google Cloud DNS routing policies](https://docs.cloud.google.com/dns/docs/routing-policies-overview)
- [Cloudflare 1.1.1.1 FAQ](https://developers.cloudflare.com/1.1.1.1/faq/) / [Cloudflare Load Balancing geo steering](https://developers.cloudflare.com/load-balancing/understand-basics/traffic-steering/steering-policies/geo-steering/)
- [Akamai GTM concepts](https://techdocs.akamai.com/gtm/docs/gtm-concepts) — ECS 白名單制
- [Google Public DNS ECS Guidelines](https://developers.google.com/speed/public-dns/docs/ecs)
