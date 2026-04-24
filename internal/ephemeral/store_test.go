package ephemeral

import (
	"context"
	"testing"
	"time"
)

func newStoreWithClock(t *testing.T, base time.Time) *Store {
	t.Helper()
	s := NewStore()
	s.now = func() time.Time { return base }
	return s
}

func (s *Store) advanceClock(d time.Duration) {
	old := s.now
	s.now = func() time.Time { return old().Add(d) }
}

// mustLookupOne asserts that Lookup returned exactly one record and returns it.
func mustLookupOne(t *testing.T, s *Store, fqdn string) Record {
	t.Helper()
	recs, ok := s.Lookup(fqdn)
	if !ok {
		t.Fatalf("Lookup(%q): ok = false", fqdn)
	}
	if len(recs) != 1 {
		t.Fatalf("Lookup(%q): got %d records, want 1", fqdn, len(recs))
	}
	return recs[0]
}

// findRecord returns the first record with the given value, or nil.
func findRecord(recs []Record, value string) *Record {
	for i := range recs {
		if recs[i].Value == value {
			return &recs[i]
		}
	}
	return nil
}

func TestStore_PutAndLookup(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	s := newStoreWithClock(t, base)

	s.Put("_acme-challenge.example.com", "abc123", 120)

	rec := mustLookupOne(t, s, "_acme-challenge.example.com")
	if rec.Value != "abc123" {
		t.Errorf("Value = %q, want %q", rec.Value, "abc123")
	}
}

func TestStore_LookupCanonicalizesFQDN(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("FOO.EXAMPLE.COM", "v1", 60)

	rec := mustLookupOne(t, s, "foo.example.com.")
	if rec.Value != "v1" {
		t.Errorf("Value = %q, want %q", rec.Value, "v1")
	}
}

func TestStore_LookupUnknownReturnsFalse(t *testing.T) {
	s := NewStore()
	recs, ok := s.Lookup("nope.example.com.")
	if ok {
		t.Fatal("expected Lookup for unknown FQDN to return ok=false")
	}
	if recs != nil {
		t.Errorf("expected nil slice, got %v", recs)
	}
}

func TestStore_LookupEmptyFQDNReturnsFalse(t *testing.T) {
	s := NewStore()
	if _, ok := s.Lookup(""); ok {
		t.Fatal("expected Lookup for empty FQDN to return ok=false")
	}
}

func TestStore_PutAppendsDistinctValues(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("_acme-challenge.example.com.", "token-A", 120)
	s.Put("_acme-challenge.example.com.", "token-B", 60)

	recs, ok := s.Lookup("_acme-challenge.example.com.")
	if !ok {
		t.Fatal("expected Lookup to succeed")
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if findRecord(recs, "token-A") == nil {
		t.Error("token-A missing from lookup result")
	}
	if findRecord(recs, "token-B") == nil {
		t.Error("token-B missing from lookup result")
	}
}

func TestStore_PutReturnsCount(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	if n := s.Put("foo.example.com.", "a", 60); n != 1 {
		t.Errorf("first Put returned %d, want 1", n)
	}
	if n := s.Put("foo.example.com.", "b", 60); n != 2 {
		t.Errorf("second Put (distinct value) returned %d, want 2", n)
	}
	if n := s.Put("foo.example.com.", "a", 120); n != 2 {
		t.Errorf("refresh of existing value returned %d, want 2 (count unchanged)", n)
	}
	if n := s.Put("", "x", 60); n != 0 {
		t.Errorf("empty FQDN Put returned %d, want 0", n)
	}
}

func TestStore_PutSameValueRefreshesTTL(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "same", 60)
	s.advanceClock(30 * time.Second)
	s.Put("foo.example.com.", "same", 120)

	recs, _ := s.Lookup("foo.example.com.")
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 (same value must refresh, not append)", len(recs))
	}

	// The original entry would have expired at T+60 (now=T+30, ttl=60).
	// After the refresh with ttl=120, the new expiry is T+30+120=T+150.
	// Advance past the original expiry (to T+90) and confirm the entry
	// survives, proving the refresh actually reset expireAt.
	s.advanceClock(60 * time.Second)
	recs, ok := s.Lookup("foo.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "same" {
		t.Errorf("entry lost after refresh: recs=%+v ok=%v, want single 'same' still live", recs, ok)
	}
}

func TestStore_LookupAfterExpirationReturnsEmpty(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "v", 60)
	s.advanceClock(61 * time.Second)

	recs, ok := s.Lookup("foo.example.com.")
	if ok {
		t.Fatal("expected expired record to be absent")
	}
	if recs != nil {
		t.Errorf("expected nil slice, got %v", recs)
	}
}

func TestStore_LookupAtExactExpirationReturnsEmpty(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "v", 60)
	s.advanceClock(60 * time.Second)

	if _, ok := s.Lookup("foo.example.com."); ok {
		t.Fatal("expected record at exact expiration time to be absent")
	}
}

