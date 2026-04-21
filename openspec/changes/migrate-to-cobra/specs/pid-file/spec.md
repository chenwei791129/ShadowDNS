## MODIFIED Requirements

### Requirement: PID file option parsed from named.conf

The server SHALL parse the `pid-file` option from the `options` block in named.conf and store it in the configuration. If `pid-file` is not specified, the server SHALL use no PID file (no file is written, and the `reload` subcommand is unavailable).

#### Scenario: pid-file option present in named.conf

- **WHEN** named.conf contains `pid-file "/var/run/named/pid";` in the options block
- **THEN** the parsed configuration SHALL contain the PID file path `/var/run/named/pid`

#### Scenario: pid-file option absent from named.conf

- **WHEN** named.conf does not contain a `pid-file` option
- **THEN** the parsed configuration SHALL have an empty PID file path
- **THEN** the server SHALL NOT write a PID file on startup

### Requirement: Reload flag sends SIGHUP to running instance

The server SHALL expose a `reload` subcommand. When the `reload` subcommand is invoked, the server SHALL NOT start a DNS server. Instead, it SHALL parse named.conf to obtain the `pid-file` path, read the PID from that file, send SIGHUP to the process identified by the PID, and exit. The `reload` subcommand SHALL accept `--named-conf` as a required flag and SHALL NOT inherit server-only flags such as `--listen`, `--metrics-addr`, `--dry-run`, `--no-notify`, or `--reload-verify`.

#### Scenario: Successful reload via CLI

- **WHEN** the operator runs `shadowdns reload --named-conf /path/to/named.conf`
- **THEN** the server SHALL parse named.conf, read the PID file, send SIGHUP to the running process, and exit with code 0

#### Scenario: PID file does not exist

- **WHEN** the operator runs `shadowdns reload --named-conf <path>` and the PID file path from named.conf does not exist on disk
- **THEN** the server SHALL print an error message identifying the missing PID file path and exit with a non-zero code

#### Scenario: PID file contains invalid content

- **WHEN** the operator runs `shadowdns reload --named-conf <path>` and the PID file contains non-numeric content
- **THEN** the server SHALL print an error message and exit with a non-zero code

#### Scenario: Process identified by PID is not running

- **WHEN** the operator runs `shadowdns reload --named-conf <path>` and the PID from the file does not correspond to a running process
- **THEN** the server SHALL print an error message and exit with a non-zero code

#### Scenario: No pid-file configured in named.conf

- **WHEN** the operator runs `shadowdns reload --named-conf <path>` and named.conf does not contain a `pid-file` option
- **THEN** the server SHALL print an error message stating that `pid-file` is required for the `reload` subcommand and exit with a non-zero code

#### Scenario: Reload requires named-conf flag

- **WHEN** the operator runs `shadowdns reload` without specifying `--named-conf`
- **THEN** the server SHALL print an error message stating that `--named-conf` is required and exit with a non-zero code
