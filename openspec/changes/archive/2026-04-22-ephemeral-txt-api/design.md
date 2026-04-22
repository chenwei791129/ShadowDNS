## Context

ShadowDNS 是一個 authoritative DNS server，zone data 在啟動時從 zone file 載入至 `zone.Zone` struct，透過 `atomic.Pointer[ServerState]` 做 hot-swap。目前沒有任何 HTTP API（僅有 Prometheus `/metrics` endpoint），也沒有動態修改 DNS record 的機制。

ACME DNS-01 challenge 需要在 `_acme-challenge.<domain>` 暫時建立 TXT record，驗證完成後即可移除。這些 record 天生是短暫的（通常 TTL 60-120 秒），不需要持久化至 zone file。

同時，現行 `aliases.yaml`（root ↔ backup domain 映射）是獨立於 `named.conf` 的另一份 YAML 設定檔，由 `--aliases` CLI flag 指定。為了避免再引入第三份 YAML（API 設定），本 change 把 aliases 與新的 ephemeral API 設定合併進一份總 ShadowDNS YAML 設定檔，用單一 `--config` flag 載入。由於功能尚未正式上線，不保留 `aliases.yaml` 與 `--aliases` 的向後相容。

現有架構的關鍵約束：

- `zone.Zone.Records` 是 load-once, read-many，無 mutex 保護——不適合動態寫入
- `ServerState` 在 SIGHUP reload 時整個被替換——ephemeral data 如果放在 state 中會被洗掉（恰好符合需求）
- DNS handler (`ServeDNS`) 透過 `s.state.Load()` 取得 snapshot，所有 lookup 在同一 snapshot 內完成
- 目前 `--aliases` flag 在 `cmd/shadowdns/main.go:237`（`registerServerFlags` 函式內）以 cobra 的 `f.StringVar(&opts.AliasesPath, "aliases", ...)` 宣告，`internal/config/aliases.go` 獨立解析該檔案

## Goals / Non-Goals

**Goals:**

- 提供 HTTP API 讓 ACME client 能新增/刪除帶 TTL 的 ephemeral TXT record
- Ephemeral records 純 in-memory，不影響 zone file
- TTL 到期自動消失，reload/restart 亦清除
- IP ACL（必填）+ 可選 pre-shared token 雙層防護
- 獨立 port、但與 aliases **共用** 一份總 ShadowDNS YAML 設定檔（由 `--config` 指定）
- SIGHUP reload 原子性：總設定檔任一 section（aliases / ephemeral_api）驗證失敗則整體不切換、保留舊狀態

**Non-Goals:**

- 支援 TXT 以外的 record type
- 持久化 ephemeral records
- 實作 DNS UPDATE (RFC 2136)
- 完整 zone management API 或 dashboard
- 修改現有 zone file reload 邏輯
- 保留 `aliases.yaml` / `--aliases` flag 的向後相容

## Decisions

### 總 ShadowDNS YAML 設定檔格式

aliases 與 ephemeral API 設定合併進一份 YAML（預設路徑 `/etc/shadowdns/shadowdns.yaml`），由 CLI flag `--config` 指定：

```yaml
aliases:
  backup.example.com: root.example.com
  backup.other.net: root.other.net

ephemeral_api:
  listen: "127.0.0.1:8053"
  allow:
    - "10.0.0.5"
    - "192.168.1.0/24"
  token: "optional-secret"  # 省略則不驗證 token
```

- 頂層為單一 YAML document；未來新增 section（例如 metrics、limits）沿用相同檔案
- `aliases` 為 map[string]string；與原 `aliases.yaml` 的語意完全一致（backup → root）
- `ephemeral_api` 為 object；若整個 section 缺席，API server 不啟動
- `aliases` section 可為空 map `{}`；整體 `aliases` key 缺席視為空映射（不報錯）
- YAML 解析使用 `gopkg.in/yaml.v3`，未知欄位回傳 error（strict decoding）避免 typo 被吞

**替代方案**：TOML 或 JSON。選 YAML 是因為專案原本 `aliases.yaml` 已是 YAML，命名與既有慣例一致。

### CLI flag：`--config` 取代 `--aliases`（不新增 `--api-conf`）

Cobra 遷移後所有 flag 使用 POSIX 雙破折號；新增/移除均在 `cmd/shadowdns/main.go` 的 `registerServerFlags` 內完成：

- 新增 `f.StringVar(&opts.ConfigPath, "config", "", "path to unified ShadowDNS YAML config (required)")`；`PersistentPreRunE` 或 `RunE` 早期檢查 `opts.ConfigPath == ""` 則 `return fmt.Errorf(...)` 讓 cobra fatal
- 移除 `f.StringVar(&opts.AliasesPath, "aliases", ...)`；cobra 對未註冊 flag 自動回報 `unknown flag: --aliases`，因此不需額外偵測舊 flag
- 不引入 `--api-conf`（原提案中規劃但尚未實作）
- `shadowdns reload` 子命令（`cmd/shadowdns/reload.go`）維持只接受 `--named-conf`，**不繼承 `--config`**：子命令只負責送 SIGHUP；實際重新讀取 `--config` 路徑由 server process 在 SIGHUP handler 內完成，保留啟動時記下的路徑

