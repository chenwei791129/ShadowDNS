package server

import (
	"bytes"
	"net"
	"slices"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/chenwei791129/ShadowDNS/internal/logging"
)

// newTestLogger returns a logger that writes console-formatted text to an
// in-memory buffer so tests can assert on log output.
func newTestLogger() (*zap.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	cfg := logging.BaseEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(cfg), zapcore.AddSync(buf), zapcore.DebugLevel)
	return zap.New(core), buf
}

// silentLogger returns a logger that discards output; for tests that do not
// care about log content.
func silentLogger() *zap.Logger {
	return zap.NewNop()
}

// withIfaceAddrs swaps the package-level ifaceAddrs for the duration of the
// test and restores it on cleanup.
func withIfaceAddrs(t *testing.T, addrs []net.Addr) {
	t.Helper()
	orig := ifaceAddrs
	ifaceAddrs = func() ([]net.Addr, error) { return addrs, nil }
	t.Cleanup(func() { ifaceAddrs = orig })
}

// mkIPNet constructs a *net.IPNet from CIDR form for test input.
func mkIPNet(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("bad CIDR %q: %v", cidr, err)
	}
	ip, _, _ := net.ParseCIDR(cidr)
	n.IP = ip
	return n
}

// ---------------------------------------------------------------------------
// ResolveListenAddresses
// ---------------------------------------------------------------------------

