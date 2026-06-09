package ratelimit

import (
	"sync"
	"time"
)

// numShards is the fixed shard count for the account table. A power of two so
// the shard index is a mask, not a modulo. Sharding spreads lock contention
// across the response hot path; 32 keeps per-shard locks short without wasting
// memory on small tables.
const numShards = 32

// table is a bounded, sharded LRU account ledger. Each shard owns an
// independent lock, a map for O(1) lookup, and an intrusive doubly-linked list
// for LRU ordering, so a full shard evicts its least-recently-used account in
// O(1) inline with the charge — table maintenance never blocks the hot path on
// a background sweep. The LRU links live inside tableEntry, so a steady-state
// hit (the common flood case) does no allocation at all.
type table struct {
	shards [numShards]tableShard
}

type tableShard struct {
	mu    sync.Mutex
	items map[accountKey]*tableEntry
	head  *tableEntry // most-recently-used
	tail  *tableEntry // least-recently-used
	size  int
	cap   int // max live accounts in this shard
}

// tableEntry pairs a key with its credit account and the intrusive LRU links.
// The account is stored by value (no pointer fields) and the prev/next links
// are reused on eviction, so the entry allocation happens at most once per
// live slot rather than per access.
type tableEntry struct {
	key        accountKey
	acct       creditAccount
	prev, next *tableEntry
}

// newTable builds an empty table whose total live accounts are bounded by
// maxSize (split evenly across shards, minimum one per shard). minSize seeds the
// per-shard map pre-allocation so a steady-state table does not rehash under
// load.
func newTable(maxSize, minSize int) *table {
	perShard := maxSize / numShards
	if perShard < 1 {
		perShard = 1
	}
	hint := minSize / numShards
	if hint < 1 {
		hint = 1
	}
	t := &table{}
	for i := range t.shards {
		t.shards[i].items = make(map[accountKey]*tableEntry, hint)
		t.shards[i].cap = perShard
	}
	return t
}

// charge debits one credit from k's account (creating it, evicting the shard's
// LRU account first when at capacity) and reports whether the response is
// over-limit. When over-limit it also advances the account's slip cadence
// counter and returns its post-increment value, so the caller can choose the
// drop/truncate action without a second lock acquisition. The whole operation
// runs under the shard lock so the account pointer never escapes; the caller
// only sees the verdict and slip count.
func (t *table) charge(k accountKey, now time.Time, rate, window float64) (overLimit bool, slip uint32) {
	s := &t.shards[shardIndex(k)]
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.touch(k)
	overLimit = e.acct.charge(now, rate, rate*window)
	if overLimit {
		e.acct.slip++
	}
	return overLimit, e.acct.slip
}

// touch returns the entry for k, creating it (evicting the LRU entry when the
// shard is at capacity) and moving it to the most-recently-used position. The
// caller must hold s.mu.
func (s *tableShard) touch(k accountKey) *tableEntry {
	if e, ok := s.items[k]; ok {
		s.moveToFront(e)
		return e
	}
	if s.size >= s.cap && s.tail != nil {
		s.evictTail()
	}
	e := &tableEntry{key: k}
	s.items[k] = e
	s.pushFront(e)
	s.size++
	return e
}

// pushFront inserts e at the head (MRU) of the shard's LRU list.
func (s *tableShard) pushFront(e *tableEntry) {
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	}
	s.head = e
	if s.tail == nil {
		s.tail = e
	}
}

// moveToFront unlinks e from its current position and reinserts it at the head.
func (s *tableShard) moveToFront(e *tableEntry) {
	if s.head == e {
		return
	}
	s.unlink(e)
	s.pushFront(e)
}

// unlink removes e from the LRU list without touching the map or size.
func (s *tableShard) unlink(e *tableEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

// evictTail removes the least-recently-used entry from both the list and map.
func (s *tableShard) evictTail() {
	victim := s.tail
	delete(s.items, victim.key)
	s.unlink(victim)
	s.size--
}

// len reports the total live account count across all shards. Used by tests.
func (t *table) len() int {
	n := 0
	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		n += s.size
		s.mu.Unlock()
	}
	return n
}

// has reports whether k currently has a live account. Used by tests.
func (t *table) has(k accountKey) bool {
	s := &t.shards[shardIndex(k)]
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[k]
	return ok
}

// shardIndex hashes an account key to a shard with an allocation-free FNV-1a
// over the masked address, category, and imputed name.
func shardIndex(k accountKey) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	b := k.block.As16()
	for _, c := range b {
		h = (h ^ uint64(c)) * prime64
	}
	h = (h ^ uint64(k.category)) * prime64
	for i := 0; i < len(k.name); i++ {
		h = (h ^ uint64(k.name[i])) * prime64
	}
	return int(h & (numShards - 1))
}