**替代方案**：保留 `--aliases` 同時新增 `--config`。缺點：雙入口會讓 reload/dry-run 邏輯分裂；「不考慮向後相容」的共識已允許直接替換。

### Ephemeral store 獨立於 ServerState

Ephemeral store 掛在 `Server` struct 上（而非 `ServerState` 內），因為：

- `ServerState` 在 reload 時整個被替換，ephemeral records 也會消失——雖然 restart 清除是需求，但 reload 清除需要由 store 自行控制（透過 `Clear()` 方法），而非被動被替換
- `Server` 的生命週期 = process 生命週期，ephemeral store 同理
- DNS handler 已可透過 `s.xxx` 存取 Server 欄位，整合自然

**替代方案**：放入 `ServerState`。缺點：reload 時 caller 需要手動搬移 ephemeral data 到新 state，增加 `BuildState` 的複雜度。

### DNS handler 整合點：zone lookup 之後、negative reply 之前

在 `handleRootQuery` 和 `handleBackupQuery` 中，當 `rootZone.Lookup()` 或 `alias.Resolve()` 回傳空結果時，插入 ephemeral store 查詢：

```
zone lookup → 有結果 → 回傳
           → 無結果 → ephemeral lookup → 有結果 → 回傳（TTL 為剩餘秒數）
                                       → 無結果 → negative reply
```

Zone file 中的靜態 record 優先於 ephemeral record（避免 ephemeral API 覆蓋正式資料）。

**替代方案**：ephemeral 優先。缺點：API 可能意外遮蔽 zone file 中的合法 record，增加誤用風險。

### TTL 過期策略：lazy eviction + periodic GC

- **Lazy eviction**：query 時檢查 `expireAt`，過期則不回傳（零成本檢查）
- **Periodic GC**：background goroutine 每 30 秒掃描一次，刪除已過期的 entry（控制記憶體）
- DNS response 中的 TTL = `max(1, expireAt - now)`，動態計算剩餘秒數

**多值情境下的 GC**：`gcSweep` 逐一檢查每個 FQDN 下的 entry slice，移除過期項；slice 變空時連同 FQDN key 從 map 中刪除，避免殘留空 slice。每個 entry 各自擁有 `expireAt`，因此不同 value 可以獨立過期。

**替代方案**：`time.AfterFunc` per record。缺點：大量 record 時 timer 數量爆炸。

### 多值 TXT 儲存結構

Ephemeral store 由 `map[fqdn]entry` 改為 `map[fqdn][]entry`：每個 FQDN 可持有多筆 `entry`，各自有自己的 `value` 與 `expireAt`。影響：

- **Lookup** 回傳 `[]Record`（一批未過期的 entry），DNS handler 為每個 entry 合成一筆 `dns.TXT` RR，放進同一個 answer section。所有 RR 的 owner name 相同，TTL 為各自剩餘秒數。
- **Put** 先比對同 FQDN 下既有 entries 的 `value` 是否相符：相符則更新該 entry 的 `expireAt`；否則 append。
- **Delete(fqdn)** 直接 `delete(records, fqdn)`——整個 slice 一次移除。
- **Clear()** 行為不變：整張 map 重置。

`dns.TXT` 每筆 RR 的 `Txt` 欄位仍維持單一字串（不把多個 value 塞進同一個 RR 的 string list）——這樣 ACME validator 可以對每筆 RR 獨立比對 token。

**替代方案**：`map[fqdn]map[value]entry`。優點是查找/更新特定 value 為 O(1)；缺點是多一層 map、記憶體開銷高，而 ACME 場景同 FQDN 的 value 數通常 ≤ 2，線性搜尋成本可忽略。

### Authentication 流程：IP ACL 先行、token 後驗

```
請求 → IP ACL check → 不在白名單 → 403 Forbidden
                    → 通過 → token 有設定？ → 有 → 驗 Authorization header → 不符 → 401 Unauthorized
                                             → 沒設定 → 放行
                                                                            → 符合 → 放行
```

IP ACL 為必填，token 為選填。兩層獨立判斷，不互相替代。

### HTTP API 設計

| Method | Path | 用途 |
|--------|------|------|
| PUT | `/v1/txt/{fqdn}` | 新增或刷新 ephemeral TXT value |
| DELETE | `/v1/txt/{fqdn}` | 一次清除該 FQDN 下所有 ephemeral TXT values |

PUT body:
```json
{
  "value": "challenge-token-here",
  "ttl": 120
}
```

- `{fqdn}` 為完整 FQDN（例如 `_acme-challenge.www.example.com`）
- TTL 上限 3600 秒（防止遺忘的 record 佔用過久），下限 1 秒
- 回傳 JSON response 含 status、canonical fqdn、ttl，以及該 FQDN 目前 ephemeral value 的總數

**PUT 語意（多值支援）**：同一 FQDN 可同時存在多筆 value；語意改為「add-or-refresh」而非「replace」：

