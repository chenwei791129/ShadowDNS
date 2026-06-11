# 安裝

ShadowDNS 提供兩種安裝方式：從原始碼編譯，或在 Debian/Ubuntu 上以 `.deb` 套件安裝（內含 systemd service、logrotate 設定與 shell completion）。

## 從原始碼編譯

前置條件：Go 1.26+。

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

## 容器內端對端測試（開發用）

```bash
make test-deb    # 需要 podman 或 docker
```
