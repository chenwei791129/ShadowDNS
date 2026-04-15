## 1. Config-loader: parse notify directive

- [x] 1.1 Add `OptionsBlock.Notify` 欄位型別：`*bool` and `case "notify":` parsing branch with yes/no validation in [internal/config/options.go](internal/config/options.go) (covers Parse named.conf options block)
- [x] 1.2 Add unit tests in [internal/config/options_test.go](internal/config/options_test.go) for `notify yes;`, `notify no;`, absent directive, and invalid value scenarios

## 2. CLI flag and precedence resolution

- [x] 2.1 Register `-no-notify` bool flag in [cmd/shadowdns/main.go](cmd/shadowdns/main.go) and implement CLI flag 顯式性偵測：使用 `flag.Visit`
- [x] 2.2 Implement 解析函式集中化：新增 `resolveNotifyEnabled()` helper in [cmd/shadowdns/main.go](cmd/shadowdns/main.go) encoding 優先順序語意：顯式 flag > config > default
- [x] 2.3 Add unit tests for `resolveNotifyEnabled` in [cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) covering all 6 input combinations (flag × config {nil, true, false} × flag {explicit, absent})

## 3. Integrate guard into startup and reload paths

- [x] 3.1 Extend `runOptions` to carry explicit-flag state so CLI flag 為 process-lifetime sticky across SIGHUP reload; thread through both startup path ([cmd/shadowdns/main.go:278](cmd/shadowdns/main.go#L278)) and reload path ([cmd/shadowdns/main.go:65](cmd/shadowdns/main.go#L65))
- [x] 3.2 Guard `dispatchNotifies()` calls at startup and inside `reload()`; emit INFO log with resolved notify state and source (flag / config / default) — covers Send NOTIFY on zone content change

## 4. End-to-end tests

- [x] 4.1 Extend [cmd/shadowdns/main_test.go](cmd/shadowdns/main_test.go) with unit tests verifying `run()` guard behavior for flag-set, config yes, config no, and default paths
- [x] 4.2 Add integration test in [test/integration/](test/integration/) starting server with `-no-notify` and asserting no NOTIFY UDP packet is emitted to any NS target
- [x] 4.3 Add integration test proving CLI flag 為 process-lifetime sticky: start with `-no-notify`, SIGHUP reload after editing config to `notify yes;`, assert still no NOTIFY emitted

## 5. Documentation

- [x] 5.1 [P] Update [packaging/named.conf.example](packaging/named.conf.example) with `notify` directive example and comment explaining the CLI flag precedence
- [x] 5.2 [P] Update [README.md](README.md) NOTIFY section to document `-no-notify` flag, `notify yes|no;` config, and the precedence order (顯式 flag > config > default)
