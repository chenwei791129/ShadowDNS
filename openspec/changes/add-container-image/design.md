## Context

ShadowDNS 的 release workflow 目前在 release-please 建立 release 後，建置單一 linux/amd64 靜態 binary 與 Debian 套件；Pull Request CI 則執行 test、lint 與 smoke。專案尚無 Docker build context、正式 container runtime 契約或 registry 發布流程。

ShadowDNS 啟動時必須收到 named.conf 與 ShadowDNS YAML 路徑，DNS 預設使用 privileged port 53，並可選擇啟動 metrics、ephemeral API、DoH、ACME HTTP-01 與 file-backed query log。Container image 必須在不放寬 non-root 權限的情況下容納這些外部設定與持久化需求。GitHub Packages API 無法自動將首次建立的 GHCR package 改為 public，因此第一次發布包含一個明確的手動步驟。

此變更跨 Docker build、GitHub Actions release／CI 與使用者文件，且涉及 runtime 身分與 registry 權限，因此需要設計文件作為 durable handoff。

## Goals / Non-Goals

**Goals:**

- 發布可重現建置的官方 linux/amd64 ShadowDNS image。
- 讓 ShadowDNS 預設以 UID/GID 65532 的 non-root 身分及非 privileged port 執行。
- 讓 Pull Request 在不接觸 secrets 或 registry write 權限的情況下驗證 Docker build。
- 讓 release-please 建立正式 release 時，同步發布精確版本 tag 與 latest tag。
- 記錄完整的設定、port、volume、signal、logging、probe 與首次公開 package 操作契約。

**Non-Goals:**

- 不發布 linux/arm64 或其他 architecture。
- 不建立 edge、major 或 minor 浮動 tags。
- 不加入 image-level Trivy scan、SBOM 或 provenance attestation。
- 不在 image 中預載可直接啟動的 named.conf、zone、GeoIP database 或 ShadowDNS YAML。
- 不加入 Docker HEALTHCHECK、shell、package manager 或除錯工具。
- 不改變 ShadowDNS CLI、DNS query behavior、既有 binary／Debian package 產物或 ACME 實作。
- 不自動修改 GHCR package visibility；GitHub UI 的首次公開操作仍由 package owner 執行。

## Decisions

### 使用 multi-stage 靜態 linux/amd64 build 與 Distroless nonroot runtime

Dockerfile 的 builder stage 使用與 go.mod 宣告版本相同的 Go toolchain，以 CGO_ENABLED=0、GOOS=linux、GOARCH=amd64 建置 cmd/shadowdns。VERSION build argument 預設為 dev；release workflow 傳入 release-please 的 tag，並用 -s -w -X main.version=<version> 注入 binary。Runtime stage 使用 gcr.io/distroless/static-debian13:nonroot，固定執行身分為 UID/GID 65532。

Builder 與 runtime base image SHALL 以 immutable digest pin 住，tag 只保留為可讀提示；更新 Go patch version或 Distroless base 時一併重新解析並審查 digest。這避免浮動 tag 在相同 source revision 下產生不同 base layers。Build context採default-deny allowlist，只傳入 `go.mod`、`go.sum`、non-test `cmd/**/*.go`、non-test `internal/**/*.go`、canonical `scripts/build-linux.sh`及builder必要的Docker control files；新增的未知檔案預設不進入context，避免private local data、cache invalidation與未來Dockerfile意外讀取。

替代方案：scratch 需要自行維護 CA bundle 與使用者 metadata，不利於 ACME HTTPS；Alpine 增加 shell、套件與 CVE surface；root runtime 或 NET_BIND_SERVICE capability 都違反預設 least privilege，因此不採用。

### 提供可覆寫的容器預設啟動契約

Image 使用 exec-form ENTRYPOINT 指向 /usr/local/bin/shadowdns，exec-form CMD 預設傳入：

- --named-conf /etc/shadowdns/named.conf
- --config /etc/shadowdns/shadowdns.yaml
- --listen 0.0.0.0:5353
- --no-color

Image 宣告 5353/udp、5353/tcp 與 9153/tcp。明確的 `0.0.0.0` host會覆寫 named.conf 中可能屬於宿主 namespace 的 `listen-on`／`listen-on-v6` 位址，使 Docker IPv4 port publishing可達；因此官方預設是 IPv4-only，使用者提供 container command 時可完整替換預設 flags。Signal直接送達 ShadowDNS PID 1，image不含 entrypoint script或額外 adapter。

替代方案：不提供 CMD 會使每次啟動都要重複兩個必填 path 與高位 port；僅提供 `:5353` 會沿用 host-specific named.conf listen addresses而無法在一般 container namespace綁定；內部使用 port 53則要求 root或capability。官方預設不承諾 dual-stack，因現有單一 explicit-host `--listen` 無法同時指定 IPv4與IPv6 wildcard；需要 dual-stack的部署須提供 container-compatible named.conf並覆寫 command，未來若要提供 first-class dual-stack則另行設計多位址 CLI。

