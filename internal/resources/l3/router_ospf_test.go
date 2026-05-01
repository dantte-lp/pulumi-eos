package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicaliseOspfArea(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		want  string
		valid bool
	}{
		{"0", "0.0.0.0", true},
		{"1", "0.0.0.1", true},
		{"256", "0.0.1.0", true},
		{"0.0.0.0", "0.0.0.0", true},
		{"10.10.10.10", "10.10.10.10", true},
		{"255.255.255.255", "255.255.255.255", true},
		{"", "", false},
		{"abc", "", false},
		{"1.2.3.300", "", false},
		{"1.2.3", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := canonicaliseOspfArea(tc.in)
			if ok != tc.valid {
				t.Fatalf("valid=%v want %v (got=%q)", ok, tc.valid, got)
			}
			if ok && got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateRouterOspf(t *testing.T) {
	t.Parallel()
	base := RouterOspfArgs{Instance: 1}
	tests := []struct {
		name string
		mut  func(RouterOspfArgs) RouterOspfArgs
		want error
	}{
		{"ok_minimal", func(a RouterOspfArgs) RouterOspfArgs { return a }, nil},
		{"instance_zero", func(a RouterOspfArgs) RouterOspfArgs { a.Instance = 0; return a }, ErrRouterOspfInstanceRange},
		{"instance_high", func(a RouterOspfArgs) RouterOspfArgs { a.Instance = 65536; return a }, ErrRouterOspfInstanceRange},
		{"router_id_bad", func(a RouterOspfArgs) RouterOspfArgs { a.RouterId = new("not-an-ip"); return a }, ErrRouterOspfRouterIDInvalid},
		{"router_id_ok", func(a RouterOspfArgs) RouterOspfArgs { a.RouterId = new("1.2.3.4"); return a }, nil},
		{"max_lsa_zero", func(a RouterOspfArgs) RouterOspfArgs { a.MaxLsa = new(0); return a }, ErrRouterOspfMaxLsaNonPositive},
		{"max_paths_high", func(a RouterOspfArgs) RouterOspfArgs { a.MaximumPaths = new(129); return a }, ErrRouterOspfMaximumPathsRange},
		{"max_paths_ok", func(a RouterOspfArgs) RouterOspfArgs { a.MaximumPaths = new(64); return a }, nil},
		{"ref_bw_zero", func(a RouterOspfArgs) RouterOspfArgs { a.AutoCostReferenceBandwidth = new(0); return a }, ErrRouterOspfRefBwNonPositive},
		{"network_bad_prefix", func(a RouterOspfArgs) RouterOspfArgs {
			a.Networks = []RouterOspfNetwork{{Prefix: "10.0.0.0", Area: "0"}}
			return a
		}, ErrRouterOspfPrefixInvalid},
		{"network_bad_area", func(a RouterOspfArgs) RouterOspfArgs {
			a.Networks = []RouterOspfNetwork{{Prefix: "10.0.0.0/24", Area: "abc"}}
			return a
		}, ErrRouterOspfAreaInvalid},
		{"network_ok", func(a RouterOspfArgs) RouterOspfArgs {
			a.Networks = []RouterOspfNetwork{{Prefix: "10.0.0.0/24", Area: "0"}}
			return a
		}, nil},
		{"area_type_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.Areas = []RouterOspfArea{{Id: "0", Type: new("totally-stubby-nssa")}}
			return a
		}, ErrRouterOspfAreaTypeInvalid},
		{"area_dflt_cost_high", func(a RouterOspfArgs) RouterOspfArgs {
			a.Areas = []RouterOspfArea{{Id: "0", DefaultCost: new(70000)}}
			return a
		}, ErrRouterOspfDefaultCostRange},
		{"area_range_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.Areas = []RouterOspfArea{{Id: "0", Ranges: []string{"10.0.0.0"}}}
			return a
		}, ErrRouterOspfSummaryPrefixInvld},
		{"area_nssa_metric_type_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.Areas = []RouterOspfArea{{Id: "0", NssaMetricType: new(3)}}
			return a
		}, ErrRouterOspfMetricTypeInvalid},
		{"redist_bad_src", func(a RouterOspfArgs) RouterOspfArgs {
			a.Redistribute = []RouterOspfRedistribute{{Source: "rip"}}
			return a
		}, ErrRouterOspfRedistSrcInvalid},
		{"redist_ok", func(a RouterOspfArgs) RouterOspfArgs {
			a.Redistribute = []RouterOspfRedistribute{{Source: "static", RouteMap: new("RM")}}
			return a
		}, nil},
		{"default_info_metric_type_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.DefaultInformationOriginate = &RouterOspfDefaultInfoOrigin{MetricType: new(3)}
			return a
		}, ErrRouterOspfMetricTypeInvalid},
		{"summary_address_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.SummaryAddresses = []string{"abc"}
			return a
		}, ErrRouterOspfSummaryPrefixInvld},
		{"distance_zero", func(a RouterOspfArgs) RouterOspfArgs {
			a.Distance = &RouterOspfDistance{IntraArea: new(0)}
			return a
		}, ErrRouterOspfDistanceRange},
		{"log_changes_bad", func(a RouterOspfArgs) RouterOspfArgs {
			a.LogAdjacencyChanges = new("verbose")
			return a
		}, ErrRouterOspfLogChangesInvalid},
		{"timer_neg", func(a RouterOspfArgs) RouterOspfArgs {
			a.Timers = &RouterOspfTimers{OutDelay: new(-1)}
			return a
		}, ErrRouterOspfTimerNonPositive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRouterOspf(tc.mut(base))
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

func TestBuildRouterOspfCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   RouterOspfArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: RouterOspfArgs{Instance: 1},
			want: []string{
				"no router ospf 1",
				"router ospf 1",
				"exit",
			},
		},
		{
			name: "vrf_with_router_id",
			args: RouterOspfArgs{
				Instance: 100,
				Vrf:      new("RED"),
				RouterId: new("10.0.0.1"),
			},
			want: []string{
				"no router ospf 100 vrf RED",
				"router ospf 100 vrf RED",
				"router-id 10.0.0.1",
				"exit",
			},
		},
		{
			name: "leaf_spine_typical",
			args: RouterOspfArgs{
				Instance:                1,
				RouterId:                new("1.1.1.1"),
				MaxLsa:                  new(12000),
				MaximumPaths:            new(64),
				PassiveInterfaceDefault: new(true),
				NoPassiveInterfaces:     []string{"Ethernet1", "Ethernet2"},
				Networks: []RouterOspfNetwork{
					{Prefix: "10.0.0.0/24", Area: "0"},
					{Prefix: "10.0.1.0/24", Area: "1"},
				},
				Areas: []RouterOspfArea{
					{Id: "1", Type: new("stub")},
				},
				Redistribute: []RouterOspfRedistribute{
					{Source: "connected"},
					{Source: "static", RouteMap: new("RM")},
				},
				LogAdjacencyChanges: new("detail"),
			},
			want: []string{
				"no router ospf 1",
				"router ospf 1",
				"router-id 1.1.1.1",
				"passive-interface default",
				"no passive-interface Ethernet1",
				"no passive-interface Ethernet2",
				"redistribute connected",
				"redistribute static route-map RM",
				"area 0.0.0.1 stub",
				"network 10.0.0.0/24 area 0.0.0.0",
				"network 10.0.1.0/24 area 0.0.0.1",
				"max-lsa 12000",
				"log-adjacency-changes detail",
				"maximum-paths 64",
				"exit",
			},
		},
		{
			name: "nssa_default_info_origin",
			args: RouterOspfArgs{
				Instance: 1,
				Areas: []RouterOspfArea{
					{
						Id:             "4",
						Type:           new("nssa-default-information-originate"),
						NssaMetric:     new(100),
						NssaMetricType: new(2),
						NssaOnly:       new(true),
					},
				},
			},
			want: []string{
				"no router ospf 1",
				"router ospf 1",
				"area 0.0.0.4 nssa default-information-originate metric 100 metric-type 2 nssa-only",
				"exit",
			},
		},
		{
			name: "distance_three_lines",
			args: RouterOspfArgs{
				Instance: 1,
				Distance: &RouterOspfDistance{
					IntraArea: new(90),
					InterArea: new(95),
					External:  new(110),
				},
			},
			want: []string{
				"no router ospf 1",
				"router ospf 1",
				"distance ospf intra-area 90",
				"distance ospf inter-area 95",
				"distance ospf external 110",
				"exit",
			},
		},
		{
			name: "default_info_originate_with_route_map",
			args: RouterOspfArgs{
				Instance: 1,
				DefaultInformationOriginate: &RouterOspfDefaultInfoOrigin{
					RouteMap: new("RM"),
				},
			},
			want: []string{
				"no router ospf 1",
				"router ospf 1",
				"default-information originate route-map RM",
				"exit",
			},
		},
		{
			name:   "delete",
			args:   RouterOspfArgs{Instance: 5, Vrf: new("BLUE")},
			remove: true,
			want:   []string{"no router ospf 5 vrf BLUE"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildRouterOspfCmds(tc.args, tc.remove)
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

func TestRouterOspfID(t *testing.T) {
	t.Parallel()
	if got := routerOspfID(1, nil); got != "router-ospf/1" {
		t.Fatalf("default-vrf id: %q", got)
	}
	if got := routerOspfID(7, new("RED")); got != "router-ospf/7/RED" {
		t.Fatalf("vrf id: %q", got)
	}
}

func TestParseRouterOspfSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router ospf 1",
		"   router-id 1.1.1.1",
		"   no shutdown",
		"   max-lsa 12000 75 ignore-time 5 ignore-count 5 reset-time 5",
		"   maximum-paths 64",
		"   passive-interface default",
		"   no passive-interface Ethernet1",
		"   redistribute connected",
		"   network 10.0.0.0/24 area 0.0.0.0",
		"router ospf 99 vrf BLUE",
		"   router-id 9.9.9.9",
		"   maximum-paths 8",
	}, "\n")

	r1, ok := parseRouterOspfSection(out, 1, "")
	if !ok {
		t.Fatal("instance 1 default vrf not found")
	}
	if r1.RouterID != "1.1.1.1" || r1.MaxLsa != 12000 || r1.MaximumPaths != 64 {
		t.Fatalf("r1 fields: %+v", r1)
	}
	if r1.PassiveDflt == nil || !*r1.PassiveDflt {
		t.Fatalf("r1 passive-default: %+v", r1)
	}
	if r1.Shutdown == nil || *r1.Shutdown {
		t.Fatalf("r1 shutdown should be false (no shutdown): %+v", r1)
	}

	r2, ok := parseRouterOspfSection(out, 99, "BLUE")
	if !ok {
		t.Fatal("instance 99 vrf BLUE not found")
	}
	if r2.RouterID != "9.9.9.9" || r2.MaximumPaths != 8 {
		t.Fatalf("r2 fields: %+v", r2)
	}

	if _, ok := parseRouterOspfSection(out, 1, "BLUE"); ok {
		t.Fatal("must not cross-match across vrfs")
	}
	if _, ok := parseRouterOspfSection(out, 7, ""); ok {
		t.Fatal("missing instance must return false")
	}
}
