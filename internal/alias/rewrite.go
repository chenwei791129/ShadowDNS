package alias

import (
	"strings"

	"github.com/miekg/dns"
)

// RewriteQName transforms a query name from the backup zone's namespace into
// the root zone's namespace by suffix replacement. All inputs MUST be
// lowercased FQDNs ending with ".".
//
// Examples:
//
//	RewriteQName("www.backup.com.", "backup.com.", "root.com.") == "www.root.com."
//	RewriteQName("backup.com.", "backup.com.", "root.com.") == "root.com."
func RewriteQName(qname, backup, root string) string {
	if qname == backup {
		return root
	}
	// qname must end with "." + backup (the dot separates labels).
	suffix := "." + backup
	if strings.HasSuffix(qname, suffix) {
		prefix := qname[:len(qname)-len(suffix)]
		return prefix + "." + root
	}
	// qname is not under backup; return as-is.
	return qname
}

// RewriteName applies the in-bailiwick rule to a single DNS name n:
//   - if n == root → return backup
//   - if n ends with "." + root → strip root suffix, append backup
//   - otherwise → return n unchanged
//
// root and backup MUST be pre-canonicalized (lowercased FQDNs with trailing dot,
// e.g. via dnsutil.Canonicalize). Only n is lowercased here.
func RewriteName(n, root, backup string) string {
	if n == "" {
		return n
	}
	lower := strings.ToLower(n)

	if lower == root {
		return backup
	}
	suffix := "." + root
	if strings.HasSuffix(lower, suffix) {
		prefix := lower[:len(lower)-len(suffix)]
		return prefix + "." + backup
	}
	return lower
}

// RewriteNameAnywhere replaces the leftmost occurrence of root that begins
// at the start of n or at a label boundary (preceded by ".") with backup,
// and returns the resulting (lowercased) name. Used by RewriteRR for RDATA
// name fields when an alias group declares rewrite_rdata_labels: true to
// handle templated CNAME / NS / MX / SRV / PTR / SOA records that embed the
// root origin as a middle label.
//
// First match wins: if root appears at multiple label-boundary positions,
// only the leftmost occurrence is rewritten. This matches the
// templated-CNAME convention where a single root-origin marker is expected.
//
// root and backup MUST be pre-canonicalized (lowercased FQDNs with trailing
// dot, e.g. via dnsutil.Canonicalize). Only n is lowercased here.
func RewriteNameAnywhere(n, root, backup string) string {
	if n == "" || root == "" {
		return n
	}
	// Allocation-free hot path: strings.Index over a string slice does not
	// copy, and strings.Builder is grown to exact capacity. strings.Split
	// (slice alloc) and strings.Replace (no label-boundary protection) are
	// deliberately avoided.
	lower := strings.ToLower(n)

	start := 0
	for start <= len(lower) {
		idx := strings.Index(lower[start:], root)
		if idx < 0 {
			return lower
		}
		absIdx := start + idx
		// Leading-edge boundary: must be name start or a label separator.
		// Trailing edge is implicit because root ends with ".".
		if absIdx == 0 || lower[absIdx-1] == '.' {
			var b strings.Builder
			b.Grow(len(lower) - len(root) + len(backup))
			b.WriteString(lower[:absIdx])
			b.WriteString(backup)
			b.WriteString(lower[absIdx+len(root):])
			return b.String()
		}
		start = absIdx + 1
	}
	return lower
}

// RewriteRR returns a new dns.RR with its owner name unconditionally rewritten
// (equivalent to RewriteName on Header().Name) AND its RDATA name fields
// rewritten via either the in-bailiwick rule or the label-anywhere rule.
//
// rewriteRDATALabels selects the rule applied to RDATA name fields:
//   - false → in-bailiwick suffix-only (RewriteName), preserving the
//     conservative DNS-standard behavior;
//   - true  → label-anywhere (RewriteNameAnywhere), used for templated-CNAME
//     alias groups whose RDATA targets carry the root origin as a middle
//     label.
//
// The owner name is always rewritten with the in-bailiwick rule regardless
// of rewriteRDATALabels, because owner names are guaranteed to live in the
// root zone's bailiwick.
//
// The input rr is NOT mutated; a copy is made (use dns.Copy from miekg/dns).
//
// Supported types for value rewrite: *dns.CNAME, *dns.NS, *dns.MX, *dns.PTR,
// *dns.SRV, *dns.SOA. Other types pass through with only the owner name rewritten.
//
// MUST NOT panic on any input (including unsupported RR types).
func RewriteRR(rr dns.RR, root, backup string, rewriteRDATALabels bool) dns.RR {
	if rr == nil {
		return nil
	}

	cp := dns.Copy(rr)

	cp.Header().Name = RewriteName(cp.Header().Name, root, backup)

	rewriteValue := RewriteName
	if rewriteRDATALabels {
		rewriteValue = RewriteNameAnywhere
	}

	switch v := cp.(type) {
	case *dns.CNAME:
		v.Target = rewriteValue(v.Target, root, backup)
	case *dns.NS:
		v.Ns = rewriteValue(v.Ns, root, backup)
	case *dns.MX:
		v.Mx = rewriteValue(v.Mx, root, backup)
	case *dns.PTR:
		v.Ptr = rewriteValue(v.Ptr, root, backup)
	case *dns.SRV:
		v.Target = rewriteValue(v.Target, root, backup)
	case *dns.SOA:
		v.Ns = rewriteValue(v.Ns, root, backup)
		v.Mbox = rewriteValue(v.Mbox, root, backup)
		// Numeric fields (Serial, Refresh, Retry, Expire, Minttl) are not touched.
		// A, AAAA, TXT: RDATA not modified.
	}

	return cp
}
