## Context

DoH 的 ACME client（`internal/doh/acme.go`）目前在 `newLegoObtainer` 內每次都呼叫 `ecdsa.GenerateKey` 產生一把只存在記憶體的 account key，再 `Registration.Register` 註冊新帳號。`newLazyLegoObtainer` 只在成功時快取 obtainer，因此註冊失敗後的每次重試都會重建一把新 key 並重新註冊。

ACME 的 new-account 速率限制以來源 IP 計算，獨立於憑證簽發配額。頻繁重啟或註冊持續失敗時，每次都鑄一個新帳號，會把 new-account 配額用盡，連合法簽發都被擋。憑證簽發本身從來不是瓶頸；問題出在「每次啟動／重試都換一把 key＝一個新帳號」。

現況限制：systemd unit（`packaging/shadowdns.service`）採 `ProtectSystem=strict`，目前唯一可寫路徑是 `ReadWritePaths=/var/log/shadowdns`；沒有可供放置持久狀態的目錄。config 載入採 strict（`KnownFields(true)`），每個欄位必填、缺欄即 fail load。

## Goals / Non-Goals

**Goals:**

- 跨重啟與註冊重試重用同一把 ACME account key，使重新註冊 idempotent，徹底避免 new-account churn。
- key 檔以 `0600` 權限、owner 為服務使用者儲存；缺檔自動產生、壞檔大聲失敗。
- 新增的 config 欄位沿用現有 strict 風格；deb 使用者複製 example 即自帶合理預設。

**Non-Goals:**

- 不持久化 registration resource。重新註冊（以穩定 key）對 CA 而言 idempotent 且不消耗 new-account 配額，存 registration 只能省下每次啟動一次的既有帳號查詢，對修復 rate-limit 無幫助（YAGNI）。
- 不持久化每張憑證的私鑰／憑證本身。憑證仍為短效（~6 天）自動續期，續期不受 account key 變動影響；憑證快取是另一個獨立議題。
- 不為 account key 檔做加密或外部 KMS 整合；以檔案系統權限（`0600` + 專用使用者 + systemd sandbox）保護。
- 不改動 HTTP-01 challenge、憑證 profile、續期排程等既有行為。
- 不開新 Go package；seam 留在 `internal/doh` 套件內。

## Decisions

### 只持久化 account key，registration 走既有 idempotent 路徑

持久化對象僅為 ECDSA account private key。`Registration.Register` 仍在 obtainer 建立時呼叫一次；因為 key 穩定，CA 對已知 key 的 `newAccount` 回傳既有帳號（RFC 8555 §7.3），不計入 new-account 配額。

替代方案：連同 registration resource 一併序列化存檔，啟動時略過 Register 呼叫。否決理由：增加 JSON 序列化與失效處理複雜度，但對「修掉 rate limit」無實質幫助——既有帳號查詢本來就不消耗配額。

### account key 以 PKCS#8 PEM 格式儲存

以 `x509.MarshalPKCS8PrivateKey` 編碼、PEM 包成 `PRIVATE KEY` 區塊寫檔；讀取時 `pem.Decode` 後 `x509.ParsePKCS8PrivateKey`。

替代方案：SEC1 `EC PRIVATE KEY`（`x509.MarshalECPrivateKey`）。PKCS#8 較通用、與未來可能換 key 型別相容性較佳，且編解碼 API 對稱清楚。

### loadOrCreateAccountKey 載入語意：missing→generate、corrupt→fail-loud

在 `internal/doh` 新增檔案 `acme_key.go`，提供 `loadOrCreateAccountKey(path string) (crypto.PrivateKey, error)`。前置條件：`path` 由呼叫端保證為非空絕對路徑（production 由 `buildDoHACME` 驗證；測試呼叫端 MUST 傳入 `t.TempDir()` 下的真實路徑，不得傳空字串）。三分支：

