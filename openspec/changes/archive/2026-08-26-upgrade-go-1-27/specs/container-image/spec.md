## MODIFIED Requirements

### Requirement: Multi-stage linux/amd64 image build

專案 SHALL 提供 multi-stage Dockerfile，以 `CGO_ENABLED=0`、`GOOS=linux` 與 `GOARCH=amd64` build `./cmd/shadowdns`，然後只將產出的 binary 複製到 Distroless static Debian 13 nonroot runtime image。Go builder 版本 SHALL 與 `go.mod` 宣告的版本一致。Builder 與 runtime base images MUST 以 immutable digest pin。Final image architecture SHALL 為 `linux/amd64`。

#### Scenario: 本機開發 image build

- **WHEN** 未提供 VERSION build argument，且以 Dockerfile build `linux/amd64`
- **THEN** build SHALL 成功，而以 `--version` 執行 image SHALL 輸出 `dev`

#### Scenario: 帶版本的 release image build

- **WHEN** VERSION 設為 `v0.9.0`，且以 Dockerfile build `linux/amd64`
- **THEN** binary SHALL 使用 `-s -w -X main.version=v0.9.0` link，而以 `--version` 執行 image SHALL 輸出 `v0.9.0`

#### Scenario: Final image 僅包含 runtime requirements

- **WHEN** 檢查 final image
- **THEN** image SHALL 使用 pinned Distroless static Debian 13 nonroot base，且 SHALL NOT 安裝 shell、package manager、DNS client、HTTP client、configuration fixture、zone data 或 GeoIP database