### 將設定與可寫狀態留給部署端掛載

Image 不複製 packaging 內的 example configs，因為它們引用部署特定的 zone、GeoIP、PID 與 log 路徑，缺少對應資料時不能構成可啟動環境。部署者將 /etc/shadowdns 掛載為唯讀，並確保 named.conf 的 include、directory 與 GeoIP absolute paths 都指向已掛載資料。

DoH ACME 若啟用，部署者將 /var/lib/shadowdns 掛載為可由 UID 65532 寫入的持久 volume。任何 file-backed query log 也必須指向另一個可寫 volume；主程序預設不帶 --log-file，將 operational logs 寫至 stderr 交由 container runtime 收集。DoH 與 ACME HTTP-01 在設定內使用高位 container ports，再由宿主、load balancer 或 ingress 映射至對外 ports。

Dockerfile 可建立預期目錄及 ownership，但不使用 VOLUME 指令強迫建立 anonymous volumes；volume lifecycle 與 read-only/read-write mode 由部署 manifest 明確管理。

### 不內建 health check 並保留原生 signal lifecycle

Distroless image 不加入 curl、dig 或 shell，也不宣告 HEALTHCHECK。部署端依實際 hosted zone 設定 DNS readiness probe，或以 /metrics endpoint 設定 HTTP liveness probe；image 不假設任何 DNS record 必然存在。

停止時由 container runtime 傳送 SIGTERM，ShadowDNS 直接 graceful shutdown。Reload 由部署端向 container 主程序傳送 SIGHUP；文件不依賴 image 內 shell 或 shadowdns reload subcommand。若部署平台無法傳送 SIGHUP，則以 rolling restart 套用設定。

### Pull Request 只驗證 Docker build

既有 Pull Request CI 增加一個獨立的 container build job或step，使用 linux/amd64、VERSION=dev 建置 Dockerfile。它不執行 registry login、不引用 secrets、不推送 image，並沿用 workflow 的 contents: read 權限。Docker build 失敗會使 CI 失敗；不加入 image vulnerability scan。

Image config assertions集中在 repo-owned `scripts/verify-container-image.sh`，由 CI在唯一一次 build後呼叫；本機透過薄的 Make target對指定 image呼叫同一 script，CLAUDE.md只記錄 Make入口而不複製 assertions。完整 UDP/TCP、metrics與signal e2e仍在本機整體驗證執行，不增加 Pull Request CI範圍。

新增的第三方 GitHub Actions SHALL pin 到完整 commit SHA，旁註對應 release tag，遵循既有供應鏈強化慣例。若直接使用 runner內建 docker build能滿足相同契約，優先避免不必要 action。既有 workflows中的浮動 action references是pre-existing hardening debt，不在此 container change一併遷移；但新增 job或step中的每一個新 `uses` reference都必須pin，即使同一 action已在另一個job出現。

### 正式 release 並行發布精確版本與 latest

release workflow增加build-and-push-image job，與既有build-and-upload job並行，兩者都依賴release-please且只在release_created為true時執行。Image job使用release-please tag_name作為VERSION，建置linux/amd64 image並先推送immutable精確tag：

- ghcr.io/<repository-owner>/shadowdns:<tag_name>

整份release workflow在固定concurrency group內序列化，確保新release不會插入舊run的newest-release查詢與latest更新之間。同一build-and-push-image job的post-build latest step查詢repository當下最新published release；只有本次tag仍為最新時才以manifest copy將 `latest` 指向精確tag，較舊且較慢的workflow run不得讓latest倒退。

Image metadata包含OCI source、revision、version與created labels；created值使用source commit timestamp（不是job執行當下wall-clock），使相同source revision、version與pinned inputs重建時不因時間欄位任意漂移。最新release的精確tag與latest指向同一image digest；不產生edge、major或minor tags。此設計承諾固定／可追溯的build inputs，不額外承諾跨BuildKit版本的byte-for-byte reproducibility。

Image job 使用 GitHub 內建 GITHUB_TOKEN，job-level permissions 僅為 contents: read 與 packages: write。既有 binary／Debian package job改用其內建 GITHUB_TOKEN，job-level permissions 僅為 contents: write；`MY_RELEASE_PLEASE_TOKEN` 只傳給 release-please action，且其 PAT scope由既有 secret requirement獨立約束，因為 YAML permissions無法縮減 PAT。既有 release-please job及 binary／Debian package job不取得 packages: write。所有本次新增的第三方 action references pin完整 commit SHA。

替代方案：release.published 事件的獨立 workflow 會建立第二套 release coordination；每次 main push 發 edge image超出選定發布策略，因此不採用。

### 將首次 package 公開設為可驗證的手動發布步驟

