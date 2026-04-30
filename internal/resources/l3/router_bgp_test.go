package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRouterBgp_TopLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RouterBgpArgs
		want error
	}{
		{"asn_min", RouterBgpArgs{Asn: 1}, nil},
		{"asn_max", RouterBgpArgs{Asn: 4294967295}, nil},
		{"asn_zero", RouterBgpArgs{Asn: 0}, ErrBgpAsnInvalid},
		{"asn_negative", RouterBgpArgs{Asn: -1}, ErrBgpAsnInvalid},
		{"asn_overflow", RouterBgpArgs{Asn: 4294967296}, ErrBgpAsnInvalid},
		{
			name: "router_id_ok",
			args: RouterBgpArgs{Asn: 65000, RouterId: new("1.1.1.1")},
		},
		{
			name: "router_id_v6",
			args: RouterBgpArgs{Asn: 65000, RouterId: new("fc00::1")},
			want: ErrBgpRouterIdBadIPv4,
		},
		{
			name: "max_paths_zero",
			args: RouterBgpArgs{Asn: 65000, MaximumPaths: &BgpMaximumPaths{Paths: 0}},
			want: ErrBgpMaximumPathsInvalid,
		},
		{
			name: "max_paths_ecmp_lower",
			args: RouterBgpArgs{Asn: 65000, MaximumPaths: &BgpMaximumPaths{Paths: 4, Ecmp: new(2)}},
			want: ErrBgpMaximumPathsInvalid,
		},
		{
			name: "max_paths_ok",
			args: RouterBgpArgs{Asn: 65000, MaximumPaths: &BgpMaximumPaths{Paths: 4, Ecmp: new(8)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRouterBgp(tc.args)
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

func TestValidateBgpPeerGroup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pg   BgpPeerGroup
		want error
	}{
		{"name_required", BgpPeerGroup{}, ErrBgpPeerGroupNameRequired},
		{"whitespace_name", BgpPeerGroup{Name: "  \t"}, ErrBgpPeerGroupNameRequired},
		{"send_community_invalid", BgpPeerGroup{Name: "X", SendCommunity: new("everything")}, ErrBgpPeerGroupSendCommunity},
		{"send_community_extended", BgpPeerGroup{Name: "X", SendCommunity: new("extended")}, nil},
		{"max_routes_negative", BgpPeerGroup{Name: "X", MaximumRoutes: new(-1)}, ErrBgpPeerGroupMaximumRoutes},
		{"max_routes_zero", BgpPeerGroup{Name: "X", MaximumRoutes: new(0)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pg := tc.pg
			err := validateBgpPeerGroup(&pg)
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

func TestValidateBgpNeighbor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		nbr  BgpNeighbor
		want error
	}{
		{"ip_pg", BgpNeighbor{Address: "1.1.1.1", PeerGroup: new("SPINE")}, nil},
		{"ip_remote_as", BgpNeighbor{Address: "1.1.1.1", RemoteAs: new(65000)}, nil},
		{"bad_ip", BgpNeighbor{Address: "not.an.ip", PeerGroup: new("X")}, ErrBgpNeighborAddressBadIPv4},
		{"v6_ip", BgpNeighbor{Address: "fc00::1", PeerGroup: new("X")}, ErrBgpNeighborAddressBadIPv4},
		{"both_pg_and_remote_as", BgpNeighbor{Address: "1.1.1.1", PeerGroup: new("X"), RemoteAs: new(65000)}, ErrBgpNeighborPeerGroupRemoteAS},
		{"neither_pg_nor_remote_as", BgpNeighbor{Address: "1.1.1.1"}, ErrBgpNeighborPeerGroupRemoteAS},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := tc.nbr
			err := validateBgpNeighbor(&n)
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

func TestValidateBgpVrf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vrf  BgpVrf
		want error
	}{
		{"ok", BgpVrf{Name: "RED", Rd: new("1.1.1.1:1"), Redistribute: []string{"connected"}}, nil},
		{"empty_name", BgpVrf{}, ErrBgpVrfNameRequired},
		{"reserved_default", BgpVrf{Name: "default"}, ErrBgpVrfNameReserved},
		{"bad_redistribute", BgpVrf{Name: "RED", Redistribute: []string{"bgp"}}, ErrBgpVrfRedistributeUnsupported},
		{"bad_router_id", BgpVrf{Name: "RED", RouterId: new("fc00::1")}, ErrBgpRouterIdBadIPv4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := tc.vrf
			err := validateBgpVrf(&v)
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

func TestBuildRouterBgpCmds_FullEvpnFabric(t *testing.T) {
	t.Parallel()
	args := RouterBgpArgs{
		Asn:                  65001,
		RouterId:             new("1.1.1.1"),
		NoDefaultIpv4Unicast: new(true),
		MaximumPaths:         &BgpMaximumPaths{Paths: 4, Ecmp: new(4)},
		Bfd:                  new(true),
		PeerGroups: []BgpPeerGroup{
			{
				Name:          "SPINE",
				RemoteAs:      new(65000),
				UpdateSource:  new("Loopback0"),
				EbgpMultihop:  new(3),
				SendCommunity: new("extended"),
				MaximumRoutes: new(12000),
				Bfd:           new(true),
				Description:   new("EVPN-RR"),
			},
		},
		Neighbors: []BgpNeighbor{
			{Address: "10.0.0.1", PeerGroup: new("SPINE")},
			{Address: "10.0.0.2", PeerGroup: new("SPINE")},
		},
		AddressFamilies: []BgpAddressFamily{
			{Name: "evpn", Activate: []string{"SPINE"}},
			{Name: "ipv4", Deactivate: []string{"SPINE"}},
		},
		Vrfs: []BgpVrf{
			{
				Name:                  "RED",
				Rd:                    new("1.1.1.1:10"),
				RouteTargetImportEvpn: new("64500:10"),
				RouteTargetExportEvpn: new("64500:10"),
				RouterId:              new("1.1.1.1"),
				Redistribute:          []string{"connected"},
			},
		},
	}
	got := buildRouterBgpCmds(args, false)
	want := []string{
		"router bgp 65001",
		"router-id 1.1.1.1",
		"no bgp default ipv4-unicast",
		"maximum-paths 4 ecmp 4",
		"bfd",
		"neighbor SPINE peer group",
		"neighbor SPINE remote-as 65000",
		"neighbor SPINE update-source Loopback0",
		"neighbor SPINE ebgp-multihop 3",
		"neighbor SPINE send-community extended",
		"neighbor SPINE maximum-routes 12000",
		"neighbor SPINE bfd",
		"neighbor SPINE description EVPN-RR",
		"neighbor 10.0.0.1 peer group SPINE",
		"neighbor 10.0.0.2 peer group SPINE",
		"address-family evpn",
		"neighbor SPINE activate",
		"exit",
		"address-family ipv4",
		"no neighbor SPINE activate",
		"exit",
		"vrf RED",
		"rd 1.1.1.1:10",
		"route-target import evpn 64500:10",
		"route-target export evpn 64500:10",
		"router-id 1.1.1.1",
		"redistribute connected",
		"exit",
		"exit",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d\ngot:\n%s\nwant:\n%s",
			len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildRouterBgpCmds_Minimal(t *testing.T) {
	t.Parallel()
	got := buildRouterBgpCmds(RouterBgpArgs{Asn: 65000}, false)
	want := []string{"router bgp 65000", "exit"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildRouterBgpCmds_Delete(t *testing.T) {
	t.Parallel()
	got := buildRouterBgpCmds(RouterBgpArgs{Asn: 65000}, true)
	want := []string{"no router bgp 65000"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRouterBgpID(t *testing.T) {
	t.Parallel()
	if got := routerBgpID(65000); got != "router-bgp/65000" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRouterBgpSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router bgp 65001",
		"   router-id 1.1.1.1",
		"   no bgp default ipv4-unicast",
		"   maximum-paths 4 ecmp 4",
		"   bfd",
		"   neighbor SPINE peer group",
		"   address-family evpn",
		"      neighbor SPINE activate",
	}, "\n")
	row := parseRouterBgpSection(out)
	if row.Asn != 65001 {
		t.Fatalf("asn = %d", row.Asn)
	}
	if row.RouterId != "1.1.1.1" {
		t.Fatalf("router-id = %q", row.RouterId)
	}
	if !row.NoDefaultIpv4Unicast {
		t.Fatal("expected NoDefaultIpv4Unicast=true")
	}
	if row.MaximumPaths == nil || row.MaximumPaths.Paths != 4 || row.MaximumPaths.Ecmp == nil || *row.MaximumPaths.Ecmp != 4 {
		t.Fatalf("maximum-paths = %+v", row.MaximumPaths)
	}
	if !row.Bfd {
		t.Fatal("expected Bfd=true")
	}
}

func TestParseBgpMaximumPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		paths int
		ecmp  *int
	}{
		{"4 ecmp 8", 4, new(8)},
		{"4", 4, nil},
		{"", 0, nil},
		{"garbage", 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := parseBgpMaximumPaths(tc.in)
			if tc.paths == 0 {
				if got != nil {
					t.Fatalf("expected nil for %q, got %+v", tc.in, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("nil for %q", tc.in)
			}
			if got.Paths != tc.paths {
				t.Fatalf("paths = %d, want %d", got.Paths, tc.paths)
			}
			if (got.Ecmp == nil) != (tc.ecmp == nil) {
				t.Fatalf("ecmp = %v, want %v", got.Ecmp, tc.ecmp)
			}
			if got.Ecmp != nil && *got.Ecmp != *tc.ecmp {
				t.Fatalf("ecmp val = %d, want %d", *got.Ecmp, *tc.ecmp)
			}
		})
	}
}
