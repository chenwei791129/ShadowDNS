// Package ephemeral provides an in-memory store for ephemeral DNS TXT records
// with TTL-based expiration. It is used by the ephemeral TXT API to support
// ACME DNS-01 challenges without touching zone files on disk.
//
// A single FQDN may hold multiple distinct TXT values simultaneously (for
// example the apex + wildcard validation tokens ACME issues against the same
// _acme-challenge.<domain> name). Put semantics are add-or-refresh; Delete
// wipes every entry associated with an FQDN in one operation; DeleteValue
// removes a single entry matching a specific value so one ACME challenge
// can clean up without disturbing a concurrent sibling challenge.
package ephemeral

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/chenwei791129/ShadowDNS/internal/dnsutil"
)

// DefaultGCInterval is the default interval between garbage-collection sweeps.
const DefaultGCInterval = 30 * time.Second

// Record is one unexpired ephemeral TXT entry returned from Lookup. The
// entry's remaining lifetime is deliberately not exposed: DNS response TTL
// is fixed by the handler, not derived from Store expiry.
type Record struct {
	Value string
}

// Store holds ephemeral TXT records keyed by lookup-fold FQDN (lowercased,
// with trailing dot, via dnsutil.LookupKey). Each FQDN may have multiple
// entries distinguished by their TXT value. All methods are safe for
// concurrent use.
type Store struct {
	mu      sync.RWMutex
	records map[string][]entry
	now     func() time.Time
}

type entry struct {
	value    string
	expireAt time.Time
}

// NewStore returns an empty Store ready for use.
func NewStore() *Store {
	return &Store{
		records: make(map[string][]entry),
		now:     time.Now,
	}
}

// Put adds or refreshes an ephemeral TXT entry for fqdn and returns the
// total number of entries currently held under the FQDN after the call.
// When the given value already exists under the FQDN, that entry's
// expiration is reset to now+ttl (idempotent refresh, count unchanged).
// Otherwise a new entry is appended (count increments). Empty FQDNs are
// ignored and the returned count is zero.
//
// The count is computed while still holding the write lock, so callers
// observe a consistent post-Put snapshot without re-acquiring the mutex.
// There is no per-FQDN cap: scan / memory growth is bounded in practice
// by the ephemeral_api IP ACL rather than a hard limit here.
func (s *Store) Put(fqdn, value string, ttl uint32) int {
	canonical := dnsutil.LookupKey(fqdn)
	if canonical == "" {
		return 0
	}
	newExpiry := s.now().Add(time.Duration(ttl) * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.records[canonical]
	for i := range entries {
		if entries[i].value == value {
			entries[i].expireAt = newExpiry
			return len(entries)
		}
	}
	entries = append(entries, entry{
		value:    value,
		expireAt: newExpiry,
	})
	s.records[canonical] = entries
	return len(entries)
}

// Lookup returns every unexpired entry for fqdn, preserving insertion order.
// When no entries exist or all have expired, ok is false and the slice is nil.
// Only the value is returned; response TTL is owned by the DNS handler.
func (s *Store) Lookup(fqdn string) ([]Record, bool) {
	canonical := dnsutil.LookupKey(fqdn)
	if canonical == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Sample now inside the lock so the expiry comparison uses the same
	// epoch as the entries slice we are reading, matching gcSweep's pattern.
	now := s.now()
	entries := s.records[canonical]
	var out []Record
	for _, e := range entries {
		if e.expireAt.After(now) {
			out = append(out, Record{Value: e.value})
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// Delete removes every ephemeral entry for fqdn in a single operation.
// Calling Delete for an FQDN with no entries is a no-op. Delete only
// touches the ephemeral store; zone file records are unaffected.
func (s *Store) Delete(fqdn string) {
	canonical := dnsutil.LookupKey(fqdn)
	if canonical == "" {
		return
	}
	s.mu.Lock()
	delete(s.records, canonical)
	s.mu.Unlock()
}

// DeleteValue removes at most one entry under fqdn matching value exactly
// (case-sensitive, no normalization). Returns whether an entry was removed.
// Removes the FQDN key when its last entry is deleted so no empty slice is
// retained.
func (s *Store) DeleteValue(fqdn, value string) bool {
	canonical := dnsutil.LookupKey(fqdn)
	if canonical == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, ok := s.records[canonical]
	if !ok {
		return false
	}
	idx := slices.IndexFunc(entries, func(e entry) bool { return e.value == value })
	if idx == -1 {
		return false
	}
	entries = slices.Delete(entries, idx, idx+1)
	if len(entries) == 0 {
		delete(s.records, canonical)
	} else {
		s.records[canonical] = entries
	}
	return true
}

// Clear removes all records unconditionally. Used during SIGHUP reload so
// ephemeral state does not survive a config reload.
func (s *Store) Clear() {
	s.mu.Lock()
	s.records = make(map[string][]entry)
	s.mu.Unlock()
}

// GC runs a periodic garbage-collection loop that removes expired entries
// every interval ticks. It blocks until ctx is cancelled. Callers typically
// launch it in a dedicated goroutine.
func (s *Store) GC(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultGCInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.gcSweep()
		}
	}
}

// gcSweep prunes expired entries from every FQDN slice. When all entries
// for an FQDN are expired, the FQDN key is removed so the map does not
// retain empty slices.
func (s *Store) gcSweep() {
	// Cheap RLock probe: when the store is empty (common in the ACME
	// DNS-01 use case), skip the write lock entirely so live DNS lookups
	// and API puts are not briefly stalled by a no-op sweep.
	s.mu.RLock()
	empty := len(s.records) == 0
	s.mu.RUnlock()
	if empty {
		return
	}
	now := s.now()
	s.mu.Lock()
	for fqdn, entries := range s.records {
		kept := entries[:0]
		for _, e := range entries {
			if e.expireAt.After(now) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(s.records, fqdn)
		} else {
			s.records[fqdn] = kept
		}
	}
	s.mu.Unlock()
}