- 檔案不存在（以 `errors.Is(err, fs.ErrNotExist)` 判別，非值比較，避免 wrapped error 漏判）→ 產生新 P256 key，確保父目錄存在（`os.MkdirAll`，`0700`），以 atomic write 落地（同目錄 `os.CreateTemp` → `Write` → `Sync()`（fsync，確保斷電後不致殘留零長度檔）→ `Close` → `Chmod 0600` → `os.Rename`，每個錯誤分支都 `os.Remove` 清掉 temp 檔），回傳新 key。
- 檔案存在但讀取／解析失敗（壞檔、權限、`path` 指向目錄產生的 `EISDIR` 等）→ 回傳明確 error（error 訊息點名 `path` 並區分「不是有效 key 檔」與「路徑是目錄」），**絕不**改鑄新 key、**絕不**覆寫該路徑。
- 檔案存在且可解析 → 回傳該 key。

`newLegoObtainer` 改為呼叫 `loadOrCreateAccountKey(cfg.AccountKeyFile)` 取得 key，取代原本的 `ecdsa.GenerateKey`。注意 `newLazyLegoObtainer` 只在成功時快取 obtainer，故 `loadOrCreateAccountKey` 會在每次 obtain 重試時被呼叫——壞檔會在每次重試重現明確錯誤（由 `certManager` 記錄 log + metric），不是僅啟動一次。

複用既有 atomic-writer：`internal/prunebackup/apply.go` 的 `applyFile` 已實作「同目錄 temp → `Sync` → `Chmod` → `Rename` + 錯誤清理」的相同 dance。實作時 SHALL 評估抽出共用 helper 或至少完整對齊其 fsync 與清理行為，不得退化成漏 fsync／漏清理的第二份手刻版本。`applyFile` 帶有 `.bak`／既有檔語意，若不直接套用，design 此處明確記錄改以對齊其durability行為的最小重寫。

替代方案：壞檔時備份舊檔並重新產生。否決理由：Issue 明確要求壞檔 fail loudly，靜默重生會重現 rate-limit 風險。

### 新增必填 config 欄位 doh.acme.account_key_file

於 `DoHACMEConfig` 加 `AccountKeyFile string` 欄位、`rawDoHACME` 加 `account_key_file` YAML tag；`buildDoHACME` 驗證為非空且絕對路徑（`filepath.IsAbs`），否則 fail load。僅在 `doh` 區塊存在時才會走到此驗證，故未啟用 DoH 的使用者不受影響。

替代方案：給內建預設值、省略即用。否決理由：與現有「每欄必填、不給隱含預設」風格不一致；改以 example 預填提供便利（見下一決策）。

### deb 用 StateDirectory 與預填 example 提供預設，不竄改使用者 config

- `shadowdns.service` 新增 `StateDirectory=shadowdns`：systemd 自動建立 `/var/lib/shadowdns`（`0700`、owner=shadowdns），提供 sandbox 下可寫的持久狀態目錄。
- `shadowdns.yaml.example` 的（註解中的）`doh.acme` 區塊預填 `account_key_file: "/var/lib/shadowdns/acme/account.key"`。使用者複製範例即自帶正確預設。

替代方案：在 `postinstall.sh` 把預設值寫進使用者實際 config。否決理由：deb 不出貨 live config（只出 `.example`），且程式化竄改 conffile 是 Debian 反模式。

## Implementation Contract

**Behavior:**

- 首次啟動（key 檔不存在）：產生 account key、以 `0600` 寫入設定路徑、註冊帳號、簽發憑證；DoH 正常服務。
- 後續重啟：載入同一把 key 檔；CA 回傳既有帳號，不產生新帳號註冊。
- 註冊持續失敗：renewal loop 以固定間隔重試，每次重試重用同一把已持久化的 key，不鑄新帳號。
- key 檔存在但毀損：obtain 回報明確錯誤、不靜默產生新 key／新帳號；因 obtainer 未被快取，錯誤會在每次重試重現，DoH 在修復前無法取得憑證（GetCertificate 持續回錯），這是「fail loudly」的刻意取捨。

