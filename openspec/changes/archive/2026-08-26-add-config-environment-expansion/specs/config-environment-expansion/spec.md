## ADDED Requirements

### Requirement: Expand required environment variables in YAML string values

The unified ShadowDNS configuration loader SHALL expand `${NAME}` expressions in value-side YAML string scalars and string sequence elements. `NAME` MUST match `[A-Za-z_][A-Za-z0-9_]*`. The loader SHALL replace each expression with the corresponding non-empty process environment value and SHALL fail the entire load when the variable is unset or set to an empty string.

#### Scenario: Required variable has a non-empty value

- **GIVEN** `API_TOKEN` is set to `synthetic-secret`
- **WHEN** `ephemeral_api.token` is configured as `${API_TOKEN}`
- **THEN** the loaded token SHALL equal `synthetic-secret`

#### Scenario: Required variable is unset

- **GIVEN** `API_TOKEN` is not present in the process environment
- **WHEN** `ephemeral_api.token` is configured as `${API_TOKEN}`
- **THEN** configuration loading SHALL fail with an error that names `API_TOKEN` and its YAML location

#### Scenario: Required variable is empty

- **GIVEN** `API_TOKEN` is set to an empty string
- **WHEN** `ephemeral_api.token` is configured as `${API_TOKEN}`
- **THEN** configuration loading SHALL fail rather than loading an empty token that disables token authentication

#### Scenario: Sequence element is expanded

- **GIVEN** `ALLOW_PREFIX` is set to `192.0.2.0/24`
- **WHEN** an `ephemeral_api.allow` element is configured as `${ALLOW_PREFIX}`
- **THEN** the loaded allow list SHALL contain `192.0.2.0/24`

### Requirement: Apply literal defaults for unset or empty variables

The loader SHALL support `${NAME:-default}`. It SHALL use the non-empty process environment value when present and SHALL otherwise use `default` as literal text. The loader SHALL NOT recursively expand expression-like text introduced by an environment value or default.

#### Scenario: Unset variable uses default

- **GIVEN** `API_LISTEN` is unset
- **WHEN** `ephemeral_api.listen` is configured as `${API_LISTEN:-127.0.0.1:8053}`
- **THEN** the loaded listen address SHALL equal `127.0.0.1:8053`

#### Scenario: Empty variable uses default

- **GIVEN** `API_LISTEN` is set to an empty string
- **WHEN** `ephemeral_api.listen` is configured as `${API_LISTEN:-127.0.0.1:8053}`
- **THEN** the loaded listen address SHALL equal `127.0.0.1:8053`

#### Scenario: Non-empty variable overrides default

- **GIVEN** `API_LISTEN` is set to `127.0.0.1:9053`
- **WHEN** `ephemeral_api.listen` is configured as `${API_LISTEN:-127.0.0.1:8053}`
- **THEN** the loaded listen address SHALL equal `127.0.0.1:9053`

#### Scenario: Default is not recursively expanded

- **GIVEN** `PRIMARY` is unset and `SECONDARY` is set to `replacement`
- **WHEN** a string value is configured as `${PRIMARY:-${SECONDARY}}`
- **THEN** the loader SHALL treat `${SECONDARY}` as literal default text rather than expanding it

### Requirement: Preserve escaped and unsupported literal dollar text

The loader SHALL replace `$$` with one literal `$` without scanning the immediately following text again during that load. This SHALL allow `$${NAME}`, `$${NAME:-fallback}`, and `$${NAME:?message}` to preserve arbitrary literal braced text. A `$` that does not begin `${...}` or `$$` SHALL remain literal. The loader SHALL fail on a malformed unescaped `${...}` expression or an unsupported operator inside one.

#### Scenario: Escaped expression remains literal

- **GIVEN** `API_TOKEN` is set to `synthetic-secret`
- **WHEN** a string value is configured as `$${API_TOKEN}`
- **THEN** the loaded value SHALL equal the literal `${API_TOKEN}`

