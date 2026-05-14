package transfer

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// DefaultBackoff is the sequence of delays between NOTIFY retries.
// The first attempt is immediate; subsequent delays follow this slice.
var DefaultBackoff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// NotifyTarget is one NOTIFY destination derived from a zone's NS records.
// Host is the NS hostname (FQDN) used for logging and de-duplication; IPs
// holds the in-zone glue A/AAAA addresses to NOTIFY directly. An empty IPs
// slice signals "no in-zone glue" — the dispatch layer skips the target
// rather than falling back to the system resolver.
type NotifyTarget struct {
	Host string
	IPs  []netip.Addr
}

// NotifyTargets returns the slave targets for a zone: every NS record target,
// excluding any that equals the SOA MNAME (which identifies the primary
// master). For each target, IPs is populated from in-zone A/AAAA glue
// records; targets with no in-zone glue carry an empty IPs slice.
func NotifyTargets(z *zone.Zone) []NotifyTarget {
	if z == nil {
		return nil
	}

	// Determine the MNAME to exclude.
	var mname string
	if z.SOA != nil {
		mname = dns.Fqdn(z.SOA.Ns)
	}

	nsRRs := z.Lookup(z.Origin, dns.TypeNS)
	var targets []NotifyTarget
	for _, rr := range nsRRs {
		ns, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		target := dns.Fqdn(ns.Ns)
		if target == mname {
			// This NS is the primary master; skip.
			continue
		}
		targets = append(targets, NotifyTarget{
			Host: target,
			IPs:  resolveInZoneGlue(z, target),
		})
	}
	return targets
}

// resolveInZoneGlue returns every A and AAAA record for host found inside
// z's own record map, converted to netip.Addr. No cross-zone resolution is
// performed and no system resolver is consulted.
func resolveInZoneGlue(z *zone.Zone, host string) []netip.Addr {
	if z == nil {
		return nil
	}
	owner := dnsutil.LookupKey(host)

	var ips []netip.Addr
	for _, rr := range z.Lookup(owner, dns.TypeA) {
		a, ok := rr.(*dns.A)
		if !ok || a.A == nil {
			continue
		}
		if addr, ok := netip.AddrFromSlice(a.A); ok {
			// Unmap so an A record stored as 16-byte ::ffff:v4 normalises to
			// a 4-byte IPv4 Addr; callers can then compare against
			// netip.MustParseAddr("v4-literal") without surprise.
			ips = append(ips, addr.Unmap())
		}
	}
	for _, rr := range z.Lookup(owner, dns.TypeAAAA) {
		aaaa, ok := rr.(*dns.AAAA)
		if !ok || aaaa.AAAA == nil {
			continue
		}
		if addr, ok := netip.AddrFromSlice(aaaa.AAAA); ok {
			ips = append(ips, addr)
		}
	}
	return ips
}

// SendNOTIFY sends a DNS NOTIFY message for `origin` to `targetAddr` (host:port)
// over UDP, with up to len(DefaultBackoff) retries.
//
// Returns nil on success, or the last error after all attempts exhausted.
// Honors ctx cancellation between retries.
func SendNOTIFY(ctx context.Context, origin string, targetAddr string, logger *zap.Logger) error {
	return sendNotifyWithBackoff(ctx, origin, targetAddr, DefaultBackoff, logger)
}

// sendNotifyWithBackoff is the internal implementation used by both SendNOTIFY
// (production) and tests (with a short backoff slice).
func sendNotifyWithBackoff(ctx context.Context, origin, targetAddr string, backoff []time.Duration, logger *zap.Logger) error {
	msg := buildNotifyMsg(origin)

	var lastErr error

	// Total attempts = 1 (first) + len(backoff) (retries).
	maxAttempts := 1 + len(backoff)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check for cancellation before each attempt.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, err := dns.Exchange(msg, targetAddr)
		if err == nil {
			return nil
		}

		lastErr = err
		// `addr` is the network address actually dialled (ip:port). Higher
		// layers (e.g. dispatchNotifies) attach the NS hostname and source
		// fields onto the logger via With(), so they appear here too.
		logger.Sugar().Warnw("NOTIFY failed",
			"zone", origin,
			"addr", targetAddr,
			"attempt", attempt+1,
			"err", err.Error(),
		)

		// If there are still retries remaining, wait (interruptibly).
		if attempt < len(backoff) {
			delay := backoff[attempt]
			select {
			case <-time.After(delay):
				// Ready for next attempt.
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("NOTIFY zone %s to %s failed after %d attempts: %w",
		origin, targetAddr, maxAttempts, lastErr)
}

// buildNotifyMsg constructs a DNS NOTIFY message for the given zone origin.
func buildNotifyMsg(origin string) *dns.Msg {
	m := new(dns.Msg)
	m.SetNotify(dns.Fqdn(origin))
	return m
}
