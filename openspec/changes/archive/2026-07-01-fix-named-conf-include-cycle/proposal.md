## Problem

A `named.conf` that includes itself, or two files that include each other, makes config loading recurse without bound until the Go runtime aborts the process with `fatal error: stack overflow`. This is a runtime fatal error, not a recoverable panic, so it cannot be caught. Because the SIGHUP reload path loads config through the same code, an accidental include cycle introduced before a reload crashes a running daemon instead of failing the reload and retaining the prior running state.

## Root Cause

The recursive loader in the config subsystem (`internal/config/zones.go`, `loadFile`, reached from `LoadNamedConf`) follows each `include "..."` directive by calling itself on the included path with no record of which files are already on the include stack and no recursion-depth limit. A self-include or mutual-include therefore recurses indefinitely.

## Proposed Solution

Detect include cycles and report them as a normal load error. Thread a set of already-visited include paths (keyed by resolved absolute path) through the recursive loader; before following an `include`, resolve the target to an absolute path and, if it is already on the active include chain, return a descriptive error (`include cycle detected at <path>`) instead of recursing. The error propagates through `LoadNamedConf` like any other parse error, preserving the existing fail-soft model: startup aborts with a clear message, and a reload that hits a cycle fails and retains the previously running state.

## Non-Goals

- Imposing a fixed maximum include depth as the primary mechanism (cycle detection by visited-set is preferred; a depth cap MAY be added later but is not required here).
- Detecting cycles created through filesystem symlinks that resolve to different literal paths beyond what absolute-path resolution already collapses.
- Changing any non-cyclic include behavior, include ordering, or path-resolution semantics for valid configs.

## Success Criteria

- Loading a `named.conf` that includes itself returns a descriptive error containing "cycle" and the offending path, and does NOT crash the process.
- Loading two files that include each other returns the same class of error without crashing.
- A valid (acyclic) include tree, including the same file legitimately included from two different branches is NOT falsely flagged — only a file already on the active include chain (an actual cycle) is rejected.
- A SIGHUP reload that encounters a newly-introduced cycle fails the reload and retains the prior running state (no crash).
- A unit test drives `LoadNamedConf` against a self-including fixture and an A↔B mutual-include fixture and asserts a cycle error with no stack overflow.

## Impact

- Affected specs: config-loader (modified)
- Affected code:
  - Modified: internal/config/zones.go
  - New: internal/config/include_cycle_test.go
