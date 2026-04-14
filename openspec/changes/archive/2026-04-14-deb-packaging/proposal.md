## Why

ShadowDNS 目前只能透過 `go build` 從原始碼編譯安裝，缺乏標準化的部署方式。目標環境是 Debian/Ubuntu 伺服器，需要一個 `.deb` 安裝包讓 ops 團隊能用 `dpkg -i` 快速部署，包含 systemd service unit 和範例配置檔，實現「安裝即可啟動」的體驗。不需要上架 apt 套件庫。

## What Changes

- 新增 nfpm 配置檔，定義 `.deb` 包的內容、metadata 和安裝腳本
- 新增 systemd service unit file（`shadowdns.service`），支援 `systemctl start/stop/reload`
- 新增範例配置檔（`named.conf.example`、`aliases.yaml.example`）
- 在 Makefile 新增 `make deb` target，一鍵產生 `.deb` 包
- 在 Makefile 新增 `make test-deb` target，使用 podman 容器自動化測試 `.deb` 安裝（cross-compile linux binary → 打包 → 在 Ubuntu 容器中安裝 → 驗證檔案佈局、user 建立、binary 執行、DNS 查詢）
- 安裝佈局遵循 FHS：
  - `/usr/bin/shadowdns` — binary
  - `/etc/shadowdns/named.conf.example` — 範例 named.conf
  - `/etc/shadowdns/aliases.yaml.example` — 範例 aliases.yaml
  - `/lib/systemd/system/shadowdns.service` — systemd unit

## Non-Goals

- **不上架 apt 套件庫**：僅產生 `.deb` 檔，手動 `dpkg -i` 安裝
- **不做 cross-compile**：先支援 `amd64` 單一架構，未來有需求再擴展
- **不包含 GeoIP mmdb 檔案**：MaxMind 授權限制，用戶需自行下載
- **不包含實際的 named.conf**：用戶有既有 BIND 配置，僅提供範例供參考
- **不做 RPM 或其他格式**：僅 `.deb`

## Capabilities

### New Capabilities

- `deb-packaging`: 使用 nfpm 將 ShadowDNS 打包為 `.deb` 安裝包，包含 binary、systemd service unit、範例配置檔及安裝腳本

### Modified Capabilities

（無）

## Impact

- 新增檔案：`nfpm.yaml`、`packaging/shadowdns.service`、`packaging/named.conf.example`、`packaging/aliases.yaml.example`、`scripts/gen-container-testdata.go`、`scripts/test-deb.sh`
- 修改檔案：`Makefile`（新增 `deb`、`test-deb` targets）、`.gitignore`（忽略 `*.deb` 產物）
- 新增工具依賴：`nfpm`（僅建置時需要，不影響 runtime）
- 新增測試依賴：`podman`（僅測試時需要，用於容器化驗證）