- 若 FQDN 尚無 ephemeral entries，或 FQDN 已有 entries 但新 value 不重複 → 追加新 entry
- 若 FQDN 已有相同 value 的 entry → 就地刷新該 entry 的 `expireAt`（idempotent retry 不會累積重複）

PUT 仍為冪等：連續兩次相同 body 的呼叫最終狀態等同於呼叫一次。ACME DNS-01 wildcard + apex 同時驗證的情境（兩個 challenge 同時寫到 `_acme-challenge.<domain>`）會各自追加一筆，彼此不覆蓋。

**DELETE 語意（全清）**：`DELETE /v1/txt/{fqdn}` 一律移除該 FQDN 下所有 ephemeral entries；刪不存在的 FQDN 仍回 `200`（冪等）。不提供「刪單一 value」——若需要精細控制可等 TTL 自然過期。DELETE 只觸及 ephemeral store，zone file 中的同名 record 完全不受影響。

**替代方案**：PUT 維持「replace 整個 RRSet」語意，body 改成 `{"values":[...],"ttl":N}`。被拒絕的原因：ACME client 往往是兩支獨立的 process 在平行驗證 apex + wildcard，彼此不會把對方的 value 放進同一個 body；若採 replace 模式，後呼叫者會蓋掉前一筆有效 token。

### Reload 原子性：全部驗證通過才切換

SIGHUP reload 流程：

```
SIGHUP → 讀取 --config 指定的檔案（server process 啟動時記下）
      → YAML decode (strict)           ── 失敗 ─→ 保留舊狀態、log error、結束 reload
      → validate aliases section       ── 失敗 ─→ 保留舊狀態、log error、結束 reload
      → validate ephemeral_api section ── 失敗 ─→ 保留舊狀態、log error、結束 reload
      → 全部通過
      → build new ServerState（含新 alias map）
      → atomic swap ServerState
      → ephemeralStore.Clear()（reload 一定清除 ephemeral records）
      → log 成功
```

關鍵點：

- 解析 + 驗證 + 建新 state 全部成功後，才做 atomic swap；中途任何失敗都不改變 server 實際行為
- `ephemeralStore.Clear()` 放在 swap 之後執行，確保「reload 成功」與「ephemeral 清除」同步發生；若 reload 失敗（保留舊狀態），ephemeral store 也不被清除
- 驗證項目包含：aliases map 不含重複 backup、不含自我 alias；ephemeral_api.listen 可解析、allow 條目皆為合法 IP/CIDR、listen address 語法正確

**替代方案**：各 section 獨立 reload（aliases 成功就先切，ephemeral 失敗不影響）。缺點：總設定檔的概念要求使用者看到一份檔案、一份驗證結果；分裂 reload 會造成「檔案半套用」難以除錯。

### 打包範例檔同步

- 新增 `dist/shadowdns.yaml.example`，同時涵蓋 `aliases` 與 `ephemeral_api` 兩段，附上註解
- 移除 `dist/aliases.yaml.example`
- `packaging/` 底下的 deb install 腳本、systemd unit 的 `ExecStart` 由 `--aliases` 改為 `--config`，安裝路徑由 `/etc/shadowdns/aliases.yaml.example` 改為 `/etc/shadowdns/shadowdns.yaml.example`（cobra 遷移已在 commit `36d6af1` 把 systemd unit 更新為雙破折號格式，本 change 只需替換 flag 名稱與路徑）

## Risks / Trade-offs

- **[破壞性變更：移除 `aliases.yaml` / `--aliases`]** → 已在 production 執行 ShadowDNS 的部署需要同步改設定檔路徑與 CLI flag。Mitigation：本功能尚未正式上線，使用者可控；release note 必須明列遷移步驟。
- **[In-memory 不持久化]** → restart 或 crash 後 ephemeral records 全部消失。對 ACME challenge 而言可接受（重新觸發 challenge 即可），但未來若有需要持久化的 record type 就不適用。Mitigation：本次明確限制為 ephemeral-only。
- **[API server 新增攻擊面]** → HTTP endpoint 可能成為 DoS 或 record injection 的入口。Mitigation：IP ACL + 可選 token、TTL 上限 3600 秒、獨立 port 可綁定 localhost。
- **[Ephemeral store 的 race condition]** → 多個 concurrent request 可能同時讀寫同一 FQDN。Mitigation：使用 `sync.RWMutex`，寫操作（PUT/DELETE/GC）取 write lock，讀操作（DNS query）取 read lock。
- **[Zone file 有同名 record 時的行為]** → 若 zone file 已有 `_acme-challenge.xxx` 的 TXT record，ephemeral store 的 record 永遠不會被查詢到（zone 優先）。Mitigation：在 API response 中不做檢查（保持簡單），由使用者自行確保不衝突。
- **[Reload 原子性 implementation error]** → 若 swap 與 validation 順序寫錯，可能導致 partial update。Mitigation：SIGHUP reload 路徑必須有 unit + integration 測試覆蓋「aliases section 失敗時舊 alias 仍生效」「ephemeral_api section 失敗時舊 API listener 仍正常」兩個具體 case。
