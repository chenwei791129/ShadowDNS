package alias

import (
	"testing"

	"github.com/miekg/dns"
)

func TestBackupSOA_NumericFieldsInherited(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Serial != 2024050101 {
		t.Errorf("Serial: got %d, want 2024050101", got.Serial)
	}
	if got.Refresh != 3600 {
		t.Errorf("Refresh: got %d, want 3600", got.Refresh)
	}
	if got.Retry != 900 {
		t.Errorf("Retry: got %d, want 900", got.Retry)
	}
	if got.Expire != 604800 {
		t.Errorf("Expire: got %d, want 604800", got.Expire)
	}
	if got.Minttl != 300 {
		t.Errorf("Minttl: got %d, want 300", got.Minttl)
	}
}

func TestBackupSOA_OwnerNameIsBackup(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Hdr.Name != "backup.com." {
		t.Errorf("Owner: got %q, want backup.com.", got.Hdr.Name)
	}
}

func TestBackupSOA_MNAMERewritten(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Ns != "ns1.backup.com." {
		t.Errorf("MNAME: got %q, want ns1.backup.com.", got.Ns)
	}
}

func TestBackupSOA_RNAMERewritten(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Mbox != "admin.backup.com." {
		t.Errorf("RNAME: got %q, want admin.backup.com.", got.Mbox)
	}
}

func TestBackupSOA_ExternalMNAMEPreserved(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.externaldns.net.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Ns != "ns1.externaldns.net." {
		t.Errorf("External MNAME modified: got %q, want ns1.externaldns.net.", got.Ns)
	}
}

func TestBackupSOA_RootNotMutated(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	_ = BackupSOA(rootSOA, "root.com.", "backup.com.")

	if rootSOA.Hdr.Name != "root.com." {
		t.Errorf("rootSOA mutated: Name = %q", rootSOA.Hdr.Name)
	}
	if rootSOA.Ns != "ns1.root.com." {
		t.Errorf("rootSOA mutated: Ns = %q", rootSOA.Ns)
	}
}

func TestBackupSOA_RRTypePreserved(t *testing.T) {
	rootSOA := newSOA("root.com.", "ns1.root.com.", "admin.root.com.",
		2024050101, 3600, 900, 604800, 300)

	got := BackupSOA(rootSOA, "root.com.", "backup.com.")

	if got.Hdr.Rrtype != dns.TypeSOA {
		t.Errorf("Rrtype: got %d, want %d (TypeSOA)", got.Hdr.Rrtype, dns.TypeSOA)
	}
}
