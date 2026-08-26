## ADDED Requirements

### Requirement: Re-expand unified-config environment expressions during SIGHUP reload

Upon each SIGHUP reload, the server SHALL invoke the unified ShadowDNS configuration loader against the current process environment before constructing replacement server state. If environment expansion, strict decoding, or semantic validation fails, the reload SHALL fail before the state swap, SHALL record a reload failure, and SHALL preserve the previously active server state and ephemeral record store.

#### Scenario: Successful reload observes a changed process environment

- **GIVEN** the running process initially loaded an environment-backed string value
- **WHEN** that process environment value is changed before SIGHUP and the expanded configuration remains valid
- **THEN** reload SHALL validate the newly expanded value and SHALL apply it to reloadable server state such as the alias map
- **THEN** startup-scoped Ephemeral API and DoH server configuration SHALL remain bound to the running instances and SHALL continue to require a process restart to change

#### Scenario: Missing required variable preserves active state

- **GIVEN** the server has a valid active state and its config contains `${REQUIRED_VALUE}`
- **WHEN** `REQUIRED_VALUE` is unset or emptied before SIGHUP
- **THEN** reload SHALL fail before state replacement, the active server state SHALL remain unchanged, and the ephemeral record store SHALL NOT be cleared

#### Scenario: Reload error does not expose environment values

- **GIVEN** a SIGHUP reload expands an environment-derived value that fails downstream validation
- **WHEN** the reload failure is returned and logged
- **THEN** the error and log entry SHALL identify the safe variable name and YAML path without containing the raw, quoted, escaped, normalized, or otherwise transformed environment-derived value or the original downstream error

### Requirement: Document process-environment limits for Kubernetes Secret updates

The operations documentation SHALL state that SIGHUP re-reads the process environment visible to the existing ShadowDNS process but does not refresh environment variables sourced from an updated Kubernetes Secret. It SHALL direct operators to restart or roll out the Pod after changing an env-backed Secret.

#### Scenario: Operator updates an env-backed Kubernetes Secret

- **WHEN** an operator changes a Kubernetes Secret referenced through a container environment variable
- **THEN** the documentation SHALL instruct the operator to perform a Pod rollout restart and SHALL NOT claim that SIGHUP alone applies the new Secret value
