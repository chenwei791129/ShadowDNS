package server

import (
	"net"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// AddrSetEqual
// ---------------------------------------------------------------------------

func TestAddrSetEqual_EqualSets(t *testing.T) {
	a := []string{"10.0.0.1:53", "127.0.0.1:53"}
	b := []string{"127.0.0.1:53", "10.0.0.1:53"} // different order, same elements
	if !AddrSetEqual(a, b) {
		t.Errorf("expected equal, got not equal: a=%v b=%v", a, b)
	}
}

func TestAddrSetEqual_EmptySets(t *testing.T) {
	if !AddrSetEqual(nil, nil) {
		t.Error("nil == nil should be true")
	}
	if !AddrSetEqual([]string{}, []string{}) {
		t.Error("empty == empty should be true")
	}
	if !AddrSetEqual(nil, []string{}) {
		t.Error("nil == empty should be true")
	}
}

func TestAddrSetEqual_DifferentLengths(t *testing.T) {
	if AddrSetEqual([]string{"a"}, []string{"a", "b"}) {
		t.Error("different length sets must not be equal")
	}
	if AddrSetEqual([]string{}, []string{"a"}) {
		t.Error("empty vs non-empty must not be equal")
	}
}

func TestAddrSetEqual_DifferentElements(t *testing.T) {
	if AddrSetEqual([]string{"10.0.0.1:53"}, []string{"10.0.0.2:53"}) {
		t.Error("distinct addresses must not be equal")
	}
	// Same length but one element different.
	if AddrSetEqual(
		[]string{"10.0.0.1:53", "127.0.0.1:53"},
		[]string{"10.0.0.1:53", "127.0.0.2:53"},
	) {
		t.Error("sets differing by one element must not be equal")
	}
}

func TestAddrSetEqual_DuplicatesAsMultiset(t *testing.T) {
	// Duplicates are treated as multiset entries; ["a","a"] != ["a"].
	// This behavior is documented in the godoc so future callers know
	// not to pre-dedupe unless they want that semantics.
	if AddrSetEqual([]string{"a", "a"}, []string{"a"}) {
		t.Error("[a,a] vs [a] must not be equal (different multisets)")
	}
	if !AddrSetEqual([]string{"a", "a"}, []string{"a", "a"}) {
		t.Error("[a,a] vs [a,a] must be equal")
	}
}

// ---------------------------------------------------------------------------
// BoundAddrStrings
// ---------------------------------------------------------------------------

func TestBoundAddrStrings_ReturnsRequestedForms(t *testing.T) {
	srv := newBareServer(t)
	// Use two ephemeral ports; BoundAddrStrings must return the requested
	// ":0" form, not the OS-assigned port. This is the contract callers
	// (reload drift detection) depend on.
	if err := srv.BindMany([]string{"127.0.0.1:0", "127.0.0.1:0"}); err != nil {
		t.Fatalf("BindMany: %v", err)
	}
	defer srv.shutdownListeners()

	got := srv.BoundAddrStrings()
	want := []string{"127.0.0.1:0", "127.0.0.1:0"}
	if len(got) != len(want) {
		t.Fatalf("len got=%d want=%d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("BoundAddrStrings[%d]=%q, want %q (NOT OS-assigned port)", i, got[i], want[i])
		}
	}

	// Sanity: the actual bound addresses have non-zero ports.
	for _, a := range srv.UDPAddrs() {
		if strings.HasSuffix(a.String(), ":0") {
			t.Errorf("UDPAddrs returned placeholder port: %s", a.String())
		}
	}
}

func TestBoundAddrStrings_EmptyWhenNothingBound(t *testing.T) {
	srv := newBareServer(t)
	got := srv.BoundAddrStrings()
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// BindMany socket-leak regression: calling BindMany twice must close the
// first batch before overwriting. Verified by attempting to re-bind one of
// the original addresses — if the first set leaked, the rebind would fail
// with EADDRINUSE.
// ---------------------------------------------------------------------------

func TestBindMany_SecondCallClosesFirstBatch(t *testing.T) {
	srv := newBareServer(t)

	// First bind: pick an ephemeral port and remember it.
	if err := srv.BindMany([]string{"127.0.0.1:0"}); err != nil {
		t.Fatalf("first BindMany: %v", err)
	}
	firstAddr := srv.UDPAddr().String()

	// Second bind on a different ephemeral port. If the first socket
	// leaked (was never closed), it would still hold firstAddr and we
	// could not re-bind to it below.
	if err := srv.BindMany([]string{"127.0.0.1:0"}); err != nil {
		t.Fatalf("second BindMany: %v", err)
	}
	defer srv.shutdownListeners()

	// Try to bind a third socket on the first-batch address. If the
	// first batch was properly closed by the second BindMany, this
	// succeeds. If it leaked, this fails with EADDRINUSE.
	pc, err := net.ListenPacket("udp", firstAddr)
	if err != nil {
		t.Fatalf("first-batch socket leaked — could not reclaim %s: %v", firstAddr, err)
	}
	_ = pc.Close()
}

func TestBind_CalledTwiceDoesNotLeakFirstSocket(t *testing.T) {
	// The legacy Bind(addr) shim delegates to BindMany([]string{addr}).
	// Verify it benefits from the same close-before-replace protection.
	srv := newBareServer(t)

	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("first Bind: %v", err)
	}
	firstAddr := srv.UDPAddr().String()

	if err := srv.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("second Bind: %v", err)
	}
	defer srv.shutdownListeners()

	pc, err := net.ListenPacket("udp", firstAddr)
	if err != nil {
		t.Fatalf("first Bind socket leaked — could not reclaim %s: %v", firstAddr, err)
	}
	_ = pc.Close()
}
