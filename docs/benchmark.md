# ShadowDNS 啟動效能基準測試

本文記錄 `--dry-run` 啟動煙霧測試的執行方式與樣本輸出。

## 什麼是 `--dry-run`？

`--dry-run` flag 讓 ShadowDNS 執行完整的載入流程（解析 `named.conf`、讀取所有 zone 檔、載入 GeoIP mmdb），但在啟動 UDP/TCP listener **之前**即退出並印出摘要。適用於：

- 確認設定檔語法正確
- 在不影響 DNS 服務的情況下計算記憶體基線
- CI/CD pipeline 的設定驗證步驟

## 建置步驟

```bash
# 在 repo 根目錄執行
go build -o ./shadowdns ./cmd/shadowdns
```

## 使用方式

```bash
./shadowdns \
    --named-conf /path/to/named.conf \
    --aliases    /path/to/aliases.yaml \
    --dry-run
```

成功時以 exit code 0 退出，並輸出：

```
level=INFO msg="dry-run: configuration loaded successfully" views=N zones=M
```

## 自動化煙霧測試腳本

`scripts/smoke.sh` 自動完成以下步驟：

1. 建置 binary
2. 將 `testdata/integration/` 複製到 `/tmp/shadowdns-smoke/`，替換 `TESTDATA_DIR_PLACEHOLDER`
3. 產生測試用 GeoIP mmdb 檔
4. 以 `/usr/bin/time` 執行 `--dry-run`，記錄記憶體使用量

```bash
./scripts/smoke.sh
```

## 樣本輸出（2026-04-13，Apple Silicon，testdata/integration 固件）

執行環境：macOS Darwin 24.6.0，Apple M 系列，Go 1.25.6

```
time=2026-04-13T23:28:01.556+08:00 level=INFO msg="shadowdns starting" \
    named_conf=...named.conf aliases=...aliases.yaml listen=:53
time=2026-04-13T23:28:01.557+08:00 level=INFO msg="loaded GeoIP country database" \
    path=.../geoip/GeoLite2-Country.mmdb
time=2026-04-13T23:28:01.558+08:00 level=INFO msg="loaded GeoIP ASN database" \
    path=.../geoip/GeoLite2-ASN.mmdb
time=2026-04-13T23:28:01.558+08:00 level=INFO msg="dry-run: configuration loaded successfully" \
    views=2 zones=4
        0.31 real         0.00 user         0.01 sys
             8437760  maximum resident set size
```

| 指標                           | 值                |
|-------------------------------|-------------------|
| 載入時間（real）               | 0.31 s            |
| 最大 RSS（maximum resident set size） | 8,437,760 bytes（≈ 8.0 MB） |
| 視圖數（views）                | 2                 |
| 載入 zone 數（zones）          | 4                 |

## 注意事項

- **此固件極小**：僅有 2 個 view × 2 個 zone（1 root + 1 backup），記憶體幾乎全由 Go runtime 佔用。
- **正式部署規模估算**：依照 Context 中的數字（3,600 root domains × 7 views，平均 zone 10 KB），ShadowDNS 預計僅載入 root zones（不重複載入 backup），記憶體約為 `3,600 × 7 × 10 KB ≈ 252 MB` 加上 GeoIP mmdb（約 60–80 MB），合計約 **330–350 MB**，相較 BIND 的 ~630 MB（含冗餘 backup）節省約 45–50%。
- 實際生產記憶體必須以生產規模設定檔實測；`--dry-run` 提供基線，實際監聽後 Go runtime 可能因 goroutine stack 等因素略增。
- 建議在 CI 加入 `./scripts/smoke.sh` 步驟，確保每次合併後設定檔能正確載入。