Dockerfile／workflow 設定 org.opencontainers.image.source 指向 public source repository，讓 GHCR package可建立 repository link。首次 image push 完成後，package owner 在 GitHub package settings 將 visibility 切換為 Public 並確認 repository link。GitHub REST Packages API 沒有更新 visibility 的 endpoint，因此不以 gh CLI 或未文件化 API 自動化。

安裝文件清楚區分一次性的 package owner 操作與一般使用者無需登入的 docker pull。後續 release沿用既有 public visibility，無需重複設定。

## Implementation Contract

**Observable behavior**

- 本地以 Dockerfile 建置時只產生 linux/amd64 image；VERSION 未指定時 shadowdns --version 回傳 dev，release build 回傳 release tag。
- Image config 顯示 user 65532、固定 entrypoint、含 `0.0.0.0:5353` 的既定 default command，以及 5353/udp、5353/tcp、9153/tcp exposed ports；官方預設是 IPv4-only。
- 未掛載兩個必填 config files 時，container 快速以非零狀態離開並由 ShadowDNS 回報 config load error；image 不注入假設定或保持空轉。
- 掛載有效設定後，典型宿主映射為 53:5353/udp、53:5353/tcp 與 9153:9153/tcp，ShadowDNS 可接收 SIGTERM graceful shutdown 與 SIGHUP reload。
- Pull Request 只建置 image；正式 release 才登入並推送 GHCR。
- 正式release先發布immutable版本tag；workflow-level serialization搭配latest guard只讓repository最新published release更新latest，且更新後兩者解析至同一linux/amd64 image digest。

**Failure modes**

- Dockerfile build、registry login 或 push 任一步失敗時，對應 GitHub Actions job 失敗；不得以 continue-on-error 隱藏。
- release_created 為 false 時，image job完全跳過，不登入 GHCR也不更新 tags。
- GITHUB_TOKEN 缺少 package write 或 package設定阻止推送時，image job明確失敗，既有 GitHub Release artifacts job仍由其獨立結果決定。
- 掛載資料不可讀或 state/log volume 對 UID 65532 不可寫時，由 ShadowDNS 原生錯誤使 startup／功能操作失敗；image 不切換成 root補救。
- GHCR package在首次發布後仍為 private 時，文件要求 owner完成一次性 visibility操作；workflow不聲稱已自動公開。

**Acceptance criteria**

- docker build --platform linux/amd64 成功；repo-owned container verification script以 docker image inspect 驗證 user、entrypoint、command、architecture、exposed ports與無 healthcheck，並由 CI與本機 Make target共同呼叫。
- docker run --rm <local-image> --version 輸出 dev；以 release-style VERSION build argument建置後輸出指定 synthetic version；匯出 final filesystem或等價檢查證明沒有 shell、package manager、fixture、zone或GeoIP檔案。
- 使用既有 fixture generator準備 RFC 2606／RFC 5737 test data並掛載唯讀設定，container能以 non-root及明確 IPv4 wildcard啟動，在映射的 UDP/TCP 5353 endpoint回答已知 authoritative query；SIGTERM可在 bounded shutdown時間內停止。
- workflow syntax可解析，Pull Request execution證明沒有 registry login／push，release execution證明兩個 tags指向相同 digest。
- 英文 README 與雙語 manual明確記錄 mount、UID 65532、ports、stderr logging、external probes、signals、DoH/ACME高位 port與首次 Public visibility操作；make docs-build --strict通過。

**Scope boundaries**

- In scope：Docker build context、GHCR release publishing、Pull Request build validation、OCI metadata與相關使用者文件。
- Out of scope：ShadowDNS Go runtime behavior、CLI additions、Kubernetes／Compose manifests、runtime image scan、attestation、SBOM、額外 architecture與自動 package visibility變更。

## Risks / Trade-offs

- [Distroless 無 shell，現場診斷較不方便] → 透過 stderr、metrics、DNS 外部 probe 與獨立 debug container觀察，不在 production image增加工具。
- [UID 65532 無法寫入 host-created volume] → 文件提供 ownership／permission先決條件，並明確拒絕以 root fallback。
- [Base image digest會增加更新工作] → 將 tag與 digest並列，Go version或 Distroless更新時以明確 review更新。
- [latest 是 mutable tag] → 同時發布 immutable精確版本 tag，部署文件建議 production pin精確 tag或 digest。
- [首次 package仍可能是 private] → 文件與 release checklist要求 owner在首次 push後手動設為 Public並驗證 anonymous pull。
- [Default config paths存在但 image不含 config] → startup fail-fast，文件把 required mounts放在第一個 run範例，避免提供不可用的假資料。
- [IPv4 wildcard預設覆寫 mounted config的IPv6 listeners] → 文件明示官方default是IPv4-only；dual-stack部署必須覆寫command並使用container-compatible listener設定，first-class dual-stack留待獨立CLI設計。
- [Release image job失敗但 GitHub Release與其他 artifacts可能已完成] → 保留獨立 job狀態並允許重新執行 workflow；不得覆寫或刪除既有 release artifacts。
