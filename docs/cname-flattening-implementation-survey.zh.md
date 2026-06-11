# CNAME Flattening（apex CNAME / ANAME / ALIAS）業界實作現況調查

本文調查 CNAME Flattening（亦稱 ANAME、ALIAS record、apex CNAME）在其他 DNS 服務的實作
狀況，作為 ShadowDNS 是否將此功能從 [功能比較表](index.md#與-bind-的功能比較)中的 *Planned* 推進為實作功能
的決策依據。

> **調查日期**：2026-06-10
> **調查方法**：多來源網路研究（開源軟體文件、各供應商官方文件、IETF datatracker / RFC、
> Cloudflare / APNIC / Akamai 等部落格、社群實測）。各節末附主要來源。

> **範圍說明**：CNAME Flattening 是**純 authoritative 端**功能——由 authoritative server 自己把
> apex 上的「類 CNAME」記錄解析成 A/AAAA 後直接回覆。因此本報告涵蓋 authoritative 軟體、標準化
> 背景與商業託管服務三個面向。

---

## 摘要

README 將 CNAME Flattening 標為 **Planned**，並在 BIND 比較欄標為「No」——兩者皆**正確**。
版圖呈現明顯的**「開源弱、商業強」分裂**：

- **三大傳統開源 authoritative server 一律不支援**——BIND 9、NSD、Knot DNS 都以「嚴格 RFC
  合規、apex 不允許 CNAME 語義」為由拒絕實作。**開源世界只有 PowerDNS（ALIAS, v4.0+）與
  Technitium（ANAME, v5+）真正支援**，且兩者都需要 server 端去遞迴解析目標。
- **商業 / 雲端託管 DNS 幾乎清一色支援**——Cloudflare、Route 53、Azure、Google Cloud DNS、
  NS1、DNSimple、DNS Made Easy / Constellix、UltraDNS、Akamai、Gcore、Oracle、easyDNS、
  Namecheap、Netlify、Vercel、Bunny 全都有。但分成兩種根本不同的哲學（見下）。
- **此功能沒有正式 RFC**。唯一的標準化嘗試 `draft-ietf-dnsop-aname` 已於 **2020 年初過期、
  2021 年 6 月退出 IETF 工作組**。現代 IETF 的正統替代方案是 **RFC 9460 的 SVCB/HTTPS
  records（2023）**，但瀏覽器與 resolver 對其 AliasMode 的支援仍不完整。
- **與 DNSSEC 全面衝突**：動態合成的 A/AAAA 無法預先簽章，是跨所有實作的共同瓶頸。

**結論**：ShadowDNS 不實作 CNAME Flattening，在**開源同類軟體中並不孤單**（與 BIND、NSD、
Knot 同列）。若要做，最大的障礙不是 DNSSEC（ShadowDNS 本就不支援，此衝突直接免除），而是
**通用版需要引入一條 outbound 遞迴解析路徑**——這與 ShadowDNS「authoritative-only、recursion
always off、NOTIFY 只認 in-zone glue、絕不碰 resolv.conf」的核心架構立場直接抵觸。

**但有一條繞過此衝突的設計路線**（見 §5）：若把 flatten 目標**限定在 ShadowDNS 自己服務的
zone**（in-bailiwick），目標解析就退化成純記憶體查詢、不需任何 outbound query——做法與 NOTIFY
的 in-zone-glue 同源。此 scoping 不只解掉 resolver 衝突，還連帶消除 GeoIP 失準與 loop detection
問題。代價是它**不服務「apex 指向外部 CDN」這個最經典用途**，只服務「apex 指向另一個本地 zone」。

---

## 1. 開源 / 自架 authoritative DNS 軟體

關鍵區分：「**在 apex 放一個指向別網域的記錄，查詢時 server 端解析成 A/AAAA 直接回覆**」
（真正的 CNAME flattening）vs.「動態回 CNAME 但不展開」「只能載入但不實作」（不算）。

| 軟體 | CNAME Flattening | 名稱 | 起始版本 | 機制 | 預設開啟？ |
|---|---|---|---|---|---|
| **PowerDNS Auth** | ✅ Yes（開源最成熟） | ALIAS（RRType 65401） | v4.0.0 (2016)；`expand-alias` 旗標自 v4.1.0 | 查詢時向**外部 resolver** 遞迴解析；AXFR 可選展開 | ❌ 需 `expand-alias=yes` + 設定 `resolver` |
| **Technitium** | ✅ Yes | ANAME | v5.0 (2020-07) | server 端自行遞迴解析目標；apex 與 subdomain 皆可 | ✅ 建立 ANAME 即生效 |
| **gdnsd** | ⚠️ 不算 | DYNC（Dynamic CNAME） | — | plugin 動態決定回哪個 CNAME，但**直接回 CNAME、不展開**，且**禁用於 apex** | — |
| **CoreDNS** | ⚠️ 不算 | `alias`（第三方 external plugin） | 不明 | response rewriting：把 `file`/`auto` 回的 CNAME chain 改寫，非真正 server-side resolution；需自行編譯 | ❌ 不在官方 build |
| **BIND 9** | ❌ No | — | — | ISC 官方拒絕：apex CNAME 屬 broken zone | — |
| **Knot DNS** | ❌ No | — | — | 嚴格 RFC 合規；3.5.4 僅能「載入」private ALIAS type，未實作展開 | — |
| **NSD** | ❌ No | — | — | 極簡、無內嵌 resolver，架構上無動態解析能力 | — |
| **YADIFA / Bundy / MaraDNS / djbdns** | ❌ No | — | — | 皆無此功能（MaraDNS 甚至刻意不做 dangling CNAME 遞迴） | — |

值得注意的點：

- **PowerDNS 是自架環境中最值得參考的對象**：有完整文件、AXFR 行為控制
  （`outgoing-axfr-expand-alias`）、商業支援。但它的設計揭示了核心約束——**`resolver` 設定
  必填，且不能指向自己（否則無限迴圈）**。也就是說，要做 CNAME flattening，就得引入一條對外
  的遞迴查詢路徑。預設關閉。
- **Technitium 是唯二真正支援者**，且 apex / subdomain 都能用、預設即生效，比 PowerDNS 直覺。
  但它定位是自架 privacy/security DNS，不是大規模 authoritative 部署。
- **gdnsd 的 DYNC 與 CoreDNS 的 alias plugin 都是「形似神不似」**：DYNC 直接回 CNAME 給
  client（不展開）且禁用於 apex；CoreDNS plugin 是事後改寫回應、需重編譯、非 production
  等級。兩者都**不滿足** apex flattening 的核心語義。
- **三大傳統 server（BIND / NSD / Knot）的一致拒絕，對 ShadowDNS 是個定位參照**：不做這功能
  並不會讓 ShadowDNS 在「嚴肅 authoritative server」這個分類中顯得落後。

---

## 2. 標準化背景（apex CNAME 限制、ANAME 草案、SVCB/HTTPS）

CNAME Flattening 沒有正式 RFC，理解它必須回到「為什麼需要」與「IETF 怎麼處理」。

### 為什麼需要——apex CNAME 禁令

- **RFC 1034 §3.6.2 的硬規則**：一個節點若有 CNAME，就不能有任何其他記錄
  （"If a CNAME RR is present at a node, no other data should be present"）。
- **zone apex 強制帶 SOA 與 NS**（RFC 1034 §4.2.1）：這兩者不可移除，因此 apex 放 CNAME 直接
  違反協定。
- **業務衝突**：CDN / 雲端負載平衡器（ALB、Cloudflare、Fastly）要求客戶用 CNAME 指向其
  hostname（因其 IP 動態變動）；但品牌需求往往要 naked domain（`example.com` 不帶 `www`）
  能直接解析。兩者交叉就撞上 apex CNAME 禁令——CNAME Flattening 就是為了繞過它而生。

### ANAME 標準化嘗試與失敗

- **起源**：2017-04 Evan Hunt（ISC）提出個人 draft，2017-05 被 DNSOP 工作組採納為
  `draft-ietf-dnsop-aname`，目標 Proposed Standard。共出 5 個 WG 版本（作者群含 ISC、
  PowerDNS、DNSimple、Cambridge 等工程師），`-04`（2019-07）為最終版。
- **過期**：**2020-01-09 過期，2021-06-25 正式退出 WG 文件列表**，stream 改為 None。
- **失敗主因**：工作組在幾個技術爭議上無法達成共識——
  1. **誰負責解析目標？** draft 設計為 primary master 週期性查目標 A/AAAA 寫回 zone；但
     ISC 認為「authoritative server 做遞迴查詢是偏離正常行為」，且解析地點變成 authoritative
     server 的地理位置（而非 client），導致 **GeoIP 路由失準**。
  2. **Loop detection 無解**：draft Appendix E 直接寫下「TODO: Solve this issue」。
  3. **Zone transfer 放大**：頻繁變動的 ANAME target 被大量 zone 引用時，每次 IP 變動都觸發
     AXFR 風暴。
  4. 各廠商早已各做各的不相容實作，標準化動機下降。

### SVCB / HTTPS records（RFC 9460）——現代正統替代方案

- **RFC 9460（2023-11 發布）**定義 `SVCB`（通用）與 `HTTPS`（HTTP 特化）兩個新 RR type。
- **AliasMode（SvcPriority=0）直接解決 apex 問題**：HTTPS/SVCB 記錄**可以**與 SOA、NS 共存於
  apex，不受 CNAME 禁令約束，且解析責任交回 resolver（resolver 知道 client 地理位置，避開
  ANAME 的 geo 失準問題）。
- **被視為 ANAME 的 IETF 正統繼任者**，但只服務 HTTP 流量，非通用 DNS aliasing。
- **採用現況（2024-25）**：Cloudflare 自動為托管域名生成 HTTPS record；Route 53 2024-10
  加入支援；Safari 完整支援、Firefox 部分、**Chromium 不支援 AliasMode**。Top 1M 域名約
  25% 部署、全域名約 4%。**在 resolver 支援普及前，CNAME Flattening 仍是較可靠的工法。**

---

## 3. 商業 / 雲端託管 DNS 與 CDN

商業託管 DNS 幾乎全面支援，但分成兩種根本哲學，選型時最關鍵：

- **(A) Server 端動態解析（真正通用）**：可指向**任意外部 hostname**。
- **(B) 靜態 alias 只指向自家資源**：只能指向供應商自家的 LB / CDN / IP。

| 供應商 | 支援？ | 名稱 | 推出 | 哲學 | 可指向任意外部 FQDN？ |
|---|---|---|---|---|---|
| **DNSimple** | ✅ Yes（**最早，2011-11**） | ALIAS | 2011-11 | A | ✅ |
| **AWS Route 53** | ⚠️ 受限 | Alias record | 2011-05 | **B** | ❌ 僅 AWS 資源（CloudFront/ELB/S3…） |
| **DNS Made Easy** | ✅ Yes | ANAME | 2012-06 | A | ✅ |
| **Cloudflare** | ✅ Yes（**術語發明者**） | CNAME Flattening | 2014-04 | A | ✅（apex 全方案強制；非 apex 限付費） |
| **UltraDNS / Vercara** | ✅ Yes | Apex Alias | ~2016 | A | ✅ |
| **Azure DNS** | ⚠️ 受限 | Alias record set | 2018-09 | **B** | ❌ 僅 Azure 資源（Public IP/TM/Front Door…） |
| **Constellix** | ✅ Yes | ANAME | ~2019 前 | A | ✅（含 ECS、AAAA、failover） |
| **Gcore** | ✅ Yes | CNAME flattening | 2023-03 | A | ✅（全方案免費） |
| **Google Cloud DNS** | ✅ Yes | ALIAS | Preview 2022-08 / GA 2025-09 | A | ✅（僅 apex、僅 public zone、不支援 DNSSEC） |
| **Akamai Edge DNS** | ✅ Yes | AKAMAITLC / AKAMAICDN | ~2015-16 | A（TLC）/ B（CDN） | ✅（AKAMAITLC 任意；AKAMAICDN 綁自家 CDN） |
| **Oracle Cloud DNS**（前 Dyn） | ✅ Yes | ALIAS | Dyn 早期 | A | ✅（與 steering policy 互斥） |
| **easyDNS** | ✅ Yes | ANAME | ~2015 前 | A | ✅（全方案免費） |
| **Namecheap** | ✅ Yes | ALIAS | 不明 | A | ✅（TTL 僅 1 或 5 分鐘可選） |
| **NS1 / IBM** | ✅ Yes | ALIAS | 不明（2023-10 補 secondary apex） | A | ✅ |
| **Netlify / Vercel** | ✅ Yes（用自家 NS 時） | flattened CNAME / ALIAS | 不明 | A | ✅（指向 `*.netlify.com` / `cname.vercel-dns.com`） |
| **Bunny DNS** | ✅ Yes | CNAME flattening | ~2022-23 | A | ✅（自家部落格卻論述 ANAME 傷 CDN 路由） |
| **DigitalOcean / Vultr** | ❌ No / 不明 | — | — | — | — |
| **Fastly** | ❌ **主動反對** | — | — | — | 建議改用 anycast A record |

幾個架構分歧值得注意：

- **「術語發明者 ≠ 功能首創者」**：常見誤解是 Cloudflare（2014）發明了這功能；實際上
  **DNSimple 早在 2011-11 就推出 ALIAS**，Route 53 Alias（2011-05，但限自家資源）、DNS Made
  Easy ANAME（2012-06）都更早。Cloudflare 只是創造了「CNAME Flattening」這個**名稱**，並因
  市佔讓它變成通用詞。
- **A vs B 哲學是選型第一考量**：Route 53 / Azure 的 Alias 只能指向自家資源（生態鎖定），
  其餘 server 端動態解析型都能指向任意 hostname。
- **CDN 對自家功能的矛盾立場**：Fastly **拒絕提供**並主動不建議（理由：傷 CDN geo 路由
  準確性），改推 anycast A record；Bunny 一邊在部落格論述 ANAME 傷路由、一邊照樣提供。這呼應
  了 ANAME draft 失敗的核心技術爭議——**server 端解析目標會用 authoritative server 的地理
  位置，而非 client 的**。

---

## 4. 背景、技術權衡與趨勢

- **此功能「成熟但碎裂」**：商業端普及多年、機制穩定，但因標準化失敗，每家名稱與細節都不同
  （ALIAS / ANAME / CNAME Flattening / Apex Alias / AKAMAITLC…），互通性差，secondary DNS
  之間常無法正確傳遞（UltraDNS、PowerDNS 都有此限制）。
- **兩種實作架構**：
  - **即時解析型**（DNSimple、NS1、Namecheap、Google Cloud DNS、PowerDNS）：每次查詢 server
    端 resolve，IP 最新但每查都有解析成本。
  - **快取監控型**（DNS Made Easy / Constellix、easyDNS、Bunny）：背景監控目標 IP、變動才
    刷新 zone，可低 TTL、甚至在目標 DNS 故障時用舊快取續命。
- **TTL 與一致性**：是反覆出現的坑。Cloudflare 早期被記錄到用 chain 的**最大** TTL（而非
  正確的最小值）；Google Cloud DNS 明確取**最小** TTL；Namecheap 只給 1/5 分鐘兩檔。
- **與 DNSSEC 全面衝突**（跨所有實作的共同瓶頸）：
  - 傳統 DNSSEC 簽章是**離線預先計算**的；動態合成的 A/AAAA 在簽章時根本不存在，無法預簽。
  - APNIC（2020）分析：CDN 即時依效能分配邊緣 IP，「無法預先得知或預測」，故無法事先簽章。
  - 即使改用 ECDSA live-signing，每次 IP 變動產生新合法簽章，會引入 **replay attack** 風險。
  - Google Cloud DNS、Akamai、Technitium 都明文「啟用此功能即不能用 DNSSEC」。Cloudflare 用
    邊緣即時 ECDSA 簽章是少數例外（但不適用 pre-signed / secondary 場景）。
- **趨勢**：vendor 實作面穩定但不再演進；標準面已由 IETF 轉向 SVCB/HTTPS（RFC 9460）。長線
  看，apex 指向問題的「正解」正在從 CNAME Flattening 緩慢遷移到 HTTPS record——但受制於
  Chromium 尚未支援 AliasMode，這個遷移還要數年。

---

## 5. 對 ShadowDNS 的判讀與建議

### 該不該做？傾向「限定 in-bailiwick 目標即可做」

**支持實作的理由：**

1. **DNSSEC 衝突對 ShadowDNS 不存在**：CNAME Flattening 最大的技術障礙是無法與 DNSSEC 共存
   ——但 ShadowDNS [功能比較表](index.md#與-bind-的功能比較)明確不支援 DNSSEC，**這條最硬的約束直接免除**。
   這反而是 ShadowDNS 相對其他軟體少數「先天有利」的功能。
2. **若客戶有 naked-domain 指向 CDN 的需求，這是唯一解**：apex 不能放 CNAME 是協定鐵律，
   要讓 `example.com`（不帶 `www`）指向 CDN 又同時保有 SOA/NS，就只能靠 flattening。
3. **README 標 Planned 沒有錯**——它是真實存在、被廣泛使用的功能。

**通用版的架構衝突（為何不能照搬 PowerDNS / Cloudflare 的做法）：**

1. **它需要一條 outbound 遞迴解析路徑——直接抵觸 ShadowDNS 核心立場**。ShadowDNS 是
   authoritative-only、`recursion no` 永遠生效，連 NOTIFY 目標都**刻意只認 in-zone glue、
   絕不碰 `/etc/resolv.conf`、不做遞迴查詢**（見 README NOTIFY 章節）。而所有 server 端
   flattening（PowerDNS 強制要 `resolver` 設定）都需要對外遞迴解析目標。引入受控的 outbound
   resolver 會破壞「零外部 DNS 依賴」的部署假設。
2. **GeoIP 路由失準**：flattening 在 server 端解析外部 CDN 目標時，用的是 ShadowDNS 自己的
   位置，不是 client 的——這正是 ANAME draft 失敗、Fastly / Bunny 反對的主因，與 ShadowDNS
   主打的 source-IP GeoIP / ECS 分流彼此打架。

### 突破口：限定 in-bailiwick 目標（只 flatten 自己服務的 zone）

把 flatten 目標**限定在 ShadowDNS 自己載入的任一 zone**（外部目標一律拒絕 / 視為無法解析），
目標解析就從「對外遞迴」退化成「記憶體 zone tree 查詢」——做法與 NOTIFY 的 in-zone-glue 完全
同源。判斷「目標是否在本地」就是對所有已載入 zone origin 做 longest-suffix match（alias
resolver 既有能力），「是否 in-bailiwick」則是 In-Bailiwick Rewrite 階段既有能力，兩者皆可複用。

此 scoping 一次解掉多個障礙：

| 障礙 | 限定 in-bailiwick 後 |
|---|---|
| 需 outbound resolver（違反 recursion-off） | ✅ 消除——純 read-path 記憶體查詢，無外部依賴 |
| GeoIP 路由失準 | ✅ 消除——在 client 已 match 的 view 裡查目標，geo 選擇被保留 |
| Loop detection 無解（殺死 ANAME draft 的主因） | ✅ trivial——本地集合有限，visited-set + 最大深度即可 |
| TTL 不確定 / chain min-vs-max 爭議 | ✅ 消除——TTL 全為本地已知值，取 chain 最小值 |
| 無法 startup 驗證 | ✅ 變可行——載入時即可驗證目標 in-bailiwick，否則 fail-fast / warn |

**代價與必須界定的設計決策：**

1. **不服務最經典用途**：業界要 flattening 九成是為 apex 指向**外部 CDN**——那正是被排除的
   情況。此版只服務「apex 指向另一個**本地** zone」（例如多個 apex 共用一組集中管理的 LB
   位址）。若真正需求是外部 CDN，應改評估 RFC 9460 的 HTTPS record。
2. **View 一致性（最關鍵）**：目標必須在 client 已 match 的 view 內解析；目標 zone 不在該
   view → 回 NODATA，否則破壞 split-horizon。
3. **AXFR 展開**：apex 不能以 CNAME 傳給 slave，必須在 AXFR 時展開成 A/AAAA（類似 PowerDNS
   `outgoing-axfr-expand-alias`），並以該 slave 所 match 的 view 展開。目標在本地，展開仍是
   純記憶體操作。
4. **與 zone aliasing 的合成順序**：backup zone 的 apex 是否也能 flatten、flatten 與
   in-bailiwick rewrite 誰先誰後，需明確定義。
5. **建議只限 apex**：非 apex 本來就能放 CNAME，把 scope 收最緊。

### 一句話總結

> ShadowDNS 實作 CNAME Flattening 在開源圈並非「不做就落後」（BIND/NSD/Knot 都沒做），其
> 最大障礙也不是別家都頭痛的 DNSSEC（ShadowDNS 免除），而是**通用版要求一條對外遞迴解析
> 路徑，與「authoritative-only、recursion-off、零外部依賴」的核心立場正面衝突**。**但只要把
> flatten 目標限定在 ShadowDNS 自己服務的 zone（in-bailiwick），衝突即消除**——目標解析退化
> 成記憶體查詢，做法與 NOTIFY in-zone-glue 同源，並連帶解掉 GeoIP 失準與 loop detection。
> 代價是只服務「apex 指向本地 zone」、不服務「apex 指向外部 CDN」；若需求其實是外部 CDN，
> 則應改評估 RFC 9460 HTTPS record。

---

## 主要衝突與未解事項

1. **Cloudflare TTL：max 還是 min？** 2014 年社群記錄為取 chain 的**最大** TTL，現行官方文件
   卻寫「minimum」。是否已修正、或對應 proxied vs. non-proxied 不同場景，需實測。
2. **Azure Alias 算不算 CNAME Flattening？** 嚴格說它是控制平面層的資源追蹤（綁定 Azure
   resource），不是 DNS wire format 的 CNAME chain 展開——效果類似但機制本質不同。
3. **Oracle Cloud DNS ALIAS 能否指向任意外部 FQDN**：文件 RDATA 格式暗示可以，但未明文，且與
   steering policy 互斥的語境模糊，需實測。
4. **多家「推出年份」不確定**：NS1、easyDNS、Constellix、Akamai、Namecheap、Netlify、Vercel、
   Bunny 的確切上線時間在公開文件中查不到精確值。
5. **ANAME 過期的詳細討論**：公開資料來自 draft 文本與 WG 投影片，DNSOP 郵件列表的逐條討論
   未直接查閱（可查 `https://mailarchive.ietf.org/arch/browse/dnsop/`）。
6. **DigitalOcean / Vultr**：文件未明確否定但多來源指不支援，需直接測試確認。

---

## 主要來源

- [RFC 1034 §3.6.2 / CNAME at the apex of a zone — ISC Blog](https://www.isc.org/blogs/cname-at-the-apex-of-a-zone/) — apex CNAME 禁令的協定根源、ISC 對 ALIAS/ANAME 的官方立場
- [ISC BIND 9 KB：CNAME at the apex of a zone](https://kb.isc.org/docs/aa-01640) — BIND 9 不支援 apex CNAME/ALIAS 的明確聲明
- [draft-ietf-dnsop-aname-04 — IETF Datatracker](https://datatracker.ietf.org/doc/html/draft-ietf-dnsop-aname-04) / [history](https://datatracker.ietf.org/doc/draft-ietf-dnsop-aname/history/) — ANAME 草案全文、resolution 設計、loop TODO、版本時間線與過期
- [RFC 9460: SVCB/HTTPS RRs — RFC Editor](https://www.rfc-editor.org/info/rfc9460/) / [RFC 9460 DNS Evolution — Peakhour](https://www.peakhour.io/blog/rfc-9460-dns-evolution/) — AliasMode 解 apex、採用率
- [HTTPS DNS Record Support Current State — Kal Feher](https://kalfeher.com/https-current-state/) — 瀏覽器支援現況（Safari 完整、Chromium 無 AliasMode）
- [Why dynamic DNS mapping prevents DNSSEC deployment — APNIC Blog (2020)](https://blog.apnic.net/2020/01/31/why-dynamic-dns-mapping-prevents-dnssec-deployment/) — CDN 動態 IP 與 DNSSEC 預簽章衝突、ECDSA replay
- [PowerDNS ALIAS records — 官方文件](https://doc.powerdns.com/authoritative/guides/alias.html) — `expand-alias`、`resolver` 必填、AXFR 展開、版本史
- [Technitium DNS Server v5 / ANAME — Blog (2020-07)](https://blog.technitium.com/2020/07/) / [help](https://technitium.com/dns/help.html) / [DNSSEC discussion #825](https://github.com/TechnitiumSoftware/DnsServer/discussions/825) — ANAME 機制、apex/subdomain、DNSSEC 不相容
- [gdnsd DYNC — zonefile wiki](https://github.com/gdnsd/gdnsd/wiki/GdnsdZonefile) / [man page](https://man.archlinux.org/man/gdnsd.zonefile.5.en) — DYNC 禁用於 apex、直接回 CNAME
- [CoreDNS alias plugin](https://coredns.io/explugins/alias/) / [GitHub](https://github.com/serverwentdown/alias) — response rewriting、需重編譯
- [Knot DNS issue #475](https://gitlab.nic.cz/knot/knot-dns/-/issues/475) / [NSD zonefile](https://github.com/NLnetLabs/nsd/blob/master/doc/manual/zonefile.rst) / [MaraDNS FAQ](https://maradns.samiam.org/faq.html) — 三家不支援的立場
- [Introducing CNAME Flattening — Cloudflare Blog (2014)](https://blog.cloudflare.com/introducing-cname-flattening-rfc-compliant-cnames-at-a-domains-root/) / [CNAME flattening docs](https://developers.cloudflare.com/dns/cname-flattening/) — 機制、apex vs 非 apex、方案差異
- [How The ALIAS Virtual Record Works — DNSimple Blog (2012)](https://blog.dnsimple.com/2012/02/how-the-alias-virtual-record-works/) — ALIAS 起源（2011-11，最早）、erl-dns 機制
- [Choosing between alias and non-alias records — AWS Route 53](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-choosing-alias-non-alias.html) / [Document history](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/History.html) — Alias 限 AWS 資源、2011-05 推出
- [Alias records overview — Azure DNS](https://learn.microsoft.com/en-us/azure/dns/dns-alias) / [GA update](https://azure.microsoft.com/en-gb/updates/azure-dns-alias-records-generally-available/) — 限 Azure 資源、2018-09 GA
- [DNS records overview — Google Cloud DNS](https://docs.cloud.google.com/dns/docs/records-overview) / [release notes](https://docs.cloud.google.com/dns/docs/release-notes) — ALIAS 限 apex/public/no-DNSSEC、Preview 2022 / GA 2025
- [Apex Alias FAQ — UltraDNS](https://dns.ultraproducts.support/hc/en-us/articles/4409649081499-Apex-Alias-Frequently-Asked-Questions) — 機制、zone transfer 限制
- [ANAME in DNS Made Easy vs Constellix — Blog](https://social.dnsmadeeasy.com/blog/aname-records-in-dns-made-easy-vs-constellix/) — ECS / IPv6 / failover 差異、快取監控型
- [Gcore CNAME flattening — Docs](https://gcore.com/docs/dns/dns-records/specify-cname-at-root) / [Blog (2023-03)](https://gcore.com/blog/gcore-dns-introduces-cname-flattening) — 全方案免費、限制
- [Akamai Edge DNS features](https://techdocs.akamai.com/edge-dns/docs/features) / [Zone Apex Mapping & DNSSEC](https://www.akamai.com/blog/security/edge-dns--zone-apex-mapping---dnssec) — AKAMAITLC / AKAMAICDN、DNSSEC 不相容
- [Using Fastly with apex domains — Fastly Docs](https://www.fastly.com/documentation/guides/full-site-delivery/domains-and-origins/using-fastly-with-apex-domains/) — Fastly 反對 flattening、推 anycast
- [How ANAME records affect CDN routing — bunny.net Blog](https://bunny.net/blog/how-aname-dns-records-affect-cdn-routing/) / [support](https://support.bunny.net/hc/en-us/articles/24872742824220-Do-you-support-CNAME-flattening) — Bunny 的矛盾立場、geo 路由問題
- [Oracle Cloud DNS supported records](https://docs.oracle.com/en-us/iaas/Content/DNS/Reference/supporteddnsresource.htm) / [easyDNS ANAME](https://easydns.com/features/aname-root-domain-alias/) / [Namecheap ALIAS](https://www.namecheap.com/support/knowledgebase/article.aspx/10128/2237/how-to-create-an-alias-record/) / [NS1 secondary apex ALIAS](https://community.ibm.com/community/user/blogs/annie-liu/2023/10/03/ns1-now-supports-apex-alias-for-secondary-zones) — 其餘供應商機制與限制
- [Netlify external DNS](https://docs.netlify.com/manage/domains/configure-domains/configure-external-dns/) / [Vercel DNS records](https://vercel.com/docs/domains/managing-dns-records) — PaaS 端的 ALIAS / flattened CNAME 建議
