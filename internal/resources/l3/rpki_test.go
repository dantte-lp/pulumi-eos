package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRpki(t *testing.T) {
	t.Parallel()
	base := RpkiArgs{Name: "C1", BgpAsn: 65000, CacheHost: "192.168.1.1"}
	tests := []struct {
		name string
		mut  func(RpkiArgs) RpkiArgs
		want error
	}{
		{"ok_minimal", func(a RpkiArgs) RpkiArgs { return a }, nil},
		{"empty_name", func(a RpkiArgs) RpkiArgs { a.Name = ""; return a }, ErrRpkiNameRequired},
		{"name_starts_digit", func(a RpkiArgs) RpkiArgs { a.Name = "1C"; return a }, ErrRpkiBadName},
		{"name_with_dot", func(a RpkiArgs) RpkiArgs { a.Name = "c.1"; return a }, ErrRpkiBadName},
		{"asn_zero", func(a RpkiArgs) RpkiArgs { a.BgpAsn = 0; return a }, ErrRpkiAsnInvalid},
		{"asn_overflow", func(a RpkiArgs) RpkiArgs { a.BgpAsn = 4294967296; return a }, ErrRpkiAsnInvalid},
		{"host_v6", func(a RpkiArgs) RpkiArgs { a.CacheHost = "fc00::1"; return a }, ErrRpkiCacheHostBadIPv4},
		{"host_garbage", func(a RpkiArgs) RpkiArgs { a.CacheHost = "not-an-ip"; return a }, ErrRpkiCacheHostBadIPv4},
		{"port_zero", func(a RpkiArgs) RpkiArgs { a.Port = new(0); return a }, ErrRpkiPortOutOfRange},
		{"port_high", func(a RpkiArgs) RpkiArgs { a.Port = new(65536); return a }, ErrRpkiPortOutOfRange},
		{"port_ok", func(a RpkiArgs) RpkiArgs { a.Port = new(3323); return a }, nil},
		{"preference_zero", func(a RpkiArgs) RpkiArgs { a.Preference = new(0); return a }, ErrRpkiPreferenceRange},
		{"preference_high", func(a RpkiArgs) RpkiArgs { a.Preference = new(11); return a }, ErrRpkiPreferenceRange},
		{"preference_ok", func(a RpkiArgs) RpkiArgs { a.Preference = new(4); return a }, nil},
		{"refresh_zero", func(a RpkiArgs) RpkiArgs { a.RefreshInterval = new(0); return a }, ErrRpkiIntervalNonPositive},
		{"retry_neg", func(a RpkiArgs) RpkiArgs { a.RetryInterval = new(-1); return a }, ErrRpkiIntervalNonPositive},
		{"expire_ok", func(a RpkiArgs) RpkiArgs { a.ExpireInterval = new(600); return a }, nil},
		{"transport_tcp", func(a RpkiArgs) RpkiArgs { a.Transport = new("tcp"); return a }, nil},
		{"transport_ssh", func(a RpkiArgs) RpkiArgs { a.Transport = new("ssh"); return a }, nil},
		{"transport_bad", func(a RpkiArgs) RpkiArgs { a.Transport = new("quic"); return a }, ErrRpkiTransportInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRpki(tc.mut(base))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildRpkiCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   RpkiArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: RpkiArgs{Name: "C1", BgpAsn: 65000, CacheHost: "192.168.1.1"},
			want: []string{
				"router bgp 65000",
				"rpki cache C1",
				"host 192.168.1.1",
				"transport tcp",
				"exit",
				"exit",
			},
		},
		{
			name: "full",
			args: RpkiArgs{
				Name:            "ROUTINATOR_1",
				BgpAsn:          65103,
				CacheHost:       "192.168.0.227",
				Vrf:             new("mgmt"),
				Port:            new(3323),
				Preference:      new(4),
				RefreshInterval: new(30),
				RetryInterval:   new(10),
				ExpireInterval:  new(600),
				LocalInterface:  new("Management1"),
				Transport:       new("tcp"),
			},
			want: []string{
				"router bgp 65103",
				"rpki cache ROUTINATOR_1",
				"host 192.168.0.227 vrf mgmt port 3323",
				"preference 4",
				"refresh-interval 30",
				"retry-interval 10",
				"expire-interval 600",
				"local-interface Management1",
				"transport tcp",
				"exit",
				"exit",
			},
		},
		{
			name: "ssh_transport",
			args: RpkiArgs{Name: "C2", BgpAsn: 65000, CacheHost: "10.0.0.1", Transport: new("ssh")},
			want: []string{
				"router bgp 65000",
				"rpki cache C2",
				"host 10.0.0.1",
				"transport ssh",
				"exit",
				"exit",
			},
		},
		{
			name:   "delete",
			args:   RpkiArgs{Name: "C1", BgpAsn: 65000},
			remove: true,
			want: []string{
				"router bgp 65000",
				"no rpki cache C1",
				"exit",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildRpkiCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRpkiID(t *testing.T) {
	t.Parallel()
	if got := rpkiID(65000, "C1"); got != "rpki/65000/C1" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRpkiSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router bgp 65103",
		"   rpki cache ROUTINATOR_1",
		"      host 192.168.0.227 vrf mgmt port 3323",
		"      preference 4",
		"      refresh-interval 30",
		"      retry-interval 10",
		"      expire-interval 600",
		"      local-interface Management1",
		"      !",
		"      transport tcp",
		"   rpki cache ROUTINATOR_2",
		"      host 192.168.0.126 vrf mgmt port 3323",
		"      preference 6",
		"      transport tcp",
		"router bgp 65999",
		"   rpki cache OTHER",
		"      host 10.0.0.1",
		"      transport tcp",
	}, "\n")

	r1, ok := parseRpkiSection(out, 65103, "ROUTINATOR_1")
	if !ok {
		t.Fatal("ROUTINATOR_1 not found")
	}
	if r1.CacheHost != "192.168.0.227" || r1.Vrf != "mgmt" || r1.Port != 3323 {
		t.Fatalf("R1 host fields: %+v", r1)
	}
	if r1.Preference != 4 || r1.RefreshInterval != 30 || r1.LocalInterface != "Management1" {
		t.Fatalf("R1 timer fields: %+v", r1)
	}

	r2, ok := parseRpkiSection(out, 65103, "ROUTINATOR_2")
	if !ok {
		t.Fatal("ROUTINATOR_2 not found")
	}
	if r2.Preference != 6 {
		t.Fatalf("R2 preference: %+v", r2)
	}

	other, ok := parseRpkiSection(out, 65999, "OTHER")
	if !ok || other.CacheHost != "10.0.0.1" {
		t.Fatalf("cross-ASN row: %+v ok=%v", other, ok)
	}

	// Wrong ASN must not match.
	if _, ok := parseRpkiSection(out, 65103, "OTHER"); ok {
		t.Fatal("must not cross-match on different ASN")
	}
	if _, ok := parseRpkiSection(out, 65103, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}