#### Scenario: Escaped supported and unsupported braced text remains literal

- **WHEN** string values contain `$${API_TOKEN:-fallback}` and `$${API_TOKEN:?required}`
- **THEN** the loaded values SHALL equal the literals `${API_TOKEN:-fallback}` and `${API_TOKEN:?required}`

#### Scenario: Plain dollar text remains literal

- **WHEN** a string value contains `$API_TOKEN` or `price-$5`
- **THEN** the loader SHALL preserve that text unchanged

#### Scenario: Malformed expression is rejected

- **WHEN** a string value contains an unterminated `${API_TOKEN`
- **THEN** configuration loading SHALL fail with the YAML location and SHALL NOT include an environment value

#### Scenario: Unsupported operator is rejected

- **WHEN** a string value contains `${API_TOKEN:?required}` or `${API_TOKEN-default}`
- **THEN** configuration loading SHALL fail rather than applying shell parameter-expansion semantics

### Requirement: Expand each string exactly once from left to right

The loader SHALL process multiple supported expressions in one scalar from left to right exactly once. Text produced by an environment lookup, a default, or an escape SHALL NOT be scanned again during that load.

#### Scenario: Multiple expressions are composed

- **GIVEN** `API_HOST` is set to `127.0.0.1` and `API_PORT` is set to `8053`
- **WHEN** `ephemeral_api.listen` is configured as `${API_HOST}:${API_PORT}`
- **THEN** the loaded listen address SHALL equal `127.0.0.1:8053`

#### Scenario: Environment value containing an expression is not recursive

- **GIVEN** `OUTER` is set to `${INNER}` and `INNER` is set to `replacement`
- **WHEN** a string value is configured as `${OUTER}`
- **THEN** the loaded value SHALL equal the literal `${INNER}`

### Requirement: Isolate environment data from YAML structure

The loader SHALL parse the source as YAML before expansion and SHALL modify only scalar nodes tagged as strings that occur as mapping values or sequence elements. It SHALL NOT expand mapping keys, non-string scalars, tags, or alias nodes. A value-side string scalar SHALL be expanded even when it carries anchor metadata, and every alias reference to that anchor SHALL resolve to the same expanded value. Characters introduced by an environment value, including colon, hash, newline, document marker, tag-like text, anchor-like text, and quotes, MUST remain data within the original scalar and MUST NOT create, remove, or rename YAML nodes.

#### Scenario: Mapping key is not expanded

- **GIVEN** `ROOT_NAME` is set to `example.com`
- **WHEN** an alias mapping key is written as `${ROOT_NAME}`
- **THEN** the mapping key SHALL remain the literal `${ROOT_NAME}` while supported expressions in its value-side `members` list are expanded

#### Scenario: Anchored value and its alias share the expanded value

- **GIVEN** `SHARED_VALUE` is set to `example.com`
- **WHEN** a value-side string scalar is configured as `&shared "${SHARED_VALUE}"` and another value references `*shared`
- **THEN** both decoded values SHALL equal `example.com`

#### Scenario: YAML-sensitive environment value remains scalar data

- **GIVEN** a variable contains `value: injected`, a newline, and `---`
- **WHEN** that variable is referenced from a YAML string value
- **THEN** the expanded content SHALL remain one string scalar and SHALL NOT create a new mapping field or YAML document

#### Scenario: Non-string scalar is not expanded

- **WHEN** a boolean or numeric YAML scalar is loaded
- **THEN** the loader SHALL preserve its YAML type and value without environment processing

### Requirement: Preserve strict decoding and semantic validation after expansion

After expansion, the loader SHALL apply the same strict field decoding, custom YAML unmarshalling, and semantic validation used for a configuration without expressions. Unknown fields and expanded invalid CIDR, IP, URL, host/port, and path values SHALL remain load errors.

#### Scenario: Unknown field remains rejected

- **WHEN** a configuration includes a supported expression and an unknown top-level or section field
- **THEN** loading SHALL fail with an error that identifies the unknown field

