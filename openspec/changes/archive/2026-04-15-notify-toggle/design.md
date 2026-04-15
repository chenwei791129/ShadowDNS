## Context

ShadowDNS 目前對 NOTIFY 無任何開關。`dispatchNotifies()` 在 [cmd/shadowdns/main.go:278](cmd/shadowdns/main.go#L278)（啟動路徑）與 [cmd/shadowdns/main.go:65](cmd/shadowdns/main.go#L65)（reload 路徑）被無條件呼叫，會為每個 zone 的每個 NS target 啟一條 goroutine 發送 NOTIFY，失敗則以 `1s + 2s + 4s` backoff 重試 3 次。

[internal/config/options.go](internal/config/options.go) 的 `OptionsBlock` 使用值型別欄位（`bool`、`string`、`[]string`），未設定的欄位無法與「顯式設為零值」區分。既有 `recursion` 與 `minimal-responses` 的 `yes/no` 解析是本 change 可以直接沿用的 pattern。

CLI flag 解析用標準 library `flag`，[cmd/shadowdns/main.go:139](cmd/shadowdns/main.go#L139) 以 `flag.BoolVar` 註冊所有 bool flags。

## Goals / Non-Goals

**Goals:**

- 使用者可透過 CLI flag **或** config 停用 NOTIFY
- CLI flag 僅在使用者顯式傳遞時生效，不因 flag 的 default value 意外覆蓋 config
- CLI flag 對整個 process lifetime 生效，SIGHUP reload 不會解除停用
- 預設行為與現況一致，避免升級 regression
- 語法與 BIND 對齊，維持「BIND-compatible subset」的專案承諾

**Non-Goals:**

- zone-level `notify` override（BIND 支援，但本專案維持全域單一設定）
- `notify explicit` 模式（依賴尚未實作的 `also-notify`）
- `also-notify { ... }` directive（列為 future work，與本 change 正交）
- 動態在 runtime 切換 notify 開關而不透過 SIGHUP reload

## Decisions

### 優先順序語意：顯式 flag > config > default

正式定義：

```
notify_enabled =
  if CLI 顯式傳遞 -no-notify          → false
  else if config 有設 options.notify  → config 的值
  else                                → true (default)
```

**Alternatives considered:**

- **Flag default 也參與決策**（拒絕）：採此方案後 `-no-notify=false`（未傳）會被視為「要求 notify=on」，config 的 `notify no;` 會被 flag default 踩過去，config 面名存實亡。
- **`-notify=true|false` 三態 string flag**（拒絕）：語法較繁冗，與既有 bool flag 風格不符（`-dry-run`、`-reload`）。

### CLI flag 顯式性偵測：使用 `flag.Visit`

Go 標準 `flag` 套件無法從 `*bool` 值直接區分「未傳」與「傳了 false」。解法是在 `flag.Parse()` 後呼叫 `flag.Visit(fn)`，它**只會 callback 被使用者實際傳過的 flag**。

```go
var noNotifyFlag bool
flag.BoolVar(&noNotifyFlag, "no-notify", false, "...")
flag.Parse()

var noNotifyExplicit bool
flag.Visit(func(f *flag.Flag) {
    if f.Name == "no-notify" {
        noNotifyExplicit = true
    }
})
```

**Alternatives considered:**

- **自訂 `flag.Value` 實作追蹤 `Set()` 呼叫**（拒絕）：可行但多出一個 type，`flag.Visit` 是 stdlib idiomatic 解法。

### `OptionsBlock.Notify` 欄位型別：`*bool`

為表達「config 有未設、設 yes、設 no」三態，使用 `*bool`：

| config 狀態 | `Notify` 值 |
|---|---|
| 未寫 `notify` | `nil` |
| `notify yes;` | `&true` |
| `notify no;` | `&false` |

**Alternatives considered:**

- **`Notify bool` + `NotifySet bool`**（拒絕）：多一個欄位要同步維護，忘記設 `NotifySet` 就會悄悄出 bug。`*bool` 把「有無」的語意綁在型別上。
- **`Notify bool`，default `true`**（拒絕）：無法區分「使用者設了 yes」與「使用者沒設」，CLI flag 的優先順序邏輯無法正確實作。

### CLI flag 為 process-lifetime sticky

若啟動時帶了 `-no-notify`，後續 SIGHUP reload 即使 config 改為 `notify yes;` 也不恢復。

**Rationale:**
- 符合「CLI flag 代表運維顯式意圖」的直覺
- 避免使用者改 config 後「咦 reload 後又開始發 NOTIFY 了？」的意外
- 實作簡單：`resolveNotifyEnabled()` 的輸入包含已解析的 `noNotifyExplicit`（process 常數）與新讀到的 `cfg.Options.Notify`（每次 reload 變動），只要 flag 為 true 就短路回 `false`

**Alternatives considered:**

- **Reload 時以 config 為準（即使 CLI 帶了 flag）**（拒絕）：違反「flag > config」的直覺語意，而且讓 CLI flag 的效力變得依賴當下 config，增加不可預測性。

### 解析函式集中化：新增 `resolveNotifyEnabled()` helper

```go
// cmd/shadowdns/main.go
func resolveNotifyEnabled(noNotifyExplicit bool, configNotify *bool) bool {
    if noNotifyExplicit {
        return false
    }
    if configNotify != nil {
        return *configNotify
    }
    return true
}
```

啟動與 reload 兩條路徑都呼叫這個 helper。優點：
- 優先順序邏輯集中在一處，易測試、易改動
- `dispatchNotifies()` 本身不需改介面，呼叫端加 guard 即可

## Risks / Trade-offs

- **[Risk] 升級後使用者忘記自己本來就沒 secondary，不知道可以關 NOTIFY**
  → Mitigation：在 [packaging/named.conf.example](packaging/named.conf.example) 加註解範例；README 更新 NOTIFY 段落說明新開關

- **[Risk] 使用者誤以為 `-no-notify` 也能「強制開啟 NOTIFY」（double negative 混淆）**
  → Mitigation：flag 描述文字寫清楚「only disables; to enable, omit this flag」；文件與 config 範例中示範三種組合

- **[Risk] 三處決策點（CLI、config、default）讓使用者 debug「為什麼沒發 NOTIFY」變複雜**
  → Mitigation：啟動時 logger.Info 記錄「notify enabled: true/false (source: flag|config|default)」，讓運維在 journalctl 一眼就能看到最終生效值與來源

- **[Trade-off] `Notify *bool` 破壞 `OptionsBlock` 內所有欄位皆為值型別的一致性**
  → 值得：正確性 > 一致性。未來若有其他 directive 也需要「三態」（區分未設 vs 顯式零值），這個 pattern 可以沿用
