package alias

import (
	"testing"

	"github.com/chenwei791129/ShadowDNS/internal/config"
)

func TestDetect(t *testing.T) {
	zones := []string{"root.com.", "backup.com."}
	aliases := config.AliasMap{
		"backup.com.": "root.com.",
	}

	tests := []struct {
		name        string
		qname       string
		loadedZones []string
		aliases     config.AliasMap
		want        Match
	}{
		{
			name:        "backup subdomain query matched as backup",
			qname:       "www.backup.com.",
			loadedZones: zones,
			aliases:     aliases,
			want:        Match{MatchedZone: "backup.com.", IsBackup: true, RootZone: "root.com."},
		},
		{
			name:        "root subdomain query matched as root",
			qname:       "www.root.com.",
			loadedZones: zones,
			aliases:     aliases,
			want:        Match{MatchedZone: "root.com.", IsBackup: false, RootZone: "root.com."},
		},
		{
			name:        "unknown query matches no zone",
			qname:       "www.unknown.com.",
			loadedZones: zones,
			aliases:     aliases,
			want:        Match{},
		},
		{
			name:        "apex backup query matched as backup",
			qname:       "backup.com.",
			loadedZones: zones,
			aliases:     aliases,
			want:        Match{MatchedZone: "backup.com.", IsBackup: true, RootZone: "root.com."},
		},
		{
			name:        "longest suffix wins over shorter suffix",
			qname:       "x.b.a.com.",
			loadedZones: []string{"a.com.", "b.a.com."},
			aliases:     config.AliasMap{},
			want:        Match{MatchedZone: "b.a.com.", IsBackup: false, RootZone: "b.a.com."},
		},
		{
			name:        "empty qname returns no match",
			qname:       "",
			loadedZones: zones,
			aliases:     aliases,
			want:        Match{},
		},
		{
			name:        "empty loaded zones returns no match",
			qname:       "www.backup.com.",
			loadedZones: []string{},
			aliases:     aliases,
			want:        Match{},
		},
		{
			name:        "nil aliases treats all zones as root",
			qname:       "www.root.com.",
			loadedZones: zones,
			aliases:     nil,
			want:        Match{MatchedZone: "root.com.", IsBackup: false, RootZone: "root.com."},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.qname, tc.loadedZones, tc.aliases)
			if got != tc.want {
				t.Errorf("Detect(%q, %v, %v) = %+v, want %+v",
					tc.qname, tc.loadedZones, tc.aliases, got, tc.want)
			}
		})
	}
}
