## Context

ShadowDNS 是一個 authoritative DNS server，zone data 在啟動時從 zone file 載入至 `zone.Zone` struct，透過 `atomic.Pointer[ServerState]` 做 hot-swap。目前沒有任何 HTTP API（僅有 Prometheus `/metrics` endpoint），也沒有動態修改 DNS record 的機制。

ACME DNS-01 challenge 需要在 `_acme-challenge.<domain>` 暫時建立 TXT record，驗證完成後即可移除。這些 record 天生是短暫的（通常 TTL 60-120 秒），不需要持久化至 zone file。

現有架構的關鍵約束：
- `zone.Zone.Records` 是 load-once, read-many，無 mutex 保護——不適合動態寫入
- `ServerState` 在 SIGHUP reload 時整個被替換——ephemeral data 如果放在 state 中會被洗掉（恰好符合需求）
- DNS handler (`ServeDNS`) 透過 `s.state.Load()` 取得 snapshot，所有 lookup 在同一 snapshot 內完成

## Goals / Non-Goals

**Goals:**

- 提供 HTTP API 讓 ACME client 能新增/刪除帶 TTL 的 ephemeral TXT record
- Ephemeral records 純 in-memory，不影響 zone file
- TTL 到期自動消失，reload/restart 亦清除
- IP ACL（必填）+ 可選 pre-shared token 雙層防護
- 獨立 port、獨立設定檔，與 BIND named.conf 解耦

**Non-Goals:**

- 支援 TXT 以外的 record type
- 持久化 ephemeral records
- 實作 DNS UPDATE (RFC 2136)
- 完整 zone management API 或 dashboard
- 修改現有 zone file reload 邏輯

## Decisions

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

**替代方案**：`time.AfterFunc` per record。缺點：大量 record 時 timer 數量爆炸。

### API 設定檔格式：YAML

獨立設定檔使用 YAML 格式（與 `aliases.yaml` 一致），不混入 named.conf：

```yaml
listen: "127.0.0.1:8053"
allow:
  - "10.0.0.5"
  - "192.168.1.0/24"
token: "optional-secret"  # 省略則不驗證 token
```

**替代方案**：TOML 或 JSON。選 YAML 是因為專案已有 YAML 先例（`aliases.yaml`），減少認知負擔。

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
| PUT | `/v1/txt/{fqdn}` | 新增或更新 ephemeral TXT record |
| DELETE | `/v1/txt/{fqdn}` | 刪除 ephemeral TXT record |

PUT body:
```json
{
  "value": "challenge-token-here",
  "ttl": 120
}
```

- 使用 PUT（冪等）而非 POST，因為同一 FQDN 的 challenge token 一次只有一筆
- `{fqdn}` 為完整 FQDN（例如 `_acme-challenge.www.example.com`）
- TTL 上限 3600 秒（防止遺忘的 record 佔用過久），下限 1 秒
- 回傳 JSON response 含 status 和 detail

### Reload 行為

SIGHUP reload 時呼叫 `ephemeralStore.Clear()` 清除所有 ephemeral records。這與「reload 時自動消失」的需求一致，且實作簡單——不需要在 `BuildState` 中搬移資料。

## Risks / Trade-offs

- **[In-memory 不持久化]** → restart 或 crash 後 ephemeral records 全部消失。對 ACME challenge 而言可接受（重新觸發 challenge 即可），但未來若有需要持久化的 record type 就不適用。Mitigation：本次明確限制為 ephemeral-only。
- **[API server 新增攻擊面]** → HTTP endpoint 可能成為 DoS 或 record injection 的入口。Mitigation：IP ACL + 可選 token、TTL 上限 3600 秒、獨立 port 可綁定 localhost。
- **[Ephemeral store 的 race condition]** → 多個 concurrent request 可能同時讀寫同一 FQDN。Mitigation：使用 `sync.RWMutex`，寫操作（PUT/DELETE/GC）取 write lock，讀操作（DNS query）取 read lock。
- **[Zone file 有同名 record 時的行為]** → 若 zone file 已有 `_acme-challenge.xxx` 的 TXT record，ephemeral store 的 record 永遠不會被查詢到（zone 優先）。Mitigation：在 API response 中不做檢查（保持簡單），由使用者自行確保不衝突。
