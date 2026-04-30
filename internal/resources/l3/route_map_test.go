package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRouteMap_TopLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RouteMapArgs
		want error
	}{
		{
			name: "ok",
			args: RouteMapArgs{Name: "M", Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}},
		},
		{"empty_name", RouteMapArgs{Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}}, ErrRouteMapNameRequired},
		{"empty_entries", RouteMapArgs{Name: "M"}, ErrRouteMapEntriesEmpty},
		{
			name: "duplicate_seq",
			args: RouteMapArgs{
				Name: "M",
				Entries: []RouteMapEntry{
					{Seq: 10, Action: "permit"},
					{Seq: 10, Action: "deny"},
				},
			},
			want: ErrRouteMapSeqDuplicate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRouteMap(tc.args)
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

func TestValidateRouteMapEntry(t *testing.T) {
	t.Parallel()
	base := RouteMapEntry{Seq: 10, Action: "permit"}
	tests := []struct {
		name string
		mut  func(RouteMapEntry) RouteMapEntry
		want error
	}{
		{"seq_negative", func(e RouteMapEntry) RouteMapEntry { e.Seq = -1; return e }, ErrRouteMapSeqOutOfRange},
		{"seq_high", func(e RouteMapEntry) RouteMapEntry { e.Seq = 65536; return e }, ErrRouteMapSeqOutOfRange},
		{"action_garbage", func(e RouteMapEntry) RouteMapEntry { e.Action = "log"; return e }, ErrRouteMapActionInvalid},
		{"continue_high", func(e RouteMapEntry) RouteMapEntry { e.Continue = new(70000); return e }, ErrRouteMapContinueOutRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := tc.mut(base)
			err := validateRouteMapEntry(&e)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateRouteMapMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		m    RouteMapMatch
		want error
	}{
		{"empty_ok", RouteMapMatch{}, nil},
		{"tag_high", RouteMapMatch{Tag: new(4294967296)}, ErrRouteMapTagOutOfRange},
		{"metric_neg", RouteMapMatch{Metric: new(-1)}, ErrRouteMapMetricMatchRange},
		{"localpref_neg", RouteMapMatch{LocalPreference: new(-1)}, ErrRouteMapLocalPrefRange},
		{"origin_garbage", RouteMapMatch{Origin: new("loopback")}, ErrRouteMapOriginInvalid},
		{"origin_igp", RouteMapMatch{Origin: new("igp")}, nil},
		{"sourceproto_garbage", RouteMapMatch{SourceProtocol: new("eigrp")}, ErrRouteMapSourceProtocol},
		{"sourceproto_bgp", RouteMapMatch{SourceProtocol: new("bgp")}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.m
			err := validateRouteMapMatch(&m)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateRouteMapSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    RouteMapSet
		want error
	}{
		{"empty_ok", RouteMapSet{}, nil},
		{
			name: "additive_and_none",
			s:    RouteMapSet{CommunityAdditive: new(true), CommunityNone: new(true)},
			want: ErrRouteMapCommunityCombo,
		},
		{"localpref_neg", RouteMapSet{LocalPreference: new(-1)}, ErrRouteMapLocalPrefRange},
		{"asn_zero", RouteMapSet{AsPathPrepend: []int{0}}, ErrRouteMapAsnInvalid},
		{"asn_high", RouteMapSet{AsPathPrepend: []int{4294967296}}, ErrRouteMapAsnInvalid},
		{"nexthop_v6", RouteMapSet{IpNextHop: new("fc00::1")}, ErrRouteMapNextHopInvalid},
		{"nexthop_unchanged", RouteMapSet{IpNextHop: new("unchanged")}, nil},
		{"nexthop_self", RouteMapSet{IpNextHop: new("self")}, nil},
		{"nexthop_v4", RouteMapSet{IpNextHop: new("192.0.2.1")}, nil},
		{"metric_garbage", RouteMapSet{Metric: new("abc")}, ErrRouteMapMetricFormat},
		{"metric_plus", RouteMapSet{Metric: new("+10")}, nil},
		{"metric_minus", RouteMapSet{Metric: new("-5")}, nil},
		{"metric_plain", RouteMapSet{Metric: new("100")}, nil},
		{"origin_garbage", RouteMapSet{Origin: new("loopback")}, ErrRouteMapOriginInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := tc.s
			err := validateRouteMapSet(&s)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildRouteMapCmds_Full(t *testing.T) {
	t.Parallel()
	args := RouteMapArgs{
		Name: "RM",
		Entries: []RouteMapEntry{
			// Out of order on input — render must sort by Seq.
			{Seq: 30, Action: "permit", Continue: new(50)},
			{
				Seq:    10,
				Action: "permit",
				Match: &RouteMapMatch{
					IpAddressPrefixList: []string{"PFX1"},
					Community:           []string{"CL1", "CL2"},
					Tag:                 new(7),
					Metric:              new(100),
					LocalPreference:     new(200),
					Origin:              new("igp"),
					SourceProtocol:      new("bgp"),
				},
				Set: &RouteMapSet{
					Community:         []string{"65000:100"},
					CommunityAdditive: new(true),
					AsPathPrepend:     []int{65000, 65001},
					IpNextHop:         new("192.0.2.1"),
					LocalPreference:   new(300),
					Metric:            new("+10"),
					Origin:            new("egp"),
					Tag:               new(42),
				},
			},
			{Seq: 20, Action: "deny", Match: &RouteMapMatch{Tag: new(99)}},
		},
	}
	got := buildRouteMapCmds(args, false)
	want := []string{
		"no route-map RM",
		"route-map RM permit 10",
		"match ip address prefix-list PFX1",
		"match community CL1 CL2",
		"match local-preference 200",
		"match metric 100",
		"match origin igp",
		"match source-protocol bgp",
		"match tag 7",
		"set as-path prepend 65000 65001",
		"set community 65000:100 additive",
		"set ip next-hop 192.0.2.1",
		"set local-preference 300",
		"set metric +10",
		"set origin egp",
		"set tag 42",
		"exit",
		"route-map RM deny 20",
		"match tag 99",
		"exit",
		"route-map RM permit 30",
		"continue 50",
		"exit",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildRouteMapCmds_CommunityNone(t *testing.T) {
	t.Parallel()
	args := RouteMapArgs{
		Name: "RM",
		Entries: []RouteMapEntry{
			{Seq: 10, Action: "permit", Set: &RouteMapSet{
				Community:     []string{"65000:1"},
				CommunityNone: new(true),
			}},
		},
	}
	got := buildRouteMapCmds(args, false)
	// `community none` wins over the explicit list when both are set.
	want := []string{
		"no route-map RM",
		"route-map RM permit 10",
		"set community none",
		"exit",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildRouteMapCmds_Delete(t *testing.T) {
	t.Parallel()
	got := buildRouteMapCmds(RouteMapArgs{Name: "RM"}, true)
	if len(got) != 1 || got[0] != "no route-map RM" {
		t.Fatalf("got %v", got)
	}
}

func TestRouteMapID(t *testing.T) {
	t.Parallel()
	if got := routeMapID("RM"); got != "route-map/RM" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRouteMapSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"route-map RM permit 10",
		"   match ip address prefix-list PFX1",
		"   match community CL1 CL2",
		"   match metric 100",
		"   set local-preference 300",
		"   set community 65000:100 additive",
		"route-map RM deny 20",
		"   match tag 99",
		"route-map RM permit 30",
		"   continue 50",
	}, "\n")
	entries := parseRouteMapSection(out, "RM")
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Seq != 10 || entries[0].Action != "permit" {
		t.Fatalf("entry[0] header mismatch: %+v", entries[0])
	}
	if entries[0].Match == nil || len(entries[0].Match.IpAddressPrefixList) != 1 || entries[0].Match.IpAddressPrefixList[0] != "PFX1" {
		t.Fatalf("entry[0] prefix-list match mismatch: %+v", entries[0].Match)
	}
	if entries[0].Match.Metric == nil || *entries[0].Match.Metric != 100 {
		t.Fatalf("entry[0] metric mismatch: %+v", entries[0].Match)
	}
	if entries[0].Set == nil || entries[0].Set.LocalPreference == nil || *entries[0].Set.LocalPreference != 300 {
		t.Fatalf("entry[0] local-pref mismatch: %+v", entries[0].Set)
	}
	if len(entries[0].Set.Community) != 1 || entries[0].Set.Community[0] != "65000:100" {
		t.Fatalf("entry[0] community mismatch: %+v", entries[0].Set.Community)
	}
	if entries[0].Set.CommunityAdditive == nil || !*entries[0].Set.CommunityAdditive {
		t.Fatalf("entry[0] additive flag mismatch")
	}
	if entries[2].Continue == nil || *entries[2].Continue != 50 {
		t.Fatalf("entry[2] continue mismatch: %+v", entries[2])
	}
}

func TestValidRouteMapSetMetric(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"100", true},
		{"+10", true},
		{"-5", true},
		{"0", true},
		{"abc", false},
		{"+abc", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := validRouteMapSetMetric(tc.in); got != tc.ok {
				t.Fatalf("validRouteMapSetMetric(%q) = %v, want %v", tc.in, got, tc.ok)
			}
		})
	}
}

func TestValidRouteMapNextHop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"192.0.2.1", true},
		{"unchanged", true},
		{"self", true},
		{"fc00::1", false},
		{"hostname", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := validRouteMapNextHop(tc.in); got != tc.ok {
				t.Fatalf("validRouteMapNextHop(%q) = %v, want %v", tc.in, got, tc.ok)
			}
		})
	}
}
