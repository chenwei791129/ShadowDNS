## ADDED Requirements

### Requirement: PID file option parsed from named.conf

The server SHALL parse the `pid-file` option from the `options` block in named.conf and store it in the configuration. If `pid-file` is not specified, the server SHALL use no PID file (no file is written, and `-reload` is unavailable).

#### Scenario: pid-file option present in named.conf

- **WHEN** named.conf contains `pid-file "/var/run/named/pid";` in the options block
- **THEN** the parsed configuration SHALL contain the PID file path `/var/run/named/pid`

#### Scenario: pid-file option absent from named.conf

- **WHEN** named.conf does not contain a `pid-file` option
- **THEN** the parsed configuration SHALL have an empty PID file path
- **THEN** the server SHALL NOT write a PID file on startup

### Requirement: PID file written on startup

When a PID file path is configured, the server SHALL write its process ID to the PID file after successfully binding listeners but before accepting queries. The file SHALL contain the PID as a decimal integer followed by a newline character, with no other content.

#### Scenario: PID file created on startup

- **WHEN** the server starts with a configured `pid-file` path
- **THEN** the server SHALL create the PID file containing the current process ID
- **THEN** the PID file content SHALL match the pattern `^[0-9]+\n$`

#### Scenario: PID file directory does not exist

- **WHEN** the server starts and the parent directory of the configured PID file path does not exist
- **THEN** the server SHALL log an error and continue startup without writing the PID file

### Requirement: PID file removed on shutdown

When the server shuts down gracefully (SIGINT or SIGTERM), it SHALL remove the PID file if one was written during startup.

#### Scenario: Graceful shutdown removes PID file

- **WHEN** the server is running with a PID file and receives SIGTERM
- **THEN** the server SHALL delete the PID file before exiting

#### Scenario: PID file already removed externally

- **WHEN** the server shuts down and the PID file has already been deleted by an external process
- **THEN** the server SHALL NOT return an error for the missing file

### Requirement: Reload flag sends SIGHUP to running instance

The server SHALL accept a `-reload` command-line flag. When `-reload` is specified, the server SHALL NOT start a DNS server. Instead, it SHALL parse named.conf to obtain the `pid-file` path, read the PID from that file, send SIGHUP to the process identified by the PID, and exit.

#### Scenario: Successful reload via CLI

- **WHEN** the operator runs `shadowdns -reload -named-conf /path/to/named.conf`
- **THEN** the server SHALL parse named.conf, read the PID file, send SIGHUP to the running process, and exit with code 0

#### Scenario: PID file does not exist

- **WHEN** the operator runs `-reload` and the PID file path from named.conf does not exist on disk
- **THEN** the server SHALL print an error message identifying the missing PID file path and exit with a non-zero code

#### Scenario: PID file contains invalid content

- **WHEN** the operator runs `-reload` and the PID file contains non-numeric content
- **THEN** the server SHALL print an error message and exit with a non-zero code

#### Scenario: Process identified by PID is not running

- **WHEN** the operator runs `-reload` and the PID from the file does not correspond to a running process
- **THEN** the server SHALL print an error message and exit with a non-zero code

#### Scenario: No pid-file configured in named.conf

- **WHEN** the operator runs `-reload` and named.conf does not contain a `pid-file` option
- **THEN** the server SHALL print an error message stating that `pid-file` is required for `-reload` and exit with a non-zero code

#### Scenario: Reload requires named-conf flag

- **WHEN** the operator runs `shadowdns -reload` without specifying `-named-conf`
- **THEN** the server SHALL print an error message stating that `-named-conf` is required and exit with a non-zero code