#### Scenario: Expanded value fails field validation

- **GIVEN** `ALLOW_PREFIX` contains an invalid CIDR string
- **WHEN** an `ephemeral_api.allow` element references `${ALLOW_PREFIX}`
- **THEN** loading SHALL fail semantic validation rather than accepting the expanded value

#### Scenario: Configuration without expressions remains compatible

- **WHEN** a valid existing configuration contains no `${...}` or `$$` syntax
- **THEN** the loader SHALL produce the same effective configuration as before environment expansion was introduced

#### Scenario: Subsequent YAML documents retain existing behavior

- **WHEN** a configuration contains a valid first YAML document followed by another document
- **THEN** the loader SHALL continue to use the first document and ignore subsequent documents as it did before environment expansion was introduced

### Requirement: Prevent environment-value disclosure

Expression errors and load/reload failure diagnostics MUST NOT log or return a raw, quoted, escaped, normalized, or otherwise transformed environment-derived value. Expression errors SHALL identify a safely parsed variable name and YAML line and column when available. When strict decoding or semantic validation fails after a non-empty environment value was used, the loader SHALL return a fail-safe diagnostic that identifies the involved variable names and YAML paths but SHALL NOT include the original downstream error or any environment-derived representation. A successful load SHALL return the expanded value in `Config`; caller-wide provenance tracking and successful operational-log redaction are outside this capability.

#### Scenario: Required variable error contains no secret value

- **GIVEN** another variable in the same configuration contains `synthetic-secret`
- **WHEN** a required variable is unset and the load fails
- **THEN** the returned error and captured logs SHALL NOT contain `synthetic-secret`

#### Scenario: Downstream validation uses a fail-safe diagnostic

- **GIVEN** `API_LISTEN` contains an invalid environment-derived host and port value
- **WHEN** host/port validation fails after `${API_LISTEN}` is expanded
- **THEN** the returned error and captured logs SHALL name `API_LISTEN` and its YAML path
- **THEN** they SHALL NOT contain the raw value, an escaped representation, or the original downstream error

#### Scenario: Normalized environment content is not disclosed

- **GIVEN** an environment-derived alias member changes through case folding or trailing-dot canonicalization before validation fails
- **WHEN** semantic validation returns an error that would otherwise include the normalized name
- **THEN** the returned fail-safe diagnostic and captured logs SHALL NOT contain either the original or normalized environment-derived name

#### Scenario: YAML-sensitive multiline value is not disclosed

- **GIVEN** an environment-derived value contains quotes, backslashes, and a newline
- **WHEN** strict decoding or semantic validation fails after expansion
- **THEN** the fail-safe diagnostic and captured logs SHALL NOT contain any representation of that value

### Requirement: Use the same expansion behavior for every unified-config load

Every invocation of the unified ShadowDNS configuration loader SHALL resolve the current process environment at invocation time. This SHALL include normal startup, `--dry-run`, the prune-backup command, and SIGHUP reload. An environment value changed within the same process before a later loader invocation SHALL affect that later invocation.

#### Scenario: Dry-run validates required environment variables

- **GIVEN** the configuration contains `${API_TOKEN}`
- **WHEN** ShadowDNS runs with `--dry-run` and `API_TOKEN` is non-empty
- **THEN** dry-run SHALL complete configuration validation without starting listeners
- **WHEN** ShadowDNS runs with `--dry-run` and `API_TOKEN` is unset or empty
- **THEN** dry-run SHALL exit with an error that names `API_TOKEN` without revealing any environment value

#### Scenario: Later load observes changed process environment

- **GIVEN** one loader invocation resolves `API_LISTEN` to `127.0.0.1:8053`
- **WHEN** the same process changes `API_LISTEN` to `127.0.0.1:9053` before a second loader invocation
- **THEN** the second invocation SHALL resolve `API_LISTEN` to `127.0.0.1:9053`
