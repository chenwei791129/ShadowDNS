## ADDED Requirements

### Requirement: Follow in-zone CNAME targets during backup zone resolution

When the alias-resolver resolves a backup-zone query and the root zone lookup yields a CNAME record whose target is within the root zone (in-bailiwick), the alias-resolver SHALL continue looking up `(target, original qtype)` in the root zone and collect the full CNAME chain plus final records. All collected records SHALL be rewritten to the backup namespace using the existing owner-name and in-bailiwick RDATA rewrite rules before returning.

The CNAME following SHALL operate entirely within the root zone's data, using exact lookup first and falling back to wildcard matching at each step. The alias-resolver SHALL stop following when:
1. A non-CNAME record of the requested qtype is found at the current target.
2. The current target is out-of-bailiwick (not within the root zone).
3. No record of any type exists at the current target.
4. The chain depth reaches 8.

#### Scenario: Backup zone query follows in-zone CNAME and returns rewritten records

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `app.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `app.backup.com. A`
- **THEN** the alias-resolver returns `app.backup.com. CNAME service.backup.com.` and `service.backup.com. A 10.0.0.1`

#### Scenario: Backup zone CNAME chain is followed within root zone

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `a.root.com. CNAME b.root.com.` AND `b.root.com. CNAME c.root.com.` AND `c.root.com. A 9.8.7.6` AND a client queries `a.backup.com. A`
- **THEN** the alias-resolver returns `a.backup.com. CNAME b.backup.com.`, `b.backup.com. CNAME c.backup.com.`, and `c.backup.com. A 9.8.7.6`

#### Scenario: Backup zone CNAME with out-of-bailiwick target stops at the CNAME

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `app.root.com. CNAME cdn.external.com.` AND a client queries `app.backup.com. A`
- **THEN** the alias-resolver returns only `app.backup.com. CNAME cdn.external.com.` (target is not rewritten because it is out-of-bailiwick)

#### Scenario: Backup zone wildcard CNAME with in-zone target is followed

- **WHEN** `backup.com.` is a backup of `root.com.` AND the root zone contains `*.root.com. CNAME service.root.com.` AND `service.root.com. A 10.0.0.1` AND a client queries `any.backup.com. A`
- **THEN** the alias-resolver returns `any.backup.com. CNAME service.backup.com.` and `service.backup.com. A 10.0.0.1`
