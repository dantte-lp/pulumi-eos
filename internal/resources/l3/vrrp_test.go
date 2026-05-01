package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVrrp(t *testing.T) {
	t.Parallel()
	base := VrrpArgs{Interface: "Vlan100", Vrid: 1}
	tests := []struct {
		name string
		mut  func(VrrpArgs) VrrpArgs
		want error
	}{
		{"ok_minimal", func(a VrrpArgs) VrrpArgs { return a }, nil},
		{"intf_empty", func(a VrrpArgs) VrrpArgs { a.Interface = ""; return a }, ErrVrrpInterfaceRequired},
		{"intf_bad", func(a VrrpArgs) VrrpArgs { a.Interface = "Wireless0"; return a }, ErrVrrpInterfaceBadName},
		{"intf_eth_subif", func(a VrrpArgs) VrrpArgs { a.Interface = "Ethernet1/1"; return a }, nil},
		{"intf_pc", func(a VrrpArgs) VrrpArgs { a.Interface = "Port-Channel10"; return a }, nil},
		{"intf_loopback", func(a VrrpArgs) VrrpArgs { a.Interface = "Loopback0"; return a }, nil},
		{"vrid_zero", func(a VrrpArgs) VrrpArgs { a.Vrid = 0; return a }, ErrVrrpVridRange},
		{"vrid_high", func(a VrrpArgs) VrrpArgs { a.Vrid = 256; return a }, ErrVrrpVridRange},
		{"prio_high", func(a VrrpArgs) VrrpArgs { a.Priority = new(255); return a }, ErrVrrpPriorityRange},
		{"prio_zero", func(a VrrpArgs) VrrpArgs { a.Priority = new(0); return a }, ErrVrrpPriorityRange},
		{"adv_zero", func(a VrrpArgs) VrrpArgs { a.TimersAdvertise = new(0); return a }, ErrVrrpAdvertiseRange},
		{"preempt_neg", func(a VrrpArgs) VrrpArgs { a.PreemptDelayMinimum = new(-1); return a }, ErrVrrpPreemptDelayRange},
		{"vaddr_bad", func(a VrrpArgs) VrrpArgs { a.VirtualAddresses = []string{"not-an-ip"}; return a }, ErrVrrpVirtualAddrInvalid},
		{"vaddr_ok_v4_v6", func(a VrrpArgs) VrrpArgs {
			a.VirtualAddresses = []string{"10.0.0.254", "fe80::1"}
			return a
		}, nil},
		{"track_empty_intf", func(a VrrpArgs) VrrpArgs {
			a.Tracks = []VrrpTrackEntry{{Interface: "", Action: "decrement", Decrement: new(50)}}
			return a
		}, ErrVrrpTrackInterfaceEmpty},
		{"track_bad_action", func(a VrrpArgs) VrrpArgs {
			a.Tracks = []VrrpTrackEntry{{Interface: "Et1", Action: "weight"}}
			return a
		}, ErrVrrpTrackActionInvalid},
		{"track_decrement_missing", func(a VrrpArgs) VrrpArgs {
			a.Tracks = []VrrpTrackEntry{{Interface: "Et1", Action: "decrement"}}
			return a
		}, ErrVrrpTrackDecrementRange},
		{"track_decrement_high", func(a VrrpArgs) VrrpArgs {
			a.Tracks = []VrrpTrackEntry{{Interface: "Et1", Action: "decrement", Decrement: new(255)}}
			return a
		}, ErrVrrpTrackDecrementRange},
		{"track_shutdown_ok", func(a VrrpArgs) VrrpArgs {
			a.Tracks = []VrrpTrackEntry{{Interface: "Et1", Action: "shutdown"}}
			return a
		}, nil},
		{"bfd_v6", func(a VrrpArgs) VrrpArgs { a.BfdPeer = new("fc00::1"); return a }, ErrVrrpBfdPeerBadIPv4},
		{"bfd_garbage", func(a VrrpArgs) VrrpArgs { a.BfdPeer = new("not-ip"); return a }, ErrVrrpBfdPeerBadIPv4},
		{"bfd_ok", func(a VrrpArgs) VrrpArgs { a.BfdPeer = new("10.0.0.2"); return a }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVrrp(tc.mut(base))
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

func TestBuildVrrpCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   VrrpArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: VrrpArgs{Interface: "Vlan100", Vrid: 1},
			want: []string{"interface Vlan100"},
		},
		{
			name: "primary_v4_only",
			args: VrrpArgs{
				Interface:        "Vlan100",
				Vrid:             1,
				VirtualAddresses: []string{"10.0.0.254"},
			},
			want: []string{
				"interface Vlan100",
				"vrrp 1 ipv4 10.0.0.254",
			},
		},
		{
			name: "v4_with_secondary_and_v6",
			args: VrrpArgs{
				Interface: "Vlan100",
				Vrid:      1,
				VirtualAddresses: []string{
					"10.0.0.254",
					"10.0.0.253",
					"fe80::1",
					"2001:db8::1",
				},
			},
			want: []string{
				"interface Vlan100",
				"vrrp 1 ipv4 10.0.0.254",
				"vrrp 1 ipv4 10.0.0.253 secondary",
				"vrrp 1 ipv6 fe80::1",
				"vrrp 1 ipv6 2001:db8::1",
			},
		},
		{
			name: "full",
			args: VrrpArgs{
				Interface:           "Vlan100",
				Vrid:                10,
				Description:         new("gateway"),
				Priority:            new(200),
				Preempt:             new(true),
				PreemptDelayMinimum: new(30),
				TimersAdvertise:     new(1),
				VirtualAddresses:    []string{"10.0.0.254"},
				Tracks: []VrrpTrackEntry{
					{Interface: "TRK1", Action: "decrement", Decrement: new(50)},
					{Interface: "TRK2", Action: "shutdown"},
				},
				BfdPeer:  new("10.0.0.2"),
				Shutdown: new(false),
			},
			want: []string{
				"interface Vlan100",
				"vrrp 10 ipv4 10.0.0.254",
				"vrrp 10 priority-level 200",
				"vrrp 10 preempt",
				"vrrp 10 preempt delay minimum 30",
				"vrrp 10 advertisement interval 1",
				"vrrp 10 session description gateway",
				"vrrp 10 tracked-object TRK1 decrement 50",
				"vrrp 10 tracked-object TRK2 shutdown",
				"vrrp 10 bfd ip 10.0.0.2",
				"no vrrp 10 disabled",
			},
		},
		{
			name:   "delete",
			args:   VrrpArgs{Interface: "Vlan100", Vrid: 5},
			remove: true,
			want: []string{
				"interface Vlan100",
				"no vrrp 5",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildVrrpCmds(tc.args, tc.remove)
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

func TestVrrpID(t *testing.T) {
	t.Parallel()
	if got := vrrpID("Vlan100", 1); got != "vrrp/Vlan100/1" {
		t.Fatalf("got %q", got)
	}
}

func TestParseVrrpSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"interface Vlan100",
		"   ip address 10.0.0.1/24",
		"   vrrp 1 ip 10.0.0.254",
		"   vrrp 1 ip 10.0.0.253 secondary",
		"   vrrp 1 ipv6 fe80::1",
		"   vrrp 1 priority 200",
		"   vrrp 1 preempt",
		"   vrrp 1 timers advertise 1",
		"   vrrp 1 description gateway",
		"   vrrp 1 track Ethernet1 decrement 50",
		"   vrrp 1 track Ethernet2 shutdown",
		"   vrrp 1 bfd peer 10.0.0.2",
		"   vrrp 2 ip 10.0.0.252",
		"   vrrp 2 priority 150",
	}, "\n")
	row, ok, err := parseVrrpSection(out, "Vlan100", 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ok {
		t.Fatal("vrrp 1 not found")
	}
	if row.Priority != 200 || row.Description != "gateway" || !row.Preempt {
		t.Fatalf("scalars: %+v", row)
	}
	if len(row.Addresses) != 3 ||
		row.Addresses[0] != "10.0.0.254" ||
		row.Addresses[1] != "10.0.0.253" ||
		row.Addresses[2] != "fe80::1" {
		t.Fatalf("addresses: %+v", row.Addresses)
	}
	if len(row.Tracks) != 2 {
		t.Fatalf("tracks: %+v", row.Tracks)
	}
	if row.BfdPeer != "10.0.0.2" {
		t.Fatalf("bfd: %q", row.BfdPeer)
	}

	row2, ok2, _ := parseVrrpSection(out, "Vlan100", 2)
	if !ok2 || row2.Priority != 150 || len(row2.Addresses) != 1 {
		t.Fatalf("vrrp 2: ok=%v %+v", ok2, row2)
	}

	if _, ok3, _ := parseVrrpSection(out, "Vlan100", 99); ok3 {
		t.Fatal("missing vrid must return false")
	}
}
