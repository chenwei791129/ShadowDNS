## Context

ShadowDNS 是一個權威 DNS server，啟動時透過 `config.LoadNamedConf()` 載入 named.conf，接著 `config.LoadAliases()` 載入 aliases，再由 `server.BuildState()` 組裝出 `ServerState`（包含 view matcher、zone 資料、alias map、transfer ACL）。目前 `Server` 結構直接嵌入 `ServerState`，`ServeDNS` handler 直接讀取欄位（如 `s.Matcher`、`s.RootZones`），沒有任何 concurrency protection。

現行架構中 state 只在啟動時建構一次，不支援執行期更新。要更新 zone 或設定必須重啟 process，造成 DNS 服務短暫中斷。

## Goals / Non-Goals

**Goals:**

- 收到 SIGHUP 時重新載入 named.conf、aliases、zone files，並以 atomic swap 替換 in-memory state
- 替換期間不中斷正在處理的 DNS 查詢（zero-downtime）
- Reload 失敗時保留舊 state 繼續服務，log error
- Reload 成功後重新 dispatch NOTIFY

**Non-Goals:**

- 不做 file system watch / hot reload
- 不重新載入 GeoIP mmdb（啟動時載入一次，生命週期綁定 `run()`）
- 不重啟 UDP/TCP listener

## Decisions

### Atomic Pointer 取代直接嵌入

將 `Server.ServerState` 從直接嵌入改為 `atomic.Pointer[ServerState]`。

**理由**：`ServeDNS` 在多個 goroutine 中並行執行。使用 `sync.RWMutex` 會讓每次 DNS 查詢都需要 `RLock/RUnlock`，在高流量下成為效能瓶頸。`atomic.Pointer` 提供 lock-free 讀取，寫入端（reload）只有一個 goroutine，不需要額外同步。

**替代方案**：`sync.RWMutex` — 簡單直觀，但讀取路徑有 lock contention；`channel-based swap` — 過度設計，不適合這個場景。

### SIGHUP 監聽位於 main.go 的 run()

在 `run()` 中使用獨立的 `signal.Notify` channel 監聽 SIGHUP。收到信號後執行 reload 流程：重新載入 config → rebuild state → `Store()` 新 state → dispatch NOTIFY。

**理由**：與現有 SIGINT/SIGTERM 的 `signal.NotifyContext` 分離，各自負責不同的關注點。reload 邏輯放在 `run()` 而非 `Server` 內部，因為它需要存取 config path、aliases path、GeoIP handles 等 `Server` 不需要知道的資訊。

**替代方案**：在 `Server` 上加 `Reload()` method — 會讓 `Server` 承擔太多職責，且需要傳入所有 config 路徑。

### Server 提供 SwapState method

在 `Server` 上新增 `SwapState(new ServerState)` method，內部呼叫 `atomic.Pointer.Store()`。reload 邏輯在 `main.go` 中呼叫此 method。

**理由**：讓 `Server` 只負責 state 的原子替換，reload 的「載入 + 組裝」邏輯由外部（`run()`）控制。這也讓測試更容易 — 可以直接建構 `ServerState` 並 swap 進去。

### ServeDNS 每次進入時 Load state

`ServeDNS` 及 `handleTransfer` 在方法開頭呼叫 `s.state.Load()` 取得 `*ServerState`，整個請求處理期間使用同一份 snapshot。

**理由**：確保單一查詢的處理過程中 state 一致。如果在處理中途 reload，查詢會繼續使用舊 state 直到完成，不會出現半新半舊的狀況。

## Risks / Trade-offs

- **舊 state 的 GC 延遲**：atomic swap 後舊的 `ServerState` 需等到所有引用它的 goroutine 完成後才會被 GC。在極端情況下（大量 zone + 長時間查詢），記憶體使用量會短暫翻倍。→ 可接受，DNS 查詢通常在毫秒級完成。
- **Reload 期間 CPU spike**：重新解析所有 zone files 是 CPU 密集操作。→ 這是一次性操作且由管理員手動觸發，可接受。
- **Config 語法錯誤導致 reload 靜默失敗**：管理員可能不知道 reload 未成功。→ 透過 log error 提醒，未來可加入 health check endpoint。
