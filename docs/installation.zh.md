# 安裝

ShadowDNS 可從原始碼編譯、在 Debian/Ubuntu 上安裝 `.deb` 套件，或使用發布於 GHCR 的官方 linux/amd64 container image。

## 從原始碼編譯

前置條件：Go 1.27+。

```bash
git clone https://github.com/chenwei791129/ShadowDNS.git
cd ShadowDNS
make build
```

Binary 產出於 `bin/shadowdns-<GOOS>-<GOARCH>`。如需在 macOS 上交叉編譯 linux/amd64 的部署用 binary：

```bash
make build-linux    # 產出 bin/shadowdns-linux-amd64
```

## .deb 套件安裝

### 建置套件

```bash
make deb    # 隱含執行 make build-linux 與 make completions
```

### 安裝

```bash
sudo dpkg -i shadowdns_<version>_amd64.deb
```

### 套件安裝內容

| 路徑 | 內容 |
|------|------|
| `/usr/bin/shadowdns` | 主程式 |
| `/lib/systemd/system/shadowdns.service` | systemd service unit |
| `/etc/logrotate.d/shadowdns` | logrotate 設定（每日輪替 `/var/log/shadowdns/*.log`，postrotate 送 SIGUSR1 讓 daemon 重開 log 檔） |
| `/etc/shadowdns/named.conf.example` | `named.conf` 範例 |
| `/etc/shadowdns/shadowdns.yaml.example` | `shadowdns.yaml` 範例 |
| `/usr/share/bash-completion/completions/shadowdns` | bash completion |
| `/usr/share/zsh/vendor-completions/_shadowdns` | zsh completion |
| `/usr/share/fish/vendor_completions.d/shadowdns.fish` | fish completion |

安裝時的 postinstall script 會自動：

- 建立 `shadowdns` 系統使用者與群組（如不存在）
- 建立 `/var/log/shadowdns` log 目錄（owner `shadowdns:shadowdns`，mode 0750）
- 執行 `systemctl daemon-reload`

### systemd 服務

套件附帶的 service unit 以下列參數啟動：

```text
/usr/bin/shadowdns \
    --named-conf /etc/shadowdns/named.conf \
    --config     /etc/shadowdns/shadowdns.yaml \
    --log-file   /var/log/shadowdns/shadowdns.log
```

因此啟用服務前，請先把設定檔放到 `/etc/shadowdns/`（可從同目錄的 `.example` 檔案複製修改）：

```bash
sudo cp /etc/shadowdns/named.conf.example     /etc/shadowdns/named.conf
sudo cp /etc/shadowdns/shadowdns.yaml.example /etc/shadowdns/shadowdns.yaml
# 編輯兩個檔案以符合你的環境後：
sudo systemctl enable --now shadowdns
```

Service unit 的安全強化重點：

- 以非特權使用者 `shadowdns` 執行，透過 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 綁定 53 port
- `ProtectSystem=strict` 沙箱，僅 `/var/log/shadowdns` 可寫
- `RuntimeDirectory=shadowdns` 於每次啟動建立 `/run/shadowdns`，供預設的 `pid-file "/var/run/shadowdns/pid"` 使用
- `ExecReload` 對應 SIGHUP，因此 `systemctl reload shadowdns` 即可熱重載設定

### 驗證安裝

```bash
shadowdns --version
sudo systemctl status shadowdns
```

應用層 log 位於 `/var/log/shadowdns/shadowdns.log`。

## Container Image

官方 image **僅支援 linux/amd64**。正式部署應使用精確版本 tag（或 digest）；`latest` 會指向最新 release。

```bash
docker pull ghcr.io/OWNER/shadowdns:vX.Y.Z
```

Image 使用 Distroless nonroot runtime，並以 UID/GID `65532` 執行；其中沒有 shell、package manager、診斷 client、內嵌設定或 Docker `HEALTHCHECK`。預設參數為：

```text
--named-conf /etc/shadowdns/named.conf
--config /etc/shadowdns/shadowdns.yaml
--listen 0.0.0.0:5353
--no-color
```

明確 listener 僅支援 IPv4，並覆寫 mounted BIND 設定中的宿主專用 `listen-on` 與 `listen-on-v6` 位址；此 image 預設不提供 first-class dual-stack container listening。

請先準備完整的 `/etc/shadowdns` 目錄樹，包含 `named.conf`、`shadowdns.yaml`、所有 include 檔案、zone files 與 GeoIP databases。Include paths 會相對於 include 所在檔案解析，但相對的 `options { directory "zones"; };` 是從 container working directory 解析，不會自動相對於 `/etc/shadowdns`；請改用 `/etc/shadowdns/zones` 等 container 內 absolute path。設定中其他 absolute paths 同樣必須以相同路徑額外掛載，或改寫至 `/etc/shadowdns` 之下；只掛載 `/etc/shadowdns` 不會自動 remap `/srv/zones`。請將下方 `OWNER` 與 `vX.Y.Z` 替換成 package owner及GHCR中確實存在的tag，再執行：

```bash
docker run --rm --name shadowdns \
  -p 53:5353/udp \
  -p 53:5353/tcp \
  -p 9153:9153/tcp \
  --mount type=bind,src=/srv/shadowdns/config,dst=/etc/shadowdns,readonly \
  ghcr.io/OWNER/shadowdns:vX.Y.Z
```

Operational logs 預設寫到 stderr，應交由 container runtime 收集。要重新載入 mounted 設定，請直接向 container 傳送 SIGHUP；SIGTERM 會執行 graceful shutdown：

```bash
docker kill --signal HUP shadowdns
docker stop --time 10 shadowdns
```

Health probes 由部署端設定，因為 image 不含 `HEALTHCHECK`。Readiness 可用外部 DNS client 查詢 authoritative zones 中已知存在的 record；liveness 可視需要探測 `http://<container-address>:9153/metrics`。

### 可寫狀態與選用 Listeners

設定 mount 可保持唯讀。若啟用 DoH ACME，請將 `/var/lib/shadowdns` 掛載到 persistent storage，並確保 UID `65532` 可寫；如此 ACME account key 才能跨 container replacement 重用。File-backed main log 或 query log同樣必須使用明確掛載、由 UID `65532` 擁有的可寫路徑；image 絕不 fallback 為 root。

DoH HTTPS、ACME HTTP-01 與 ephemeral API 必須使用大於 1023 的 container ports，再將外部 443 與 80 port map 或 route 至這些非特權 listeners。

### 首次發布 GHCR Package

第一次 release workflow 推送 package 後，package owner 必須在 GitHub package settings：

1. 將 visibility 改為 **Public**。
2. 確認 package 已連結 source repository。
3. 從未登入的環境驗證 anonymous pull。
4. 確認精確 release tag 與 `latest` 指向相同 image digest。

GitHub Packages API 與 `gh` CLI 目前沒有可修改 package visibility 的受支援操作，因此這是一次性的 UI 步驟；後續 release 會保留既有 public visibility。

## 容器內端對端測試（開發用）

```bash
make test-deb          # 需要 podman 或 docker
make container-image   # 建置 linux/amd64 的 shadowdns:dev
make verify-container  # 驗證本機 image 契約
```
