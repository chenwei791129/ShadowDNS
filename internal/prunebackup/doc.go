// Package prunebackup implements the offline pruning engine behind the
// `shadowdns prune-backup` sub-command. It tokenizes a backup zone file into
// physical line ranges, recursively expands $include fragments, diffs each
// RRSet against the aliased root zone, and rewrites the affected files
// line-based so hand-authored formatting (relative owners, $TTL/$ORIGIN
// directives, trailing comments) is preserved. No DNS traffic is exchanged
// and the running server is not touched.
package prunebackup
