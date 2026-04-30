package l3

import (
	"errors"
	"testing"
)

func TestValidateStaticRoute_Prefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		prefix string
		want   error
	}{
		{"ok_v4_24", "10.0.0.0/24", nil},
		{"ok_v4_32", "10.0.0.1/32", nil},
		{"missing_mask", "10.0.0.0", ErrStaticRouteBadPrefix},
		{"v6", "fc00::/64", ErrStaticRouteBadPrefix},
		{"garbage", "not.an.ip/24", ErrStaticRouteBadPrefix},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStaticRoute(StaticRouteArgs{Prefix: tc.prefix, NextHop: "192.0.2.1"})
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

func TestValidateStaticRoute_NextHop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		nh   string
		want error
	}{
		{"ipv4", "192.0.2.1", nil},
		{"null0", "Null0", nil},
		{"ethernet", "Ethernet1", nil},
		{"ethernet_breakout", "Ethernet11/1", nil},
		{"ethernet_sub", "Ethernet1.4011", nil},
		{"loopback", "Loopback0", nil},
		{"port_channel", "Port-Channel10", nil},
		{"vlan", "Vlan100", nil},
		{"vxlan", "Vxlan1", nil},
		{"management", "Management1", nil},
		{"empty", "", ErrStaticRouteBadNextHop},
		{"ipv6", "fc00::1", ErrStaticRouteBadNextHop},
		{"hostname", "router1.example.net", ErrStaticRouteBadNextHop},
		{"lowercase_eth", "ethernet1", ErrStaticRouteBadNextHop},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStaticRoute(StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: tc.nh})
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

