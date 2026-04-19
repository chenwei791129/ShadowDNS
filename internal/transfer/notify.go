package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/chenwei791129/ShadowDNS/internal/zone"
)

// DefaultBackoff is the sequence of delays between NOTIFY retries.
// The first attempt is immediate; subsequent delays follow this slice.
var DefaultBackoff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// NotifyTargets returns the slave targets for a zone: every NS record target,
// excluding any that equals the SOA MNAME (which identifies the primary master).
//
// Targets are returned as bare hostnames; the caller resolves them to IPs.
func NotifyTargets(z *zone.Zone) []string {
	if z == nil {
		return nil
	}

	// Determine the MNAME to exclude.
	var mname string
	if z.SOA != nil {
		mname = dns.Fqdn(z.SOA.Ns)
	}

	nsRRs := z.Lookup(z.Origin, dns.TypeNS)
	var targets []string
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
		targets = append(targets, target)
	}
	return targets
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
		logger.Sugar().Warnw("NOTIFY failed",
			"zone", origin,
			"target", targetAddr,
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
