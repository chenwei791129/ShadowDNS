## Context

`test/integration/notify_test.go:133-151` 的 `freeLoopbackPort` helper 以「bind-then-close」的方式讓 OS 分配一個 loopback port：

```go
pc, err := net.ListenPacket("udp", "127.0.0.1:0")
port := pc.LocalAddr().(*net.UDPAddr).Port
_ = pc.Close()
// release TCP side too
ln, tcpErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
if tcpErr == nil { _ = ln.Close() }
return port
```

取得 port 後，呼叫者在 `startShadowDNS` ([notify_test.go:157](test/integration/notify_test.go#L157)) 用 `exec.Command` 啟動 shadowdns binary 並帶 `-listen 127.0.0.1:<port>`。

**Race window**：`pc.Close()` 到 child process 實際完成 `net.Listen` 之間，任何 OS 上的 process（包括平行執行的其他 test）都可能搶先 bind 同一個 port。CI 環境因：
- GitHub Actions runner 上其他 background process 會動態開 ephemeral port
- 同一 test package 的多個 test 用 `t.Parallel()` 大量平行化
- runner 的 ephemeral port range 相對窄

偶發觸發 `bind: address already in use`。helper godoc 本身就承認了這個 race。

已觀察實際失敗案例：`TestIntegration_ConfigNo_SuppressesAllSends`（CI run 24549681507）。同 branch 前一次 run 是綠的，證明非 deterministic。

## Goals / Non-Goals

**Goals:**

- 消除 `freeLoopbackPort` → `startShadowDNS` 路徑上的 port allocation race
- 對既有測試案例保持行為相容：測試邏輯、斷言、CLI 啟動方式都不需要大改
- 解法對本地與 CI 皆生效，不依賴 runner 特定行為
- 統一單一 port allocation 路徑，避免將來再新增測試時重犯同一錯誤

**Non-Goals:**

- 不重寫 integration tests 成「in-process」形式（那是更大的重構，另開 change）
- 不擴充 shadowdns 的 runtime 功能，只在必要時新增 test-oriented 啟動路徑
- 不處理其他類別的 flakiness（GeoIP 檔讀取、TLS handshake、DNS resolve timeout 等）
- 不保證 100% 消除所有 port 衝突（例如 child process 自己 crash 後別的 process 立即佔 port 的極端情況仍可能發生，但機率級別差異極大）

## Decisions

### Decision 1: 採用 retry-with-fresh-port 作為主要修法

**選擇**：在 `startShadowDNS` 包一層 retry loop——child 啟動後若 stderr 於短時間內出現 `bind: address already in use`，`Kill()` 並重選 port 再啟一次，最多重試 3 次。

**考慮過的替代方案**：

| 方案 | 優點 | 缺點 |
|---|---|---|
| **A. Parent pre-bind + ExtraFiles** | 完全消除 race | 需要 shadowdns 支援「從繼承 fd 啟動 listener」的 code path，侵入 production code；跨平台 fd 行為需額外測試 |
| **B. SO_REUSEPORT** | 語意直接 | macOS/Linux 行為不一致；multiple listener 上來會 load-balance，不是我們要的語意 |
| **C. `-listen :0` + 從 log 讀實際 port** | 也能消除 race | shadowdns 得改 CLI 支援 `:0`，還要改 startup log 格式；測試也要改 port 讀取邏輯 |
| **D. Retry-with-fresh-port** ✓ | 不動 production code；邏輯集中在 helper；race 期望值已很低，retry 幾乎一次就過 | race 理論上仍在，只是機率被降到可忽略 |

**Why D（retry）over A（fd inheritance）**：fd inheritance 是「消除 race」的正解，但需要在 `cmd/shadowdns/main.go` 新增 `-listen-fd` flag、改造 listener 初始化流程、處理 systemd socket activation 的類似抽象。對一個「純測試基礎設施 bug」而言，這個成本比例不對。**Retry 能把偶發失敗率從「幾次/週」降到實質上「零」**，且 retry 的正確性邊界清楚：若第一次 bind 成功就走原路徑、完全沒變；只有在失敗時才進 retry。

**Why D over C（`-listen :0`）**：C 要改 production code 的 listener 啟動路徑，而且「從 stderr 讀回實際 port」本身又是一個需要時序協調的操作，複雜度與 fd inheritance 類似。

### Decision 2: retry 觸發條件與上限

- **觸發訊號**：child stderr/stdout 內出現 `"address already in use"`，或 `bind: no listeners bound` 字樣。檢測窗口為 child 啟動後的前 3 秒內（本地測試 shadowdns 能順利 bind 時多半在 100ms 內寫出 `shadowdns ready` 成功 log；CI 上載 mmdb + build state 可能需要更久，所以窗口放寬到 3 秒）
- **重試上限**：3 次（首次 + 2 次 retry）。連續 3 次都撞 port 的機率已在 10⁻⁶ 量級，若仍失敗視為真的系統問題，直接 `t.Fatalf` 並把三次的 log 一起印出來方便 debug
- **每次 retry 取新 port**：不重用上次的 port number，直接再跑一次 `freeLoopbackPort`

### Decision 3: 統一 helper 位置與命名

- 把 port allocation 與 child launch 整合成單一 helper：`startShadowDNSWithRetry(t, namedConf, extraArgs...) (*exec.Cmd, *syncBuffer, func())`
- 既有的 `startShadowDNS` 被此新 helper 取代（rename + 重寫）
- `freeLoopbackPort` 仍然保留但只在 `startShadowDNSWithRetry` 內部被呼叫
- 掃 `test/integration/` 全部 test file，把所有直接呼叫 `startShadowDNS` 的地方統一走新 helper；如果 `helpers_test.go` 或其他檔案有獨立的 port 分配路徑，也合併進來

### Decision 4: 偵測 child 啟動成功的訊號

除了「不要出現 bind 失敗訊息」之外，retry loop 需要一個 **正向** 成功訊號才能提前結束等待：

- 偵測 child stdout/stderr 出現 `"shadowdns ready"` 這條 INFO log（由 `cmd/shadowdns/main.go` 在 `srv.BoundAddrStrings()` 成功後輸出）即認定成功
  - 早期版本曾誤用 `"shadowdns starting"`，但該 log 在 bind **之前** 就輸出，child 即使 bind 失敗也會先打這條 log；CI 上實際被撞到（[PR #11 run 24575628095](https://github.com/chenwei791129/ShadowDNS/actions/runs/24575628095)）——harness 誤判為成功、回報給測試、測試於是把後續 bind 失敗 log 當作測試邏輯的問題
  - `"shadowdns ready"` 只在所有 listener 都 bind 成功後才會出現，是 race-free 的成功訊號
- 若 3 秒內未出現成功或失敗任一訊號，視為 hung，Kill + retry（CI 載 mmdb + build state 可能超過 1 秒）
- 這也會順帶修掉一個既有的潛在 flakiness：child 還沒啟動完，測試就送 query 過去

## Risks / Trade-offs

- **[Risk] retry 只是降低機率，沒根治** → Mitigation：文件明寫這是 probabilistic 修法、非 deterministic；若 3 次都撞到，測試失敗訊息會清楚標示是 port race 而非應用邏輯 bug，方便 ops 判讀
- **[Risk] log string matching 脆弱** → Mitigation：偵測字串用 `"shadowdns ready"`（既有穩定 log，寫在 `cmd/shadowdns/main.go` `srv.BoundAddrStrings()` 之後）與 `"address already in use"` / `"bind: no listeners bound"`（前者由 Go runtime stdlib 穩定輸出；後者由 `internal/server/listener.go` 穩定輸出）。若未來改 log 格式，有失敗時 log 一起印出仍可 debug；也可考慮加 init-time assertion 驗證 main 裡仍有該 log 字串
- **[Risk] child process leak on retry** → Mitigation：retry 前務必 `cmd.Process.Signal(SIGKILL)` 並 `cmd.Wait()` 收屍，測試結束時的 cleanup hook 也保留原來的 SIGTERM 路徑
- **[Trade-off] 偶發 retry 會讓測試跑慢 ~100ms-1s** → 可接受：相較 CI 整體 5 分鐘跑完、且 retry 本身是罕見事件

## Migration Plan

一次性切換，不需要漸進 rollout：

1. 實作 `startShadowDNSWithRetry` helper
2. 把既有 `startShadowDNS` caller 改走新 helper（或直接把 `startShadowDNS` 就地改寫，rename 非必要）
3. 本地跑 `go test -run TestIntegration_ConfigNo_SuppressesAllSends -count=50 ./test/integration/` 驗證 stability
4. Push 到 feature branch，觀察 3-5 次 CI 全綠
5. 合入 main

Rollback：直接 revert 整個 change，回到原 helper；production code 未改，無兼容性問題。
