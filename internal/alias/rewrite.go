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

// RewriteRR returns a new dns.RR with its owner name unconditionally rewritten
// (equivalent to RewriteName on Header().Name) AND its RDATA name fields
// rewritten via the in-bailiwick rule when applicable.
//
// The input rr is NOT mutated; a copy is made (use dns.Copy from miekg/dns).
//
// Supported types for value rewrite: *dns.CNAME, *dns.NS, *dns.MX, *dns.PTR,
// *dns.SRV, *dns.SOA. Other types pass through with only the owner name rewritten.
//
// MUST NOT panic on any input (including unsupported RR types).
func RewriteRR(rr dns.RR, root, backup string) dns.RR {
	if rr == nil {
		return nil
	}

	// Create a deep copy so the original is never mutated.
	cp := dns.Copy(rr)

	// Always rewrite the owner name.
	cp.Header().Name = RewriteName(cp.Header().Name, root, backup)

	// Rewrite RDATA name fields for supported types.
	switch v := cp.(type) {
	case *dns.CNAME:
		v.Target = RewriteName(v.Target, root, backup)
	case *dns.NS:
		v.Ns = RewriteName(v.Ns, root, backup)
	case *dns.MX:
		v.Mx = RewriteName(v.Mx, root, backup)
	case *dns.PTR:
		v.Ptr = RewriteName(v.Ptr, root, backup)
	case *dns.SRV:
		v.Target = RewriteName(v.Target, root, backup)
	case *dns.SOA:
		v.Ns = RewriteName(v.Ns, root, backup)
		v.Mbox = RewriteName(v.Mbox, root, backup)
		// Numeric fields (Serial, Refresh, Retry, Expire, Minttl) are not touched.
		// A, AAAA, TXT: RDATA not modified.
	}

	return cp
}