func TestStore_PerEntryExpiration(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "short", 30)
	s.Put("foo.example.com.", "long", 300)

	s.advanceClock(31 * time.Second)

	recs, ok := s.Lookup("foo.example.com.")
	if !ok {
		t.Fatal("expected the long-TTL entry to still be present")
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 (short must expire independently of long)", len(recs))
	}
	if recs[0].Value != "long" {
		t.Errorf("surviving record value = %q, want long", recs[0].Value)
	}
}

func TestStore_DeleteRemovesAllEntriesForFQDN(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "a", 120)
	s.Put("foo.example.com.", "b", 120)
	s.Put("foo.example.com.", "c", 120)

	s.Delete("foo.example.com.")

	if _, ok := s.Lookup("foo.example.com."); ok {
		t.Fatal("expected all entries for FQDN to be deleted")
	}
}

func TestStore_DeleteCanonicalizesFQDN(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "v", 120)
	s.Delete("FOO.EXAMPLE.COM")

	if _, ok := s.Lookup("foo.example.com."); ok {
		t.Fatal("expected Delete to match canonical form")
	}
}

func TestStore_DeleteNonExistentIsNoOp(t *testing.T) {
	s := NewStore()
	s.Delete("missing.example.com.")
}

func TestStore_DeleteValueRemovesOnlyMatchingEntry(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("_acme-challenge.example.com.", "token-A", 120)
	s.Put("_acme-challenge.example.com.", "token-B", 120)

	if !s.DeleteValue("_acme-challenge.example.com.", "token-A") {
		t.Fatal("DeleteValue returned false, want true for matching entry")
	}
	recs, ok := s.Lookup("_acme-challenge.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "token-B" {
		t.Errorf("after DeleteValue: recs=%+v ok=%v, want single token-B", recs, ok)
	}
}

func TestStore_DeleteValueReturnsFalseWhenNoMatch(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "token-A", 120)

	if s.DeleteValue("foo.example.com.", "token-X") {
		t.Fatal("DeleteValue returned true, want false for non-matching value")
	}
	recs, ok := s.Lookup("foo.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "token-A" {
		t.Errorf("store mutated after non-matching DeleteValue: recs=%+v ok=%v", recs, ok)
	}
}

func TestStore_DeleteValueRemovesFQDNKeyWhenLastEntry(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "only", 120)

	if !s.DeleteValue("foo.example.com.", "only") {
		t.Fatal("DeleteValue returned false, want true")
	}
	// Directly inspect the internal map to confirm the FQDN key is gone
	// (not just an empty slice retained).
	s.mu.RLock()
	_, present := s.records["foo.example.com."]
	s.mu.RUnlock()
	if present {
		t.Error("expected FQDN key to be removed from the map when last entry is deleted")
	}
}

func TestStore_DeleteValueUnknownFQDNReturnsFalse(t *testing.T) {
	s := NewStore()
	if s.DeleteValue("missing.example.com.", "whatever") {
		t.Fatal("DeleteValue on unknown FQDN returned true, want false")
	}
}

func TestStore_DeleteValueCanonicalizesFQDN(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("foo.example.com.", "v", 120)

	if !s.DeleteValue("FOO.EXAMPLE.COM", "v") {
		t.Fatal("DeleteValue returned false for non-canonical FQDN, want true after canonicalization")
	}
	if _, ok := s.Lookup("foo.example.com."); ok {
		t.Error("entry still present after DeleteValue with non-canonical FQDN")
	}
}

func TestStore_GCSweepRemovesExpired(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	s := newStoreWithClock(t, base)

	s.Put("keep.example.com.", "k", 300)
	s.Put("expire.example.com.", "e1", 10)
	s.Put("expire.example.com.", "e2", 20)
	s.Put("mixed.example.com.", "gone", 10)
	s.Put("mixed.example.com.", "alive", 300)

	s.advanceClock(30 * time.Second)
	s.gcSweep()

	if _, ok := s.Lookup("expire.example.com."); ok {
		t.Error("expected expire.example.com. to be gone after GC")
	}
	recs, ok := s.Lookup("mixed.example.com.")
	if !ok || len(recs) != 1 || recs[0].Value != "alive" {
		t.Errorf("mixed.example.com. lookup = %+v ok=%v, want single 'alive'", recs, ok)
	}
	if recs, ok := s.Lookup("keep.example.com."); !ok || len(recs) != 1 {
		t.Errorf("keep.example.com. lookup = %+v ok=%v, want unchanged", recs, ok)
	}
}

func TestStore_GCStopsOnContextCancellation(t *testing.T) {
	s := NewStore()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.GC(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("GC did not return within 1s after context cancellation")
	}
}

func TestStore_Clear(t *testing.T) {
	s := newStoreWithClock(t, time.Unix(1_000_000, 0))

	s.Put("a.example.com.", "va", 60)
	s.Put("b.example.com.", "v1", 60)
	s.Put("b.example.com.", "v2", 60)

	s.Clear()

	if _, ok := s.Lookup("a.example.com."); ok {
		t.Fatal("expected a to be cleared")
	}
	if _, ok := s.Lookup("b.example.com."); ok {
		t.Fatal("expected b to be cleared (both entries)")
	}
}
