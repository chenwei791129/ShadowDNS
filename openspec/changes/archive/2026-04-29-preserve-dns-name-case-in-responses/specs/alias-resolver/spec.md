## ADDED Requirements

### Requirement: Preserve DNS name case across alias rewrite

The alias-resolver SHALL preserve case in rewritten DNS names. When rewriting an owner name or an RDATA name field for a backup-zone response, the resolver SHALL match labels against the root origin using case-insensitive comparison, and SHALL emit the result by concatenating the input name's original-case prefix (the part outside the matched root suffix) with the alias configuration's original-case backup origin (the case as written in the alias YAML). Internal lookup keys SHALL remain lowercase per RFC 4343 case-insensitive matching, but no normative output SHALL be lowercased.

#### Scenario: Mixed-case owner from query is preserved in rewritten owner

- **WHEN** the alias YAML configures `root.com` with backup `Backup.Com` and the query is `WwW.Backup.Com.`, and the root zone record returned has owner `www.root.com.`
- **THEN** the response record owner is `WwW.Backup.Com.` (query case prefix `WwW.` plus alias-config case suffix `Backup.Com.`)

#### Scenario: Capital backup name in alias config is preserved in CNAME target

- **WHEN** the alias YAML configures `originzone.com` with backup `Example.com` and the root zone CNAME target is `service-host.originzone.com.`
- **THEN** the rewritten CNAME target is `service-host.Example.com.` (zone-file case prefix `service-host.` plus alias-config case suffix `Example.com.`)

#### Scenario: All-lowercase backup name continues to lowercase in output

- **WHEN** the alias YAML configures backup as `backup.com` (all lowercase) and zone CNAME target is `service.root.com.`
- **THEN** the rewritten target is `service.backup.com.` unchanged from prior behavior

#### Scenario: Anywhere-match rewrite preserves case on both sides of replacement

- **WHEN** `rewrite_rdata_labels` is true for alias group `originapp.com` with backup `backupapp.com`, and the root zone CNAME target is `pull.originapp.com.outercdn.com.`
- **THEN** the rewritten target is `pull.backupapp.com.outercdn.com.` (prefix `pull.` from zone-file case, replacement `backupapp.com` from alias-config case, suffix `.outercdn.com.` from zone-file case)

##### Example: case combinations across query and config

| Query name        | Backup in YAML | Root zone CNAME target          | Rewritten target                |
|-------------------|----------------|---------------------------------|---------------------------------|
| `www.backup.com.` | `Backup.Com`   | `service.root.com.`             | `service.Backup.Com.`           |
| `WWW.BACKUP.COM.` | `Backup.Com`   | `Service.Root.Com.`             | `Service.Backup.Com.`           |
| `Www.Backup.Com.` | `backup.com`   | `Service.Root.Com.`             | `Service.backup.com.`           |
| `pull.lab.com.`   | `lab.com`      | `pull.Root.Com.cdn.example.com.` | `pull.lab.com.cdn.example.com.` (anywhere-match, root case folded for match, lookup-config case used for output) |

### Requirement: Match alias map keys case-insensitively while preserving config case

The alias-resolver's internal alias map SHALL use lowercase-folded keys for lookups (root and backup origin matching), but the configuration record values (root origin string, backup member strings, per-group flags) SHALL retain the exact case authored in the alias YAML so that rewrite output can use original case.

#### Scenario: Lookup uses lowercase fold

- **WHEN** the alias YAML configures backup `Backup.Com` (capital B) and a query arrives for `www.BACKUP.com.`
- **THEN** the alias map lookup for backup origin succeeds (case-insensitive match)

#### Scenario: Configuration record retains original case

- **WHEN** alias YAML reads `Example.com` as a member of group `originzone.com`
- **THEN** the in-memory alias group struct exposes the member string as `Example.com` (capital G preserved) when read by the rewrite path