func TestValidateStaticRoute_KnobRanges(t *testing.T) {
	t.Parallel()
	base := StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1"}
	tests := []struct {
		name string
		mut  func(StaticRouteArgs) StaticRouteArgs
		want error
	}{
		{"distance_min", func(a StaticRouteArgs) StaticRouteArgs { a.Distance = new(1); return a }, nil},
		{"distance_max", func(a StaticRouteArgs) StaticRouteArgs { a.Distance = new(255); return a }, nil},
		{"distance_zero", func(a StaticRouteArgs) StaticRouteArgs { a.Distance = new(0); return a }, ErrStaticRouteBadDistance},
		{"distance_high", func(a StaticRouteArgs) StaticRouteArgs { a.Distance = new(256); return a }, ErrStaticRouteBadDistance},
		{"tag_zero", func(a StaticRouteArgs) StaticRouteArgs { a.Tag = new(0); return a }, nil},
		{"tag_max", func(a StaticRouteArgs) StaticRouteArgs { a.Tag = new(4294967295); return a }, nil},
		{"tag_negative", func(a StaticRouteArgs) StaticRouteArgs { a.Tag = new(-1); return a }, ErrStaticRouteBadTag},
		{"metric_ok", func(a StaticRouteArgs) StaticRouteArgs { a.Metric = new(99); return a }, nil},
		{"metric_zero", func(a StaticRouteArgs) StaticRouteArgs { a.Metric = new(0); return a }, ErrStaticRouteBadMetric},
		{"name_ok", func(a StaticRouteArgs) StaticRouteArgs { a.Name = new("uplink"); return a }, nil},
		{"name_empty", func(a StaticRouteArgs) StaticRouteArgs { a.Name = new(""); return a }, ErrStaticRouteEmptyText},
		{"name_with_space", func(a StaticRouteArgs) StaticRouteArgs { a.Name = new("up link"); return a }, ErrStaticRouteSpacesInText},
		{"track_ok", func(a StaticRouteArgs) StaticRouteArgs { a.Track = new("uplink-track"); return a }, nil},
		{"track_empty", func(a StaticRouteArgs) StaticRouteArgs { a.Track = new(""); return a }, ErrStaticRouteEmptyText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStaticRoute(tc.mut(base))
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

func TestBuildStaticRouteCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   StaticRouteArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1"},
			want: []string{"ip route 10.0.0.0/24 192.0.2.1"},
		},
		{
			name: "minimal_with_distance",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1", Distance: new(200)},
			want: []string{"ip route 10.0.0.0/24 192.0.2.1 200"},
		},
		{
			name: "default_distance_omitted",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1", Distance: new(1)},
			want: []string{"ip route 10.0.0.0/24 192.0.2.1"},
		},
		{
			name: "vrf_with_full_knobs",
			args: StaticRouteArgs{
				Prefix:   "10.99.0.0/24",
				NextHop:  "192.0.2.1",
				Vrf:      new("OVERLAY"),
				Distance: new(200),
				Tag:      new(42),
				Name:     new("pulumi-it"),
				Metric:   new(99),
				Track:    new("uplink-track"),
			},
			want: []string{"ip route vrf OVERLAY 10.99.0.0/24 192.0.2.1 200 tag 42 name pulumi-it metric 99 track uplink-track"},
		},
		{
			name: "iface_nexthop",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "Null0"},
			want: []string{"ip route 10.0.0.0/24 Null0"},
		},
		{
			name:   "delete_minimal",
			args:   StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1"},
			remove: true,
			want:   []string{"no ip route 10.0.0.0/24 192.0.2.1"},
		},
		{
			name:   "delete_with_distance",
			args:   StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1", Distance: new(200)},
			remove: true,
			want:   []string{"no ip route 10.0.0.0/24 192.0.2.1 200"},
		},
		{
			name:   "delete_vrf",
			args:   StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1", Vrf: new("OVERLAY")},
			remove: true,
			want:   []string{"no ip route vrf OVERLAY 10.0.0.0/24 192.0.2.1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildStaticRouteCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestStaticRouteID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args StaticRouteArgs
		want string
	}{
		{
			name: "default_vrf_default_distance",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1"},
			want: "route/default/10.0.0.0/24/192.0.2.1/1",
		},
		{
			name: "vrf_explicit_distance",
			args: StaticRouteArgs{Prefix: "10.0.0.0/24", NextHop: "192.0.2.1", Vrf: new("OVERLAY"), Distance: new(200)},
			want: "route/OVERLAY/10.0.0.0/24/192.0.2.1/200",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := staticRouteID(tc.args); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseStaticRouteLines(t *testing.T) {
	t.Parallel()
	out := "ip route 10.99.0.0/24 192.0.2.1 200 tag 42 name pulumi-it metric 99\nip route vrf OVERLAY 10.100.0.0/24 192.0.2.1 track uplink\n"

	row, found := parseStaticRouteLines(out, StaticRouteArgs{Prefix: "10.99.0.0/24", NextHop: "192.0.2.1"})
	if !found {
		t.Fatal("expected to find first row")
	}
	if row.Distance != 200 || row.Tag != 42 || row.Name != "pulumi-it" || row.Metric != 99 {
		t.Fatalf("first row mismatch: %+v", row)
	}

	row2, found2 := parseStaticRouteLines(out, StaticRouteArgs{Prefix: "10.100.0.0/24", NextHop: "192.0.2.1", Vrf: new("OVERLAY")})
	if !found2 {
		t.Fatal("expected to find second (vrf) row")
	}
	if row2.Distance != 1 || row2.Track != "uplink" {
		t.Fatalf("second row mismatch: %+v", row2)
	}

	if _, found := parseStaticRouteLines(out, StaticRouteArgs{Prefix: "10.99.0.0/24", NextHop: "192.0.2.2"}); found {
		t.Fatal("must not match wrong next-hop")
	}
}

func TestStaticRouteSessionToken(t *testing.T) {
	t.Parallel()
	tok := staticRouteSessionToken(StaticRouteArgs{
		Prefix:   "10.99.0.0/24",
		NextHop:  "192.0.2.1",
		Vrf:      new("OVERLAY"),
		Distance: new(200),
	})
	want := "OVERLAY-10-99-0-0-24-192-0-2-1-200"
	if tok != want {
		t.Fatalf("got %q, want %q", tok, want)
	}
}
