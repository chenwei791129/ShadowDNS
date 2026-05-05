## Summary

統一 `shadowdns.yaml` 中 `aliases` 區塊的 backup domain 表示方式：移除 sequence form（legacy），所有條目一律使用 mapping form（`members` + `rewrite_rdata_labels`）。

## Motivation

目前 `aliases` 接受兩種等價的 YAML 形式：

- Sequence form（legacy）：`root: [b1, b2]`，等同 `rewrite_rdata_labels: false`
- Mapping form：`root: {members: [...], rewrite_rdata_labels: true|false}`

兩種形式並存只是語法糖，反而帶來成本：

- `internal/shadowdnscfg/config.go` 的 `rawAliasGroup.UnmarshalYAML` 必須維護兩條解析分支與兩組錯誤訊息
- `.local/tool/dump-aliases.py` 的 `build_aliases()` 必須依 `CDN_ROOTS` 名單分流輸出，邏輯被切成 if/else
- ns2 上的生成檔同時混雜兩種形式，operator 看 yaml 時要先判斷哪種才是預期
- spec 場景數量翻倍（既要驗 sequence 也要驗 mapping）

統一成 mapping form 後，schema 收斂為單一形狀，解析、輸出、文件、測試都同步簡化。Sequence form 沒有任何語意能力是 mapping form 表達不了的，移除不會造成功能損失。

## Proposed Solution

- YAML schema：`aliases.<root>` 必須是 mapping，含 `members: [string, ...]`（必填、非空）和 `rewrite_rdata_labels: bool`（選填，預設 `false`）
- `rawAliasGroup.UnmarshalYAML` 移除 `SequenceNode` 分支；遇到 sequence node 直接回傳 type-mismatch 錯誤，錯誤訊息指向新 schema
- `dump-aliases.py` 的 `build_aliases()` 一律輸出 mapping form，移除 if/else 分支；`CDN_ROOTS` 仍寫死在 script 內以決定 `rewrite_rdata_labels` 值（dns-management API 不暴露此資訊，遷移到 API 是另一個獨立工作）
- `packaging/shadowdns.yaml.example` 與 `testdata/integration/shadowdns.yaml` 重寫為 mapping form
- ns2 上的 `/etc/shadowdns/shadowdns.yaml` 由 operator 重跑 `dump-aliases.py` 覆蓋（runtime SIGHUP reload，不需重啟）

## Non-Goals

- 不重構 `CDN_ROOTS` 的來源 — 仍然寫死在 `dump-aliases.py`，不引入 API 變更或新的 operator workflow
- 不為 sequence form 提供 deprecation warning 過渡期 — v0.x.x experimental 階段、單一部署點，直接 hard reject 即可
- 不改動 alias 的 runtime 解析行為（`internal/config/aliases.go` 的 `BuildAliasMap`、`internal/alias/rewrite.go` 不動）
- 不改 `ephemeral_api` 區塊

## Alternatives Considered

- **保留兩種形式**：現狀。維護成本持續存在，無收益。
- **統一成 sequence form**：得放棄 `rewrite_rdata_labels: true`，但 ns2 已在 `cdn-root.example.com`、`cdn-mirror.example.com` 上使用此功能，不可行。
- **保留 sequence form 但加 deprecation warning**：v0.x.x 階段沒有外部使用者需要保護，過渡期只是增加 `UnmarshalYAML` 複雜度。

## Impact

- Affected specs: `shadowdns-config`
- Affected code:
  - Modified: internal/shadowdnscfg/config.go
  - Modified: internal/shadowdnscfg/config_test.go
  - Modified: packaging/shadowdns.yaml.example
  - Modified: testdata/integration/shadowdns.yaml
  - Modified: .local/tool/dump-aliases.py
