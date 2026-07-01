## ADDED Requirements

### Requirement: Detect and reject named.conf include cycles

The config loader SHALL detect cyclic `include` directives and report them as a load error instead of recursing without bound. The loader SHALL track the set of include files currently on the active include chain, keyed by resolved absolute path. Before following an `include`, the loader SHALL resolve the target to an absolute path and, if that path is already on the active chain, SHALL return a descriptive error identifying the offending path and SHALL NOT recurse into it. A file that is legitimately included from two separate branches of an acyclic tree SHALL NOT be flagged. The process SHALL NOT abort with a stack overflow on any cyclic input.

#### Scenario: Self-including file is rejected

- **WHEN** `LoadNamedConf` loads a file that contains an `include` of itself
- **THEN** loading returns an error identifying the cycle and the offending path, and the process does not crash

#### Scenario: Mutual include cycle is rejected

- **WHEN** `LoadNamedConf` loads file A that includes file B, where B includes A
- **THEN** loading returns a cycle error and the process does not crash

#### Scenario: Reload hitting a cycle retains prior state

- **WHEN** a SIGHUP reload loads a config whose include tree now contains a cycle
- **THEN** the reload fails with the cycle error and the server continues serving the previously loaded state
