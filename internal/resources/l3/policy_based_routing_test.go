package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePbr(t *testing.T) {
	t.Parallel()
	base := PolicyBasedRoutingArgs{
		Name: "P1",
		Sequences: []PbrSequence{{
			Seq:    10,
			Class:  "C1",
			Action: PbrAction{SetNexthop: new("10.0.0.1")},
		}},
	}
	tests := []struct {
		name string
		mut  func(PolicyBasedRoutingArgs) PolicyBasedRoutingArgs
		want error
	}{
		{"ok_minimal", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs { return a }, nil},
		{"name_empty", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs { a.Name = ""; return a }, ErrPbrNameRequired},
		{"no_sequences", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs { a.Sequences = nil; return a }, ErrPbrEmptySequences},
		{"seq_zero", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 0, Class: "C1", Action: PbrAction{SetNexthop: new("10.0.0.1")}}}
			return a
		}, ErrPbrSeqRange},
		{"seq_no_class", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "", Action: PbrAction{SetNexthop: new("10.0.0.1")}}}
			return a
		}, ErrPbrSeqClassMissing},
		{"action_empty", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{}}}
			return a
		}, ErrPbrActionEmpty},
		{"action_mutex_drop_plus_nexthop", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{
				SetNexthop: new("10.0.0.1"),
				Drop:       new(true),
			}}}
			return a
		}, ErrPbrActionMutex},
		{"nexthop_bad", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{SetNexthop: new("garbage")}}}
			return a
		}, ErrPbrNexthopBadIP},
		{"ipv6_nexthop_ok", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{SetNexthop: new("2001:db8::1")}}}
			return a
		}, nil},
		{"nexthop_with_vrf", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{
				SetNexthop:    new("10.0.0.1"),
				SetNexthopVrf: new("MGMT"),
			}}}
			return a
		}, nil},
		{"nexthop_group_ok", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{SetNexthopGroup: new("NG1")}}}
			return a
		}, nil},
		{"drop_only", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.Sequences = []PbrSequence{{Seq: 10, Class: "C1", Action: PbrAction{Drop: new(true)}}}
			return a
		}, nil},
		{"attach_bad_intf", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.AttachInterfaces = []PbrInterfaceAttachment{{Interface: "BadIntf"}}
			return a
		}, ErrPbrInterfaceBadName},
		{"attach_bad_dir", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.AttachInterfaces = []PbrInterfaceAttachment{{Interface: "Ethernet1", Direction: new("output")}}
			return a
		}, ErrPbrInterfaceDirection},
		{"attach_ok", func(a PolicyBasedRoutingArgs) PolicyBasedRoutingArgs {
			a.AttachInterfaces = []PbrInterfaceAttachment{{Interface: "Ethernet1"}}
			return a
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validatePbr(tc.mut(base))
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

func TestBuildPbrCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   PolicyBasedRoutingArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal_nexthop",
			args: PolicyBasedRoutingArgs{
				Name: "P1",
				Sequences: []PbrSequence{{
					Seq:    10,
					Class:  "C1",
					Action: PbrAction{SetNexthop: new("10.0.0.1")},
				}},
			},
			want: []string{
				"no policy-map type pbr P1",
				"policy-map type pbr P1",
				"10 class C1",
				"set nexthop 10.0.0.1",
				"exit",
				"exit",
			},
		},
		{
			name: "drop_action",
			args: PolicyBasedRoutingArgs{
				Name: "P2",
				Sequences: []PbrSequence{{
					Seq:    20,
					Class:  "BLOCKLIST",
					Action: PbrAction{Drop: new(true)},
				}},
			},
			want: []string{
				"no policy-map type pbr P2",
				"policy-map type pbr P2",
				"20 class BLOCKLIST",
				"drop",
				"exit",
				"exit",
			},
		},
		{
			name: "full_with_attach",
			args: PolicyBasedRoutingArgs{
				Name: "P3",
				Sequences: []PbrSequence{
					{Seq: 10, Class: "WEB", Action: PbrAction{
						SetNexthop:    new("10.1.1.1"),
						SetNexthopVrf: new("INTERNET"),
					}},
					{Seq: 20, Class: "VIP", Action: PbrAction{SetNexthopGroup: new("VIP_NHG")}},
				},
				AttachInterfaces: []PbrInterfaceAttachment{
					{Interface: "Ethernet1"},
					{Interface: "Ethernet2", Direction: new("input")},
				},
			},
			want: []string{
				"interface Ethernet1",
				"no service-policy type pbr input P3",
				"exit",
				"interface Ethernet2",
				"no service-policy type pbr input P3",
				"exit",
				"no policy-map type pbr P3",
				"policy-map type pbr P3",
				"10 class WEB",
				"set nexthop 10.1.1.1 vrf INTERNET",
				"exit",
				"20 class VIP",
				"set nexthop-group VIP_NHG",
				"exit",
				"exit",
				"interface Ethernet1",
				"service-policy type pbr input P3",
				"exit",
				"interface Ethernet2",
				"service-policy type pbr input P3",
				"exit",
			},
		},
		{
			name: "delete",
			args: PolicyBasedRoutingArgs{
				Name: "P4",
				AttachInterfaces: []PbrInterfaceAttachment{
					{Interface: "Ethernet1"},
				},
			},
			remove: true,
			want: []string{
				"interface Ethernet1",
				"no service-policy type pbr input P4",
				"exit",
				"no policy-map type pbr P4",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildPbrCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s",
					strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPbrID(t *testing.T) {
	t.Parallel()
	if got := pbrID("P1"); got != "policy-based-routing/P1" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePbrSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"policy-map type pbr OTHER",
		"   10 class FOO",
		"      drop",
		"policy-map type pbr P_AUDIT",
		"   description audit",
		"   10 class WEB",
		"      set nexthop 10.0.0.1 vrf MGMT",
		"   20 class VIP",
		"      set nexthop-group NG1",
		"   30 class BLOCK",
		"      drop",
		"policy-map type pbr STILL_OTHER",
		"   10 class BAR",
		"      set nexthop 1.1.1.1",
	}, "\n")
	row, ok := parsePbrSection(out, "P_AUDIT")
	if !ok {
		t.Fatal("P_AUDIT not found")
	}
	if row.Description != "audit" {
		t.Fatalf("description (parsed for forward-compat): %q", row.Description)
	}
	if len(row.Sequences) != 3 {
		t.Fatalf("seq count: %d", len(row.Sequences))
	}
	if row.Sequences[0].Class != "WEB" || row.Sequences[0].Action.SetNexthop == nil ||
		*row.Sequences[0].Action.SetNexthop != "10.0.0.1" ||
		row.Sequences[0].Action.SetNexthopVrf == nil ||
		*row.Sequences[0].Action.SetNexthopVrf != "MGMT" {
		t.Fatalf("seq 10: %+v", row.Sequences[0])
	}
	if row.Sequences[1].Action.SetNexthopGroup == nil ||
		*row.Sequences[1].Action.SetNexthopGroup != "NG1" {
		t.Fatalf("seq 20 group: %+v", row.Sequences[1])
	}
	if row.Sequences[2].Action.Drop == nil || !*row.Sequences[2].Action.Drop {
		t.Fatalf("seq 30 drop: %+v", row.Sequences[2])
	}
}
