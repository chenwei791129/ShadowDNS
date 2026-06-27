> 實作對照：spec requirement「ACME account key is persisted and reused across restarts」「systemd unit provides a writable state directory for the ACME account key」「Example configuration pre-fills the ACME account key path」分別由第 1–2、3.1、3.2 章覆蓋。

## 1. Account key 持久化核心邏輯

- [x] 1.1 依設計決策「loadOrCreateAccountKey 載入語意：missing→generate、corrupt→fail-loud」與「account key 以 PKCS#8 PEM 格式儲存」，在 `internal/doh/acme_key.go` 新增 `loadOrCreateAccountKey(path string) (crypto.PrivateKey, error)`：以 `errors.Is(err, fs.ErrNotExist)`（非值比較）判別缺檔；缺檔時 `os.MkdirAll(parent, 0700)`、產生 P256 key、以 PKCS#8 PEM atomic write（同目錄 `os.CreateTemp` → `Write` → `Sync()`（fsync）→ `Close` → `Chmod 0600` → `os.Rename`，每個錯誤分支 `os.Remove` 清掉 temp 檔——對齊 `internal/prunebackup/apply.go` 的 `applyFile` durability 行為，評估抽共用 helper 而非手刻第二份）；路徑可解析時回傳該 key；存在但解析失敗（含 `path` 為目錄產生的 `EISDIR`）時回非 nil error、error 點名 `path` 並區分目錄/壞檔、且不覆寫原檔。完成定義：函式符合上述三分支行為且不漏 fsync／不漏 temp 清理。
- [x] 1.2 [P] 為 `loadOrCreateAccountKey` 撰寫單元測試 `internal/doh/acme_key_test.go`：(a) 不存在路徑→產生檔、stat 權限為 `0600`、再次解析得同一把 key；(b) 同路徑第二次呼叫回傳相同 key（以 `(*ecdsa.PrivateKey).Equal` 比較，非 `==`／`reflect.DeepEqual`）；(c) 寫入毀損內容後呼叫→回非 nil error 且該檔內容未被改寫、且同目錄無殘留 temp 檔；(d) `path` 指向既有目錄→回非 nil error 而非誤判為缺檔。所有 case 以 `t.TempDir()` 下的真實絕對路徑呼叫，不得傳空字串。完成定義：`go test ./internal/doh/ -run AccountKey` 通過。
- [x] 1.3 依設計決策「只持久化 account key，registration 走既有 idempotent 路徑」，將 `newLegoObtainer`（`internal/doh/acme.go`）改為呼叫 `loadOrCreateAccountKey(cfg.AccountKeyFile)` 取得 account key，取代原本的 `ecdsa.GenerateKey`；保留既有 `acmeUser`/`Register` 路徑使其因 key 穩定而 idempotent。同時更新 `acmeUser` 上方現已過時的 doc comment（原述「key lives only in memory (regenerated on every process restart, like the DNS cookie secret)」）使其反映「跨重啟持久化重用」的新行為。此即實現 spec requirement「ACME account key is persisted and reused across restarts」的重用行為。完成定義：obtainer 使用持久化 key、doc comment 不再宣稱 in-memory，現有 `internal/doh` 測試仍通過。

## 2. Config 欄位

- [x] 2.1 依設計決策「新增必填 config 欄位 doh.acme.account_key_file」，在 `internal/shadowdnscfg/config.go` 為 `DoHACMEConfig` 加 `AccountKeyFile string` 欄位、`rawDoHACME` 加 `account_key_file` YAML tag；`buildDoHACME` 驗證為非空且 `filepath.IsAbs` 為真，否則回錯誤點名 `account_key_file`。完成定義：合法絕對路徑載入成功並填入 `AccountKeyFile`；缺欄或相對路徑 fail load。
- [x] 2.2 [P] 在 `internal/shadowdnscfg/doh_test.go` 加測試：缺 `account_key_file`→載入失敗且錯誤含欄位名；相對路徑→失敗；合法絕對路徑→成功且 `cfg.DoH.ACME.AccountKeyFile` 正確。完成定義：`go test ./internal/shadowdnscfg/` 通過。
- [x] 2.3 更新所有「載入有效 doh config」的既有正向 fixture，使其包含新必填欄位，否則它們會因缺 `account_key_file` 而 fail load 連帶讓不相關測試轉紅（修正 review 指出 task 1.3「現有測試仍通過」會被破壞的問題）：(a) `internal/shadowdnscfg/doh_test.go` 內所有有效 YAML 案例補上 `account_key_file`；(b) `cmd/shadowdns/doh_startup_test.go`、`cmd/shadowdns/doh_reload_test.go` 的 `dohYAMLValid` 等有效 config 字串補上該欄位；(c) `internal/doh/acme_integration_test.go`、`internal/doh/helpers_test.go` 中以結構建構 `DoHACMEConfig` 之處，`AccountKeyFile` 填入 `t.TempDir()` 下的真實絕對路徑（不得留空，否則 `newLegoObtainer` 收到空路徑而失敗）。完成定義：`go test ./...` 全綠，無因新欄位而 fail load 的既有測試。

## 3. Packaging

依設計決策「deb 用 StateDirectory 與預填 example 提供預設，不竄改使用者 config」。

- [x] 3.1 [P] 實現 spec requirement「systemd unit provides a writable state directory for the ACME account key」：在 `packaging/shadowdns.service` 的 `[Service]` 區塊新增 `StateDirectory=shadowdns`（保留既有 `ReadWritePaths=/var/log/shadowdns` 與 `RuntimeDirectory=shadowdns`）。完成定義：unit 啟動後 `/var/lib/shadowdns` 由 systemd 以 `0700`、owner=shadowdns 建立。
- [x] 3.2 [P] 實現 spec requirement「Example configuration pre-fills the ACME account key path」：在 `packaging/shadowdns.yaml.example` 的（註解中）`doh.acme` 區塊加入 `account_key_file: "/var/lib/shadowdns/acme/account.key"`，並同步更新檔內 `# Required fields:` 散文清單，把 `acme.account_key_file` 列入必填欄位說明（避免 YAML 片段與上方文字自相矛盾、漏列已成必填的欄位）。完成定義：複製範例並啟用 doh 即帶有指向 state 目錄的絕對路徑，且 Required fields 清單含 `account_key_file`。

## 4. 文件

- [x] 4.1 [P] 更新 `docs/configuration/shadowdns-yaml.md` 與 `docs/configuration/shadowdns-yaml.zh.md`：在 doh.acme 欄位表新增 `account_key_file`（必填、絕對路徑、`0600`、敏感性說明）；同時把頁面內**未註解的 `doh:` 範例片段**補上 `account_key_file`，否則該範例會缺必填欄位、與頁面「All fields are required」的敘述自相矛盾。完成定義：兩語系同步、欄位表與（live）範例皆含新欄位。
- [x] 4.2 [P] 更新 `docs/guides/doh.md` 與 `docs/guides/doh.zh.md`：說明 account key 持久化行為、key 檔位置（`/var/lib/shadowdns/acme/account.key`）、權限與敏感性、缺檔/壞檔語意，並載明變更 `account_key_file` 需重啟才生效（reload/SIGHUP 會記錄「restart to apply」DoH drift，與其他 acme 欄位一致）。完成定義：兩語系同步說明上述行為。

## 5. 驗證

- [x] 5.1 執行 `make test`（race 全綠）與 `make lint`（無新增問題）。完成定義：兩者皆通過。
- [x] 5.2 執行 `make docs-build`（`--strict`）確認文件無壞連結與 nav 不一致。完成定義：build 成功。
