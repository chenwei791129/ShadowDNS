## ADDED Requirements

### Requirement: Shell completion files

The `.deb` package SHALL install shell completion files for bash, zsh, and fish at the standard vendor paths for each shell, so that tab-completion for the `shadowdns` command is available to users who have installed the corresponding shell.

The install paths SHALL be:

| Shell | Path                                                    |
| ----- | ------------------------------------------------------- |
| bash  | `/usr/share/bash-completion/completions/shadowdns`      |
| zsh   | `/usr/share/zsh/vendor-completions/_shadowdns`          |
| fish  | `/usr/share/fish/vendor_completions.d/shadowdns.fish`   |

The completion files SHALL be owned by the `shadowdns` Debian package (tracked by dpkg) so that `apt remove shadowdns` removes them and `apt upgrade` replaces them atomically with the binary.

#### Scenario: Bash completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/bash-completion/completions/shadowdns` SHALL exist and be non-empty

#### Scenario: Zsh completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/zsh/vendor-completions/_shadowdns` SHALL exist and be non-empty

#### Scenario: Fish completion file is installed

- **WHEN** the `.deb` package is installed via `dpkg -i`
- **THEN** the file `/usr/share/fish/vendor_completions.d/shadowdns.fish` SHALL exist and be non-empty

#### Scenario: Completion files are owned by dpkg

- **WHEN** `dpkg -L shadowdns` is executed after install
- **THEN** the output SHALL list all three completion file paths

#### Scenario: Completion files are removed on package removal

- **WHEN** the package is removed via `apt remove shadowdns` or `dpkg -r shadowdns`
- **THEN** all three completion file paths SHALL NOT exist on the filesystem

### Requirement: Makefile deb target generates shell completion files

The Makefile `deb` target SHALL generate the bash, zsh, and fish completion files into the `bin/` directory before invoking nfpm, using the Cobra `completion` subcommand of the `shadowdns` CLI (invoked via `go run ./cmd/shadowdns`) so that the generated content tracks the current command tree without requiring a pre-built host binary.

#### Scenario: make deb produces completion files alongside the binary

- **WHEN** `make deb` is executed from a clean `bin/` directory
- **THEN** `bin/` SHALL contain three generated completion files (one for each of bash, zsh, fish) before nfpm is invoked

#### Scenario: Completion generation works on a macOS host

- **WHEN** `make deb` is executed on a macOS host (where the target `linux/amd64` binary cannot be executed)
- **THEN** the completion files SHALL still be generated successfully via `go run ./cmd/shadowdns completion <shell>`

### Requirement: Container integration test validates shell completion

The `make test-deb` target SHALL verify that the installed `.deb` package places all three shell completion files at their expected paths and that each is non-empty and owned by dpkg.

#### Scenario: test-deb asserts all completion files exist

- **WHEN** `make test-deb` is executed
- **THEN** the test SHALL fail if any of the three completion file paths is missing, empty, or not listed by `dpkg -L shadowdns`
