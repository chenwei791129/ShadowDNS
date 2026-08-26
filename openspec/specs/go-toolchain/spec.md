# go-toolchain Specification

## Purpose

TBD - created by archiving change 'upgrade-go-1-27'. Update Purpose after archive.

## Requirements

### Requirement: 最低 Go toolchain 版本

專案 SHALL 宣告 Go 1.27.0 為最低 Go toolchain 版本，而所有受支援語言的開發者安裝文件 MUST 將需求寫為 Go 1.27 或更新版本。

#### Scenario: 本機 toolchain 選擇

- **WHEN** 開發者在啟用自動 toolchain 選擇的情況下，於 repository 內執行 Go command
- **THEN** 選用的 toolchain SHALL 為 Go 1.27.0 或更新版本

#### Scenario: 文件所述開發需求

- **WHEN** 開發者閱讀 README，或英文／正體中文的 getting-started 與 installation 手冊
- **THEN** 每一處 prerequisite SHALL 寫為 Go 1.27 或更新版本


<!-- @trace
source: upgrade-go-1-27
updated: 2026-08-26
code:
  - docs/installation.md
  - docs/installation.zh.md
  - Dockerfile
  - docs/getting-started.zh.md
  - docs/getting-started.md
  - go.mod
  - README.md
-->

---
### Requirement: Toolchain 版本對齊

每一個宣告 Go 版本的 build environment SHALL 從 `go.mod` 取得版本，或 SHALL 與 `go.mod` 宣告的精確版本一致。Container builder image MUST 保持 immutable digest pin。

#### Scenario: CI toolchain 選擇

- **WHEN** CI job 為 repository build 設定 Go
- **THEN** 該 job SHALL 從 `go.mod` 取得版本，而非宣告獨立版本

#### Scenario: Container builder 對齊

- **WHEN** 檢查 Dockerfile 與 `go.mod`
- **THEN** builder image tag SHALL 與 `go.mod` 的精確 Go 版本一致，且 SHALL 包含 immutable digest

<!-- @trace
source: upgrade-go-1-27
updated: 2026-08-26
code:
  - docs/installation.md
  - docs/installation.zh.md
  - Dockerfile
  - docs/getting-started.zh.md
  - docs/getting-started.md
  - go.mod
  - README.md
-->