package transfer

import (
	"net/netip"
	"testing"
)

func TestNewACL_SingleIP(t *testing.T) {
	acl, err := NewACL([]string{"192.0.2.10"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must allow the exact IP.
	if !acl.Allows(netip.MustParseAddr("192.0.2.10")) {
		t.Error("expected 192.0.2.10 to be allowed")
	}
	// Must deny adjacent IP.
	if acl.Allows(netip.MustParseAddr("192.0.2.11")) {
		t.Error("expected 192.0.2.11 to be denied")
	}
}

func TestNewACL_CIDR(t *testing.T) {
	acl, err := NewACL([]string{"192.0.2.0/24"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must allow an address in the subnet.
	if !acl.Allows(netip.MustParseAddr("192.0.2.50")) {
		t.Error("expected 192.0.2.50 to be allowed")
	}
	// Must deny an address outside the subnet.
	if acl.Allows(netip.MustParseAddr("198.51.100.1")) {
		t.Error("expected 198.51.100.1 to be denied")
	}
}

func TestNewACL_Empty_DeniesAll(t *testing.T) {
	acl, err := NewACL(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty ACL must deny everything, including localhost.
	for _, ip := range []string{"127.0.0.1", "::1", "192.0.2.1", "10.0.0.1"} {
		if acl.Allows(netip.MustParseAddr(ip)) {
			t.Errorf("empty ACL unexpectedly allowed %s", ip)
		}
	}
}

func TestNewACL_Mixed(t *testing.T) {
	acl, err := NewACL([]string{"10.0.0.1", "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !acl.Allows(netip.MustParseAddr("10.0.0.1")) {
		t.Error("expected 10.0.0.1 to be allowed")
	}
	if !acl.Allows(netip.MustParseAddr("192.168.1.100")) {
		t.Error("expected 192.168.1.100 to be allowed")
	}
	if acl.Allows(netip.MustParseAddr("10.0.0.2")) {
		t.Error("expected 10.0.0.2 to be denied")
	}
}

func TestNewACL_Malformed(t *testing.T) {
	_, err := NewACL([]string{"not-an-ip"})
	if err == nil {
		t.Error("expected error for malformed entry")
	}
	// Error message must include the offending string.
	if err != nil && err.Error() == "" {
		t.Error("error message must not be empty")
	}
}

func TestACL_NilAllowsFalse(t *testing.T) {
	var a *ACL
	if a.Allows(netip.MustParseAddr("127.0.0.1")) {
		t.Error("nil ACL must deny all")
	}
}
