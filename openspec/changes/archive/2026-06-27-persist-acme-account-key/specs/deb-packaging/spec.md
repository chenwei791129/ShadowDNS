## ADDED Requirements

### Requirement: systemd unit provides a writable state directory for the ACME account key

The packaged systemd service unit SHALL declare `StateDirectory=shadowdns` so that systemd creates `/var/lib/shadowdns` owned by the `shadowdns` user with mode `0700` on every start. This SHALL provide a writable, persistent location under the `ProtectSystem=strict` sandbox for the DoH ACME account key. The existing `ReadWritePaths=/var/log/shadowdns` directive and the runtime directory directive SHALL remain in place.

The account-key persistence guarantee depends on the unit running as a static service user (`User=shadowdns`). The unit SHALL NOT use `DynamicUser=yes`, because a per-boot dynamic UID would change `StateDirectory` ownership and render a previously persisted key unreadable on the next boot, silently reintroducing new-account churn.

#### Scenario: State directory exists for the service user

- **WHEN** the `shadowdns` service is started from the packaged unit
- **THEN** `/var/lib/shadowdns` SHALL exist, be owned by `shadowdns:shadowdns`, and be writable by the service so the DoH ACME account key can be persisted there

#### Scenario: Service uses a stable user so the persisted key survives reboots

- **WHEN** the packaged unit is inspected
- **THEN** it SHALL run as `User=shadowdns` and SHALL NOT set `DynamicUser=yes`, so the persisted account key written under `/var/lib/shadowdns` remains readable by the same UID across reboots

### Requirement: Example configuration pre-fills the ACME account key path

The packaged example configuration file SHALL include a `doh.acme.account_key_file` entry within its `doh.acme` section set to `/var/lib/shadowdns/acme/account.key`, so that an operator copying the example obtains a working default that aligns with the unit's state directory. The package SHALL NOT programmatically modify an operator's live configuration file to inject this value.

#### Scenario: Operator copying the example gets a valid account key path

- **WHEN** an operator copies the packaged example configuration and enables the `doh` section
- **THEN** the `doh.acme.account_key_file` value SHALL already point to `/var/lib/shadowdns/acme/account.key`, an absolute path under the unit's state directory
