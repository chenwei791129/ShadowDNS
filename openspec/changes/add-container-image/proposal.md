## Why

ShadowDNS 目前只發布 linux/amd64 binary 與 Debian 套件，缺少可由容器平台一致部署的官方 image。新增以 non-root 執行的 GHCR image，可在不改變伺服器程式行為的前提下提供固定 toolchain 與 base-image 輸入、低攻擊面的容器化安裝途徑，並讓 Pull Request 在發布前驗證 image 仍可建置。

## What Changes

- 新增 multi-stage Dockerfile，將 linux/amd64 靜態 ShadowDNS binary 放入 Distroless static nonroot runtime image。
- 定義容器的預設 entrypoint、設定路徑、高位 DNS port、metrics port、non-root 身分、OCI metadata 與 volume 契約。
- 在 Pull Request CI 中建置但不推送 container image。
- 在 release-please 建立正式 release 時，以內建 GITHUB_TOKEN 將精確版本 tag 與 latest tag 推送至 GHCR。
- 將官方 container image 納入英文 README 與雙語安裝／入門文件，包含 port mapping、唯讀設定掛載、可寫 ACME state、外部 probe、reload 與首次公開 package 的操作說明。

## Capabilities

### New Capabilities

- `container-image`: 定義官方 linux/amd64 ShadowDNS container image 的建置、runtime 安全性、預設啟動契約與部署介面。

### Modified Capabilities

- `ci-workflow`: Pull Request CI 新增 container image build 驗證，但不登入 registry 或推送 image。
- `release-workflow`: 正式 release 新增 GHCR image 發布 job 與版本 tag 契約。

## Impact

- Affected specs: container-image, ci-workflow, release-workflow
- Affected code:
  - New:
    - Dockerfile
    - .dockerignore
    - scripts/verify-container-image.sh
    - openspec/changes/add-container-image/specs/container-image/spec.md
    - openspec/changes/add-container-image/specs/ci-workflow/spec.md
    - openspec/changes/add-container-image/specs/release-workflow/spec.md
  - Modified:
    - .github/workflows/ci.yml
    - .github/workflows/release-please.yml
    - Makefile
    - README.md
    - docs/index.md
    - docs/index.zh.md
    - docs/getting-started.md
    - docs/getting-started.zh.md
    - docs/installation.md
    - docs/installation.zh.md
    - CLAUDE.md
  - Removed: none
- External systems: GitHub Actions and GitHub Container Registry at ghcr.io/<repository-owner>/shadowdns
- Runtime compatibility: container image supports linux/amd64 only; existing binary and Debian package outputs remain unchanged
