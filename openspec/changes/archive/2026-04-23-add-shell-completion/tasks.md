## 1. Makefile deb target generates shell completion files

- [x] 1.1 [P] 在 Makefile `deb` target 中，於 `nfpm package` 之前加入三行 `go run ./cmd/shadowdns completion <shell>` 指令，將輸出分別寫入 `bin/shadowdns.bash`、`bin/shadowdns.zsh`、`bin/shadowdns.fish`（此步驟滿足 Makefile deb target generates shell completion files 的要求）
- [x] 1.2 [P] 更新 Makefile `deb` 目標的 `.PHONY` 或新增註解，說明 completion 檔案的生成時機與路徑；同時確認 `bin/` 已被 `.gitignore` 排除（此步驟屬於 Makefile deb target generates shell completion files 的支援設定）

## 2. nfpm packaging — install shell completion files

- [x] 2.1 在 `nfpm.yaml` 的 `contents:` 區段新增三個 entry，對應 bash（`/usr/share/bash-completion/completions/shadowdns`）、zsh（`/usr/share/zsh/vendor-completions/_shadowdns`）、fish（`/usr/share/fish/vendor_completions.d/shadowdns.fish`）的安裝路徑，使 dpkg 追蹤這三個檔案（此步驟滿足 Shell completion files 的 install 與 dpkg ownership 要求）
- [x] 2.2 本地執行 `make deb`，用 `dpkg-deb --contents shadowdns_*.deb` 驗證三個 completion 檔案路徑確實被打包進 `.deb`（此步驟驗證 Shell completion files 的 install paths）

## 3. Container integration test — validate completion files

- [x] 3.1 在 `scripts/test-deb.sh` 的「Verify installation」區段，新增三個 `test -s <path>` 檢查（bash/zsh/fish），加上 `dpkg -L shadowdns | grep` 斷言三個路徑都由 dpkg 擁有（此步驟滿足 Container integration test validates shell completion 的要求）
- [x] 3.2 在 `scripts/test-deb.sh` 新增一個 `dpkg -r shadowdns` 的反向測試：移除 package 後，三個 completion 檔案路徑皆不存在於檔案系統（此步驟滿足 Shell completion files 的 Completion files are removed on package removal scenario）

## 4. End-to-end verification

- [x] 4.1 在本機執行 `make test-deb`，確認新的 completion 檢查與 removal scenario 全部通過（此步驟驗證整合結果，同時覆蓋 Shell completion files、Makefile deb target generates shell completion files、Container integration test validates shell completion 三項要求）
- [x] 4.2 更新 CLAUDE.md 若 deb 流程或檔案結構有需要揭露給未來 agent 的新資訊（例如 `bin/shadowdns.bash|zsh|fish` 為 build artifact），視情況加入