func TestResolveListenAddresses_OverrideBranch(t *testing.T) {
	// When -listen is not the default, it takes precedence over listen-on.
	got, err := ResolveListenAddresses("127.0.0.1:5353", []string{"any"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"127.0.0.1:5353"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_OverrideIgnoresListenOn(t *testing.T) {
	// Even when listen-on contains specific addresses, the override wins.
	got, err := ResolveListenAddresses("10.9.9.9:53", []string{"192.168.0.1", "192.168.0.2"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.9.9.9:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_ListenOnBranch(t *testing.T) {
	// Default -listen AND non-empty listen-on: use listen-on with port from -listen.
	got, err := ResolveListenAddresses(":53", []string{"10.0.0.1", "192.168.1.1"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53", "192.168.1.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_ListenOnInheritsPortFromListenFlag(t *testing.T) {
	// -listen ":5353" is still "default host with non-default port". We treat
	// only pure ":53" as default; anything else (including different port) is
	// an override hint. But the port component from -listen is still applied
	// to listen-on IPs — see design doc "Port 解析與預設 port".
	//
	// This test covers the case where -listen carries a non-default port but
	// no host, which means "use listen-on IPs at this port".
	got, err := ResolveListenAddresses(":5353", []string{"10.0.0.1"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:5353"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_FallbackAnyBranch(t *testing.T) {
	// Default -listen AND empty listen-on: implicit "any" → expand interfaces.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "127.0.0.1/8"),
	})
	got, err := ResolveListenAddresses(":53", nil, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Order from net.InterfaceAddrs is OS-dependent; sort for comparison.
	slices.Sort(got)
	want := []string{"10.0.0.1:53", "127.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_AnyTokenExpands(t *testing.T) {
	// Explicit "any" in listen-on: same as fallback expansion.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "127.0.0.53/8"),
	})
	got, err := ResolveListenAddresses(":53", []string{"any"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(got)
	want := []string{"10.0.0.1:53", "127.0.0.53:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_NoneTokenAlone_Fatal(t *testing.T) {
	// listen-on { none; }: explicitly empty → fatal error.
	_, err := ResolveListenAddresses(":53", []string{"none"}, nil, silentLogger())
	if err == nil {
		t.Fatal("expected error for listen-on { none; }, got nil")
	}
	if !strings.Contains(err.Error(), "no IPv4 listeners") {
		t.Errorf("error should mention 'no IPv4 listeners', got: %v", err)
	}
}

func TestResolveListenAddresses_NoneWithAny_NoneIsSkipped(t *testing.T) {
	// listen-on { any; none; }: "none" is an empty-set marker; mixed with any
	// it contributes nothing, so the result comes from "any". This matches
	// BIND semantics for mixed empty-set-plus-something lists.
	withIfaceAddrs(t, []net.Addr{mkIPNet(t, "10.0.0.1/24")})
	got, err := ResolveListenAddresses(":53", []string{"any", "none"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_UnsupportedTokenWarnedAndSkipped(t *testing.T) {
	logger, buf := newTestLogger()
	withIfaceAddrs(t, []net.Addr{mkIPNet(t, "10.0.0.1/24")})

	got, err := ResolveListenAddresses(":53", []string{"!10.0.0.99", "any"}, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "!addr" is dropped; "any" still expands.
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "!10.0.0.99") {
		t.Errorf("WARN log should mention the offending token, got: %s", logOutput)
	}
	if !strings.Contains(strings.ToLower(logOutput), "warn") {
		t.Errorf("should log at WARN level, got: %s", logOutput)
	}
}

func TestResolveListenAddresses_IPv6LiteralTokenSkipped(t *testing.T) {
	// Raw IPv6 literals in the IPv4 listen-on list remain unsupported: IPv6
	// addresses belong in listen-on-v6, so a literal here is skipped with WARN.
	logger, buf := newTestLogger()
	withIfaceAddrs(t, []net.Addr{mkIPNet(t, "10.0.0.1/24")})

	got, err := ResolveListenAddresses(":53", []string{"::1", "10.0.0.1"}, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if !strings.Contains(buf.String(), "::1") {
		t.Errorf("WARN log should mention the skipped IPv6 token, got: %s", buf.String())
	}
}

func TestResolveListenAddresses_Deduplicates(t *testing.T) {
	// Duplicates in listen-on (user typo) collapse to one entry.
	got, err := ResolveListenAddresses(":53", []string{"10.0.0.1", "10.0.0.1"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_AllTokensUnsupported_Fatal(t *testing.T) {
	// If every token is unsupported/skipped, result is empty → fatal, same as
	// listen-on { none; }. Better to fail loudly than silently listen nowhere.
	_, err := ResolveListenAddresses(":53", []string{"!10.0.0.1", "::1"}, nil, silentLogger())
	if err == nil {
		t.Fatal("expected error when all listen-on tokens are unsupported, got nil")
	}
}

func TestResolveListenAddresses_MalformedListenFlag(t *testing.T) {
	// Bad -listen format should be rejected early with a clear error.
	_, err := ResolveListenAddresses("not a host port", nil, nil, silentLogger())
	if err == nil {
		t.Fatal("expected error for malformed -listen, got nil")
	}
}

// ---------------------------------------------------------------------------
// ResolveListenAddresses — IPv6 (listen-on-v6)
// ---------------------------------------------------------------------------

func TestResolveListenAddresses_UnionV4AndV6Ordered(t *testing.T) {
	// :53 with both families declared: result is v4 set ++ v6 set, v4 first,
	// v6 in bracket form. Spec scenario "listen-on-v6 with explicit addresses".
	got, err := ResolveListenAddresses(":53", []string{"10.0.0.1"}, []string{"2001:db8::1"}, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53", "[2001:db8::1]:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_V6IPv4LiteralSkipped(t *testing.T) {
	// IPv4 literal in listen-on-v6 is unsupported: skipped with WARN, only the
	// IPv6 literal survives. Spec scenario "IPv4 literal in listen-on-v6".
	// listen-on { none; } isolates the v4 family so this test exercises only
	// the v6 token resolution.
	logger, buf := newTestLogger()
	got, err := ResolveListenAddresses(":53", []string{"none"}, []string{"10.0.0.1", "2001:db8::1"}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"[2001:db8::1]:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if !strings.Contains(buf.String(), "10.0.0.1") {
		t.Errorf("WARN log should mention the skipped IPv4 token, got: %s", buf.String())
	}
}

func TestResolveListenAddresses_V6OnlyNoneCoexistsWithV4(t *testing.T) {
	// listen-on { 10.0.0.1; } + listen-on-v6 { none; }: an empty v6 family does
	// not fail startup while v4 is non-empty. Spec scenario "listen-on-v6
	// { none; } yields IPv4-only behavior".
	got, err := ResolveListenAddresses(":53", []string{"10.0.0.1"}, []string{"none"}, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_V6AbsentYieldsIPv4Only(t *testing.T) {
	// listen-on-v6 absent (nil) is opt-in empty: it must NOT imply "any".
	got, err := ResolveListenAddresses(":53", []string{"10.0.0.1"}, nil, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_V6LiteralEscapeHatch(t *testing.T) {
	// --listen with an IPv6 bracket literal host overrides both named.conf
	// blocks. Spec scenario "--listen with IPv6 literal host binds only that".
	got, err := ResolveListenAddresses("[::1]:5353", []string{"any"}, []string{"any"}, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"[::1]:5353"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_AnyExpansionMixingBothFamilies(t *testing.T) {
	// Spec Example: any expansion mixing both families. Both link-locals
	// excluded, both loopbacks retained, v4 ordered before v6.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "169.254.1.1/16"),
		mkIPNet(t, "2001:db8::1/64"),
		mkIPNet(t, "fe80::1/64"),
		mkIPNet(t, "::1/128"),
	})
	got, err := ResolveListenAddresses(":53", []string{"any"}, []string{"any"}, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1:53", "[2001:db8::1]:53", "[::1]:53"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveListenAddresses_BothFamiliesEmpty_Fatal(t *testing.T) {
	// listen-on { none; } + listen-on-v6 { none; }: union is empty → fatal.
	_, err := ResolveListenAddresses(":53", []string{"none"}, []string{"none"}, silentLogger())
	if err == nil {
		t.Fatal("expected error when both families resolve empty, got nil")
	}
}

// ---------------------------------------------------------------------------
// expandAnyIPv4
// ---------------------------------------------------------------------------

func TestExpandAnyIPv4_FiltersIPv6(t *testing.T) {
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "fe80::1/64"),
		mkIPNet(t, "::1/128"),
	})
	got, err := expandAnyIPv4()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv4_FiltersLinkLocal(t *testing.T) {
	// 169.254.0.0/16 is link-local IPv4 — filtered per design.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "169.254.1.1/16"),
	})
	got, err := expandAnyIPv4()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv4_KeepsLoopbackAliases(t *testing.T) {
	// 127.0.0.53 etc. must be included — this is the core of our value prop.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "127.0.0.1/8"),
		mkIPNet(t, "127.0.0.53/8"),
		mkIPNet(t, "127.0.0.54/8"),
	})
	got, err := expandAnyIPv4()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(got)
	want := []string{"127.0.0.1", "127.0.0.53", "127.0.0.54"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv4_AcceptsIPAddrType(t *testing.T) {
	// net.InterfaceAddrs can also return *net.IPAddr (not always *net.IPNet).
	// Both should be handled.
	withIfaceAddrs(t, []net.Addr{
		&net.IPAddr{IP: net.ParseIP("10.0.0.2")},
		mkIPNet(t, "10.0.0.1/24"),
	})
	got, err := expandAnyIPv4()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(got)
	want := []string{"10.0.0.1", "10.0.0.2"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv4_EmptyResultIsNotAnError(t *testing.T) {
	// If a host has no IPv4 at all (e.g. IPv6-only container), expandAnyIPv4
	// returns an empty slice without erroring; the caller decides what that
	// means. This keeps the helper pure.
	withIfaceAddrs(t, []net.Addr{mkIPNet(t, "::1/128")})
	got, err := expandAnyIPv4()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// expandAnyIPv6
// ---------------------------------------------------------------------------

func TestExpandAnyIPv6_FiltersLinkLocalAndV4KeepsLoopback(t *testing.T) {
	// Mirror of expandAnyIPv4 filtering: IPv4 excluded, fe80::/10 excluded,
	// ::1 retained. Task 1.1 fixture: {2001:db8::1, fe80::1, ::1} → {2001:db8::1, ::1}.
	withIfaceAddrs(t, []net.Addr{
		mkIPNet(t, "10.0.0.1/24"),
		mkIPNet(t, "2001:db8::1/64"),
		mkIPNet(t, "fe80::1/64"),
		mkIPNet(t, "::1/128"),
	})
	got, err := expandAnyIPv6()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(got)
	want := []string{"2001:db8::1", "::1"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv6_AcceptsIPAddrType(t *testing.T) {
	// net.InterfaceAddrs can return *net.IPAddr as well as *net.IPNet.
	withIfaceAddrs(t, []net.Addr{
		&net.IPAddr{IP: net.ParseIP("2001:db8::2")},
		mkIPNet(t, "2001:db8::1/64"),
	})
	got, err := expandAnyIPv6()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(got)
	want := []string{"2001:db8::1", "2001:db8::2"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAnyIPv6_EmptyResultIsNotAnError(t *testing.T) {
	// IPv4-only host: expandAnyIPv6 returns an empty slice without erroring.
	withIfaceAddrs(t, []net.Addr{mkIPNet(t, "10.0.0.1/24")})
	got, err := expandAnyIPv6()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
