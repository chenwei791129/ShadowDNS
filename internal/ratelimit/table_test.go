package ratelimit

import (
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func keyN(i int) accountKey {
	return accountKey{
		block:    netip.MustParseAddr("192.0.2.0"),
		category: CategoryResponses,
		name:     fmt.Sprintf("h%d.example.com.", i),
	}
}

func TestTableCapacity(t *testing.T) {
	const maxSize, minSize = 320, 64
	now := time.Unix(1_000_000, 0)
	// rate/ceiling large enough that charge never over-limits; we only care
	// about table occupancy here, not the credit verdict.
	const rate, window = 1e9, 15

	t.Run("table size never exceeds max-table-size", func(t *testing.T) {
		tbl := newTable(maxSize, minSize)
		for i := 0; i < 5000; i++ {
			tbl.charge(keyN(i), now, rate, window)
		}
		if got := tbl.len(); got > maxSize {
			t.Errorf("table len = %d, want <= max-table-size %d", got, maxSize)
		}
		if got := tbl.len(); got < minSize {
			t.Errorf("table len = %d, want >= min-table-size %d after saturating with 5000 keys", got, minSize)
		}
	})

	t.Run("most-recently-used account survives eviction", func(t *testing.T) {
		tbl := newTable(maxSize, minSize)
		hot := accountKey{block: netip.MustParseAddr("192.0.2.0"), category: CategoryResponses, name: "hot.example.com."}
		tbl.charge(hot, now, rate, window)
		for i := 0; i < 5000; i++ {
			tbl.charge(keyN(i), now, rate, window)
			// Keep `hot` most-recently-used in its shard.
			tbl.charge(hot, now, rate, window)
		}
		if !tbl.has(hot) {
			t.Errorf("hot account evicted despite being most-recently-used")
		}
	})

	t.Run("concurrent charges complete without deadlock or races", func(t *testing.T) {
		tbl := newTable(maxSize, minSize)
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(base int) {
				defer wg.Done()
				for i := 0; i < 2000; i++ {
					tbl.charge(keyN(base*2000+i), now, rate, window)
				}
			}(g)
		}
		wg.Wait()
		if got := tbl.len(); got > maxSize {
			t.Errorf("after concurrent load: table len = %d, want <= %d", got, maxSize)
		}
	})
}
