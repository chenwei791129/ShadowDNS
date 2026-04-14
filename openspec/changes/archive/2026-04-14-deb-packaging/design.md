## Context

ShadowDNS 是一個 Go 編譯的單一 binary 權威 DNS 伺服器，目前只能從原始碼建置。目標部署環境為 Debian/Ubuntu Linux 伺服器，ops 團隊需要用 `dpkg -i` 安裝。專案已有 Makefile 管理建置流程（`make build`），需要在此基礎上擴展打包能力。

## Goals / Non-Goals

**Goals:**

- 透過 `make deb` 一鍵產生可安裝的 `.deb` 包
- 安裝後提供 systemd service unit，支援 `systemctl` 管理生命週期
- 提供範例配置檔，降低首次部署的門檻
- 遵循 FHS 和 Debian 慣例的安裝路徑

**Non-Goals:**

- 不建立 apt repository 或 PPA
- 不做多架構 cross-compile（僅 amd64）
- 不包含 GeoIP mmdb 檔案（授權限制）
- 不做 RPM 或其他格式的打包

## Decisions

### 使用 nfpm 而非 dpkg-deb 手工打包

nfpm 是一個輕量級的打包工具，專為 Go 專案設計，透過一個 YAML 配置檔即可產生 `.deb`（和 `.rpm`）。相比 dpkg-deb 手工建立 `DEBIAN/control` 和目錄結構，nfpm 更簡潔且易維護。

**替代方案：**
- `dpkg-deb`：需要手動管理 `DEBIAN/` 目錄結構、control file、maintainer scripts，維護成本較高
- `goreleaser`：功能強大但過於重量級，包含 release management、changelog 等本次不需要的功能

### 安裝路徑遵循 FHS 標準

| 檔案 | 安裝路徑 | 理由 |
|------|---------|------|
| binary | `/usr/bin/shadowdns` | `.deb` 安裝的程式放 `/usr/bin/`（非 `/usr/local/`） |
| systemd unit | `/lib/systemd/system/shadowdns.service` | 套件提供的 unit file 標準位置 |
| 範例配置 | `/etc/shadowdns/` | 服務配置的標準位置 |

### systemd service unit 設計

- `Type=notify` 或 `Type=simple`：ShadowDNS 不實作 sd_notify，使用 `Type=simple`
- `ExecReload=/bin/kill -HUP $MAINPID`：對應 main.go 的 SIGHUP reload 機制
- `DynamicUser=yes`：以非 root 身份運行，搭配 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 綁定 port 53
- `ProtectSystem=strict` + `ReadOnlyPaths=`：最小權限原則

### nfpm 配置檔放在專案根目錄

配置檔命名為 `nfpm.yaml`，放在專案根目錄（與 Makefile 同層），這是 nfpm 的預設慣例。打包用的靜態資源（service file、範例配置）放在 `dist/` 目錄下。

### Makefile 整合

新增 `deb` target，依賴 `build`，確保 binary 先編譯再打包。打包產物放在專案根目錄（`shadowdns_*.deb`）。

### 容器化測試（make test-deb）

使用 podman 在 Ubuntu 容器中端到端驗證 `.deb` 安裝結果。測試流程：

1. Cross-compile binary 為 `linux/amd64`（開發機為 macOS/arm64）
2. 用 cross-compiled binary 建置測試用 `.deb`
3. 用 `scripts/gen-container-testdata.go` 產生帶 mock GeoIP mmdb 的完整 testdata
4. 啟動 `ubuntu:24.04` 容器（`--platform linux/amd64`）
5. 容器內執行 `dpkg -i` 安裝並驗證：檔案路徑、user/group 建立、`--dry-run` 載入配置、DNS 查詢回應
6. 測試完成後自動清理容器

**替代方案：**
- 在 CI 的 Linux runner 上直接 `dpkg -i` 測試：不需要 cross-compile，但本地 macOS 開發時無法跑
- 用 Docker 取代 podman：podman 在 macOS 上對 `--platform` emulation 支援更好，且不需要 daemon

## Risks / Trade-offs

- **[Risk] nfpm 需要額外安裝** → 在 Makefile 和文件中說明安裝方式（`go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`），CI 環境可用 `go install` 或 pre-built binary
- **[Risk] `DynamicUser=yes` 可能與 GeoIP 檔案路徑權限衝突** → service unit 中明確加入 `ReadOnlyPaths=/usr/local/share/GeoIP` 確保可讀
- **[Risk] 版本號管理** → 初期在 nfpm.yaml 中寫死版本號，未來可改為從 git tag 動態讀取
