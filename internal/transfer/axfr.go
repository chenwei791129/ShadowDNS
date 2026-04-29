package transfer

import (
	"slices"
	"sync"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/alias"
	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// HandleAXFR is the entry point for AXFR (and IXFR-fallback) queries.
//
// Rules:
//   - AXFR over UDP → REFUSED (RFC 5936 §2.1)
//   - z == nil → REFUSED (zone not loaded)
//   - Otherwise streams SOA → all records → SOA over TCP
//
// MUST NOT panic on any input.
func HandleAXFR(w dns.ResponseWriter, req *dns.Msg, z *zone.Zone, logger *zap.Logger) {
	// Network guard: AXFR over UDP is always REFUSED.
	if dnsutil.IsUDP(w) {
		replyRefused(w, req)
		return
	}

	// Zone guard: if the zone is not loaded, REFUSED.
	if z == nil {
		replyRefused(w, req)
		return
	}

	// Collect all non-SOA records in a deterministic order.
	records := collectNonSOA(z)

	streamAXFR(w, req, z.SOA, records, logger)
}

// HandleAliasAXFR handles AXFR for a backup zone by streaming the root zone's
// records with in-bailiwick rewrite applied, substituting TXT/MX/SRV override
// records from the backup zone where present.
//
// rewriteRDATALabels selects between in-bailiwick suffix-only and label-anywhere
// RDATA rewriting (see alias.RewriteRR).
//
// rootZone MUST not be nil. backupZone MAY be nil (alias declared without its
// own .fwd file).
//
// MUST NOT panic on any input.
func HandleAliasAXFR(w dns.ResponseWriter, req *dns.Msg, backupOrigin string, rootZone *zone.Zone, backupZone *zone.Zone, rewriteRDATALabels bool, logger *zap.Logger) {
	// Network guard: AXFR over UDP is always REFUSED.
	if dnsutil.IsUDP(w) {
		replyRefused(w, req)
		return
	}

	// rootZone must be present.
	if rootZone == nil {
		replyRefused(w, req)
		return
	}

	// Build the backup SOA.
	soa := alias.BackupSOA(rootZone.SOA, rootZone.Origin, backupOrigin)

	// Walk root zone records deterministically (sorted by owner, then by type).
	// Skip root SOA; emit override or rewritten records.
	records := buildAliasRecords(rootZone, backupZone, rootZone.Origin, backupOrigin, rewriteRDATALabels)

	streamAXFR(w, req, soa, records, logger)
}

// buildAliasRecords produces the non-SOA record list for a backup-zone AXFR.
func buildAliasRecords(rootZone, backupZone *zone.Zone, rootOrigin, backupOrigin string, rewriteRDATALabels bool) []dns.RR {
	// Collect and sort owners for determinism.
	owners := make([]string, 0, len(rootZone.Records))
	for owner := range rootZone.Records {
		owners = append(owners, owner)
	}
	slices.Sort(owners)

	// Build a quick lookup: backupOwner+type → override records.
	// backupOwner = alias.RewriteName(rootOwner, rootOrigin, backupOrigin).
	var overrideKey = func(owner string, rrtype uint16) string {
		return owner + "\x00" + dns.TypeToString[rrtype]
	}

	overrides := make(map[string][]dns.RR)
	if backupZone != nil {
		for ownerFqdn, s := range backupZone.Records {
			s.Each(func(rrtype uint16, rrs []dns.RR) {
				k := overrideKey(ownerFqdn, rrtype)
				overrides[k] = append(overrides[k], rrs...)
			})
		}
	}

	var result []dns.RR

	for _, owner := range owners {
		// Translate root owner → backup owner for override lookup.
		backupOwner := alias.RewriteName(owner, rootOrigin, backupOrigin)

		// Collect per-qtype RRs from the store, then sort qtypes for determinism.
		typeMap := make(map[uint16][]dns.RR)
		rootZone.Records[owner].Each(func(rrtype uint16, rrs []dns.RR) {
			typeMap[rrtype] = rrs
		})

		types := make([]uint16, 0, len(typeMap))
		for t := range typeMap {
			types = append(types, t)
		}
		slices.Sort(types)

		for _, rrtype := range types {
			// Skip the root SOA — we already sent the BackupSOA.
			if rrtype == dns.TypeSOA {
				continue
			}

			// Check for backup override when the type is overridable.
			if dnsutil.OverridableTypes[rrtype] {
				k := overrideKey(backupOwner, rrtype)
				if ov, ok := overrides[k]; ok {
					// Emit override records (already in backup namespace).
					result = append(result, ov...)
					continue
				}
			}

			// No override: rewrite root records into backup namespace.
			for _, rr := range typeMap[rrtype] {
				result = append(result, alias.RewriteRR(rr, rootOrigin, backupOrigin, rewriteRDATALabels))
			}
		}
	}

	return result
}

// streamAXFR sends the full AXFR envelope sequence: SOA → records → SOA.
func streamAXFR(w dns.ResponseWriter, req *dns.Msg, soa *dns.SOA, records []dns.RR, logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	tr := new(dns.Transfer)
	ch := make(chan *dns.Envelope)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := tr.Out(w, req, ch); err != nil {
			logger.Sugar().Warnw("AXFR stream error",
				"zone", soa.Hdr.Name,
				"err", err.Error(),
			)
		}
	}()

	// Stream: SOA first.
	ch <- &dns.Envelope{RR: []dns.RR{soa}}

	// Stream: all non-SOA records (can be a single envelope or split).
	if len(records) > 0 {
		ch <- &dns.Envelope{RR: records}
	}

	// Stream: SOA last (closing sentinel).
	ch <- &dns.Envelope{RR: []dns.RR{soa}}

	close(ch)
	wg.Wait()
}

// collectNonSOA returns all non-SOA records from the zone in a deterministic order.
func collectNonSOA(z *zone.Zone) []dns.RR {
	owners := make([]string, 0, len(z.Records))
	for owner := range z.Records {
		owners = append(owners, owner)
	}
	slices.Sort(owners)

	var result []dns.RR
	for _, owner := range owners {
		z.Records[owner].Each(func(rrtype uint16, rrs []dns.RR) {
			if rrtype == dns.TypeSOA {
				return
			}
			result = append(result, rrs...)
		})
	}
	return result
}

// replyRefused sends an RCODE=REFUSED response.
func replyRefused(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = false
	m.Authoritative = false
	m.Rcode = dns.RcodeRefused
	_ = w.WriteMsg(m)
}
