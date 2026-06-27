## Why

DoH 的 ACME client 每次行程啟動都重新在記憶體裡產生一把新的 account key 並向 CA 註冊一個全新帳號（`internal/doh/acme.go` 的 `newLegoObtainer`）。註冊失敗時，lazy obtainer 只在成功才快取，因此每次重試又重建一把新 key、再註冊一次。ACME 的 new-account 限制是「以來源 IP 計算」、獨立於憑證簽發之外；在頻繁重啟（crash loop、supervisor 自動重啟）或註冊持續失敗（CA 短暫不可達、challenge 埠尚未就緒）時，會把 new-account 配額用盡，連合法簽發都被擋數小時，把一個可復原的小故障放大成自我造成的長時間封鎖。

## What Changes

- 把 ACME account private key 持久化到磁碟，跨重啟與重試重用同一把 key。CA 對已知 key 的 `newAccount` 回傳既有帳號（RFC 8555 §7.3），不消耗 new-account 配額，因此重新註冊變為 idempotent。
- 新增 config 欄位 `doh.acme.account_key_file`（絕對路徑），**僅在啟用 `doh` 區塊時為必填**；沿用現有 strict 載入風格，缺欄或非絕對路徑即 fail load。v0.x 實驗階段，視為可接受的 config 變更（不標記 BREAKING）。
- 載入語意：檔案不存在→產生新 key、以 `0600` atomic 寫入（temp + fsync + rename）後使用；檔案存在但無法解析→**大聲失敗**（回報明確錯誤），絕不靜默改鑄新帳號。寫入前確保父目錄存在。因 `loadOrCreateAccountKey` 是由 lazy obtainer 在每次 obtain 重試時呼叫（非僅啟動一次），壞檔錯誤會於首次 obtain 即浮現並在每次重試重現，不會被靜默吞掉。
- registration 仍走既有路徑，但因 key 穩定而 idempotent；不持久化 registration resource。
- packaging：`shadowdns.service` 新增 `StateDirectory=shadowdns`（systemd 自動建立 `/var/lib/shadowdns`，`0700`、owner=shadowdns）；`shadowdns.yaml.example` 的 `doh.acme` 區塊預填推薦值 `account_key_file: "/var/lib/shadowdns/acme/account.key"`，使用者複製範例即自帶預設，無需 postinstall 竄改使用者 config。
- 文件：說明 key 檔位置、敏感性與權限。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `doh-endpoint`: ACME account key 改為持久化於磁碟並跨重啟/重試重用；新增必填 config 欄位 `doh.acme.account_key_file`；定義 missing→generate、corrupt→fail-loud 的載入語意與 `0600` 權限。
- `deb-packaging`: systemd unit 新增 `StateDirectory=shadowdns` 提供可寫的 `/var/lib/shadowdns`；example config 預填 `account_key_file` 推薦值。

## Impact

- Affected specs: `doh-endpoint`、`deb-packaging`
- Affected code:
  - Modified:
    - internal/doh/acme.go
    - internal/shadowdnscfg/config.go
    - packaging/shadowdns.service
    - packaging/shadowdns.yaml.example
    - docs/configuration/shadowdns-yaml.md
    - docs/configuration/shadowdns-yaml.zh.md
    - docs/guides/doh.md
    - docs/guides/doh.zh.md
    - internal/shadowdnscfg/doh_test.go
    - cmd/shadowdns/doh_startup_test.go
    - cmd/shadowdns/doh_reload_test.go
    - internal/doh/acme_integration_test.go
    - internal/doh/helpers_test.go
  - New:
    - internal/doh/acme_key.go
    - internal/doh/acme_key_test.go
