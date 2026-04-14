## 1. 打包靜態資源

- [x] [P] 1.1 建立 `dist/shadowdns.service` systemd service unit file，使用 `Type=simple`、`ExecReload=/bin/kill -HUP $MAINPID`、`DynamicUser=yes`、`AmbientCapabilities=CAP_NET_BIND_SERVICE`、`ProtectSystem=strict`、`ReadOnlyPaths=/usr/local/share/GeoIP`（對應 spec: systemd service unit，對應 design: systemd service unit 設計）
- [x] [P] 1.2 建立 `dist/named.conf.example` 範例配置檔，包含 `options` block（`directory`、`geoip-directory`、`listen-on`、`allow-transfer`、`recursion no`）和一個範例 `view` block（對應 spec: example configuration files — named.conf）
- [x] [P] 1.3 建立 `dist/aliases.yaml.example` 範例配置檔，包含一組 root-to-backup domain 範例映射（對應 spec: example configuration files — aliases.yaml）

## 2. nfpm 配置

- [x] 2.1 建立 `nfpm.yaml` nfpm configuration file，定義 package name `shadowdns`、architecture `amd64`、maintainer、description、contents 映射（`bin/shadowdns` → `/usr/bin/shadowdns`、`dist/shadowdns.service` → `/lib/systemd/system/shadowdns.service`、`dist/named.conf.example` → `/etc/shadowdns/named.conf.example`、`dist/aliases.yaml.example` → `/etc/shadowdns/aliases.yaml.example`），以及 `systemd-daemon-reload` postinstall script（對應 spec: nfpm configuration file、binary installation path）

## 3. 建置整合

- [x] [P] 3.1 在 Makefile 新增 `deb` target（依賴 `build`），執行 `nfpm package --packager deb`，產生 `.deb` 檔案至專案根目錄（對應 spec: Makefile deb target，對應 design: Makefile 整合、使用 nfpm 而非 dpkg-deb 手工打包）
- [x] [P] 3.2 在 `.gitignore` 新增 `*.deb` 排除規則（對應 spec: build artifacts excluded from version control）

## 4. 驗證

- [x] 4.1 執行 `make deb` 確認 `.deb` 檔案成功產生，並以 `dpkg-deb --info` 和 `dpkg-deb --contents` 檢查 metadata 和安裝路徑是否正確（對應 spec: nfpm configuration file、binary installation path、安裝路徑遵循 FHS 標準、nfpm 配置檔放在專案根目錄）

## 5. 容器化測試

- [x] 5.1 建立 `scripts/test-deb.sh` shell script，自動化容器測試流程：cross-compile `GOOS=linux GOARCH=amd64` binary → 用 linux binary 建置測試用 `.deb` → 用 `go run scripts/gen-container-testdata.go` 產生 testdata → 啟動 `ubuntu:24.04` podman 容器（`--platform linux/amd64`）→ 容器內 `dpkg -i` 安裝 → 驗證檔案路徑、shadowdns user/group、`/var/log/shadowdns/` 目錄 → 執行 `shadowdns --dry-run` → 啟動 server 並用 `dig` 查詢 `example.com A` 驗證回應 → 清理容器。失敗時以非零 exit code 退出，並確保 trap 清理容器（對應 spec: container integration test、container testdata generator，對應 design: 容器化測試（make test-deb））
- [x] 5.2 在 Makefile 新增 `test-deb` target，執行 `scripts/test-deb.sh`（對應 spec: container integration test，對應 design: 容器化測試（make test-deb））

## 6. 容器測試驗證

- [x] 6.1 執行 `make test-deb` 確認端到端測試通過（對應 spec: container integration test — make test-deb validates installation、validates binary execution、validates DNS query、cleans up）
