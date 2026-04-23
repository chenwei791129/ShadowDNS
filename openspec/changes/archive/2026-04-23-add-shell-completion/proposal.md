## Why

使用者在 shell 中輸入 `shadowdns` 指令時沒有 tab 自動補完，flag 名稱需要全憑記憶或 `--help` 查詢。Cobra 內建支援生成 completion script，但目前 `.deb` 套件沒有安裝任何 completion 檔案，因此即使使用者安裝了 bash/zsh/fish，也無法享受補完。這次 change 補上這個 packaging 缺口。

## What Changes

- Deb 套件新增安裝三個 shell completion 檔案：
  - `/usr/share/bash-completion/completions/shadowdns`
  - `/usr/share/zsh/vendor-completions/_shadowdns`
  - `/usr/share/fish/vendor_completions.d/shadowdns.fish`
- `make deb` 流程在打包前呼叫 `go run ./cmd/shadowdns completion <shell>` 於 `bin/` 下生成三個 completion 檔案。
- `nfpm.yaml` `contents:` 新增三個 entry，由 dpkg 擁有這三個檔案（`apt remove` 會清除、升級時會同步更新）。
- `scripts/test-deb.sh` 端對端測試加入驗證：package 安裝後三個 completion 檔案都存在於預期路徑。

## Non-Goals

- **不支援 PowerShell completion**：Linux 上 pwsh 使用者極少，且 deb 套件的主要使用者群是 bash/zsh/fish。未來有實際需求再擴充。
- **不透過 postinstall script 動態生成 completion**：採用 build-time 生成並由 nfpm 打包、dpkg 擁有。拒絕此方案的原因是 postinstall 動態生成的檔案 dpkg 不會追蹤，導致 `apt remove` 殘留檔案、升級時 completion 不會隨 binary 同步。
- **不在 completion 生成階段建立 host binary**：改用 `go run ./cmd/shadowdns completion <shell>` 直接執行，理由是 completion 輸出只取決於 cobra command tree，與 target 架構無關，因此在 macOS host 上 cross-compile linux/amd64 binary 的情況下依然可行。
- **不加入 Makefile `completion` 獨立 target**：completion 檔案只在打 deb 時需要，沒有獨立生成的使用情境，避免增加不必要的 target。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `deb-packaging`：新增 shell completion 檔案的安裝需求（bash/zsh/fish 三個路徑），並擴充 Makefile deb target 的職責（生成 completion 檔案）與 container integration test 的驗證範圍。

## Impact

- Affected specs: `deb-packaging`
- Affected code:
  - Modified:
    - Makefile
    - nfpm.yaml
    - scripts/test-deb.sh
  - New:
    - （無新增原始碼檔案；completion 檔案為 build artifact，於 `bin/` 下生成，不納入版控）
  - Removed:
    - （無）
