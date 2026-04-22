## 1. Schema redesign in `shadowdnscfg` (TDD)

- [x] 1.1 Extend `internal/shadowdnscfg/config_test.go` with failing cases covering the new one-to-many schema per the **選擇一對多 schema（`root: [backups]`）而非保留一對一** decision: well-formed `root.com: [backup.com, mirror.com]` produces two map entries (requirement: Load unified ShadowDNS configuration from a YAML file); duplicate backup under two different roots fails; self-alias `root.com: [root.com]` fails; empty backup list is accepted; legacy bare-string value `backup.com: root.com` fails YAML decoding (requirement: Validate aliases section; requirement: Parse aliases.yaml).
- [x] 1.2 Update `internal/shadowdnscfg/config.go`: change `rawConfig.Aliases` to `map[string][]string`; implement the root-to-backups flatten directly inside `Load` per the **解析邏輯在 `shadowdnscfg.Load()` 內展開，不引入新 public API** decision, reusing `config.BuildAliasMap` for normalization and duplicate/self-alias rejection; make the tests from 1.1 pass without changing `BuildAliasMap`'s signature.

## 2. Remove legacy `LoadAliases` loader

- [x] 2.1 Delete `LoadAliases` (the legacy standalone-file variant of Parse aliases.yaml) from `internal/config/aliases.go` — the file-reading entry point and the `map[string][]string` YAML unmarshal + flatten loop; keep `BuildAliasMap` and `normalizeDomain` unchanged.
- [x] 2.2 Remove the nine `TestLoadAliases_*` tests from `internal/config/aliases_test.go`; keep every `TestBuildAliasMap_*` test untouched.

## 3. Migrate integration tests to unified loader

- [x] 3.1 Create `testdata/integration/shadowdns.yaml` with `aliases: {example.com: [backup.example]}` to replace the legacy fixture per the **Integration tests 改用 `shadowdnscfg.Load()` 而非直餵 `BuildAliasMap`** decision.
- [x] 3.2 Delete `testdata/integration/aliases.yaml`.
- [x] 3.3 Update `testdata/integration/README.md` to describe `shadowdns.yaml` (root-to-backups format) in place of the old `aliases.yaml` line.
- [x] 3.4 [P] Migrate `test/integration/helpers_test.go:66-74` from `config.LoadAliases` to `shadowdnscfg.Load(filepath.Join(tmpDir, "shadowdns.yaml"), logger)` and read `cfg.Aliases`.
- [x] 3.5 [P] Migrate `test/integration/listenon_test.go` to the unified loader.
- [x] 3.6 [P] Migrate all three call sites in `test/integration/reload_diff_test.go` to the unified loader.
- [x] 3.7 [P] Migrate `test/integration/axfr_test.go` to the unified loader.
- [x] 3.8 [P] Migrate `test/integration/notify_test.go` to the unified loader.

## 4. Documentation and packaging

- [x] 4.1 [P] Rewrite the `aliases` section and header comment in `packaging/shadowdns.yaml.example` to show the one-to-many format per the **直接 breaking change，不提供 deprecation window** decision, including the rule statements (root is the key, list of backups is the value, duplicate-backup-across-roots rejected, self-alias rejected, empty or omitted `aliases:` valid).
- [x] 4.2 [P] Update `README.md` migration note around lines 231-235 to describe the final `root: [backups]` schema and drop the interim "invert to `backup: root`" instruction.

## 5. Spec duplicate cleanup

- [x] 5.1 During `/spectra:archive --sync`, confirm that `openspec/specs/config-loader/spec.md` ends with exactly one `### Requirement: Parse aliases.yaml` block (the one from the MODIFIED delta) and that the older duplicate (describing a `-aliases` CLI flag and a standalone `aliases.yaml`) is removed per the **Spec 清理範圍限於本次 change 觸及的 Requirement** decision; if the sync tool leaves both blocks, delete the older one manually and re-run `spectra validate aliases-root-to-backups-schema`.

## 6. Verification and deployment

- [x] 6.1 Run `make test` and confirm the race-enabled suite passes end-to-end with the new loader and fixture.
- [x] 6.2 Run `make lint` and `make smoke`.
- [ ] 6.3 Execute the Migration Plan from `design.md`: on `bench-ns2`, rewrite `/etc/shadowdns/shadowdns.yaml` `aliases` section to the `root: [backups]` structure before deploying the new binary via the `release-shadowdns` skill; verify `systemctl status shadowdns` and `journalctl -u shadowdns -n 100` show a clean startup.
