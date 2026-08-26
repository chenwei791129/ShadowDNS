## 1. 工具鏈與文件一致性

- [x] 1.1 依「維持純 toolchain 升級」、「除非 compatibility 要求，否則保留 dependency graph」、「最低 Go toolchain 版本」與「Toolchain 版本對齊」將 `go.mod` 升為 `go 1.27.0`，使用Go 1.27.0執行`go mod tidy`，並以`git diff -- go.mod go.sum`證明dependency versions未發生無關變動且正式程式碼未導入`encoding/json/v2`或Go 1.27新功能；若發現不相容dependency，先記錄證據與最低必要版本並停止等待scope決定。
- [x] 1.2 依「固定單一且精確的 Go 1.27.0 基準」、「Toolchain 版本對齊」與更新後的「Multi-stage linux/amd64 image build」解析並驗證official Go 1.27.0 Alpine linux/amd64 immutable manifest digest，更新`Dockerfile` builder pin，並以image metadata及container內`go version`確認tag、architecture、digest與`go.mod`一致。
- [x] [P] 1.3 依「最低 Go toolchain 版本」同步更新`README.md`、`docs/getting-started.md`、`docs/getting-started.zh.md`、`docs/installation.md`與`docs/installation.zh.md`的最低版本為Go 1.27，並以repository grep證明所有developer prerequisites不再宣告Go 1.26。
- [x] [P] 1.4 依「重用既有驗證與部署介面」檢查project `CLAUDE.md`是否因新build command、workflow、structure、dependency或troubleshooting情境而需要同步；以content review明確記錄需要更新或無需修改的結論。

## 2. Go 1.27驗證矩陣

- [x] 2.1 依「Toolchain upgrade verification matrix」在Go 1.27.0下執行`go fmt ./cmd/... ./internal/...`並確認未產生非預期format diff，再執行`make lint`、`make test`、`make smoke`與`make docs-build`；每個command均須成功，任何failure只修正本repo的compatibility問題。
- [x] 2.2 依「重用既有驗證與部署介面」以`VERSION=0.0.0-upgrade-go-1-27 make deb`建立後續deployment使用的local-change package，並以`make test-deb`驗證install、layout、user與runtime packaging contract；接受既有test harness為隔離測試建立自己的`shadowdns_test_amd64.deb`，但deployment必須使用帶有change version的local-change package。
- [x] 2.3 依「Multi-stage linux/amd64 image build」與「Unavailable container runtime」執行`make container-image`、`make verify-container`與`make test-container`，記錄既有runtime selector實際選用的Docker或Podman，並以inspect output確認linux/amd64、Distroless nonroot、minimal-runtime與Go 1.27.0 build information；未被selector選用的runtime記為not exercised，不宣稱passed。

## 3. Bounded review與back-to-back Perf-Guard

- [x] 3.1 依「維持純 toolchain 升級」對完整implementation diff執行一次`simplify`與一次`auto-code-review xhigh --fix` bounded chain，確認未混入application behavior、dependency、API或hot-path refactor；第一輪後即停止並記錄修正與剩餘非阻斷findings。
- [x] 3.2 依「使用對稱且緊密相連的 Perf-Guard protocol」與「Back-to-back toolchain performance guard」重用`local-dnspyre-crosshost-benchmark`，先記錄目前deployment version，再對CNAME與A workload各執行discarded warm-up及三分鐘baseline；raw outputs存在`.local/dnspyre/report/`且從此task開始到post-change完成之間只允許candidate package deployment與health verification。
- [x] 3.3 依「重用既有驗證與部署介面」重用`release-shadowdns` local-change mode部署task 2.2建立的`0.0.0-upgrade-go-1-27` package，reconcile service configuration並restart；以`shadowdns --version`、Go build information、service active state、journal與application log startup scan證明deployment健康。
- [x] 3.4 依「使用對稱且緊密相連的 Perf-Guard protocol」立即重用`local-dnspyre-crosshost-benchmark`，以與baseline相同的client、target、query lists、duration與concurrency，對CNAME與A各執行discarded warm-up及三分鐘post-change measurement。
- [x] 3.5 依「Benchmark evidence保持local且先行完成sanitization gate」掃描change artifacts與完整working-tree diff，確認只有synthetic hostnames、RFC-reserved addresses與generic workloads；sanitization grep須為零leak suspects且所有變更保持unstaged、uncommitted。
- [x] 3.6 依「Performance regression verdict」、「QPS regression」、「Tail-latency regression」與「No significant regression」產生`.local/dnspyre/report/perfguard-upgrade-go-1-27-<timestamp>.md`，列出baseline與post-change QPS、p50/p95/p99、NXDOMAIN／REFUSED rate、percentage deltas與thresholds；任一QPS下降超過5%或p99上升超過15%即標示REGRESSION並在完成handoff bookkeeping後停止，否則標示PASS。