**Interface / data shape:**

- config：`doh.acme.account_key_file`（string，YAML），必填、絕對路徑。對應 `shadowdnscfg.DoHACMEConfig.AccountKeyFile`。
- 函式：`loadOrCreateAccountKey(path string) (crypto.PrivateKey, error)`（`internal/doh`）。
- 磁碟格式：PKCS#8 PEM（`-----BEGIN PRIVATE KEY-----`）、權限 `0600`、父目錄 `0700`。
- systemd：`shadowdns.service` 的 `[Service]` 區塊含 `StateDirectory=shadowdns`。

**Failure modes:**

- 缺 `account_key_file` 或非絕對路徑 → config 載入錯誤、不啟動 DoH（與既有必填欄位一致）。
- key 檔解析失敗 → `loadOrCreateAccountKey` 回非 nil error，obtainer 建立失敗，憑證取得失敗並依既有 renewal loop 記錄（log + metric）；不靜默改鑄。
- `account_key_file` 指向既有目錄 → 讀取得到 `EISDIR`（非 `fs.ErrNotExist`），歸類為錯誤分支並回明確 error（不視為「缺檔」而誤鑄新 key）。
- 父目錄建立或 atomic write（含 fsync／rename）失敗 → 回 error 並清掉 temp 檔，同上路徑處理。

**Acceptance criteria:**

- 單元測試（`internal/doh`）：(a) 路徑不存在→產生檔案且權限為 `0600`、內容可被再次解析為同一把 key；(b) 同一路徑第二次呼叫回傳與第一次相同的 key（位元組相等）；(c) 路徑指向毀損內容→回非 nil error 且不覆寫該檔。
- 單元測試（`internal/shadowdnscfg`）：缺 `account_key_file`、相對路徑 → 載入失敗並於錯誤訊息點名該欄位；合法絕對路徑 → 載入成功且 `AccountKeyFile` 正確。
- `make test` 全綠、`make lint` 無新增問題、`make docs-build`（strict）通過。

**Scope boundaries:**

- In scope：account key 持久化、config 欄位、systemd StateDirectory、example 預填、docs 更新（`shadowdns-yaml.md`/`.zh.md`、`doh.md`/`.zh.md`）。
- Out of scope：registration resource 持久化、憑證／憑證私鑰快取、key 加密／KMS、HTTP-01 與續期排程行為變更。

## Risks / Trade-offs

- [key 檔外洩風險] → `0600` + 專用服務使用者 + systemd `ProtectSystem=strict`/`StateDirectory`（`0700`）；docs 明確標註敏感性。
- [使用者把 `account_key_file` 指到唯讀或 sandbox 外路徑] → 啟動時 atomic write 失敗並回明確 error；docs 與 example 指引使用 `/var/lib/shadowdns` 之下。
- [舊有部署升級後缺欄位導致載入失敗] → v0.x 實驗階段可接受 config 變更；example 已預填，升級指引於 docs 說明需新增該欄位。
- [壞檔 fail-loud 讓 DoH 起不來] → 這是刻意取捨：相較於靜默重鑄帳號觸發 rate-limit，明確失敗讓 operator 立即發現並修復是較安全的預設。
- [SIGHUP reload 行為] → `AccountKeyFile` 加入 `DoHACMEConfig` 後，會被 `cmd/shadowdns/main.go` 既有的整值 DoH drift 偵測（`dohConfigEqual`）納入；reload 時若該欄位變動，會記錄「restart to apply」提示。這與其他 acme 欄位一致、屬預期行為（key 路徑變更需重啟），docs 應載明此欄位變更需重啟才生效。
- [持久化保證依賴靜態服務使用者] → key 重用依賴 `User=shadowdns` 為固定 uid；若 unit 未來改用 `DynamicUser=yes`，每次開機 uid 變動會使既有 key 檔不可讀、`StateDirectory` 擁有者改變，靜默重現 new-account churn。deb-packaging spec 將「靜態服務使用者」列為持久化保證的前置條件。
