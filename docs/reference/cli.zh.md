# CLI 參考

所有 flag 都只在啟動時解析一次。SIGHUP 會依啟動時記錄的路徑重新讀取 `named.conf`、統一設定檔（`--config`）與 zone file，但**不會**重新解析 flag —— 要改 flag 值必須重啟 process。

## 啟動 flag

| Flag | 預設值 | 必填 | 說明 |
|------|--------|------|------|
| `--named-conf` | — | 是 | `named.conf` 路徑。其中的 `geoip-directory` 選項控制 mmdb 檔案的讀取位置。 |
| `--config` | — | 是 | 統一 ShadowDNS YAML 設定檔路徑（`aliases` + `ephemeral_api` 區段），見 [shadowdns.yaml](../configuration/shadowdns-yaml.md)。 |
| `--listen` | `:53` | 否 | UDP/TCP 監聽位址。含 host 的形式（如 `127.0.0.1:53`、IPv6 bracket 字面值 `[::1]:53`）會覆寫 `named.conf` 的 `listen-on`/`listen-on-v6`，只綁定該單一位址；不含 host 的 `:PORT` 形式則把 port 套用到 `listen-on`（IPv4）與 `listen-on-v6`（IPv6）位址的聯集 —— `listen-on` 不存在時使用所有 IPv4 介面位址，`listen-on-v6` 為 opt-in（不存在就沒有 IPv6 listener）。 |
| `--log-file` | （空，輸出到 stderr） | 否 | 將輸出寫入指定檔案（`O_APPEND\|O_CREATE`，mode 0640）。送 SIGUSR1 讓 daemon 重開檔案（供 logrotate postrotate 使用）。 |
| `--metrics-addr` | `:9153` | 否 | Prometheus `/metrics` HTTP 監聽位址，空字串停用。 |
| `--pprof-enable` | `false` | 否 | 在 metrics HTTP server 上開放 `/debug/pprof/` 端點，需要 `--metrics-addr` 非空。只在啟動時讀取，SIGHUP 不會改變其值。**只應在受信任網路或 loopback 綁定下啟用**：pprof 沒有驗證機制，會回傳 debugger 等級的 runtime 狀態，CPU/trace profile 端點還可被用來讓 process 停頓指定時長。 |
| `--ecs-enable` | `false` | 否 | 啟用 RFC 7871 EDNS Client Subnet 處理。查詢中合法的 ECS option 會驅動 GeoIP view 選擇（僅 country/ASN 規則；IP/CIDR ACL 規則永遠以真實來源 IP 評估），回應會 echo ECS option 且 scope 等於 source prefix length。預設關閉：查詢中的 ECS option 一律忽略、回應不帶 ECS option，行為與 BIND 一致。只在啟動時讀取，SIGHUP 不會改變其值。 |
| `--reload-verify` | `hash` | 否 | SIGHUP reload 時的 zone file 變更偵測策略：`hash`（預設，對 `rsync -avc --inplace` 安全）、`size`（只比 mtime+size，不讀檔）、`none`（一律完整重建）。 |
| `--dry-run` | `false` | 否 | 載入設定與 zone、輸出摘要後結束，不開啟 listener。 |
| `--no-notify` | （未指定） | 否 | 停用整個 process 生命週期的 NOTIFY 發送。未指定時 NOTIFY 依 `named.conf` 的 `options.notify`（預設啟用）；指定時覆寫設定檔指令，且跨 SIGHUP reload 持續有效。 |
| `--no-color` | `false` | 否 | 強制無色 log 輸出。同時尊重 [`NO_COLOR`](https://no-color.org) 環境變數；非 TTY 的 stderr 也會自動偵測並停用色彩。 |
| `-v`, `--version` | `false` | 否 | 印出版本後結束。 |

### NOTIFY 優先序

flag 與設定檔指令同時存在時的規則：

1. 明確指定 `--no-notify` flag → NOTIFY 在 process 生命週期內停用
2. 否則以 `named.conf` 的 `options.notify` 為準（`yes` 或 `no`）
3. 否則 NOTIFY 預設啟用

`--no-notify` 刻意設計成「只能關」—— 要重新啟用 NOTIFY，請拿掉 flag 後重啟，避免 `--no-notify=false` 這種雙重否定造成混淆。

## Subcommand

### shadowdns reload

```bash
shadowdns reload --named-conf /etc/shadowdns/named.conf
```

依 `named.conf` 中設定的 pid-file 找到運行中的伺服器並送 SIGHUP。只接受 `--named-conf`，伺服器啟動用的 flag 會被拒絕。

### shadowdns prune-backup

```bash
shadowdns prune-backup \
    --named-conf /etc/shadowdns/named.conf \
    --config /etc/shadowdns/shadowdns.yaml \
    [--apply]
```

離線比對備援 zone file 與其 alias 的 root zone，回報冗餘紀錄（預設 dry-run），加上 `--apply` 時改寫檔案。不開啟 socket，也不對運行中的伺服器發送 signal。

比對邏輯：

- 對 `named.conf` 中宣告的每組 `(view, backup-zone)`，與同一 view 的 root 對應 zone 比對。
- 不可覆寫的紀錄類型（`TXT`/`MX`/`SRV` 以外的全部）一律標記為冗餘。
- 可覆寫類型只有在整個 RRSet 與 root 完全一致（忽略 TTL 與順序）時才標記。
- `SOA` 與 apex `NS` RRSet 永遠保留，確保 zone file 維持 RFC 1035 有效。
- `--apply` 時每個改寫的檔案以原子方式替換，改寫前的副本保留在 `<path>.bak`。

### shadowdns completion

```bash
shadowdns completion bash|zsh|fish
```

產生指定 shell 的自動補全 script。`.deb` 套件安裝時已自動帶入三種 shell 的 completion。

## Signal

| Signal | 行為 |
|--------|------|
| `SIGHUP` | 熱重載：重新讀取 `named.conf`、`shadowdns.yaml` 與 zone file；有設定 `geoip-directory` 時也重新讀取 GeoIP mmdb。失敗時保持先前狀態。 |
| `SIGUSR1` | 重開 `--log-file` 與 query log 檔案 descriptor（供 logrotate postrotate 使用）。 |
